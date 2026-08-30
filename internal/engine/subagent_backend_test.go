package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// openedBackend is a provider a test can watch: it records what it was opened
// for and whether it was closed.
type openedBackend struct {
	model  string
	mu     sync.Mutex
	closed bool
	inner  ChatBackend
}

func (b *openedBackend) StreamChat(ctx context.Context, model string, msgs []provider.Message,
	tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.inner.StreamChat(ctx, model, msgs, tools, onToken)
}

func (b *openedBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

func (b *openedBackend) wasClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// A subagent that shares the parent's backend shares its vendor conversation
// and its mutex: several tasks would serialise on one process and write their
// briefings into one transcript. Each gets its own.
func TestEachSubagentTalksToItsOwnProvider(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: "one"}, enginetest.Step{Text: "two"},
		enginetest.Step{Text: "three"}, enginetest.Step{Text: "four"},
	)
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)

	var mu sync.Mutex
	var opened []*openedBackend
	agent.SubagentBackend = func(_ context.Context, model, _, _ string) (ChatBackend, error) {
		mu.Lock()
		defer mu.Unlock()
		backend := &openedBackend{model: model, inner: agent.sessionBackend()}
		opened = append(opened, backend)
		return backend, nil
	}

	_, err := agent.runTasks(context.Background(), "two things", []Task{
		{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch},
	})
	if err != nil {
		t.Fatalf("runTasks: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 2 {
		t.Fatalf("opened %d backends, want one per task", len(opened))
	}
	if opened[0] == opened[1] {
		t.Error("both subagents were handed the same provider")
	}
}

// A provider owns a child process, and nothing else will release it.
func TestASubagentBackendIsClosedOnEveryPathOutOfATask(t *testing.T) {
	for _, tc := range []struct {
		name string
		step enginetest.Step
	}{
		{"success", enginetest.Step{Text: "done"}},
		{"failure", enginetest.Step{StatusCode: 500, ErrorBody: "boom"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := enginetest.New(tc.step)
			defer srv.Close()
			agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)

			var opened *openedBackend
			agent.SubagentBackend = func(_ context.Context, model, _, _ string) (ChatBackend, error) {
				opened = &openedBackend{model: model, inner: agent.sessionBackend()}
				return opened, nil
			}
			_, _ = agent.runTasks(context.Background(), "one thing", []Task{{Title: "a", Kind: KindResearch}})

			if opened == nil {
				t.Fatal("no backend was opened")
			}
			if !opened.wasClosed() {
				t.Error("the subagent's provider was left open; it owns a child process")
			}
		})
	}
}

// A subagent runs one supervised vendor call. It is never the session's own
// mode, because agent mode on a child would ask the vendor to orchestrate.
func TestASubagentIsAlwaysOpenedInCodeMode(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)

	var gotMode string
	agent.SubagentBackend = func(_ context.Context, model, mode, _ string) (ChatBackend, error) {
		gotMode = mode
		return &openedBackend{model: model, inner: agent.sessionBackend()}, nil
	}
	_, _ = agent.runTasks(context.Background(), "one thing", []Task{{Title: "a", Kind: KindResearch}})

	if gotMode != ModeCode {
		t.Errorf("subagent opened in %q mode, want code", gotMode)
	}
}

// With no port configured, everything works exactly as it did: every task
// shares the session's backend.
func TestWithoutThePortEverySubagentSharesTheSessionBackend(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "one"}, enginetest.Step{Text: "two"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SubagentBackend = nil

	outcomes, err := agent.runTasks(context.Background(), "two things", []Task{
		{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch},
	})
	if err != nil {
		t.Fatalf("runTasks: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(outcomes))
	}
}

// A provider that will not open must fail the task, not the run, and must not
// be reported as a success.
func TestATaskWhoseProviderWillNotOpenFails(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "unused"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		return nil, errors.New("no such provider")
	}
	outcomes, err := agent.runTasks(context.Background(), "one thing", []Task{{Title: "a", Kind: KindResearch}})
	if err != nil {
		t.Fatalf("one unopenable provider threw the whole run away: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != statusFailed {
		t.Errorf("outcome = %+v, want a failure", outcomes)
	}
}

// The graft the judges asked for. streamChat rewrites `model` in its own loop —
// free-model rotation and the metered fallback both do — and a provider opened
// for one model must not be handed another. A claude process asked for a
// gateway id burns the turn to discover it.
func TestAPinnedProviderIsDroppedOnceTheModelMoves(t *testing.T) {
	pinned := pinnedBackend{backend: &openedBackend{}, model: "claude-sonnet"}

	if got := pinned.forModel("claude-sonnet"); got == nil {
		t.Error("the provider was dropped for the model it was opened for")
	}
	for _, moved := range []string{"claude-haiku", "openrouter/free", ""} {
		if got := pinned.forModel(moved); got != nil {
			t.Errorf("a provider opened for claude-sonnet was handed %q", moved)
		}
	}
}

// An empty pin is what every non-subagent call passes, and it must never
// shadow the route.
func TestAnEmptyPinNeverShadowsTheRoute(t *testing.T) {
	var empty pinnedBackend
	for _, model := range []string{"claude-sonnet", "", "anything"} {
		if got := empty.forModel(model); got != nil {
			t.Errorf("an empty pin answered for %q", model)
		}
	}
}

// Releasing is safe to call whatever happened, because the caller defers it
// before it can know which case it got.
func TestReleasingASubagentProviderIsAlwaysSafe(t *testing.T) {
	agent := &Agent{}

	// No port at all.
	backend, release, err := agent.openSubagentBackend(context.Background(), "m")
	if err != nil || backend != nil {
		t.Fatalf("no port gave backend=%v err=%v, want both nil", backend, err)
	}
	release()

	// A backend that is not a Closer.
	agent.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		return notACloser{}, nil
	}
	if _, release, err = agent.openSubagentBackend(context.Background(), "m"); err != nil {
		t.Fatalf("open: %v", err)
	}
	release()

	// A failed open.
	agent.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		return nil, errors.New("nope")
	}
	if _, release, err = agent.openSubagentBackend(context.Background(), "m"); err == nil {
		t.Error("a failed open reported success")
	}
	release()
}

type notACloser struct{}

func (notACloser) StreamChat(context.Context, string, []provider.Message, []provider.Tool,
	func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}
