package engine

import (
	"context"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/mcp"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Every Agent field is either renewed for a new session or kept for the
// process. A new field has to say which, here, before /new can be trusted to
// start clean: a whole-struct overwrite used to decide it for everything, and
// raced every goroutine still holding the agent.
// renewedFields go back to a fresh agent's value on ReplaceSession;
// keptFields belong to the process and stay.
var (
	renewedFields = []string{"lastTurnID", "hopsThisRun", "askedThisRun", "resumeClaim", "mainWorkTurn", "mainWorkSequence",
		"subagentIDs", "subagentStatus", "subagentIDTurn", "subagentRunning", "slotChoice", "limitDecided", "limitModel",
		"lastPromptTokens", "preCompact", "postCompact", "runSpend", "sessionSpend", "planLimits", "lastArchive",
		"saveState", "execution", "partialMain", "partialTask", "pausedChildren", "pausedTasks", "attemptBefore"}
	keptFields = []string{"Options", "archiveMu", "turnDepth", "resumeClaims", "loadExtraOnce", "resumeMu", "resume", "resumeBase", "resumeStop",
		"resumeClosed", "resumeDelivering", "resumeDeliveries", "mainWorkMu", "subagentMu", "slotMu", "limitMu", "modelMu",
		"planLimitsMu", "statsWarnOnce", "rulesMu", "executionMu", "sessMu", "resumeParent", "resumeRunning"}
)

func TestEveryAgentFieldIsClassifiedForANewSession(t *testing.T) {
	renewed, kept := renewedFields, keptFields
	classified := map[string]bool{}
	for _, name := range append(append([]string{}, renewed...), kept...) {
		if classified[name] {
			t.Errorf("%s is listed twice", name)
		}
		classified[name] = true
	}
	var fields []string
	agent := reflect.TypeOf(Agent{})
	for i := 0; i < agent.NumField(); i++ {
		fields = append(fields, agent.Field(i).Name)
		if !classified[agent.Field(i).Name] {
			t.Errorf("Agent.%s is neither renewed nor kept by ReplaceSession: decide, and say so in both places", agent.Field(i).Name)
		}
	}
	sort.Strings(fields)
	for name := range classified {
		if i := sort.SearchStrings(fields, name); i == len(fields) || fields[i] != name {
			t.Errorf("%s is classified but is no Agent field", name)
		}
	}
}

// countingTools is an extra tool server pool that counts its starts.
type countingTools struct{ loads atomic.Int32 }

func (c *countingTools) Load(context.Context) []mcp.Report { c.loads.Add(1); return nil }
func (c *countingTools) Definitions() []provider.Tool      { return nil }
func (c *countingTools) Execute(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

// A new session starts where a fresh agent would: nothing spent, nothing
// measured, no run or turn carried over, the system prompt in place. What
// belongs to the process stays: the extra tool servers are not started again.
func TestANewSessionStartsFreshAndKeepsTheProcess(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "one"}, enginetest.Step{Text: "two"})
	defer srv.Close()
	old := enginetest.NewFakeSession("s_old", "mock/model")
	pool := &countingTools{}
	a := New(Options{Client: provider.NewCompatibleClient(srv.URL), Model: "mock/model", Mode: ModeCode,
		Permission: PermissionFullAuto, Sess: old, Out: io.Discard, ExtraTools: pool})
	defer a.Close()
	if err := a.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	a.lastPromptTokens.Store(1234)
	a.sessionSpend.add(0.5)
	a.hopsThisRun, a.askedThisRun = 3, true
	a.limitDecided, a.limitModel = true, "m"

	fresh := enginetest.NewFakeSession("s_new", "mock/model")
	a.ReplaceSession(fresh, nil)

	if a.Sess != SessionPort(fresh) || a.Session() != SessionPort(fresh) {
		t.Fatal("the agent is not on the new session")
	}
	if msgs := fresh.GetMessages(); len(msgs) != 1 || msgs[0].Role != "system" {
		t.Fatalf("the new session holds %+v, want the system prompt alone", msgs)
	}
	if a.lastTurnID != "" || a.lastPromptTokens.Load() != 0 || a.SessionCostUSD() != 0 || a.hopsThisRun != 0 || a.askedThisRun ||
		a.limitDecided || a.limitModel != "" {
		t.Fatalf("session state carried over: turn %q tokens %d cost %v hops %d asked %v limit %v %q",
			a.lastTurnID, a.lastPromptTokens.Load(), a.SessionCostUSD(), a.hopsThisRun, a.askedThisRun, a.limitDecided, a.limitModel)
	}
	if len(old.GetMessages()) < 3 {
		t.Fatal("the old session lost its conversation")
	}
	if err := a.RunTurn(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if n := len(fresh.GetMessages()); n != 3 {
		t.Fatalf("the new session holds %d messages after one turn, want 3", n)
	}
	if n := pool.loads.Load(); n != 1 {
		t.Fatalf("the extra tool servers were started %d times across /new, want once", n)
	}
}

// A pause being watched belongs to the session it was met in. /new stops that
// watch, and any delivery in flight, before the agent moves on: the old
// session keeps its pause for a later /resume, the new one starts unpaused,
// and nothing probes on the old session's behalf afterwards. The status line
// reads the agent from its own goroutine throughout (run under -race).
func TestANewSessionStopsThePreviousPausesWatch(t *testing.T) {
	old := enginetest.NewFakeSession("s_old", "mock/model")
	pause := continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
		Connector: "claude", Model: "mock/model", Since: time.Now(), ResetAt: time.Now().Add(time.Hour), PendingTurn: "waiting"}
	old.SetPaused(&pause)
	waiting, withdrawn := make(chan struct{}, 1), make(chan struct{})
	var probes atomic.Int32
	a := New(Options{Model: "mock/model", Sess: old, Out: io.Discard,
		ResumeWait: func(ctx context.Context, _ time.Duration) error {
			select {
			case waiting <- struct{}{}:
			default:
			}
			<-ctx.Done()
			close(withdrawn)
			return ctx.Err()
		},
		ProbeLimit:  func(context.Context, continuity.Pause) (bool, error) { probes.Add(1); return true, nil },
		ResumeReady: func(context.Context, string) bool { return true },
	})
	defer a.Close()
	a.WatchPauses(context.Background())
	<-waiting

	// As after any turn: the meter reads the measured count, and touches no
	// session lock that would order its read of the session behind the swap.
	a.lastPromptTokens.Store(1)
	// Two readers, one per accessor: a lock taken by one would order the
	// other's unlocked read and hide it from the race detector.
	stop := make(chan struct{})
	var drawing sync.WaitGroup
	for _, read := range []func(){func() { _ = a.Context() }, func() { _ = a.Session() }} {
		drawing.Add(1)
		go func() {
			defer drawing.Done()
			for {
				select {
				case <-stop:
					return
				default:
					read()
				}
			}
		}()
	}
	fresh := enginetest.NewFakeSession("s_new", "mock/model")
	a.ReplaceSession(fresh, nil)
	time.Sleep(20 * time.Millisecond) // the status line keeps drawing across the swap
	close(stop)
	drawing.Wait()
	select {
	case <-withdrawn:
	default:
		t.Fatal("the old session's watch is still waiting after /new")
	}

	if p := old.Paused(); p == nil || p.PendingTurn != "waiting" {
		t.Fatalf("the old session's pause = %+v, want it kept", p)
	}
	if fresh.Paused() != nil {
		t.Fatal("the new session starts paused")
	}
	time.Sleep(50 * time.Millisecond)
	if probes.Load() != 0 {
		t.Fatalf("the old watch probed %d times after /new", probes.Load())
	}
}

