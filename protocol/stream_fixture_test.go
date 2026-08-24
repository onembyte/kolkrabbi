package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var wholeTurnFixtureTypes = map[string][]EventType{
	"code-turn": {
		EventSessionStarted, EventTurnStarted, EventReasoningDelta,
		EventMessageDelta, EventMessageDelta, EventMessageCompleted,
		EventUsageReported, EventTurnFinished,
	},
	"permission-denied": {
		EventSessionStarted, EventTurnStarted, EventToolRequested,
		EventPermissionRequested, EventPermissionResolved, EventToolFinished,
		EventMessageCompleted, EventUsageReported, EventTurnFinished,
	},
	"agent-fanout": {
		EventSessionStarted, EventTurnStarted, EventSubagentStarted,
		EventTurnStarted, EventMessageDelta, EventMessageCompleted,
		EventUsageReported, EventTurnFinished, EventSubagentFinished,
		EventMessageCompleted, EventUsageReported, EventTurnFinished,
	},
}

func TestWholeTurnFixtureInventoryAndCrossFormatConformance(t *testing.T) {
	dir := filepath.Join("..", "spec", "testdata", "streams")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var gotFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected stream fixture directory %s", entry.Name())
		}
		gotFiles = append(gotFiles, entry.Name())
	}
	sort.Strings(gotFiles)
	var wantFiles []string
	for name := range wholeTurnFixtureTypes {
		wantFiles = append(wantFiles, name+".ndjson", name+".sse")
	}
	sort.Strings(wantFiles)
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Fatalf("stream fixture inventory = %v, want %v", gotFiles, wantFiles)
	}

	for name, wantTypes := range wholeTurnFixtureTypes {
		t.Run(name, func(t *testing.T) {
			ndjson := readStreamFixture(t, dir, name+".ndjson")
			sse := readStreamFixture(t, dir, name+".sse")
			fromNDJSON := decodeStreamFixture(t, ndjson, StreamNDJSON)
			fromSSE := decodeStreamFixture(t, sse, StreamSSE)
			assertStreamEnvelopesEqual(t, fromSSE, fromNDJSON)
			assertCanonicalWholeTurn(t, fromNDJSON, wantTypes)

			var canonicalNDJSON, canonicalSSE bytes.Buffer
			for _, envelope := range fromNDJSON {
				line, err := EncodeNDJSON(envelope)
				if err != nil {
					t.Fatal(err)
				}
				canonicalNDJSON.Write(line)
				block, err := EncodeSSE(envelope)
				if err != nil {
					t.Fatal(err)
				}
				canonicalSSE.Write(block)
			}
			if !bytes.Equal(ndjson, canonicalNDJSON.Bytes()) {
				t.Error("NDJSON fixture is not canonical concatenated encoder output")
			}
			if !bytes.Equal(sse, canonicalSSE.Bytes()) {
				t.Error("SSE fixture is not canonical concatenated encoder output")
			}
		})
	}
}

func TestCodeTurnFixtureDeltasMatchCompletedMessage(t *testing.T) {
	envelopes := decodeNamedNDJSONFixture(t, "code-turn")
	var streamed strings.Builder
	for _, envelope := range envelopes {
		if envelope.Type == EventMessageDelta {
			streamed.WriteString(decodeFixtureData[MessageDeltaData](t, envelope).Text)
		}
	}
	completed := decodeFixtureData[MessageCompletedData](t, envelopes[5])
	usage := decodeFixtureData[Usage](t, envelopes[6])
	if streamed.String() != completed.Text {
		t.Fatalf("streamed message = %q, completed = %q", streamed.String(), completed.Text)
	}
	if usage.Role != "main" || usage.Attempt != 1 {
		t.Fatalf("code-turn usage = role %q, attempt %d", usage.Role, usage.Attempt)
	}
}

func TestPermissionDeniedFixtureCorrelatesWithoutFalseStart(t *testing.T) {
	envelopes := decodeNamedNDJSONFixture(t, "permission-denied")
	requested := decodeFixtureData[ToolRequestedData](t, envelopes[2])
	permission := decodeFixtureData[PermissionRequestedData](t, envelopes[3])
	resolved := decodeFixtureData[PermissionResolvedData](t, envelopes[4])
	finished := decodeFixtureData[ToolFinishedData](t, envelopes[5])

	if requested.ID != finished.ID || requested.Executor != ToolExecutorKolk ||
		finished.Executor != ToolExecutorKolk || finished.OK {
		t.Fatalf("tool correlation = requested %+v, finished %+v", requested, finished)
	}
	if permission.ID != resolved.ID || permission.Tool != requested.Name ||
		resolved.Decision != PermissionDecisionDeny {
		t.Fatalf("permission correlation = requested %+v, resolved %+v", permission, resolved)
	}
	for _, envelope := range envelopes {
		if envelope.Type == EventToolStarted {
			t.Fatal("denied tool fixture falsely claims tool execution started")
		}
	}
}

