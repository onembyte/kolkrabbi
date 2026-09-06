package agentcli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// V34.4c.2, observed live on 2026-09-06 (CLI 1.0.82): the argv kolk sends is
// the prompt, silent stats, the JSON event stream, the four privacy flags,
// the model only when one is named (the Free plan accepts only auto, so none
// is), every tool allowed only under full-auto in a tool mode, and the
// session resumed once the vendor has named one.
func TestBuildCopilotInvocationShapesTheArgv(t *testing.T) {
	privacy := "--no-remote-export --no-remote --no-auto-update --no-color"
	invocation, err := BuildCopilotInvocationWithOptions("", "code", "", "", "say hello", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"-p", "say hello", "-s", "--output-format", "json"}, strings.Fields(privacy)...)
	want = append(want, "--allow-all-tools")
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("argv = %v, want %v", invocation.Args, want)
	}

	explicit, err := BuildCopilotInvocationWithOptions("gpt-5.6-luna", "code", "max", "sess-1", "again", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(explicit.Args, " ")
	for _, part := range []string{"--model gpt-5.6-luna", "--effort xhigh", "--resume sess-1"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("explicit argv = %v, want %q", explicit.Args, part)
		}
	}
	if ultra, _ := BuildCopilotInvocationWithOptions("gpt-5.6-luna", "code", "ultra", "", "x", ExecutionOptions{}); !strings.Contains(strings.Join(ultra.Args, " "), "--effort max") {
		t.Fatalf("ultra argv = %v, want the vendor's max", ultra.Args)
	}
	// The vendor refuses an effort on auto, observed: kolk never sends one.
	if auto, _ := BuildCopilotInvocationWithOptions("auto", "code", "max", "", "x", ExecutionOptions{}); strings.Contains(strings.Join(auto.Args, " "), "--effort") || strings.Contains(strings.Join(auto.Args, " "), "--model") {
		t.Fatalf("auto argv = %v, want neither --model nor --effort", auto.Args)
	}

	asking, err := BuildCopilotInvocationWithOptions("", "code", "", "", "say hello", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(asking.Args, " "), "--allow-all-tools") {
		t.Fatalf("without full-auto every tool was allowed: %v", asking.Args)
	}
	chat, err := BuildCopilotInvocationWithOptions("", "chat", "", "", "say hello", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(chat.Args, " "), "--allow") {
		t.Fatalf("chat mode granted a tool: %v", chat.Args)
	}
	if _, err := BuildCopilotInvocationWithOptions("", "code", "", "", "   ", ExecutionOptions{}); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
}

// The workspace envelope: the process runs in the workspace, and every extra
// directory is granted with the vendor's own --add-dir.
func TestBuildCopilotInvocationCarriesTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	extra := t.TempDir()
	invocation, err := BuildCopilotInvocationWithOptions("", "code", "", "", "list files", ExecutionOptions{Workspace: workspace, AdditionalDirs: []string{extra}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(invocation.Args, " ")
	if !strings.Contains(joined, "--add-dir "+resolvedDir(t, extra)) {
		t.Fatalf("extra directory not granted: %v", invocation.Args)
	}
	if invocation.ProcessOptions.Dir != resolvedDir(t, workspace) {
		t.Fatalf("process dir = %q, want the workspace", invocation.ProcessOptions.Dir)
	}
}

// The backend on the live fixture: the stream becomes one reply with the
// vendor's model and usage, the session id is kept for the next turn, and
// a cancelled run ends with the context's error and nothing invented.
func TestCopilotBackendTurnsTheStreamIntoOneReplyAndKeepsTheSession(t *testing.T) {
	var seen []string
	backend, err := NewCopilotBackendWithOptions("", "code", "medium", "", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(_ context.Context, executable string, args []string, _ io.Reader, onLine func([]byte) error) error {
		seen = append(seen, executable+" "+strings.Join(args, " "))
		return replayFixture(t, "pong.jsonl", onLine)
	}
	var streamed strings.Builder
	msg, meta, err := backend.StreamChat(context.Background(), "", []provider.Message{{Role: "user", Content: "pong please"}}, nil, func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != "assistant" || msg.Content != "pong" || streamed.String() != "pong" {
		t.Fatalf("message = %+v streamed %q, want the reply", msg, streamed.String())
	}
	if meta.Billing != provider.BillingSubscription || meta.Model != "gpt-5.6-luna" || meta.PromptTokens != 14788 {
		t.Fatalf("meta = %+v, want subscription billing, the vendor's model and its usage", meta)
	}
	if backend.ProviderHandle() != "082b7ee5-7873-4b24-bea0-80a6235933d4" {
		t.Fatalf("handle = %q, want the result's session id", backend.ProviderHandle())
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], CopilotBinary+" -p ") || !strings.Contains(seen[0], "--output-format json") || !strings.HasSuffix(seen[0], "--allow-all-tools") {
		t.Fatalf("ran %v", seen)
	}
	// The next turn resumes what the vendor named.
	_, _, _ = backend.StreamChat(context.Background(), "", []provider.Message{{Role: "user", Content: "again"}}, nil, nil)
	if len(seen) != 2 || !strings.Contains(seen[1], "--resume 082b7ee5-7873-4b24-bea0-80a6235933d4") {
		t.Fatalf("second run %v, want --resume with the session", seen)
	}
}

func TestCopilotBackendReportsADeniedToolAsTheTurnsError(t *testing.T) {
	backend, err := NewCopilotBackendWithOptions("", "code", "", "", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
		return replayFixture(t, "denied.jsonl", onLine)
	}
	_, _, err = backend.StreamChat(context.Background(), "", []provider.Message{{Role: "user", Content: "make a file"}}, nil, nil)
	if !errors.Is(err, ErrCopilotToolsDenied) {
		t.Fatalf("denied run err = %v, want ErrCopilotToolsDenied", err)
	}
}

func TestCopilotBackendCancellationEndsWithTheContextsError(t *testing.T) {
	backend, err := NewCopilotBackendWithOptions("", "code", "", "", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(ctx context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
		_ = onLine([]byte(`{"type":"assistant.message_delta","data":{"deltaContent":"partial"}}`))
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go cancel()
	_, _, err = backend.StreamChat(ctx, "", []provider.Message{{Role: "user", Content: "go"}}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run err = %v, want context.Canceled", err)
	}
}

func replayFixture(t *testing.T, name string, onLine func([]byte) error) error {
	t.Helper()
	f, err := os.Open("testdata/copilot/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		if err := onLine(append([]byte(nil), scanner.Bytes()...)); err != nil {
			return err
		}
	}
	return nil
}

func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
