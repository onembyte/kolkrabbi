package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// A cooldown keeps the pause's rule: a vendor's reset counts only while it is
// still ahead. A stale one would mark a cooldown that has already expired, and
// the next turn would walk straight back into the limit.
func TestACooldownTrustsOnlyAResetStillAhead(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	c := cooldownsAt(t, filepath.Join(dir, "s.cooldowns.json"), filepath.Join(dir, "cooldowns.json"), &now)
	stale := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude", ResetAt: now.Add(-time.Minute), Source: "vendor-frame"}
	cd, ok := c.Mark(stale)
	if !ok || !cd.Until.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("a stale reset = %+v %v, want the kind's default cooldown", cd, ok)
	}
	atNow := stale
	atNow.ResetAt = now
	if cd, ok := c.Mark(atNow); !ok || !cd.Until.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("a reset at this very moment = %+v %v, want the kind's default cooldown", cd, ok)
	}
	ahead := stale
	ahead.ResetAt = now.Add(72 * time.Hour)
	if cd, ok := c.Mark(ahead); !ok || !cd.Until.Equal(ahead.ResetAt) {
		t.Fatalf("a reset still ahead = %+v %v, want the vendor's time", cd, ok)
	}
}

// The resume monitor's wait follows the wall clock. A single timer for a
// three-day wait would not count time the machine spent asleep, and would
// run the whole wait again after waking; a bounded slice re-reads the clock.
func TestTheResumeWaitFollowsTheWallClock(t *testing.T) {
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	t.Run("asleep past the reset", func(t *testing.T) {
		clock := start
		var slices []time.Duration
		sleep := func(_ context.Context, d time.Duration) error {
			slices = append(slices, d)
			clock = clock.Add(d)
			if len(slices) == 1 {
				clock = clock.Add(3 * time.Hour) // the lid was closed
			}
			return nil
		}
		if err := waitUntilWall(context.Background(), start.Add(3*time.Hour), func() time.Time { return clock }, sleep); err != nil {
			t.Fatal(err)
		}
		if len(slices) != 1 {
			t.Fatalf("slept %v after the wall clock had passed the reset", slices)
		}
		if slices[0] > resumeWaitSlice {
			t.Fatalf("one sleep of %v; each must be at most %v", slices[0], resumeWaitSlice)
		}
	})
	t.Run("awake", func(t *testing.T) {
		clock := start
		var total time.Duration
		sleep := func(_ context.Context, d time.Duration) error {
			if d > resumeWaitSlice || d <= 0 {
				t.Fatalf("a sleep of %v", d)
			}
			total += d
			clock = clock.Add(d)
			return nil
		}
		deadline := start.Add(3*time.Hour + 30*time.Second)
		if err := waitUntilWall(context.Background(), deadline, func() time.Time { return clock }, sleep); err != nil {
			t.Fatal(err)
		}
		if total != deadline.Sub(start) {
			t.Fatalf("slept %v in all, want exactly %v: no early wake, no overshoot", total, deadline.Sub(start))
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		sleep := func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
		if err := waitUntilWall(ctx, start.Add(time.Hour), func() time.Time { return start }, sleep); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the cancellation", err)
		}
	})
	t.Run("a real wait", func(t *testing.T) {
		begun := time.Now()
		if err := waitWallClock(context.Background(), 60*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(begun); waited < 60*time.Millisecond || waited > 5*time.Second {
			t.Fatalf("waited %v for a 60ms wait", waited)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := waitWallClock(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled hour-long wait = %v, want the cancellation", err)
		}
	})
	t.Run("the default", func(t *testing.T) {
		a := New(Options{Sess: enginetest.NewFakeSession("s_test", "m"), Out: io.Discard})
		defer a.Close()
		if reflect.ValueOf(a.ResumeWait).Pointer() != reflect.ValueOf(waitWallClock).Pointer() {
			t.Fatal("the resume monitor's default wait is not the wall-clock wait")
		}
	})
}

// vendorLimitError is the shape a vendor adapter returns: the vendor's own
// sentence, classified underneath as a Limit carrying its reset.
type vendorLimitError struct{ provider.Limit }

func (e vendorLimitError) Error() string { return e.Message }
func (e vendorLimitError) Unwrap() error { return e.Limit }

type vendorLimitBackend struct{ err error }

func (b vendorLimitBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, b.err
}

// End to end: the vendor's reset becomes the pause's, the pause says "reset
// at", and the monitor's first wait runs to that reset, not to kolk's guess.
func TestAVendorResetIsWhenTheSessionComesBack(t *testing.T) {
	resets := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	limit := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, ResetAt: resets,
		Message: "claude plan limit reached: the seven-day window is fully used", Source: "vendor-frame"}
	waits := make(chan time.Duration, 4)
	a := New(Options{
		Backend: vendorLimitBackend{err: vendorLimitError{limit}}, Mode: ModeCode, Model: "claude/opus",
		OnSubscriptionLimit: OnLimitStop, Permission: PermissionFullAuto, Out: io.Discard,
		Sess: enginetest.NewFakeSession("s_test", "claude/opus"),
		ResumeWait: func(ctx context.Context, d time.Duration) error {
			waits <- d
			<-ctx.Done()
			return ctx.Err()
		},
		ProbeLimit:  func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(context.Context, string) bool { return true },
	})
	defer a.Close()
	a.WatchPauses(context.Background())

	var paused *PausedError
	if err := a.RunTurn(context.Background(), "keep going"); !errors.As(err, &paused) {
		t.Fatalf("the plan limit did not pause: %v", err)
	}
	if !paused.Pause.ResetAt.Equal(resets) || paused.Pause.Estimated {
		t.Fatalf("pause = %+v, want the vendor's reset %v", paused.Pause, resets)
	}
	if saved := a.Sess.Paused(); saved == nil || !saved.ResetAt.Equal(resets) || saved.PendingTurn != "keep going" {
		t.Fatalf("saved pause = %+v, want the vendor's reset and the waiting turn", saved)
	}
	select {
	case d := <-waits:
		if want := time.Until(resets); d < want-time.Minute || d > want+time.Minute {
			t.Fatalf("the monitor waits %v, want about %v: until the vendor's reset", d, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the monitor never started waiting")
	}
}

// The cooldown line sits in the same status bar as the pause, so it names the
// day the same way; "resumes 01:03" above "reset at Thu 01:03" contradicts.
func TestACooldownDaysAwayNamesItsDay(t *testing.T) {
	until := time.Now().Add(72 * time.Hour)
	cd := Cooldown{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude", Until: until}
	want := "resumes " + continuity.ResetClock(until, time.Now(), time.Local)
	if got := cd.Describe(); !strings.HasSuffix(got, want) || len(strings.Fields(strings.TrimPrefix(want, "resumes "))) != 2 {
		t.Fatalf("Describe() = %q, want it to end %q, with the day", got, want)
	}
}

// The provider.limit event states the vendor's reset only while it is still
// ahead. A stale one is not when anything happens: the pause estimated its
// own time, and turn.finished carries that.
func TestAPauseEventNeverStatesAResetAlreadyPast(t *testing.T) {
	limit := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(-time.Hour), Message: "claude plan limit reached", Source: "vendor-frame"}
	a := New(Options{
		Backend: vendorLimitBackend{err: vendorLimitError{limit}}, Bus: newTestBus(t), Mode: ModeCode, Model: "claude/opus",
		OnSubscriptionLimit: OnLimitStop, Permission: PermissionFullAuto, Out: io.Discard,
		Sess: enginetest.NewFakeSession("s_test", "claude/opus"),
	})
	defer a.Close()
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "keep going"); !errors.As(err, &paused) {
		t.Fatalf("did not pause: %v", err)
	}
	if !paused.Pause.Estimated {
		t.Fatalf("pause = %+v, want kolk's estimate", paused.Pause)
	}
	seen := 0
	for _, env := range bReplay(t, a.Bus) {
		if env.Type != protocol.EventProviderLimit {
			continue
		}
		var data protocol.ProviderLimitData
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatal(err)
		}
		seen++
		if data.ResetAt != "" {
			t.Errorf("provider.limit{%s} states reset_at %s, an hour gone", data.Action, data.ResetAt)
		}
	}
	if seen == 0 {
		t.Fatal("no provider.limit event was published")
	}
}

// The resume probe asks the check that fits where the model runs: a vendor
// CLI's sign-in when the model runs through that connector, and otherwise the
// endpoint itself. connectorFor's gateway fallback is no connector anybody
// signed into; asking it would answer no for ever.
func TestTheResumeProbeAsksWhereTheModelRuns(t *testing.T) {
	var lists int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lists++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/model"}]}`)
	}))
	defer srv.Close()
	var asked []string
	a := &Agent{Options: Options{
		Client:           provider.NewCompatibleClient(srv.URL),
		ConnectorName:    func(model string) string { return map[string]string{"claude-opus": "claude"}[model] },
		HandoverSignedIn: func(name string) bool { asked = append(asked, name); return true },
	}}
	cases := []struct {
		name  string
		pause continuity.Pause
		signs []string
		lists int
	}{
		{"a handover", continuity.Pause{Connector: "claude", Model: "claude-opus"}, []string{"claude"}, 0},
		{"a gateway", continuity.Pause{Connector: "openrouter", Model: "vendor/model"}, nil, 1},
		{"a handover saved before its connector was known", continuity.Pause{Connector: "openrouter", Model: "claude-opus"}, nil, 1},
		{"a gateway pause saved with no connector", continuity.Pause{Model: "vendor/model"}, nil, 1},
	}
	for _, c := range cases {
		asked, lists = nil, 0
		lifted, err := a.probeLifted(context.Background(), c.pause)
		if err != nil || !lifted {
			t.Errorf("%s: lifted %v, %v", c.name, lifted, err)
		}
		if !reflect.DeepEqual(asked, c.signs) || lists != c.lists {
			t.Errorf("%s: asked sign-ins %v and the endpoint %d times; want %v and %d", c.name, asked, lists, c.signs, c.lists)
		}
	}
}

// Moving to the metered model moves the session to the gateway: the session
// records that pair, not the metered model beside the plan's connector, which
// would name the plan's CLI for a model it does not run.
func TestMovingToMeteredRecordsTheGateway(t *testing.T) {
	sess := enginetest.NewFakeSession("s_test", "claude-opus")
	sess.SetConnector("claude")
	a := New(Options{Sess: sess, Client: provider.NewCompatibleClient("http://127.0.0.1:1"), Model: "claude-opus", Out: io.Discard})
	defer a.Close()
	a.moveToMetered("openai/gpt-5.6-luna")
	if model, connector := sess.Route(); model != "openai/gpt-5.6-luna" || connector != "" {
		t.Fatalf("route = %q via %q, want the metered model via the gateway", model, connector)
	}
}
