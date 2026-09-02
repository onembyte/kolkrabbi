package agentcli

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

func TestBuildClaudeSessionArgsCarriesTheVendorModelAndEffort(t *testing.T) {
	args, err := BuildClaudeSessionArgs("claude-opus", "code", "high", "0b5e0e2a-1111-4222-8333-444455556666", false)
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

func TestBuildClaudeSessionArgsWithExecutionOptionsCarriesAdditionalDirectories(t *testing.T) {
	workspace := t.TempDir()
	additional := t.TempDir()
	args, err := BuildClaudeSessionArgsWithOptions("claude-opus", "agent", "high", "handle", false, ExecutionOptions{
		Workspace:      workspace,
		AdditionalDirs: []string{additional},
		NetworkAccess:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantAdditional, err := filepath.EvalSymlinks(additional)
	if err != nil {
		t.Fatal(err)
	}
	if index := slices.Index(args, "--add-dir"); index < 0 || index+1 >= len(args) || args[index+1] != wantAdditional {
		t.Fatalf("args = %v, want --add-dir %q", args, wantAdditional)
	}
}

func TestBuildClaudeSessionArgsWithExecutionOptionsRejectsUnverifiedAdditionalDirectory(t *testing.T) {
	if _, err := BuildClaudeSessionArgsWithOptions("claude-opus", "agent", "high", "", false, ExecutionOptions{AdditionalDirs: []string{"relative/additional"}}); err == nil {
		t.Fatal("relative additional directory was accepted")
	}
}

func TestBuildClaudeSessionArgsWithExecutionOptionsFailsClosedWhenNetworkIsDisabled(t *testing.T) {
	if _, err := BuildClaudeSessionArgsWithOptions("claude-opus", "agent", "high", "", false, ExecutionOptions{Workspace: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "cannot prove network-disabled") {
		t.Fatalf("network-disabled Claude envelope error = %v, want a fail-closed capability diagnosis", err)
	}
}

func TestClaudeBackendStartsInTheDeclaredWorkspace(t *testing.T) {
	workspace := t.TempDir()
	additional := t.TempDir()
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "agent", "high", "", false, ExecutionOptions{
		Workspace:      workspace,
		AdditionalDirs: []string{additional},
		NetworkAccess:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotOptions shell.ProcessOptions
	var gotArgs []string
	backend.startWithOptions = func(_ context.Context, _ string, args []string, options shell.ProcessOptions) (lineProcess, error) {
		gotArgs = append([]string(nil), args...)
		gotOptions = options
		return &fakeLineProcess{}, nil
	}
	if _, err := backend.getSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	wantAdditional, err := filepath.EvalSymlinks(additional)
	if err != nil {
		t.Fatal(err)
	}
	if gotOptions.Dir != wantWorkspace {
		t.Fatalf("claude process directory = %q, want %q", gotOptions.Dir, wantWorkspace)
	}
	index := slices.Index(gotArgs, "--add-dir")
	if index < 0 || index+1 >= len(gotArgs) || gotArgs[index+1] != wantAdditional {
		t.Fatalf("claude args = %v, want --add-dir %q", gotArgs, wantAdditional)
	}
}

func TestBuildClaudeSessionArgsOmitsAnEmptyModel(t *testing.T) {
	args, err := BuildClaudeSessionArgs("", "code", "high", "", false)
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
	if _, err := BuildClaudeSessionArgs("opus", "code", "bogus", "", false); err == nil {
		t.Fatal("an effort level outside the vendor's closed set must be refused")
	}
	args, err := BuildClaudeSessionArgs("opus", "code", "", "", false)
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
	args, err := BuildClaudeSessionArgs("claude-opus", "code", "high", handle, true)
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

// Mode is structural on this backend: the vendor's own tool loop carries the
// mode, because kolk runs no tool executor of its own here.
func TestClaudeModeFlagsShapeTheVendorToolSet(t *testing.T) {
	code, err := claudeModeFlags("code")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(code, " "); !strings.Contains(got, "--permission-mode acceptEdits") ||
		!strings.Contains(got, "Read") || !strings.Contains(got, "Bash") || strings.Contains(got, "Task") {
		t.Fatalf("code flags = %q, want the vendor tool set without Task", got)
	}
	chat, err := claudeModeFlags("chat")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(chat, " "), "Bash") {
		t.Fatalf("chat flags = %q, chat must run with no built-in tool", chat)
	}
	if !strings.Contains(strings.Join(chat, " "), "--permission-mode dontAsk") {
		t.Fatalf("chat flags = %q, want dontAsk", strings.Join(chat, " "))
	}
	// Agent mode takes the same flags as code mode. It was refused here until
	// 2026-08-30, on a reason that was true of the vendor's Task tool rather
	// than of kolk's orchestrator — and Task has never been in this tool set.
	agentFlags, err := claudeModeFlags("agent")
	if err != nil {
		t.Fatalf("agent mode was refused: %v", err)
	}
	if strings.Join(agentFlags, " ") != strings.Join(code, " ") {
		t.Errorf("agent flags = %q, want the same as code mode %q", agentFlags, code)
	}
}

// The one thing that must stay true in every mode, forever: kolk's bus cannot
// represent a vendor subagent tree, so the vendor never gets its own scheduler.
//
// Pinned as a contract rather than left as a convenience. The tool set is a
// string kolk sends to a vendor whose defaults are not kolk's to control — the
// day `Task` becomes a default, or the day someone adds a tool to this list
// without reading why the list exists, this test is what says so.
func TestTheVendorNeverGetsItsOwnSubagentScheduler(t *testing.T) {
	for _, mode := range []string{"", "code", "agent", "chat"} {
		flags, err := claudeModeFlags(mode)
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if strings.Contains(strings.Join(flags, " "), "Task") {
			t.Errorf("mode %q hands the vendor its own subagent scheduler: %q", mode, flags)
		}
	}
	if strings.Contains(claudeCodeTools, "Task") {
		t.Error("Task is in the tool set; kolk's bus cannot represent a vendor subagent tree")
	}
}

func TestClaudeSessionArgsCarryTheModeToolsAndPermission(t *testing.T) {
	args, err := BuildClaudeSessionArgs("claude-opus", "chat", "high", "", false)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(args, "--tools")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("args %q lack --tools", args)
	}
	if next := args[i+1]; next != "" && strings.Contains(next, "Bash") {
		t.Fatalf("--tools = %q, chat mode must clear the built-in tool set", next)
	}
	p := slices.Index(args, "--permission-mode")
	if p < 0 || p+1 >= len(args) || args[p+1] != "dontAsk" {
		t.Fatalf("args %q lack --permission-mode dontAsk", args)
	}
}

// The variadic flags consume every following bare token, so each one gets
// exactly one comma-separated string and nothing ever rides unshielded.
func TestClaudeModeAndHandleFlagsNeverExposeBareTokens(t *testing.T) {
	args, err := BuildClaudeSessionArgs("claude-opus", "code", "high", "conv-handle", true)
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		switch arg {
		case "--tools", "--permission-mode", "--resume", "--session-id", "--model", "--effort":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				t.Fatalf("%s at %d is either unvalued or followed by another flag: %q", arg, i, args)
			}
		}
	}
}

func TestBuildClaudeSessionArgsMatchesTheOneShotSpine(t *testing.T) {
	session, err := BuildClaudeSessionArgs("opus", "code", "high", "0b5e0e2a-1111-4222-8333-444455556666", false)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := BuildClaudeInvocation("opus", "code", "high", "inspect this repository")
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
	backend, err0 := NewClaudeBackendFromHandle("claude-opus", "code", "high", "", false)
	if err0 != nil {
		t.Fatal(err0)
	}
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
	backend, err0 := NewClaudeBackendFromHandle("claude-opus", "code", "high", "", false)
	if err0 != nil {
		t.Fatal(err0)
	}
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
	backend, err0 := NewClaudeBackendFromHandle("claude-opus", "code", "high", "", false)
	if err0 != nil {
		t.Fatal(err0)
	}
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
	backend, err0 := NewClaudeBackendFromHandle("claude-opus", "code", "high", "", false)
	if err0 != nil {
		t.Fatal(err0)
	}
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
	backend, err0 := NewClaudeBackendFromHandle("claude-opus", "code", "high", "handle-the-vendor-forgot", true)
	if err0 != nil {
		t.Fatal(err0)
	}
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
