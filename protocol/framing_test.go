package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNDJSONAndSSEFramingAreByteIdentical(t *testing.T) {
	frame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "message.delta.json"))
	if err != nil {
		t.Fatal(err)
	}
	frame = bytes.TrimSpace(frame)
	envelope, err := Decode(frame)
	if err != nil {
		t.Fatal(err)
	}

	ndjson, err := EncodeNDJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if want := append(append([]byte(nil), frame...), '\n'); !bytes.Equal(ndjson, want) {
		t.Errorf("NDJSON = %q, want %q", ndjson, want)
	}
	wantSSE := []byte("id: 412\nevent: message.delta\ndata: " + string(frame) + "\n\n")
	sse, err := EncodeSSE(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sse, wantSSE) {
		t.Errorf("SSE = %q, want %q", sse, wantSSE)
	}

	dataPrefix := []byte("data: ")
	dataStart := bytes.Index(sse, dataPrefix)
	dataEnd := bytes.LastIndex(sse, []byte("\n\n"))
	if dataStart < 0 || dataEnd < 0 {
		t.Fatalf("SSE lacks one data line: %q", sse)
	}
	data := sse[dataStart+len(dataPrefix) : dataEnd]
	if !bytes.Equal(data, bytes.TrimSuffix(ndjson, []byte{'\n'})) {
		t.Errorf("SSE data differs from NDJSON frame\n data: %s\n line: %s", data, ndjson)
	}
}

func TestFramingKeepsUnicodeAndEmbeddedNewlinesOnOnePhysicalLine(t *testing.T) {
	raw, err := json.Marshal(MessageDeltaData{Text: "lína 1\nlína 2 🐙\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	envelope := Envelope{
		Seq: 42, Timestamp: time.Date(2026, time.August, 23, 22, 50, 6, 0, time.UTC),
		Session: goldenSession, Turn: goldenTurn,
		Type: EventMessageDelta, Data: raw,
	}
	ndjson, err := EncodeNDJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(ndjson, []byte{'\n'}) != 1 || bytes.Contains(ndjson, []byte{'\r'}) {
		t.Errorf("NDJSON is not exactly one LF-terminated physical line: %q", ndjson)
	}
	if !bytes.Contains(ndjson, []byte(`l\u00edna 1\nl\u00edna 2`)) &&
		!bytes.Contains(ndjson, []byte(`lína 1\nlína 2`)) {
		t.Errorf("NDJSON lost Unicode or escaped newline: %s", ndjson)
	}
	sse, err := EncodeSSE(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(sse, []byte{'\n'}) != 4 || bytes.Contains(sse, []byte{'\r'}) {
		t.Errorf("SSE does not have exactly three fields and one blank line: %q", sse)
	}
	if strings.Count(string(sse), "data: ") != 1 {
		t.Errorf("SSE folded one envelope across data lines: %q", sse)
	}
}

func TestFramingRejectsInvalidEnvelopeBeforeOutput(t *testing.T) {
	invalid := Envelope{}
	if got, err := EncodeNDJSON(invalid); err == nil || got != nil {
		t.Errorf("EncodeNDJSON(invalid) = %q, %v; want nil error output", got, err)
	}
	if got, err := EncodeSSE(invalid); err == nil || got != nil {
		t.Errorf("EncodeSSE(invalid) = %q, %v; want nil error output", got, err)
	}
}

func TestSSEHeartbeatIsExactAndDefensive(t *testing.T) {
	want := []byte(": ping\n\n")
	first := SSEHeartbeat()
	if !bytes.Equal(first, want) {
		t.Fatalf("heartbeat = %q, want %q", first, want)
	}
	first[0] = '!'
	if got := SSEHeartbeat(); !bytes.Equal(got, want) {
		t.Errorf("heartbeat storage was mutable: %q", got)
	}
}
