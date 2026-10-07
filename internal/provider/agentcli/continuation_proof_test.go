package agentcli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// What each adapter proves about its latest turn decides how a stopped vendor
// turn comes back. All three continue a confirmed conversation by its handle
// and say when the vendor itself closed a turn. Claude and codex can also
// prove a turn's prompt never reached a process; Copilot cannot.
func TestEachAdapterSaysWhatItCanProve(t *testing.T) {
	claude, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	codex, err := NewCodexBackendFromHandleWithOptions("gpt-5", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	copilot, err := NewCopilotBackendWithOptions("gpt-5", "code", "high", "", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name        string
		backend     any
		provesStart bool
	}{{"claude", claude, true}, {"codex", codex, true}, {"copilot", copilot, false}} {
		if _, ok := c.backend.(interface{ TurnNeverStarted() bool }); ok != c.provesStart {
			t.Errorf("%s can prove a turn never started = %v, want %v", c.name, ok, c.provesStart)
		}
		if _, ok := c.backend.(interface{ TurnClosed() bool }); !ok {
			t.Errorf("%s cannot say whether the vendor closed a turn", c.name)
		}
		resumable, ok := c.backend.(interface{ ResumesConversation() bool })
		if !ok || !resumable.ResumesConversation() {
			t.Errorf("%s does not say it resumes a confirmed conversation", c.name)
		}
	}
}

var claudeToolThenDeath = [][]byte{
	[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"vendor-h"}`),
	[]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}`),
	[]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"deployed"}]}}`),
}

// Found by the vendor-recovery verifier. A turn whose prompt reached the
// process is never proven unstarted, and without the vendor's result frame
// it was never closed, whatever became of the process. An ordinary exit
// keeps the conversation confirmed; a kill retires the handle.
func TestADeliveredClaudeTurnIsNeverProvenUnstarted(t *testing.T) {
	for _, c := range []struct {
		name          string
		hard          bool
		wantConfirmed bool
	}{{"the process exited mid-turn", false, true}, {"the process was killed", true, false}} {
		t.Run(c.name, func(t *testing.T) {
			backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "vendor-h", false, ExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			backend.start = func(context.Context, string, []string) (lineProcess, error) {
				return &fakeLineProcess{hardExit: c.hard, lines: claudeToolThenDeath}, nil
			}
			if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "deploy"}}, nil, func(string) {}); err == nil {
				t.Fatal("the dead process did not fail the turn")
			}
			if backend.TurnNeverStarted() || backend.TurnClosed() {
				t.Fatalf("never started %v, closed %v; want a delivered, unclosed turn", backend.TurnNeverStarted(), backend.TurnClosed())
			}
			if got := backend.ProviderHandleConfirmed(); got != c.wantConfirmed {
				t.Fatalf("handle %q confirmed = %v, want %v", backend.ProviderHandle(), got, c.wantConfirmed)
			}
		})
	}
}

// Claude's result frame closes its turn, even a limit's or an error's.
func TestClaudesResultFrameClosesTheTurn(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{lines: [][]byte{
			[]byte(`{"type":"system","subtype":"init","model":"opus","session_id":"vendor-h"}`),
			[]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"usage limit reached","session_id":"vendor-h"}`),
		}}, nil
	}
	_, _, _ = backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if !backend.TurnClosed() || backend.TurnNeverStarted() || !backend.ProviderHandleConfirmed() {
		t.Fatalf("closed %v, never started %v, confirmed %v; want a closed, delivered, confirmed turn",
			backend.TurnClosed(), backend.TurnNeverStarted(), backend.ProviderHandleConfirmed())
	}
}

// A turn whose process could not start never reached the vendor, and that is
// provable.
func TestAClaudeTurnWithNoProcessIsProvenUnstarted(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return nil, errors.New("claude is not installed")
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err == nil {
		t.Fatal("a process that never started answered")
	}
	if !backend.TurnNeverStarted() {
		t.Fatal("a turn with no process is not proven unstarted")
	}
}

