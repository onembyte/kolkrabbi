package agentcli

import (
	"context"
	"slices"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestBuildClaudeSessionArgsCarriesTheVendorModelAndEffort(t *testing.T) {
	args, err := BuildClaudeSessionArgs("claude-opus", "high")
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "-p" {
		t.Fatalf("first flag = %q, want -p", args[0])
	}
	for _, want := range []string{"--verbose", "--output-format", "stream-json", "--input-format", "stream-json", "--safe-mode", "--model", "opus", "--effort", "high"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if index := slices.Index(args, "--model"); index+1 >= len(args) || args[index+1] != "opus" {
		t.Fatalf("--model is not followed by the vendor alias: %q", args)
	}
}

func TestBuildClaudeSessionArgsOmitsAnEmptyModel(t *testing.T) {
	args, err := BuildClaudeSessionArgs("", "high")
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--model" {
			t.Fatalf("--model present in %q (index %d) with no model requested", args, i)
		}
	}
}

// The vendor warn-and-runs on an unknown --effort value, which would leave the
// effort dial silently doing nothing: Kolkrabbi refuses instead.
func TestBuildClaudeSessionArgsRefusesAnUnknownEffort(t *testing.T) {
	if _, err := BuildClaudeSessionArgs("opus", "bogus"); err == nil {
		t.Fatal("an effort level outside the vendor's closed set must be refused")
	}
	args, err := BuildClaudeSessionArgs("opus", "")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--effort") {
		t.Fatalf("--effort present in %q with empty effort", args)
	}
}

func TestBuildClaudeSessionArgsMatchesTheOneShotSpine(t *testing.T) {
	session, err := BuildClaudeSessionArgs("opus", "high")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := BuildClaudeInvocation("opus", "high", "inspect this repository")
	if err != nil {
		t.Fatal(err)
	}
	// The one-shot form positions the prompt as its last argument; everything
	// else in the two command lines must match, apart from the --input-format
	// pair that only the persistent form passes.
	for i := 0; i+1 < len(session); i++ {
		if session[i] == "--input-format" {
			session = append(session[:i], session[i+2:]...)
			break
		}
	}
	if !slices.Equal(session, invocation.Args) {
		t.Fatalf("session argv %q diverges from the one-shot spine %q", session, invocation.Args)
	}
}

func TestClaudeVendorModelMapsCatalogNamesAndPassesFullIDsThrough(t *testing.T) {
	for in, want := range map[string]string{
		"claude-sonnet": "sonnet",
		"claude-opus":   "opus",
		"claude-haiku":  "haiku",
		"claude-opus-5": "claude-opus-5",
		"":              "",
	} {
		if got := ClaudeVendorModel(in); got != want {
			t.Fatalf("ClaudeVendorModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeEffortValidAcceptsTheVendorSet(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", "xhigh", "max", "", "High"} {
		if !ClaudeEffortValid(level) {
			t.Fatalf("%q is in the vendor's set but reported invalid", level)
		}
	}
	for _, level := range []string{"bogus", "ultra", "minimal"} {
		if ClaudeEffortValid(level) {
			t.Fatalf("%q is NOT in the vendor's set but reported valid", level)
		}
	}
}

// The model in the spawn args must equal the vendor mapping of the model the
// backend was built for, not whatever the first turn happens to call a model:
// a stream-json process cannot take a new argv later.
func TestClaudeBackendSessionSpawnsWithItsConstructedModel(t *testing.T) {
	var spawned []string
	backend := NewClaudeBackend("claude-opus", "high")
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawned = append([]string(nil), args...)
		// No frames: the turn ends at EOF, but only after the process spawned.
		return &fakeLineProcess{}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err == nil {
		t.Fatal("a claude turn with an empty stream must report the early exit")
	}
	pairs := map[string]string{"--model": "opus", "--effort": "high"}
	for flag, want := range pairs {
		index := slices.Index(spawned, flag)
		if index < 0 || index+1 >= len(spawned) || spawned[index+1] != want {
			t.Fatalf("spawned argv %q lacks %s %s", spawned, flag, want)
		}
	}
}

// StreamChat with a fake process that answers one turn normally.
func TestClaudeBackendHappyPathEndsCleanlyWithConstructedModel(t *testing.T) {
	process := &fakeLineProcess{lines: claudeTurnFrames("hello")}
	backend := NewClaudeBackend("claude-opus", "high")
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	}
	message, meta, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" || meta.Model != "opus" {
		t.Fatalf("message=%+v meta=%+v", message, meta)
	}
}