// Every renewed field is back to its zero value after the swap, each one set
// first: a reset line dropped from ReplaceSession fails here by name.
func TestANewSessionRenewsEveryRenewedField(t *testing.T) {
	a := New(Options{Model: "mock/model", Sess: enginetest.NewFakeSession("s_old", "mock/model"), Out: io.Discard})
	defer a.Close()
	a.lastTurnID, a.hopsThisRun, a.askedThisRun, a.resumeClaim = "t", 1, true, "c"
	a.mainWorkTurn, a.mainWorkSequence = "t", 1
	a.subagentIDs, a.subagentStatus = map[int]string{1: "x"}, map[int]SubagentStatus{1: {}}
	a.subagentIDTurn, a.subagentRunning = "t", 1
	a.slotChoice = map[string]string{"slot": "model"}
	a.limitDecided, a.limitModel = true, "m"
	a.lastPromptTokens.Store(5)
	a.preCompact, a.postCompact = []provider.Message{{Role: "user"}}, []provider.Message{{Role: "user"}}
	a.runSpend = &spend{}
	a.sessionSpend.add(1)
	a.sessionSpend.noteBilling(provider.BillingSubscription)
	a.planLimits = map[string]provider.PlanLimit{"seven_day": {}}
	a.lastArchive = "archive"
	a.saveState.pending, a.saveState.warned, a.saveState.last, a.saveState.recoveryLost = true, true, time.Now(), true
	a.execution = &continuity.Run{ID: "r"}
	a.partialMain, a.partialTask = &strings.Builder{}, map[int]*strings.Builder{0: {}}
	a.attemptBefore = map[int]attemptFacts{-1: {}}
	a.pausedChildren, a.pausedTasks = []pausedChild{{index: 0}}, []int{1}

	a.ReplaceSession(enginetest.NewFakeSession("s_new", "mock/model"), nil)

	agent := reflect.ValueOf(a).Elem()
	for _, name := range renewedFields {
		if !agent.FieldByName(name).IsZero() {
			t.Errorf("Agent.%s survived ReplaceSession", name)
		}
	}
}