// Found by the vendor-recovery verifier. Under a work log a vendor tool
// reaches observe rather than onToken, so "nothing was streamed" is no proof
// that nothing happened. A delivered prompt the vendor acted on is not
// retried with the same prompt.
func TestClaudeDoesNotResendAPromptTheVendorTookUp(t *testing.T) {
	for _, c := range []struct {
		name   string
		handle string
		resume bool
	}{{"a fresh conversation", "", false}, {"a resumed conversation", "known-h", true}} {
		t.Run(c.name, func(t *testing.T) {
			backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", c.handle, c.resume, ExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var prompts []string
			backend.start = func(context.Context, string, []string) (lineProcess, error) {
				if len(prompts) == 0 {
					return &recordingLineProcess{fakeLineProcess: fakeLineProcess{lines: claudeToolThenDeath}, sent: &prompts}, nil
				}
				return &recordingLineProcess{fakeLineProcess: fakeLineProcess{lines: claudeTurnFrames("deployed again")}, sent: &prompts}, nil
			}
			ctx := provider.WithToolProgress(context.Background())
			_, _, _ = backend.StreamChatObserved(ctx, "claude-opus", []provider.Message{{Role: "user", Content: "deploy"}}, nil,
				func(string) {}, func(provider.ProgressEvent) {})
			if len(prompts) != 1 {
				t.Fatalf("the prompt was sent %d times; the vendor took up the first and must not get it again", len(prompts))
			}
		})
	}
}

// A turn whose prompt could not reach the process, because it had already
// exited, is tried once more on a new one: nothing was delivered, so nothing
// can be repeated, and that retry is what makes an expired login cost one
// turn instead of two.
func TestClaudeRetriesATurnWhosePromptNeverArrived(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	spawns := 0
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		spawns++
		if spawns == 1 {
			return &recordingLineProcess{fakeLineProcess: fakeLineProcess{exitWhenDrained: true}, sent: &prompts}, nil
		}
		return &recordingLineProcess{fakeLineProcess: fakeLineProcess{lines: claudeTurnFrames("hello")}, sent: &prompts}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("the retry did not answer: %v", err)
	}
	if len(prompts) != 1 || backend.TurnNeverStarted() {
		t.Fatalf("delivered %d times, never started = %v; want one delivery, to the new process", len(prompts), backend.TurnNeverStarted())
	}
}

type recordingLineProcess struct {
	fakeLineProcess
	sent *[]string
}

func (p *recordingLineProcess) Send(line []byte) error {
	p.Queue(line)
	return nil
}

func (p *recordingLineProcess) Queue(line []byte) bool {
	if !p.fakeLineProcess.Queue(line) {
		return false
	}
	*p.sent = append(*p.sent, string(line))
	return true
}

// lostOutputProcess takes the prompt, acts on it, and its output never
// arrives; with drain, the interrupted turn's frames arrive in the drain.
type lostOutputProcess struct {
	effect string
	sends  *int
	reads  int
	drain  bool
}

func (p *lostOutputProcess) Send([]byte) error {
	*p.sends++
	return os.WriteFile(p.effect, []byte("vendor action completed"), 0o600)
}