func TestAgentFanoutFixtureScopesChildAndParentEvents(t *testing.T) {
	envelopes := decodeNamedNDJSONFixture(t, "agent-fanout")
	started := decodeFixtureData[SubagentStartedData](t, envelopes[2])
	finished := decodeFixtureData[SubagentFinishedData](t, envelopes[8])
	parentTurn := envelopes[1].Turn

	if started.ID != finished.ID || started.ChildTurn != finished.ChildTurn ||
		started.Mode != finished.Mode || !finished.OK {
		t.Fatalf("subagent correlation = started %+v, finished %+v", started, finished)
	}
	for i := 3; i <= 7; i++ {
		if envelopes[i].Turn != started.ChildTurn {
			t.Errorf("child event %d turn = %s, want %s", i, envelopes[i].Turn, started.ChildTurn)
		}
	}
	for _, i := range []int{0, 1, 2, 8, 9, 10, 11} {
		if envelopes[i].Turn != parentTurn {
			t.Errorf("parent event %d turn = %s, want %s", i, envelopes[i].Turn, parentTurn)
		}
	}
	childUsage := decodeFixtureData[Usage](t, envelopes[6])
	parentUsage := decodeFixtureData[Usage](t, envelopes[10])
	if childUsage.Role != "subagent" || parentUsage.Role != "main" {
		t.Fatalf("usage roles = child %q, parent %q", childUsage.Role, parentUsage.Role)
	}
}

func assertCanonicalWholeTurn(t *testing.T, envelopes []Envelope, wantTypes []EventType) {
	t.Helper()
	if len(envelopes) != len(wantTypes) {
		t.Fatalf("decoded %d envelopes, want %d", len(envelopes), len(wantTypes))
	}
	for i, envelope := range envelopes {
		if envelope.Seq != uint64(i+1) {
			t.Errorf("event %d seq = %d, want %d", i, envelope.Seq, i+1)
		}
		if envelope.Type != wantTypes[i] {
			t.Errorf("event %d type = %q, want %q", i, envelope.Type, wantTypes[i])
		}
		if envelope.Session != envelopes[0].Session {
			t.Errorf("event %d session = %q, want %q", i, envelope.Session, envelopes[0].Session)
		}
		if i > 0 && envelope.Timestamp.Before(envelopes[i-1].Timestamp) {
			t.Errorf("event %d timestamp %s precedes %s", i, envelope.Timestamp, envelopes[i-1].Timestamp)
		}
	}
	if envelopes[0].Type != EventSessionStarted || envelopes[len(envelopes)-1].Type != EventTurnFinished {
		t.Errorf("whole turn boundaries = %q ... %q", envelopes[0].Type, envelopes[len(envelopes)-1].Type)
	}
}

func readStreamFixture(t *testing.T, dir, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeNamedNDJSONFixture(t *testing.T, name string) []Envelope {
	t.Helper()
	raw := readStreamFixture(t, filepath.Join("..", "spec", "testdata", "streams"), name+".ndjson")
	return decodeStreamFixture(t, raw, StreamNDJSON)
}

func decodeStreamFixture(t *testing.T, raw []byte, format StreamFormat) []Envelope {
	t.Helper()
	var envelopes []Envelope
	if err := DecodeStream(bytes.NewReader(raw), format, func(envelope Envelope) error {
		envelopes = append(envelopes, envelope)
		return nil
	}); err != nil {
		t.Fatalf("DecodeStream(%s): %v", format, err)
	}
	return envelopes
}

func assertStreamEnvelopesEqual(t *testing.T, got, want []Envelope) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("decoded envelope counts = %d, %d", len(got), len(want))
	}
	for i := range want {
		gotFrame, gotErr := Encode(got[i])
		wantFrame, wantErr := Encode(want[i])
		if gotErr != nil || wantErr != nil || !bytes.Equal(gotFrame, wantFrame) {
			t.Errorf("envelope %d differs\n got: %s (%v)\nwant: %s (%v)", i, gotFrame, gotErr, wantFrame, wantErr)
		}
	}
}

func decodeFixtureData[T any](t *testing.T, envelope Envelope) T {
	t.Helper()
	var data T
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}
