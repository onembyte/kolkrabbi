package agentcli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"testing"
)

// codexFixtureLine replays one captured vendor stream, one JSONL line at a
// time, through the translator. The fixtures are the real codex output this
// adapter was written against — see spec/testdata/foreign/README.md — so what
// passes here is what the vendor sent the day this shipped.
func codexFixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	raw, err := os.ReadFile("../../../spec/testdata/foreign/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, []byte(line))
		}
	}
	return lines
}

func translateCodexAll(t *testing.T, lines [][]byte) []Event {
	t.Helper()
	var events []Event
	for _, line := range lines {
		translated, err := TranslateCodex(line)
		if err != nil {
			t.Fatalf("translating %s: %v", line, err)
		}
		events = append(events, translated...)
	}
	return events
}

// The plain fixture proves the whole happy path in four frames: the vendor
// names its thread (kolk's resume handle), the answer arrives whole, and the
// turn's usage lands where the dashboard reads it.
func TestTranslateCodexProjectsTheCapturedPlainStream(t *testing.T) {
	events := translateCodexAll(t, codexFixtureLines(t, "codex-plain.jsonl"))

	if len(events) != 4 {
		t.Fatalf("events = %d, want 4: %+v", len(events), events)
	}
	if events[0].Kind != EventInit || events[0].SessionID != "00000000-0000-4000-8000-0000000000aa" {
		t.Fatalf("init = %+v, want the vendor's own thread id", events[0])
	}
	if events[1].Kind != EventMessageDelta || events[1].Text != "ok" {
		t.Fatalf("delta = %+v, want the whole answer streamed once", events[1])
	}
	if events[2].Kind != EventMessageCompleted || events[2].Text != "ok" {
		t.Fatalf("completed = %+v, want the same text as the final answer", events[2])
	}
	usage := events[3]
	if usage.Kind != EventUsage ||
		usage.InputTokens != 14876 || usage.OutputTokens != 5 ||
		usage.CacheRead != 11008 || usage.CacheCreation != 0 {
		t.Fatalf("usage = %+v, want the turn's own accounting", usage)
	}
}

// The tool-use fixture is the whole vendor tool loop: prose, a file write, a
// shell verification, prose, another command, and the final answer. Kolk runs
// none of it — the events are a record, with the announcement paired to its
// outcome by the item id.
func TestTranslateCodexStreamsTheVendorToolLoop(t *testing.T) {
	events := translateCodexAll(t, codexFixtureLines(t, "codex-tool-use.jsonl"))

	var runs, outcomes, announcements int
	var shellCommand, shellOutput, shellItem string
	var fileDetail string
	for _, event := range events {
		if event.Kind != EventTool {
			continue
		}
		if event.ToolName != "" {
			announcements++
			switch event.ToolName {
			case "shell":
				shellCommand = event.ToolInput
				// The outcome trails under the same item id; grabbing the first
				// non-empty output in the stream would read the file-change's,
				// and the second shell run would overwrite the first's.
				if shellItem == "" {
					shellItem = event.ToolCallID
				}
			case "file-change":
				fileDetail = event.ToolInput
			}
			continue
		}
		outcomes++
		if event.ToolCallID == shellItem {
			shellOutput = event.ToolOutput
		}
		if event.ToolOutput != "" && shellOutput == "" {
			shellOutput = event.ToolOutput
		}
		if event.ToolIsError {
			t.Fatalf("the captured write succeeded; no event may read as an error: %+v", event)
		}
	}
	// Three file/command items, each announced and completed, plus nothing for
	// the agent messages. A lost outcome would leave kolk reporting runs it
	// never saw finish; a fourth announcement would double-count a tool call.
	if runs != 0 || announcements != 3 || outcomes != 3 {
		t.Fatalf("tool events = %d announcements + %d outcomes (runs %d), want 3+3 and none lost", announcements, outcomes, runs)
	}
	if !strings.Contains(shellCommand, "od -An -tx1 -v hello.txt") {
		t.Fatalf("shell announcement = %q, want the vendor's own command", shellCommand)
	}
	if !strings.Contains(shellOutput, "6f 6b 0a") {
		t.Fatalf("shell outcome = %q, want the vendor's own output", shellOutput)
	}
	if !strings.Contains(fileDetail, "add /work/hello.txt") {
		t.Fatalf("file-change detail = %q, want the path and kind", fileDetail)
	}
	for _, event := range events {
		if event.Kind == EventUsage {
			if event.InputTokens != 45381 || event.OutputTokens != 539 {
				t.Fatalf("usage = %+v, want input 45381 and output 395+144 reasoning tokens", event)
			}
		}
		if event.Kind == EventMessageCompleted {
			if !strings.Contains(event.Text, "Created [hello.txt](/work/hello.txt)") {
				continue
			}
			return
		}
	}
	t.Fatalf("no final agent_message arrived among %+v", events)
}

