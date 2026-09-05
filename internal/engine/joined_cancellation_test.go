package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// slowShutdownBackend blocks a turn until its context is cancelled, then takes
// a while to come back -- the shape of a vendor child being torn down -- and
// counts how many turns are still inside it.
type slowShutdownBackend struct {
	shutdown time.Duration
	inside   atomic.Int32
	closed   atomic.Bool
}

func (b *slowShutdownBackend) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.inside.Add(1)
	defer b.inside.Add(-1)
	<-ctx.Done()
	time.Sleep(b.shutdown)
	return provider.Message{}, provider.Meta{}, ctx.Err()
}

func (b *slowShutdownBackend) Close() error { b.closed.Store(true); return nil }

// A cancelled run is over when runTasks returns: every subagent has stopped
// and every backend is closed. Returning earlier lets a goroutine keep writing
// results, ending checkpoints, recording cost and publishing events into a
// turn the caller has already declared finished.
func TestACancelledRunReturnsOnlyAfterEverySubagentHasStopped(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "unused"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Root = t.TempDir()
	agent.MaxConcurrentTasks = 2

	var mu sync.Mutex
	backends := map[string]*slowShutdownBackend{}
	agent.SubagentBackend = func(_ context.Context, _, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
		mu.Lock()
		defer mu.Unlock()
		// The first subagent shuts down at once, the second slowly: a runTasks
		// that returns on the first result comes back while the second is
		// still inside its turn.
		d := 10 * time.Millisecond
		if len(backends) == 1 {
			d = 300 * time.Millisecond
		}
		b := &slowShutdownBackend{shutdown: d}
		backends[string(rune('a'+len(backends)))] = b
		return b, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(backends)
			mu.Unlock()
			if n == 2 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond) // both inside StreamChat
		cancel()
	}()
	_, err := agent.runTasks(ctx, "two things", []Task{{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch}})
	if err == nil {
		t.Fatal("a cancelled run reported success")
	}
	mu.Lock()
	defer mu.Unlock()
	for name, b := range backends {
		if b.inside.Load() != 0 {
			t.Errorf("subagent %s was still inside its turn when runTasks returned", name)
		}
		if !b.closed.Load() {
			t.Errorf("subagent %s's backend was still open when runTasks returned", name)
		}
	}
}

// slowCloseBackend answers at once and takes a while to close.
type slowCloseBackend struct {
	inner  ChatBackend
	closed atomic.Bool
}

func (b *slowCloseBackend) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.inner.StreamChat(ctx, model, msgs, tools, onToken)
}

func (b *slowCloseBackend) Close() error {
	time.Sleep(50 * time.Millisecond)
	b.closed.Store(true)
	return nil
}

// On success too: a task's result is reported only once its backend is closed,
// so the caller never sees a finished run with a child still alive.
func TestASubagentBackendIsClosedBeforeItsResultIsReported(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Root = t.TempDir()
	var opened *slowCloseBackend
	agent.SubagentBackend = func(_ context.Context, _, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
		opened = &slowCloseBackend{inner: agent.sessionBackend()}
		return opened, nil
	}
	if _, err := agent.runTasks(context.Background(), "one thing", []Task{{Title: "a", Kind: KindResearch}}); err != nil {
		t.Fatal(err)
	}
	if opened == nil || !opened.closed.Load() {
		t.Fatal("runTasks returned before the subagent's backend finished closing")
	}
}
