package agentcli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// V34.4c.2: the Copilot CLI's documented programmatic contract (read
// 2026-09-05): `-p PROMPT` runs one prompt and exits, `-s` prints only the
// agent's response, `--model` picks the model, `--allow-all-tools` lets every
// tool run, `--add-dir` widens the allowed paths. Kolk sends exactly that and
// nothing undocumented: the prompt on the command line as the vendor says,
// every tool allowed only under full-auto in a tool mode, and chat mode
// granting nothing.
func TestBuildCopilotInvocationShapesTheArgv(t *testing.T) {
	invocation, err := BuildCopilotInvocationWithOptions("gpt-5.4", "code", "say hello", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "say hello", "-s", "--model", "gpt-5.4", "--allow-all-tools"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("argv = %v, want %v", invocation.Args, want)
	}

	asking, err := BuildCopilotInvocationWithOptions("gpt-5.4", "code", "say hello", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(asking.Args, " "), "--allow-all-tools") {
		t.Fatalf("without full-auto every tool was allowed: %v", asking.Args)
	}
	chat, err := BuildCopilotInvocationWithOptions("gpt-5.4", "chat", "say hello", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(chat.Args, " "), "--allow") {
		t.Fatalf("chat mode granted a tool: %v", chat.Args)
	}

	if _, err := BuildCopilotInvocationWithOptions("gpt-5.4", "code", "   ", ExecutionOptions{}); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
	if _, err := BuildCopilotInvocationWithOptions("", "code", "hi", ExecutionOptions{}); err != nil {
		t.Fatalf("no model must mean the vendor's default, got %v", err)
	}
	if noModel, _ := BuildCopilotInvocationWithOptions("", "code", "hi", ExecutionOptions{}); strings.Contains(strings.Join(noModel.Args, " "), "--model") {
		t.Fatalf("an empty model still sent --model: %v", noModel.Args)
	}
}

// The workspace envelope: the process runs in the workspace, and every extra
// directory is granted with the vendor's own --add-dir.
func TestBuildCopilotInvocationCarriesTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	extra := t.TempDir()
	invocation, err := BuildCopilotInvocationWithOptions("gpt-5.4", "code", "list files", ExecutionOptions{Workspace: workspace, AdditionalDirs: []string{extra}})
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

func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// The backend: the CLI's `-s` output is the reply, line by line, as one
// assistant message billed as the plan's turn; a cancelled run ends with the
// context's error and nothing invented. No live run happened here — the
// binary is the owner's to install — so this pins the documented contract.
func TestCopilotBackendTurnsPrintedLinesIntoOneReply(t *testing.T) {
	var seen []string
	backend, err := NewCopilotBackendWithOptions("gpt-5.4", "code", ExecutionOptions{BypassPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(_ context.Context, executable string, args []string, _ io.Reader, onLine func([]byte) error) error {
		seen = append(seen, executable+" "+strings.Join(args, " "))
		for _, line := range []string{"first line", "second line"} {
			if err := onLine([]byte(line)); err != nil {
				return err
			}
		}
		return nil
	}
	var streamed strings.Builder
	msg, meta, err := backend.StreamChat(context.Background(), "gpt-5.4", []provider.Message{{Role: "user", Content: "say two lines"}}, nil, func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != "assistant" || msg.Content != "first line\nsecond line\n" {
		t.Fatalf("message = %+v, want the printed lines as one reply", msg)
	}
	if streamed.String() != msg.Content {
		t.Fatalf("streamed %q, want the same text the message holds", streamed.String())
	}
	if meta.Billing != provider.BillingSubscription || meta.Model != "gpt-5.4" {
		t.Fatalf("meta = %+v, want subscription billing on the model asked for", meta)
	}
	// The prompt carries the role prefix every handover gets; the rest is
	// the documented argv, verbatim, on the copilot binary.
	if len(seen) != 1 || !strings.HasPrefix(seen[0], CopilotBinary+" -p ") || !strings.Contains(seen[0], "say two lines") ||
		!strings.HasSuffix(seen[0], " -s --model gpt-5.4 --allow-all-tools") {
		t.Fatalf("ran %v, want the documented argv on the copilot binary", seen)
	}
}

func TestCopilotBackendCancellationEndsWithTheContextsError(t *testing.T) {
	backend, err := NewCopilotBackendWithOptions("gpt-5.4", "code", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.run = func(ctx context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
		_ = onLine([]byte("partial"))
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go cancel()
	_, _, err = backend.StreamChat(ctx, "gpt-5.4", []provider.Message{{Role: "user", Content: "go"}}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run err = %v, want context.Canceled", err)
	}
}
