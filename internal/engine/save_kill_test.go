package engine_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// killChildDir names the directory the re-executed child works in, and its
// presence is what turns this test binary into that child.
const killChildDir = "KOLK_O3_KILL_CHILD_DIR"

// TestMain re-execs this binary as the victim of the durability test below.
//
// A killed process cannot be faked from inside one: anything that unwinds — a
// panic, os.Exit, a cancelled context — still runs whatever the runtime was
// going to run, and the whole question here is what survives when nothing gets
// to run at all.
func TestMain(m *testing.M) {
	if dir := os.Getenv(killChildDir); dir != "" {
		runUntilKilled(dir)
		os.Exit(2) // unreachable: runUntilKilled blocks until it is killed
	}
	os.Exit(m.Run())
}

// interruptedBackend answers one tool call and nothing else. The kill happens
// inside the tool, not here, so the second call only exists to fail loudly if
// the child ever gets that far.
type interruptedBackend struct{ target string }

func (b interruptedBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{
		Role:    "assistant",
		Content: "Writing the file now.",
		ToolCalls: []provider.ToolCall{{
			ID: "call_interrupted",
			Function: provider.FunctionCall{
				Name:      "write_file",
				Arguments: `{"path":"` + jsonEsc(b.target) + `","content":"written before the kill\n"}`,
			},
		}},
	}, provider.Meta{Model: "mock/model"}, nil
}

// runUntilKilled is the child: one turn, one file-writing tool, and then a
// deliberate stop in the window between the file changing and the tool result
// being recorded. That window is the one O3's coalescing widened, so it is the
// one the parent kills in.
func runUntilKilled(dir string) {
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		os.Exit(3)
	}
	sess := session.New(dir, "mock/model")
	target := filepath.Join(work, "made.txt")

	agent := engine.New(engine.Options{
		Backend: interruptedBackend{target: target},
		Model:   "mock/model", Mode: engine.ModeCode, Effort: engine.EffortMax,
		Permission: engine.PermissionFullAuto,
		Sess:       sess, Root: work, Out: os.Stdout,
		// An hour: nothing on disk can be there because an interval elapsed.
		// Whatever the parent finds was written by a boundary, deliberately.
		SaveInterval: time.Hour,
		// The seam is exactly after the tool changed the tree and before the
		// engine records what it did.
		PostWrite: func(string, string) {
			// The session id tells the parent which transcript to read, and
			// writing it is also the "I am here now" signal.
			_ = os.WriteFile(filepath.Join(dir, "ready"), []byte(sess.ID), 0o600)
			select {} // wait to be killed
		},
	})
	_ = agent.RunTurn(context.Background(), "write the file")
	os.Exit(4) // the turn was supposed to never finish
}

// The promise O3 must not break: coalescing may lose chat, never a boundary.
// A session killed with SIGKILL between a tool changing the tree and the engine
// recording the result comes back at the last flushed boundary, with the
// interrupted call repaired, and the API-valid history that repair exists for.
func TestAKilledToolLoopResumesFromTheLastFlushedBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("re-execs this binary and kills it; not for -short")
	}
	dir := t.TempDir()

	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), killChildDir+"="+dir)
	child.Stdout, child.Stderr = nil, nil
	if err := child.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	id := waitForReady(t, child, filepath.Join(dir, "ready"))

	// Kill, not signal: on unix this is SIGKILL, which no handler, defer or
	// atexit hook can intercept. Nothing the engine could have arranged for
	// "on the way out" runs.
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("killing the child: %v", err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("the child exited on its own; it was supposed to be killed mid-tool-loop")
	}

	// 1. The tree really did change before the kill, so this is the awkward
	//    case and not a turn that never started.
	if _, err := os.Stat(filepath.Join(dir, "work", "made.txt")); err != nil {
		t.Fatalf("the tool never wrote its file, so nothing was interrupted: %v", err)
	}

	// 2. The last flushed boundary is on disk: the user's message and the
	//    assistant's tool call, written by the flush that runs before a tool
	//    is allowed to touch the tree. The tool's result is not — the interval
	//    was an hour and the round never reached its own save.
	loaded, err := session.Load(dir, id)
	if err != nil {
		t.Fatalf("loading the killed session: %v", err)
	}
	saved := loaded.GetMessages()
	if len(saved) != 3 {
		t.Fatalf("saved %d messages, want system + user + the assistant tool call:\n%s", len(saved), describe(saved))
	}
	if saved[1].Role != "user" || !strings.Contains(saved[1].Content, "write the file") {
		t.Errorf("message 1 = %+v, want the user's turn", saved[1])
	}
	if len(saved[2].ToolCalls) != 1 || saved[2].ToolCalls[0].ID != "call_interrupted" {
		t.Fatalf("message 2 = %+v, want the assistant's interrupted tool call", saved[2])
	}

	// 3. Resuming repairs it: the dangling call gets an answer, so the next
	//    request to a provider is valid rather than rejected.
	resumed := engine.New(engine.Options{
		Backend: interruptedBackend{}, Model: "mock/model", Mode: engine.ModeCode,
		Sess: loaded, Out: &strings.Builder{},
	})
	_ = resumed
	repaired := loaded.GetMessages()
	if len(repaired) != 4 {
		t.Fatalf("after repair there are %d messages, want the synthetic tool result appended:\n%s", len(repaired), describe(repaired))
	}
	last := repaired[len(repaired)-1]
	if last.Role != "tool" || last.ToolCallID != "call_interrupted" {
		t.Fatalf("repaired message = %+v, want a tool result for call_interrupted", last)
	}
	if !strings.Contains(last.Content, "Interrupted") {
		t.Errorf("repaired content = %q, want it to say the call was interrupted", last.Content)
	}
}

// waitForReady blocks until the child reaches the kill window, failing rather
// than hanging if it dies or never gets there.
func waitForReady(t *testing.T, child *exec.Cmd, marker string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if id, err := os.ReadFile(marker); err == nil && len(id) > 0 {
			return string(id)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	t.Fatal("the child never reached the point where its tool had written the file")
	return ""
}

func describe(messages []provider.Message) string {
	var b strings.Builder
	for i, m := range messages {
		fmt.Fprintf(&b, "  [%d] %s calls=%d %s\n", i, m.Role, len(m.ToolCalls), compact(m.Content))
	}
	return b.String()
}

func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