// The failure fixture proves three things at once: a warning error item on an
// otherwise live turn is a trail line, not a failure; the terminal turn.failed
// is the real cause; and the vendor's JSON-encoded error is unwrapped to the
// sentence a person can read.
func TestTranslateCodexReadsTheVendorFailureUnderItsWrap(t *testing.T) {
	lines := codexFixtureLines(t, "codex-error.jsonl")
	var events []Event
	var sawWarning bool
	for _, line := range lines {
		translated, err := TranslateCodex(line)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range translated {
			if event.Kind == EventTool && event.ToolName == "codex-warning" {
				sawWarning = true
				events = events[:0]
				continue
			}
			events = append(events, event)
		}
	}
	if !sawWarning {
		t.Fatalf("the vendor's metadata warning never surfaced in %+v", events)
	}
	terminal := 0
	for _, event := range events {
		if event.Kind == EventError {
			terminal++
			want := "The 'gpt-4.1' model is not supported when using Codex with a ChatGPT account."
			if event.Error != want {
				t.Fatalf("error = %q, want the unwrapped vendor cause", event.Error)
			}
		}
	}
	// Both the top-level error frame and turn.failed carry the same cause; the
	// collector reads the first, whichever order a vendor release ships them in.
	if terminal < 1 {
		t.Fatalf("error events = %d, want the terminal failure named", terminal)
	}
}

// A shimmed machine puts version-manager prose ahead of the first frame, and
// codex notes on stderr are not frames. Skipping them is what keeps a real
// machine's first turn from dying on line one.
func TestTranslateCodexSkipsTheMachinesOwnNoise(t *testing.T) {
	for _, noise := range []string{"mise ~/.config/mise/config.toml tools: codex@0.149.1", "", "   ", "Reading prompt from stdin..."} {
		events, err := TranslateCodex([]byte(noise))
		if err != nil || events != nil {
			t.Fatalf("noise %q → (%+v, %v), want nothing", noise, events, err)
		}
	}
	// A *malformed object* stays silent too — the fixture gate is that unknown
	// shapes never break a live turn.
	events, err := TranslateCodex([]byte(`{"type":"item.completed","item":{"id":"item_9","type":"reasoning_dump","blob":1}}`))
	if err != nil || events != nil {
		t.Fatalf("unknown item → (%+v, %v), want silence", events, err)
	}
}

