package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestReleaseModesAreExactlyChatCodeAndAgent(t *testing.T) {
	want := []string{ModeChat, ModeCode, ModeAgent}
	if !reflect.DeepEqual(Modes, want) {
		t.Fatalf("release modes = %q, want %q", Modes, want)
	}

	ag := New(Options{Mode: ModeCode, Sess: enginetest.NewFakeSession("s_1", "mock/model")})
	for _, mode := range want {
		if err := ag.SetMode(mode); err != nil {
			t.Fatalf("SetMode(%q): %v", mode, err)
		}
		if ag.Mode != mode {
			t.Fatalf("mode = %q after SetMode(%q)", ag.Mode, mode)
		}
	}
	if err := ag.SetMode("delegate"); err == nil {
		t.Fatal("SetMode(delegate) accepted an unknown mode")
	}
	if ag.Mode != ModeAgent {
		t.Fatalf("rejected mode changed current mode to %q, want %q", ag.Mode, ModeAgent)
	}
}

func TestVisibleResponseLabelUsesTheActiveKolkMode(t *testing.T) {
	for _, mode := range Modes {
		ag := New(Options{Mode: mode, Sess: enginetest.NewFakeSession("s_1", "mock/model")})
		if got, want := ag.responseLabel(), "kolk-"+mode; got != want {
			t.Fatalf("response label in %s mode = %q, want %q", mode, got, want)
		}
	}
}

func TestToolCallDescriptionsAreReadableAndNeverExposeRawPayloads(t *testing.T) {
	tests := []struct {
		name string
		call provider.ToolCall
		want string
	}{
		{"bash", provider.ToolCall{Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"go test ./...","description":"Run focused tests"}`}}, "Running command — Run focused tests"},
		{"read", provider.ToolCall{Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"PLAN.md"}`}}, "Reading file — PLAN.md"},
		{"write", provider.ToolCall{Function: provider.FunctionCall{Name: "write_file", Arguments: `{"path":"internal/new.go","content":"secret body"}`}}, "Writing file — internal/new.go"},
		{"edit", provider.ToolCall{Function: provider.FunctionCall{Name: "edit_file", Arguments: `{"path":"README.md","old_str":"old","new_str":"new"}`}}, "Editing file — README.md"},
		{"list", provider.ToolCall{Function: provider.FunctionCall{Name: "list_dir", Arguments: `{}`}}, "Listing directory — ."},
		{"malformed", provider.ToolCall{Function: provider.FunctionCall{Name: "write_file", Arguments: `{"content":"must not leak"`}}, "Using tool — write_file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := describeToolCall(test.call)
			if got != test.want {
				t.Fatalf("description = %q, want %q", got, test.want)
			}
			if strings.Contains(got, "secret body") || strings.Contains(got, "must not leak") || strings.Contains(got, `{"`) {
				t.Fatalf("description exposed raw tool arguments: %q", got)
			}
		})
	}
}
