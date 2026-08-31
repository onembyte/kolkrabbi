package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// A cheaper rung that will not spawn must not lose the task. The work still
// needs doing, and the model the user selected can always do it.
func TestARungThatWillNotStartFallsBackToTheModelTheUserChose(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("claude-sonnet")

	var opened []string
	agent.SubagentBackend = func(_ context.Context, model, _, _ string) (ChatBackend, error) {
		opened = append(opened, model)
		if model == "claude-haiku" {
			return nil, errors.New("no such vendor process")
		}
		return &openedBackend{model: model, inner: agent.sessionBackend()}, nil
	}

	outcomes, err := agent.runTasks(context.Background(), "one thing",
		[]Task{{Title: "commit", Kind: KindResearch, Model: "claude-haiku"}})
	if err != nil {
		t.Fatalf("runTasks: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status == statusFailed {
		t.Errorf("the task failed instead of falling back: %+v", outcomes)
	}
	if len(opened) != 2 || opened[1] != "claude-sonnet" {
		t.Errorf("providers opened = %v, want the cheap rung then the ceiling", opened)
	}
}

// Silently running on a more expensive model is the exact surprise this whole
// feature exists to prevent — even when the direction is "up to what you
// already chose".
func TestTheFallbackToTheCeilingIsAnnouncedNotSilent(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("claude-sonnet")
	agent.SubagentBackend = func(_ context.Context, model, _, _ string) (ChatBackend, error) {
		if model == "claude-haiku" {
			return nil, errors.New("no such vendor process")
		}
		return &openedBackend{model: model, inner: agent.sessionBackend()}, nil
	}

	_, _ = agent.runTasks(context.Background(), "one thing",
		[]Task{{Title: "commit", Kind: KindResearch, Model: "claude-haiku"}})

	said := out.String()
	if !strings.Contains(said, "claude-haiku") || !strings.Contains(said, "claude-sonnet") {
		t.Errorf("the fallback did not name both models:\n%s", said)
	}
}

func TestFallbackUpdatesTheLiveSubagentRouteWhileItIsWorking(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("claude-sonnet")
	agent.MaxConcurrentTasks = 1
	agent.SubagentBackend = func(_ context.Context, model, _, _ string) (ChatBackend, error) {
		if model == "claude-haiku" {
			return nil, errors.New("no such vendor process")
		}
		return &openedBackend{model: model, inner: agent.sessionBackend()}, nil
	}
	var seen []SubagentStatus
	agent.Subagents = func(status SubagentStatus) { seen = append(seen, status) }

	_, _ = agent.runTasks(context.Background(), "one thing",
		[]Task{{Title: "inspect", Kind: KindResearch, Model: "claude-haiku"}})

	if len(seen) != 3 {
		t.Fatalf("lifecycle updates = %+v, want initial route, fallback route, and finish", seen)
	}
	if seen[0].Model != "claude-haiku" || seen[0].State != SubagentWorking {
		t.Fatalf("initial status = %+v, want the attempted cheap route", seen[0])
	}
	if seen[1].Model != "claude-sonnet" || seen[1].State != SubagentWorking {
		t.Fatalf("fallback status = %+v, want the live row moved to the session model", seen[1])
	}
	if seen[2].Model != "claude-sonnet" || seen[2].State != SubagentDone {
		t.Fatalf("finished status = %+v, want the route that actually completed", seen[2])
	}
}

// A ceiling that will not start either is a real failure. Climbing further is
// not possible and retrying forever is not a plan.
func TestASecondFailureOnTheCeilingFailsTheTask(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "unused"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("claude-sonnet")

	attempts := 0
	agent.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		attempts++
		return nil, errors.New("nothing will start")
	}

	outcomes, err := agent.runTasks(context.Background(), "one thing",
		[]Task{{Title: "commit", Kind: KindResearch, Model: "claude-haiku"}})
	if err != nil {
		t.Fatalf("one unstartable task threw the whole run away: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != statusFailed {
		t.Errorf("outcome = %+v, want a failure", outcomes)
	}
	if attempts != 2 {
		t.Errorf("the provider was opened %d times, want the rung then the ceiling and no more", attempts)
	}
}

// A task already ON the ceiling has nowhere to fall back to, so it must not
// try the same model twice.
func TestATaskAlreadyOnTheCeilingIsNotRetried(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "unused"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("claude-sonnet")

	attempts := 0
	agent.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		attempts++
		return nil, errors.New("nothing will start")
	}
	_, _ = agent.runTasks(context.Background(), "one thing",
		[]Task{{Title: "x", Kind: KindResearch, Model: "claude-sonnet"}})

	if attempts != 1 {
		t.Errorf("a task already on the ceiling was opened %d times, want once", attempts)
	}
}

// Eight subagents meeting the same exhausted plan must ask the user once. The
// decision is about the session, not about whichever task happened to arrive
// first, and eight identical prompts is the failure people remember.
func TestEightSubagentsHittingTheLimitAskOnce(t *testing.T) {
	chooser := &countingChooser{answer: "Continue on openrouter/metered"}
	agent := &Agent{Options: Options{
		Model:               "claude-sonnet",
		OnSubscriptionLimit: OnLimitAsk,
		MeteredModel:        func() string { return "openrouter/metered" },
		Ask:                 chooser,
	}}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = agent.resolveSubscriptionLimit(context.Background(), "claude-sonnet")
		}()
	}
	wg.Wait()

	if got := chooser.calls(); got != 1 {
		t.Errorf("the user was asked %d times about one exhausted plan, want once", got)
	}
}

// countingChooser answers the limit question and counts how often it was asked.
type countingChooser struct {
	mu     sync.Mutex
	answer string
	n      int
}

func (c *countingChooser) Choose(context.Context, Choice) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.answer, true
}

func (c *countingChooser) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