func TestBuildCodexInvocationShapesTheArgv(t *testing.T) {
	invocation, err := BuildCodexInvocation("gpt-5.6-sol", "code", "high", "", false, "say hello")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--json", "--skip-git-repo-check", "-s", "workspace-write",
		"-m", "gpt-5.6-sol", "-c", "model_reasoning_effort=high"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("argv = %v, want %v", invocation.Args, want)
	}

	resumed, err := BuildCodexInvocation("gpt-5.6-sol", "chat", "xhigh", "00000000-0000-4000-8000-0000000000aa", true, "again")
	if err != nil {
		t.Fatal(err)
	}
	// Chat's files-cannot-be-touched claim is the vendor's read-only sandbox,
	// and a fresh thread is never passed as a resume.
	if !strings.Contains(strings.Join(resumed.Args, " "), "-s read-only") {
		t.Fatalf("chat argv = %v, want the vendor's read-only sandbox", resumed.Args)
	}
	if joined := strings.Join(resumed.Args, " "); !strings.Contains(joined, "resume 00000000-0000-4000-8000-0000000000aa") {
		t.Fatalf("resume argv = %v, want `resume <id>` with the prompt still on stdin", resumed.Args)
	}

	canonicalMax, err := BuildCodexInvocation("gpt-5.6-sol", "code", "max", "", false, "think deeply")
	if err != nil {
		t.Fatalf("canonical max was not translated for codex: %v", err)
	}
	if joined := strings.Join(canonicalMax.Args, " "); !strings.Contains(joined, "model_reasoning_effort=xhigh") {
		t.Fatalf("max argv = %v, want provider-native xhigh", canonicalMax.Args)
	}

	for _, probe := range []struct {
		model, mode, effort string
		want                string
	}{
		// Agent mode is no longer among these. It was refused while every
		// subagent shared one CodexBackend, and therefore one vendor thread —
		// several would have interleaved into a single transcript. A backend
		// per subagent removes the sharing, so the reason is gone; the case
		// below asserts it is accepted rather than rejected.
		{"", "code", "impossible", "effort level"},
		{"", "sideways", "", "unknown mode"},
		{"", "code", "", "prompt cannot be empty"},
	} {
		_, err := BuildCodexInvocation(probe.model, probe.mode, probe.effort, "", false, "")
		if err == nil || !strings.Contains(err.Error(), probe.want) {
			t.Fatalf("(model=%q mode=%q effort=%q) err = %v, want it to name %q", probe.model, probe.mode, probe.effort, err, probe.want)
		}
	}

	// Agent mode takes the same sandbox as code mode, now that a subagent gets
	// its own thread instead of resuming the backend's.
	agentMode, err := BuildCodexInvocation("gpt-5.6-sol", "agent", "", "", false, "do the thing")
	if err != nil {
		t.Fatalf("agent mode was refused: %v", err)
	}
	codeMode, err := BuildCodexInvocation("gpt-5.6-sol", "code", "", "", false, "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(agentMode.Args, " ") != strings.Join(codeMode.Args, " ") {
		t.Errorf("agent argv = %v, want the same as code %v", agentMode.Args, codeMode.Args)
	}
}

func TestBuildCodexInvocationWithExecutionOptionsCarriesWorkspaceAndNetwork(t *testing.T) {
	workspace := t.TempDir()
	additional := t.TempDir()
	invocation, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "agent", "high", "", false, "inspect the repository", ExecutionOptions{
		Workspace:      workspace,
		AdditionalDirs: []string{additional},
		NetworkAccess:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--cd", wantWorkspace, "-c", "sandbox_workspace_write.network_access=true"}
	for i := 0; i < len(want); i += 2 {
		if index := slices.Index(invocation.Args, want[i]); index < 0 || index+1 >= len(invocation.Args) || invocation.Args[index+1] != want[i+1] {
			t.Fatalf("argv = %v, want %s %s", invocation.Args, want[i], want[i+1])
		}
	}
	if invocation.ProcessOptions.Dir != wantWorkspace {
		t.Fatalf("process directory = %q, want canonical workspace %q", invocation.ProcessOptions.Dir, wantWorkspace)
	}
	wantAdditional, err := filepath.EvalSymlinks(additional)
	if err != nil {
		t.Fatal(err)
	}
	if index := slices.Index(invocation.Args, "--add-dir"); index < 0 || index+1 >= len(invocation.Args) || invocation.Args[index+1] != wantAdditional {
		t.Fatalf("argv = %v, want --add-dir %q", invocation.Args, wantAdditional)
	}
}

func TestBuildCodexInvocationWithExecutionOptionsRejectsUnverifiedWorkspace(t *testing.T) {
	for _, workspace := range []string{"relative/workspace", "/path/that/does/not/exist"} {
		if _, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "agent", "high", "", false, "inspect", ExecutionOptions{Workspace: workspace}); err == nil {
			t.Fatalf("workspace %q was accepted", workspace)
		}
	}
}