func (p *lostOutputProcess) Next(context.Context) ([]byte, error) {
	p.reads++
	if p.drain {
		switch p.reads {
		case 1:
			return nil, errors.New("reader interrupted")
		case 2:
			return []byte(`{"type":"system","subtype":"init","session_id":"confirmed-in-drain"}`), nil
		case 3:
			return []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"done","name":"Bash","input":{"command":"deploy"}}]}}`), nil
		case 4:
			return []byte(`{"type":"result","subtype":"success","result":"deployed","session_id":"confirmed-in-drain"}`), nil
		}
	}
	return nil, io.EOF
}

func (*lostOutputProcess) Close() error   { return nil }
func (*lostOutputProcess) HardExit() bool { return false }

// Found by Codex's review. Absence of output after delivery is not proof of
// non-delivery: the process may have acted and lost its stdout. The prompt is
// not sent again, and the turn is not proven unstarted.
func TestADeliveredClaudePromptWithLostOutputIsUncertain(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	effect, sends := filepath.Join(t.TempDir(), "effect"), 0
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &lostOutputProcess{effect: effect, sends: &sends}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "deploy exactly once"}}, nil, nil); err == nil {
		t.Fatal("expected the lost output to fail the turn")
	}
	if sends != 1 {
		t.Errorf("the delivered prompt was sent %d times after its output was lost", sends)
	}
	if backend.TurnNeverStarted() {
		t.Error("a delivered prompt with lost output is proven never started")
	}
}

// Found by Codex's review. Frames the drain consumes after an interrupted read
// still belong to the turn: its conversation is confirmed, the vendor's result
// closed it, and it was not unstarted.
func TestWhatTheDrainReadsStillCountsForTheTurn(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	effect, sends := filepath.Join(t.TempDir(), "effect"), 0
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return &lostOutputProcess{effect: effect, sends: &sends, drain: true}, nil
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "deploy"}}, nil, nil); err == nil {
		t.Fatal("expected the interrupted read to fail the turn")
	}
	if backend.TurnNeverStarted() || !backend.TurnClosed() || !backend.ProviderHandleConfirmed() {
		t.Fatalf("never started %v, closed %v, confirmed %v; the drained init, tool and result were lost",
			backend.TurnNeverStarted(), backend.TurnClosed(), backend.ProviderHandleConfirmed())
	}
}

// Found by Codex's review. A codex process started with the prompt may act
// and lose its stdout: once it ran, the turn is not proven unstarted. Codex
// closes a turn with turn.completed or turn.failed, and only then.
func TestCodexProvesAndClosesTurnsOnlyFromWhatItSaw(t *testing.T) {
	for _, c := range []struct {
		name       string
		lines      []string
		wantClosed bool
	}{
		{"output lost", nil, false},
		{"turn.completed", []string{`{"type":"thread.started","thread_id":"th-1"}`, `{"type":"turn.completed"}`}, true},
		{"turn.failed", []string{`{"type":"thread.started","thread_id":"th-1"}`, `{"type":"turn.failed","error":{"message":"usage limit"}}`}, true},
		{"stopped mid-turn", []string{`{"type":"thread.started","thread_id":"th-1"}`, `{"type":"turn.started"}`}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			backend, err := NewCodexBackendFromHandleWithOptions("gpt-5", "code", "", "", false, ExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			backend.run = func(_ context.Context, _ string, _ []string, input io.Reader, onLine func([]byte) error) error {
				if input != nil {
					_, _ = io.ReadAll(input)
				}
				for _, line := range c.lines {
					if err := onLine([]byte(line)); err != nil {
						return err
					}
				}
				return errors.New("codex exited")
			}
			_, _, _ = backend.StreamChat(context.Background(), "gpt-5", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
			if backend.TurnNeverStarted() {
				t.Fatal("a codex run that received its prompt is proven never started")
			}
			if got := backend.TurnClosed(); got != c.wantClosed {
				t.Fatalf("closed = %v, want %v", got, c.wantClosed)
			}
		})
	}
}

// Copilot's result frame, the only one that names its session, closes the
// turn; without it the turn is open.
func TestCopilotClosesATurnOnlyWithItsResultFrame(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  bool
	}{
		{"result arrived", []string{`{"type":"result","sessionId":"cp-1","exitCode":0}`}, true},
		{"no result", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			backend, err := NewCopilotBackendWithOptions("gpt-5", "code", "high", "", ExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
				for _, line := range c.lines {
					if err := onLine([]byte(line)); err != nil {
						return err
					}
				}
				return errors.New("copilot exited")
			}
			_, _, _ = backend.StreamChat(context.Background(), "gpt-5", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
			if got := backend.TurnClosed(); got != c.want {
				t.Fatalf("closed = %v, want %v", got, c.want)
			}
		})
	}
}

// Claude's one-shot path, with no persistent process, closes its turn on
// the result frame too, and only then.
func TestClaudesOneShotTurnClosesOnItsResultFrame(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  bool
	}{
		{"result arrived", []string{`{"type":"system","subtype":"init","model":"opus","session_id":"h"}`, `{"type":"result","result":"ok","subtype":"success","session_id":"h"}`}, true},
		{"stopped before a result", []string{`{"type":"system","subtype":"init","model":"opus","session_id":"h"}`}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			backend.start = nil
			backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
				for _, line := range c.lines {
					if err := onLine([]byte(line)); err != nil {
						return err
					}
				}
				if !c.want {
					return errors.New("claude exited")
				}
				return nil
			}
			_, _, _ = backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
			if got := backend.TurnClosed(); got != c.want {
				t.Fatalf("closed = %v, want %v", got, c.want)
			}
		})
	}
}

// Found as a surviving mutant by the round-2 verifier. Every turn starts
// with nothing proven: a turn closed before is no proof about the next.
func TestEveryClaudeTurnStartsUnclosed(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = nil
	turns := [][]string{
		{`{"type":"system","subtype":"init","model":"opus","session_id":"h"}`, `{"type":"result","result":"ok","subtype":"success","session_id":"h"}`},
		{`{"type":"system","subtype":"init","model":"opus","session_id":"h"}`},
	}
	for i, lines := range turns {
		backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
			for _, line := range lines {
				if err := onLine([]byte(line)); err != nil {
					return err
				}
			}
			return errors.New("claude exited")
		}
		_, _, _ = backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
		if want := i == 0; backend.TurnClosed() != want {
			t.Fatalf("turn %d closed = %v, want %v", i+1, backend.TurnClosed(), want)
		}
	}
}

// sendFailingProcess cannot say whether a line was queued; it can only fail
// the write, as a process with no Queue does when its child is gone.
type sendFailingProcess struct{}

func (sendFailingProcess) Send([]byte) error                    { return io.ErrClosedPipe }
func (sendFailingProcess) Next(context.Context) ([]byte, error) { return nil, io.EOF }
func (sendFailingProcess) Close() error                         { return nil }
func (sendFailingProcess) HardExit() bool                       { return false }

// A process without Queue that fails the write never took the prompt: the
// session is done for, and the turn is tried once more on a new one.
func TestAFailedWriteWithoutQueueIsRetriedOnANewProcess(t *testing.T) {
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spawns := 0
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		spawns++
		if spawns == 1 {
			return sendFailingProcess{}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("hello")}, nil
	}
	msg, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil || msg.Content != "hello" || spawns != 2 {
		t.Fatalf("answer %q, err %v after %d spawns; want the retry on a new process to answer", msg.Content, err, spawns)
	}
}

// Claude's one-shot path proves a turn unstarted when its process could not
// run at all.
func TestClaudesOneShotTurnWithNoBinaryIsProvenUnstarted(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = nil
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err == nil {
		t.Fatal("a missing claude answered")
	}
	if !backend.TurnNeverStarted() {
		t.Fatal("a claude that could not run is not proven unstarted")
	}
}

// A turn whose prompt cannot even be built sends nothing; it does not carry
// the facts of the turn before it.
func TestATurnThatSendsNothingStartsWithNothingProven(t *testing.T) {
	claude, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claude.start = func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{lines: claudeTurnFrames("ok")}, nil
	}
	codex, err := NewCodexBackendFromHandleWithOptions("gpt-5", "code", "", "", false, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	codex.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
		for _, line := range []string{`{"type":"thread.started","thread_id":"th"}`, `{"type":"turn.completed"}`} {
			if err := onLine([]byte(line)); err != nil {
				return err
			}
		}
		return nil
	}
	for name, backend := range map[string]interface {
		StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error)
		TurnNeverStarted() bool
		TurnClosed() bool
	}{"claude": claude, "codex": codex} {
		_, _, _ = backend.StreamChat(context.Background(), "m", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
		if backend.TurnNeverStarted() || !backend.TurnClosed() {
			t.Fatalf("%s setup: first turn never started %v, closed %v", name, backend.TurnNeverStarted(), backend.TurnClosed())
		}
		if _, _, err := backend.StreamChat(context.Background(), "m", nil, nil, nil); err == nil {
			t.Fatalf("%s: an empty prompt was sent", name)
		}
		if !backend.TurnNeverStarted() || backend.TurnClosed() {
			t.Errorf("%s: a turn that sent nothing reports never started %v, closed %v", name, backend.TurnNeverStarted(), backend.TurnClosed())
		}
	}
}

// Found as a surviving mutant by the round-4 verifier. Copilot's turns start
// unclosed too: one closed before is no proof about the next.
func TestEveryCopilotTurnStartsUnclosed(t *testing.T) {
	backend, err := NewCopilotBackendWithOptions("gpt-5", "code", "high", "", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i, lines := range [][]string{{`{"type":"result","sessionId":"cp-1","exitCode":0}`}, nil} {
		backend.run = func(_ context.Context, _ string, _ []string, _ io.Reader, onLine func([]byte) error) error {
			for _, line := range lines {
				if err := onLine([]byte(line)); err != nil {
					return err
				}
			}
			if lines == nil {
				return errors.New("copilot exited")
			}
			return nil
		}
		_, _, _ = backend.StreamChat(context.Background(), "gpt-5", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
		if got, want := backend.TurnClosed(), i == 0; got != want {
			t.Fatalf("turn %d closed = %v, want %v", i+1, got, want)
		}
	}
}

// drainOnlyProcess takes the prompt, says nothing until the turn is
// cancelled, then hands the drain what it had and ends.
type drainOnlyProcess struct {
	waited bool
	lines  [][]byte
}

func (p *drainOnlyProcess) Send([]byte) error { return nil }
func (p *drainOnlyProcess) Queue([]byte) bool { return true }
func (p *drainOnlyProcess) Close() error      { return nil }
func (p *drainOnlyProcess) HardExit() bool    { return false }
func (p *drainOnlyProcess) Next(ctx context.Context) ([]byte, error) {
	if !p.waited {
		p.waited = true
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(p.lines) == 0 {
		return nil, io.EOF
	}
	line := p.lines[0]
	p.lines = p.lines[1:]
	return line, nil
}

// Found as a surviving mutant by the round-4 verifier. A resumed process
// whose only answer reached the drain after a cancellation did answer: its
// conversation is alive, and the next turn goes on resuming it.
func TestAnAnswerOnlyTheDrainSawIsNotADeadResume(t *testing.T) {
	spawned := [][]string{}
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "handle-alive", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawned = append(spawned, append([]string(nil), args...))
		if len(spawned) == 1 {
			return &drainOnlyProcess{lines: claudeTurnFrames("partly")[:1]}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("again")}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, _, err := backend.StreamChat(ctx, "claude-opus", []provider.Message{{Role: "user", Content: "one"}}, nil, nil); err == nil {
		t.Fatal("a cancelled turn answered")
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", []provider.Message{{Role: "user", Content: "two"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 2 {
		t.Fatalf("spawned %d processes, want two", len(spawned))
	}
	if i := slices.Index(spawned[1], "--resume"); i < 0 || spawned[1][i+1] != "handle-alive" {
		t.Fatalf("next spawn %q left a conversation that had answered", spawned[1])
	}
}

// A new kolk session on the same backend leaves the old session's
// conversation: the process ends, and the next turn opens a new one.
func TestClaudeForgetsItsConversationWhenAsked(t *testing.T) {
	spawned := [][]string{}
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-opus", "code", "high", "old-session-h", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	backend.start = func(_ context.Context, _ string, args []string) (lineProcess, error) {
		spawned = append(spawned, append([]string(nil), args...))
		return &fakeLineProcess{lines: claudeTurnFrames("ok")}, nil
	}
	if _, err := oneTurn(t, backend, "one"); err != nil {
		t.Fatal(err)
	}
	if backend.ProviderHandleRetired() {
		t.Fatal("a live conversation reads retired")
	}
	backend.ForgetConversation()
	if backend.ProviderHandle() != "" || !backend.ProviderHandleRetired() {
		t.Fatalf("after forgetting: handle %q, retired %v", backend.ProviderHandle(), backend.ProviderHandleRetired())
	}
	if _, err := oneTurn(t, backend, "two"); err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 2 || slices.Contains(spawned[1], "old-session-h") || !slices.Contains(spawned[1], "--session-id") {
		t.Fatalf("spawns %q; want the second turn on a new conversation", spawned)
	}
	if backend.ProviderHandleRetired() {
		t.Fatal("a new conversation still reads retired")
	}
}

func TestCodexAndCopilotForgetTheirConversationsWhenAsked(t *testing.T) {
	codex, err := NewCodexBackendFromHandleWithOptions("gpt-5.6-sol", "code", "", "th-old", true, ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	codex.ForgetConversation()
	if codex.ProviderHandle() != "" {
		t.Fatalf("codex still drives %q", codex.ProviderHandle())
	}
	copilot, err := NewCopilotBackendWithOptions("gpt-5", "code", "high", "cp-old", ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	copilot.ForgetConversation()
	if copilot.ProviderHandle() != "" {
		t.Fatalf("copilot still drives %q", copilot.ProviderHandle())
	}
}
