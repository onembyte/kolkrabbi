package agentcli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestBuildClaudeSessionArgsCarriesTheVendorModelAndEffort(t *testing.T) {
	args, err := BuildClaudeSessionArgs("claude-opus", "high", "0b5e0e2a-1111-4222-8333-444455556666", false)
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
	if index := slices.Index(args, "--session-id"); index+1 >= len(args) || args[index+1] != "0b5e0e2a-1111-4222-8333-444455556666" {
		t.Fatalf("--session-id is not followed by the minted handle: %q", args)
	}
}

func TestBuildClaudeSessionArgsOmitsAnEmptyModel(t *testing.T) {
	args, err := BuildClaudeSessionArgs("", "high", "", false)
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
	if _, err := BuildClaudeSessionArgs("opus", "bogus", "", false); err == nil {
		t.Fatal("an effort level outside the vendor's closed set must be refused")
	}
	args, err := BuildClaudeSessionArgs("opus", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--effort") {
		t.Fatalf("--effort present in %q with empty effort", args)
	}
}

// A resume names a conversation the vendor already keeps, so the handle rides
// --resume instead of --session-id; everything else in the argv is unchanged,
// because the vendor replays no flag vector on resume and model and effort
// must be re-passed alongside it every time.
func TestBuildClaudeSessionArgsResumesWithTheSameModelAndEffort(t *testing.T) {
	handle := "0b5e0e2a-1111-4222-8333-444455556666"
	args, err := BuildClaudeSessionArgs("claude-opus", "high", handle, true)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--session-id") {
		t.Fatalf("resume argv %q still claims --session-id", args)
	}
	index := slices.Index(args, "--resume")
	if index < 0 || index+1 >= len(args) || args[index+1] != handle {
		t.Fatalf("--resume is not followed by the stored handle: %q", args)
	}
	for _, want := range []string{"--model", "opus", "--effort", "high"} {
		if !slices.Contains(args, want) {
			t.Fatalf("resume argv %q missing %q", args, want)
		}
	}
}

func TestBuildClaudeSessionArgsMatchesTheOneShotSpine(t *testing.T) {
	session, err := BuildClaudeSessionArgs("opus", "high", "0b5e0e2a-1111-4222-8333-444455556666", false)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := BuildClaudeInvocation("opus", "high", "inspect this repository")
	if err != nil {
		t.Fatal(err)
	}
	// The one-shot form positions the prompt as its last argument; everything
	// else in the two command lines must match, apart from the --input-format
	// pair that only the persistent form passes and the --session-id pair that
	// only the persistent form uses.
	for _, flag := range []string{"--input-format", "--session-id"} {
		for i := 0; i+1 < len(session); i++ {
			if session[i] == flag {
				session = append(session[:i], session[i+2:]...)
				break
			}
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
	backend := NewClaudeBackendFromHandle("claude-opus", "high", "", false)
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
	backend := NewClaudeBackendFromHandle("claude-opus", "high", "", false)
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

func TestNewVendorHandleIsAWellFormedUUID(t *testing.T) {
	handle := NewVendorHandle()
	parts := strings.Split(handle, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Fatalf("NewVendorHandle() = %q, want a v4 UUID shape", handle)
	}
	if strings.HasPrefix(parts[2], "4") == false || !strings.ContainsRune("89ab", rune(parts[3][0])) {
		t.Fatalf("NewVendorHandle() = %q, want v4 version/variant bits", handle)
	}
}

// Kolkrabbi mints the conversation handle before the first process exists, and
// every later process resumes that same conversation — otherwise each spawned
// `claude` child would walk into its own empty history.
func TestClaudeBackendMintsTheHandleOnceAndResumesEverAfter(t *testing.T) {
	spawned := [][]string{}
	backend := NewClaudeBackendFromHandle("claude-opus", "high", "", false)
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawned = append(spawned, append([]string(nil), args...))
		return &fakeLineProcess{lines: claudeTurnFrames("ok")}, nil
	}
	for _, text := range []string{"one", "two"} {
		// A dead session between turns forces the second spawn through the
		// replacement path, which is exactly how a later process comes to exist.
		process := &fakeLineProcess{lines: claudeTurnFrames(text)}
		backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
			spawned = append(spawned, append([]string(nil), args...))
			return process, nil
		}
		if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: text}}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(spawned) != 2 {
		t.Fatalf("spawned %d processes, want two", len(spawned))
	}
	minted, resumed := spawned[0], spawned[1]
	idIndex := slices.Index(minted, "--session-id")
	if idIndex < 0 || idIndex+1 >= len(minted) {
		t.Fatalf("first spawn %q must open a session-id", minted)
	}
	handle := minted[idIndex+1]
	rIndex := slices.Index(resumed, "--resume")
	if rIndex < 0 || rIndex+1 >= len(resumed) || resumed[rIndex+1] != handle {
		t.Fatalf("second spawn %q must resume the minted handle %q", resumed, handle)
	}
	for _, want := range []string{"--model", "opus", "--effort", "high"} {
		if !slices.Contains(resumed, want) {
			t.Fatalf("resume spawn %q dropped %q", resumed, want)
		}
	}
}

// The vendor's own confirmation of the conversation id is the ground truth the
// session file stores, so a later Kolkrabbi process can --resume it.
func TestClaudeBackendReportsTheVendorConfirmedHandle(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"vendor-confirmed"}`),
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"hi"}]}}`),
		[]byte(`{"type":"result","result":"hi","subtype":"success","session_id":"vendor-confirmed"}`),
	}}
	backend := NewClaudeBackendFromHandle("claude-opus", "high", "", false)
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if handle := backend.ProviderHandle(); handle != "vendor-confirmed" {
		t.Fatalf("ProviderHandle() = %q, want the vendor's own confirmation", handle)
	}
}

// A stored handle the vendor no longer keeps (expired transcript, a child that
// died before its conversation existed) must degrade into a fresh
// conversation, not wedge every later turn behind the same dead resume.
func TestClaudeBackendForgetsAStoredHandleThatResumesDead(t *testing.T) {
	spawned := [][]string{}
	backend := NewClaudeBackendFromHandle("claude-opus", "high", "handle-the-vendor-forgot", true)
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawned = append(spawned, append([]string(nil), args...))
		if len(spawned) == 1 {
			// Nothing streamed: EOF before any frame, the dead-resume signature.
			return &fakeLineProcess{}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("fresh conversation")}, nil
	}
	message, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "fresh conversation" {
		t.Fatalf("message = %q, want the fresh conversation's answer", message.Content)
	}
	if len(spawned) != 2 {
		t.Fatalf("spawned %d processes, want the dead resume retried once", len(spawned))
	}
	if !slices.Contains(spawned[0], "handle-the-vendor-forgot") {
		t.Fatalf("first spawn %q did not use the stored handle", spawned[0])
	}
	rIndex := slices.Index(spawned[1], "--resume")
	if rIndex >= 0 {
		t.Fatalf("retry spawn %q resumed the dead handle again", spawned[1])
	}
	idIndex := slices.Index(spawned[1], "--session-id")
	if idIndex < 0 || idIndex+1 >= len(spawned[1]) || spawned[1][idIndex+1] == "" {
		t.Fatalf("retry spawn %q must open a fresh session-id", spawned[1])
	}
}
