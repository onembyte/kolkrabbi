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

func TestSagaPostureIsAnInternalSystemDirective(t *testing.T) {
	defaultAgent := New(Options{Mode: ModeCode, Sess: enginetest.NewFakeSession("s_1", "mock/model")})
	sagaAgent := New(Options{Mode: ModeCode, Posture: PostureSaga, Sess: enginetest.NewFakeSession("s_2", "mock/model")})

	defaultPrompt := defaultAgent.systemPrompt(ModeCode)
	sagaPrompt := sagaAgent.systemPrompt(ModeCode)
	if strings.Contains(defaultPrompt, sagaPostureInstruction) {
		t.Fatal("default system prompt contains the SAGA directive")
	}
	if !strings.Contains(sagaPrompt, sagaPostureInstruction) {
		t.Fatal("SAGA system prompt is missing the internal posture directive")
	}
	if defaultPrompt == sagaPrompt {
		t.Fatal("SAGA posture did not change system construction")
	}

	chapter := chapterPrompt(Chapter{Number: 1, Title: "inspect the repository"}, "improve the project", "")
	if strings.Contains(chapter, sagaPostureInstruction) {
		t.Fatal("SAGA directive was copied into the durable user/chapter prompt")
	}
}

func TestDefaultPosturePreservesTheOrdinarySystemPrompt(t *testing.T) {
	withoutPosture := New(Options{Mode: ModeCode, Sess: enginetest.NewFakeSession("s_1", "mock/model")})
	withDefaultPosture := New(Options{Mode: ModeCode, Posture: Posture(""), Sess: enginetest.NewFakeSession("s_2", "mock/model")})
	if got, want := withDefaultPosture.systemPrompt(ModeCode), withoutPosture.systemPrompt(ModeCode); got != want {
		t.Fatalf("default posture changed ordinary system prompt\n got: %q\nwant: %q", got, want)
	}
}

func TestPostureCanEnterAndLeaveSAGAOnTheCurrentSession(t *testing.T) {
	sess := enginetest.NewFakeSession("s_posture", "mock/model")
	ag := New(Options{Mode: ModeCode, Sess: sess})
	ordinary := sess.GetMessages()[0].Content

	if err := ag.SetPosture(PostureSaga); err != nil {
		t.Fatalf("SetPosture(SAGA): %v", err)
	}
	saga := sess.GetMessages()[0].Content
	if !strings.Contains(saga, sagaPostureInstruction) {
		t.Fatal("entering SAGA posture did not update the current session system message")
	}
	if got := strings.Count(saga, sagaPostureInstruction); got != 1 {
		t.Fatalf("SAGA directive count = %d, want one", got)
	}

	if err := ag.SetPosture(Posture("")); err != nil {
		t.Fatalf("SetPosture(default): %v", err)
	}
	if got := sess.GetMessages()[0].Content; got != ordinary {
		t.Fatalf("leaving SAGA posture did not restore the ordinary system message\n got: %q\nwant: %q", got, ordinary)
	}

	if err := ag.SetPosture(Posture("unknown")); err == nil {
		t.Fatal("unknown posture was accepted")
	}
	if got := ag.Posture; got != Posture("") {
		t.Fatalf("rejecting unknown posture changed current posture to %q", got)
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
