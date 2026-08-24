package protocol

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeNDJSONStreamDeliversEnvelopesInOrder(t *testing.T) {
	envelopes := streamGoldenEnvelopes(t)
	var stream bytes.Buffer
	for _, envelope := range envelopes {
		frame, err := EncodeNDJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(frame)
	}

	var got []Envelope
	err := DecodeStream(&stream, StreamNDJSON, func(envelope Envelope) error {
		got = append(got, envelope)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeStream(NDJSON): %v", err)
	}
	assertEnvelopeSequence(t, got, envelopes)
}

func TestDecodeSSEStreamDeliversEnvelopesAndIgnoresHeartbeats(t *testing.T) {
	envelopes := streamGoldenEnvelopes(t)
	var stream bytes.Buffer
	stream.Write(SSEHeartbeat())
	for _, envelope := range envelopes {
		frame, err := EncodeSSE(envelope)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(frame)
		stream.Write(SSEHeartbeat())
	}

	var got []Envelope
	err := DecodeStream(&stream, StreamSSE, func(envelope Envelope) error {
		got = append(got, envelope)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeStream(SSE): %v", err)
	}
	assertEnvelopeSequence(t, got, envelopes)
}

func TestDecodeStreamAcceptsEmptyInput(t *testing.T) {
	for _, format := range []StreamFormat{StreamNDJSON, StreamSSE} {
		t.Run(string(format), func(t *testing.T) {
			called := false
			err := DecodeStream(strings.NewReader(""), format, func(Envelope) error {
				called = true
				return nil
			})
			if err != nil || called {
				t.Fatalf("DecodeStream(empty) = called %v, err %v; want false, nil", called, err)
			}
		})
	}
}

func TestDecodeNDJSONStreamRejectsMalformedTransport(t *testing.T) {
	valid := streamGoldenNDJSON(t)
	tests := map[string][]byte{
		"blank line":          append([]byte{'\n'}, valid...),
		"carriage return":     bytes.Replace(valid, []byte{'\n'}, []byte{'\r', '\n'}, 1),
		"leading whitespace":  append([]byte{' '}, valid...),
		"trailing whitespace": bytes.Replace(valid, []byte{'\n'}, []byte{' ', '\n'}, 1),
		"unterminated frame":  bytes.TrimSuffix(valid, []byte{'\n'}),
		"invalid envelope":    []byte("{}\n"),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			if err := DecodeStream(bytes.NewReader(input), StreamNDJSON, func(Envelope) error {
				called = true
				return nil
			}); err == nil {
				t.Fatal("DecodeStream accepted malformed NDJSON")
			}
			if called {
				t.Fatal("callback ran for malformed first frame")
			}
		})
	}
}

func TestDecodeSSEStreamRejectsMalformedTransport(t *testing.T) {
	envelope := streamGoldenEnvelopes(t)[0]
	block, err := EncodeSSE(envelope)
	if err != nil {
		t.Fatal(err)
	}
	dataLine := "data: " + string(bytes.TrimSuffix(streamGoldenNDJSON(t), []byte{'\n'})) + "\n"
	tests := map[string][]byte{
		"carriage return":     bytes.Replace(block, []byte{'\n'}, []byte{'\r', '\n'}, 1),
		"missing id":          []byte("event: hello\n" + dataLine + "\n"),
		"reordered fields":    []byte("event: hello\nid: 1\n" + dataLine + "\n"),
		"noncanonical id":     bytes.Replace(block, []byte("id: 1\n"), []byte("id: 01\n"), 1),
		"mismatched id":       bytes.Replace(block, []byte("id: 1\n"), []byte("id: 2\n"), 1),
		"mismatched event":    bytes.Replace(block, []byte("event: hello\n"), []byte("event: log\n"), 1),
		"multiline data":      bytes.Replace(block, []byte("\n\n"), []byte("\ndata: {}\n\n"), 1),
		"extension field":     bytes.Replace(block, []byte("data: "), []byte("retry: 1000\ndata: "), 1),
		"empty block":         []byte("\n"),
		"unknown comment":     []byte(": future\n\n"),
		"malformed heartbeat": []byte(": ping\n"),
		"unterminated block":  bytes.TrimSuffix(block, []byte{'\n'}),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			if err := DecodeStream(bytes.NewReader(input), StreamSSE, func(Envelope) error {
				called = true
				return nil
			}); err == nil {
				t.Fatal("DecodeStream accepted malformed SSE")
			}
			if called {
				t.Fatal("callback ran for malformed first block")
			}
		})
	}
}