// A delivery in flight is cancelled and joined before the swap. Its pause goes
// back on the session it came from; the new session never receives it.
func TestANewSessionJoinsADeliveryInFlight(t *testing.T) {
	old := enginetest.NewFakeSession("s_old", "mock/model")
	old.SetPaused(&continuity.Pause{Kind: string(provider.LimitEndpointCapacity), Scope: string(provider.ScopeEndpoint),
		Model: "mock/model", Since: time.Now(), ResetAt: time.Now(), PendingTurn: "waiting"})
	delivering := make(chan struct{})
	a := New(Options{Model: "mock/model", Sess: old, Out: io.Discard,
		ResumeWait: func(context.Context, time.Duration) error { return nil },
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		// A busy surface holding the turn until the delivery is withdrawn.
		ResumeReady: func(ctx context.Context, _ string) bool {
			close(delivering)
			<-ctx.Done()
			return false
		},
	})
	defer a.Close()
	a.WatchPauses(context.Background())
	<-delivering
	fresh := enginetest.NewFakeSession("s_new", "mock/model")
	swapped := make(chan struct{})
	go func() { a.ReplaceSession(fresh, nil); close(swapped) }()
	select {
	case <-swapped:
	case <-time.After(5 * time.Second):
		t.Fatal("ReplaceSession never returned: the delivery was not withdrawn")
	}
	time.Sleep(50 * time.Millisecond)
	if p := old.Paused(); p == nil || p.PendingTurn != "waiting" {
		t.Fatalf("the old session's pause = %+v, want it back for a later /resume", p)
	}
	if p := fresh.Paused(); p != nil {
		t.Fatalf("the new session received the old session's pause: %+v", p)
	}
}

// A watch runs past its delivery: after the surface accepts the turn, the
// monitor still reads the session to decide whether to watch again. /new
// joins that whole lifetime, not only the delivery, or the swap races the
// read, however long ago it seemed to finish (run under -race).
func TestANewSessionJoinsAWatchsWholeLifetime(t *testing.T) {
	old := enginetest.NewFakeSession("s_old", "mock/model")
	old.SetPaused(&continuity.Pause{Kind: string(provider.LimitEndpointCapacity), Scope: string(provider.ScopeEndpoint),
		Model: "mock/model", Since: time.Now(), ResetAt: time.Now(), PendingTurn: "waiting"})
	accepted := make(chan string, 1)
	a := New(Options{Model: "mock/model", Mode: ModeCode, Sess: old, Out: io.Discard,
		ResumeWait:  func(context.Context, time.Duration) error { return nil },
		ProbeLimit:  func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(_ context.Context, pending string) bool { accepted <- pending; return true },
	})
	defer a.Close()
	a.WatchPauses(context.Background())
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the lifted pause was never delivered")
	}
	time.Sleep(50 * time.Millisecond) // long enough to look finished; no ordering
	fresh := enginetest.NewFakeSession("s_new", "mock/model")
	a.ReplaceSession(fresh, nil)
	// The watch machinery survives the swap: a pause in the new session is
	// watched like the first one was.
	fresh.SetPaused(&continuity.Pause{Kind: string(provider.LimitEndpointCapacity), Scope: string(provider.ScopeEndpoint),
		Model: "mock/model", Since: time.Now(), ResetAt: time.Now(), PendingTurn: "next"})
	if !a.armResume() {
		t.Fatal("a pause in the new session is not watched")
	}
	select {
	case pending := <-accepted:
		if pending != "next" {
			t.Fatalf("delivered %q, want the new session's turn", pending)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the new session's pause was never delivered")
	}
}
