package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A reader looking at a wide run wants to know what it cost, and the honest
// answer is which rung did what: "four subagents, three on haiku" is a
// different fact from "four subagents".
func TestASubagentEventSaysWhichRungRanIt(t *testing.T) {
	started, err := json.Marshal(SubagentStartedData{
		ID: "k_01M14ZF2X6G36379PQJ27KMF3Z", ChildTurn: "t_01M14ZF2X6G36379PQJ27KMF3Z",
		Task: "commit and push", Mode: "code", Index: 1, Total: 4,
		Level: "trivial", Model: "claude-haiku",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"level":"trivial"`, `"model":"claude-haiku"`} {
		if !strings.Contains(string(started), want) {
			t.Errorf("subagent.started is missing %s:\n%s", want, started)
		}
	}

	finished, err := json.Marshal(SubagentFinishedData{
		ID: "k_01M14ZF2X6G36379PQJ27KMF3Z", ChildTurn: "t_01M14ZF2X6G36379PQJ27KMF3Z",
		Mode: "code", OK: true, Model: "claude-haiku",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(finished), `"model":"claude-haiku"`) {
		t.Errorf("subagent.finished does not say which model ran it:\n%s", finished)
	}
}

// Additive means additive: an event from a build that predates the rungs still
// validates, and one that simply had nothing to say omits the fields rather
// than sending empty strings.
func TestAnEventWithoutALevelStillValidates(t *testing.T) {
	bare, err := json.Marshal(SubagentStartedData{
		ID: "k_01M14ZF2X6G36379PQJ27KMF3Z", ChildTurn: "t_01M14ZF2X6G36379PQJ27KMF3Z",
		Task: "x", Mode: "code", Index: 1, Total: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "level") || strings.Contains(string(bare), "model") {
		t.Errorf("an unstated level was sent as an empty field:\n%s", bare)
	}
	if err := validateEventData(EventSubagentStarted, bare); err != nil {
		t.Errorf("an event with no rung fields was refused: %v", err)
	}
}

// The schema and the Go struct are two statements of one contract, and a
// reader trusting the schema must not be surprised by the wire.
func TestTheSchemaAndTheGoStructAgreeOnTheNewFields(t *testing.T) {
	for _, tc := range []struct {
		file   string
		fields []string
	}{
		{"spec/schemas/events/subagent.started.json", []string{"level", "model"}},
		{"spec/schemas/events/subagent.finished.json", []string{"model"}},
	} {
		raw, err := os.ReadFile("../" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		for _, field := range tc.fields {
			if _, declared := schema.Properties[field]; !declared {
				t.Errorf("%s does not declare %q", tc.file, field)
			}
			for _, required := range schema.Required {
				if required == field {
					t.Errorf("%s requires %q; the change is additive and an older event must still validate",
						tc.file, field)
				}
			}
		}
	}
}