func TestDecodeStreamEnforcesExactFrameLimitAcrossTransports(t *testing.T) {
	envelope := streamEnvelopeAtSize(t, MaxStreamFrameBytes)
	oversized := streamEnvelopeAtSize(t, MaxStreamFrameBytes+1)

	for _, tc := range []struct {
		name   string
		format StreamFormat
		encode func(Envelope) ([]byte, error)
	}{
		{"ndjson", StreamNDJSON, EncodeNDJSON},
		{"sse", StreamSSE, EncodeSSE},
	} {
		t.Run(tc.name+" exact", func(t *testing.T) {
			input, err := tc.encode(envelope)
			if err != nil {
				t.Fatal(err)
			}
			called := 0
			if err := DecodeStream(bytes.NewReader(input), tc.format, func(Envelope) error {
				called++
				return nil
			}); err != nil || called != 1 {
				t.Fatalf("exact limit = called %d, err %v; want 1, nil", called, err)
			}
		})
		t.Run(tc.name+" oversized", func(t *testing.T) {
			input, err := tc.encode(oversized)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = DecodeStream(bytes.NewReader(input), tc.format, func(Envelope) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrStreamFrameTooLarge) || called {
				t.Fatalf("oversized = called %v, err %v; want false, ErrStreamFrameTooLarge", called, err)
			}
		})
	}
}

func TestDecodeStreamStopsOnCallbackError(t *testing.T) {
	envelopes := streamGoldenEnvelopes(t)
	var stream bytes.Buffer
	for _, envelope := range envelopes {
		frame, err := EncodeNDJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(frame)
	}
	want := errors.New("consumer stopped")
	called := 0
	err := DecodeStream(&stream, StreamNDJSON, func(Envelope) error {
		called++
		return want
	})
	if !errors.Is(err, want) || called != 1 {
		t.Fatalf("callback stop = called %d, err %v; want 1, original error", called, err)
	}
}

func TestDecodeStreamRejectsUnsupportedFormatAndNilCallback(t *testing.T) {
	if err := DecodeStream(strings.NewReader(""), StreamFormat("future"), func(Envelope) error { return nil }); err == nil {
		t.Fatal("DecodeStream accepted an unsupported format")
	}
	if err := DecodeStream(strings.NewReader(""), StreamNDJSON, nil); err == nil {
		t.Fatal("DecodeStream accepted a nil callback")
	}
}

func streamGoldenEnvelopes(t *testing.T) []Envelope {
	t.Helper()
	names := []string{"hello.json", "message.delta.json", "turn.finished.json"}
	envelopes := make([]Envelope, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", name))
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := Decode(bytes.TrimSpace(raw))
		if err != nil {
			t.Fatalf("Decode(%s): %v", name, err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func streamGoldenNDJSON(t *testing.T) []byte {
	t.Helper()
	frame, err := EncodeNDJSON(streamGoldenEnvelopes(t)[0])
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func streamEnvelopeAtSize(t *testing.T, size int) Envelope {
	t.Helper()
	envelope := Envelope{
		Seq: 1, Timestamp: time.Date(2026, time.August, 23, 23, 30, 0, 0, time.UTC),
		Session: goldenSession, Turn: goldenTurn, Type: EventType("future.padding"),
		Data: []byte(`{"padding":""}`),
	}
	base, err := Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	padding := size - len(base)
	if padding < 0 {
		t.Fatalf("requested frame size %d is below base size %d", size, len(base))
	}
	envelope.Data = []byte(`{"padding":"` + strings.Repeat("x", padding) + `"}`)
	encoded, err := Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != size {
		t.Fatalf("constructed frame size = %d, want %d", len(encoded), size)
	}
	return envelope
}

func assertEnvelopeSequence(t *testing.T, got, want []Envelope) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("decoded %d envelopes, want %d", len(got), len(want))
	}
	for i := range want {
		gotFrame, gotErr := Encode(got[i])
		wantFrame, wantErr := Encode(want[i])
		if gotErr != nil || wantErr != nil || !bytes.Equal(gotFrame, wantFrame) {
			t.Errorf("envelope %d differs\n got: %s (%v)\nwant: %s (%v)", i, gotFrame, gotErr, wantFrame, wantErr)
		}
	}
}
