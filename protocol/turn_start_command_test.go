package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// turn.start is what a paired device has been missing: it can watch a session
// and answer its prompts, but not ask it for anything (item 26, I26.7).
//
// It follows turn.cancel's shape rather than inventing a third: one command
// name in the catalogue, a typed payload, a schema, and validation that refuses
// anything the server would have to guess about.
func TestTurnStartCommandIsInTheCatalogue(t *testing.T) {
	if string(CommandTurnStart) != "turn.start" {
		t.Fatalf("command constant = %q, want turn.start", CommandTurnStart)
	}
	var found bool
	for _, name := range KnownCommandTypes() {
		if name == CommandTurnStart {
			found = true
		}
	}
	if !found {
		t.Error("turn.start is not in KnownCommandTypes, so no client can discover it")
	}
}

func TestTurnStartCommandRequiresSomethingToSay(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing":    {},
		"empty":      {"prompt": ""},
		"whitespace": {"prompt": "   \n\t "},
		"null":       {"prompt": nil},
		"number":     {"prompt": 3},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateTurnStartCommand(raw); err == nil {
				t.Errorf("accepted a turn.start with nothing to say: %#v", data)
			}
		})
	}
}

// A remote prompt is a prompt: it is bounded so that one device cannot post a
// megabyte into a session that has to carry it in every later request.
func TestTurnStartCommandBoundsThePrompt(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"prompt": strings.Repeat("x", maxRemotePromptBytes+1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTurnStartCommand(raw); err == nil {
		t.Error("accepted an unbounded prompt from a remote device")
	}
	ok, err := json.Marshal(map[string]any{"prompt": strings.Repeat("x", maxRemotePromptBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTurnStartCommand(ok); err != nil {
		t.Errorf("rejected a prompt exactly at the limit: %v", err)
	}
}

func TestTurnStartCommandAcceptsTheGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "commands", "turn.start.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.TrimSpace(raw)
	if err := validateTurnStartCommand(raw); err != nil {
		t.Fatalf("validateTurnStartCommand(golden): %v", err)
	}
	var got TurnStartCommand
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got.Prompt) == "" {
		t.Error("the golden fixture carries no prompt")
	}
}

// Additive fields must not break an older server: the envelope's rule is that
// decoders accept what they do not know.
func TestTurnStartCommandToleratesFutureFields(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"prompt": "run the tests", "future": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTurnStartCommand(raw); err != nil {
		t.Fatalf("rejected an additive field: %v", err)
	}
}