func TestBuildCodexInvocationWithExecutionOptionsOmitsNetworkByDefault(t *testing.T) {
	invocation, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "agent", "high", "", false, "inspect", ExecutionOptions{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(invocation.Args, "sandbox_workspace_write.network_access=true") {
		t.Fatalf("network override was enabled without a declaration: %v", invocation.Args)
	}
}

func TestRunCodexWithOptionsPassesTheDeclaredProcessDirectory(t *testing.T) {
	workspace := t.TempDir()
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "agent", "high", "", false, "inspect", ExecutionOptions{
		Workspace:     workspace,
		NetworkAccess: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got shell.ProcessOptions
	err = runCodexWithOptions(context.Background(), invocation, func(_ context.Context, _ string, _ []string, _ io.Reader, _ func([]byte) error, options shell.ProcessOptions) error {
		got = options
		return nil
	}, invocation.ProcessOptions, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != wantWorkspace {
		t.Fatalf("codex process directory = %q, want canonical workspace %q", got.Dir, wantWorkspace)
	}
}

func TestCodexBackendStoresCanonicalMaxAsProviderNativeXHigh(t *testing.T) {
	backend, err := NewCodexBackendFromHandle("gpt-5.6-sol", "code", "max", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Effort != "xhigh" {
		t.Fatalf("backend effort = %q, want xhigh", backend.Effort)
	}
}

func TestCodexKnowsTheGPT56SubscriptionFamily(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		if !CodexKnowsModel(model) {
			t.Errorf("CodexKnowsModel(%q) = false, want the current GPT-5.6 subscription family", model)
		}
	}
	for _, model := range []string{"gpt-5.5", "gpt-4.1", "gpt-5.6-unknown"} {
		if CodexKnowsModel(model) {
			t.Errorf("CodexKnowsModel(%q) = true, want an unknown model rejected", model)
		}
	}
}

// One captured turn through the backend: the invocation is shaped by the
// session's mode and effort, the answer and usage land, and the thread id the
// vendor minted becomes the handle the next turn resumes.
func TestCodexBackendLearnsTheThreadAndResumesIt(t *testing.T) {
	var spawnArgs [][]string
	var prompts []string
	playback, err := NewCodexBackendFromHandle("gpt-5.6-sol", "code", "high", "", false)
	if err != nil {
		t.Fatal(err)
	}
	playback.run = func(_ context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
		if executable != "codex" {
			t.Fatalf("runner command = %q", executable)
		}
		spawnArgs = append(spawnArgs, args)
		b, _ := io.ReadAll(stdin)
		prompts = append(prompts, string(b))
		for _, line := range codexFixtureLines(t, "codex-plain.jsonl") {
			if err := onLine(line); err != nil {
				return err
			}
		}
		return nil
	}
	var trail strings.Builder
	message, meta, err := playback.StreamChat(context.Background(), "gpt-5.6-sol",
		[]provider.Message{{Role: "user", Content: "Reply with exactly: ok"}},
		nil, func(token string) { trail.WriteString(token) })
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "ok" || message.Role != "assistant" {
		t.Fatalf("message = %+v, want the vendor's answer", message)
	}
	if meta.PromptTokens != 14876 || meta.CompletionTokens != 5 || meta.CacheReadTokens != 11008 {
		t.Fatalf("meta = %+v, want the turn's own usage", meta)
	}
	if meta.ToolCalls != 0 {
		t.Fatalf("tool calls = %d on a plain turn, want 0", meta.ToolCalls)
	}
	if !strings.Contains(trail.String(), "ok") {
		t.Fatalf("stream = %q, want the answer to have streamed", trail.String())
	}
	if got := playback.ProviderHandle(); got != "00000000-0000-4000-8000-0000000000aa" {
		t.Fatalf("handle = %q, want the vendor-minted thread id", got)
	}
	if !strings.Contains(prompts[0], "Reply with exactly: ok") || strings.Contains(strings.Join(spawnArgs[0], " "), "Reply with exactly: ok") {
		t.Fatalf("prompt = stdin %q argv %v, want it on stdin and out of the process table", prompts[0], spawnArgs[0])
	}

	// The next turn resumes the same thread: the vendor replays no argv, so
	// model and effort ride along every time.
	if _, _, err := playback.StreamChat(context.Background(), "gpt-5.6-sol",
		[]provider.Message{{Role: "user", Content: "again"}},
		nil, nil); err != nil {
		t.Fatal(err)
	}
	second := strings.Join(spawnArgs[1], " ")
	if !strings.Contains(second, "resume 00000000-0000-4000-8000-0000000000aa") {
		t.Fatalf("second argv = %v, want the captured thread resumed", spawnArgs[1])
	}
	if !strings.Contains(second, "-m gpt-5.6-sol") || !strings.Contains(second, "model_reasoning_effort=high") {
		t.Fatalf("second argv = %v, want model and effort re-passed", spawnArgs[1])
	}
	if !strings.Contains(prompts[1], "again") || strings.Contains(prompts[1], "Reply with exactly: ok") {
		t.Fatalf("second stdin = %q, want only this turn's prompt", prompts[1])
	}
}

// The vendor's tool loop through the backend end to end: the trail names every
// tool run and its outcome, and the turn's answer is only the last thing codex
// said, not every prose line between the tools.
func TestCodexBackendStreamsTheToolLoopTrail(t *testing.T) {
	backend, err := NewCodexBackendFromHandle("gpt-5.6-sol", "code", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(_ context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
		for _, line := range codexFixtureLines(t, "codex-tool-use.jsonl") {
			if err := onLine(line); err != nil {
				return err
			}
		}
		return nil
	}
	var trail strings.Builder
	message, meta, err := backend.StreamChat(context.Background(), "gpt-5.6-sol",
		[]provider.Message{{Role: "user", Content: "write hello.txt"}},
		nil, func(token string) { trail.WriteString(token) })
	if err != nil {
		t.Fatal(err)
	}
	if meta.ToolCalls != 3 {
		t.Fatalf("tool calls = %d, want one per vendor item that ran", meta.ToolCalls)
	}
	for _, want := range []string{"· file-change: add /work/hello.txt", "· shell: /usr/bin/bash", "→ ok", " 6f 6b 0a"} {
		if !strings.Contains(trail.String(), want) {
			t.Fatalf("trail is missing %q:\n%s", want, trail.String())
		}
	}
	if !strings.Contains(message.Content, "Created [hello.txt](/work/hello.txt)") {
		t.Fatalf("message = %q, want only the vendor's final word", message.Content)
	}
}

func TestCodexBackendObservedStreamKeepsProviderToolIdentity(t *testing.T) {
	backend, err := NewCodexBackendFromHandle("gpt-5.6-sol", "code", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
		for _, line := range codexFixtureLines(t, "codex-tool-use.jsonl") {
			if err := onLine(line); err != nil {
				return err
			}
		}
		return nil
	}
	var observed []provider.ProgressEvent
	if _, _, err := backend.StreamChatObserved(context.Background(), "gpt-5.6-sol",
		[]provider.Message{{Role: "user", Content: "write hello.txt"}}, nil, nil,
		func(event provider.ProgressEvent) { observed = append(observed, event) }); err != nil {
		t.Fatal(err)
	}
	var start, finish *provider.ProgressEvent
	for index := range observed {
		event := &observed[index]
		if event.Kind == provider.ProgressToolStarted && start == nil {
			start = event
		}
		if event.Kind == provider.ProgressToolFinished && finish == nil {
			finish = event
		}
	}
	if start == nil || finish == nil {
		t.Fatalf("provider progress = %+v, want a tool start and finish", observed)
	}
	if start.ID == "" || start.ID != finish.ID || start.Name == "" || start.Name != finish.Name {
		t.Fatalf("tool correlation = start %+v, finish %+v", start, finish)
	}
}

// A failing turn reads as the vendor's own cause, not a wall of escaped JSON.
func TestCodexBackendSurfacesTheVendorCause(t *testing.T) {
	playback, err := NewCodexBackendFromHandle("gpt-5.6-sol", "code", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	playback.run = func(_ context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
		for _, line := range codexFixtureLines(t, "codex-error.jsonl") {
			if err := onLine(line); err != nil {
				return err
			}
		}
		return nil
	}
	_, meta, err := playback.StreamChat(context.Background(), "gpt-5.6-sol",
		[]provider.Message{{Role: "user", Content: "hello"}}, nil, nil)
	if err == nil {
		t.Fatal("a turn.failed ended with no error")
	}
	if want := "The 'gpt-4.1' model is not supported when using Codex with a ChatGPT account."; !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want the vendor's unwrapped cause %q", err, want)
	}
	_ = meta
}
