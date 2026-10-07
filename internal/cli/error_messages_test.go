package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Adopted from the V43.5 §7 item 5 review (E5-1): with nothing paused,
// /continue said "the pause stands and kolk resumes at the reset", a pause
// that did not exist. It says what /resume says: nothing is paused.
func TestContinueWithNothingPausedSaysSo(t *testing.T) {
	for _, line := range []string{"/continue", "/continue 2"} {
		a, out, _ := newTestApp(t, "")
		ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode, Sess: session.New(t.TempDir(), "mock/model")})
		a.slash(context.Background(), ag, line)
		got := out.String()
		if !strings.Contains(got, "nothing is paused") || strings.Contains(got, "the pause stands") {
			t.Errorf("%s with nothing paused says:\n%s", line, got)
		}
	}
}

// A surface with no session has nothing paused either.
func TestContinueWithoutASessionSaysNothingIsPaused(t *testing.T) {
	ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode})
	if _, _, err := ag.ContinueOn(context.Background(), 0); !errors.Is(err, engine.ErrNothingPaused) {
		t.Fatalf("ContinueOn without a session = %v, want ErrNothingPaused", err)
	}
}

// Adopted from the V43.5 item 5 verification (I5-2): with the manual resume
// policy nothing comes back at the reset on its own, so /continue with no
// equivalent must not promise that it will; it names /resume instead.
func TestContinueUnderManualResumeNamesResume(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	ag.ResumePolicy = engine.ResumeManual
	ag.Switch = func(context.Context, continuity.Candidate) (string, error) { return "", errors.New("unused") }
	ag.Sess.SetPaused(&continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
		Connector: "claude", Model: ag.SessionModel(), Since: time.Now(), ResetAt: time.Now().Add(time.Hour), PendingTurn: "go on"})
	a.slash(context.Background(), ag, "/continue")
	got := out.String()
	if strings.Contains(got, "kolk resumes at the reset") || !strings.Contains(got, "/resume") || ag.Sess.Paused() == nil {
		t.Fatalf("manual policy, no equivalent: %q paused=%v", got, ag.Sess.Paused() != nil)
	}
	// Callers that test for the condition still recognise it.
	if _, _, err := ag.ContinueOn(context.Background(), 0); !errors.Is(err, engine.ErrNothingToContinue) {
		t.Fatalf("ContinueOn = %v, want it to be ErrNothingToContinue", err)
	}
}

// Under the automatic policy the old words are the true ones.
func TestContinueUnderAutomaticResumeSaysItResumes(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	ag.Switch = func(context.Context, continuity.Candidate) (string, error) { return "", errors.New("unused") }
	ag.Sess.SetPaused(&continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
		Connector: "claude", Model: ag.SessionModel(), Since: time.Now(), ResetAt: time.Now().Add(time.Hour), PendingTurn: "go on"})
	a.slash(context.Background(), ag, "/continue 3")
	if !strings.Contains(out.String(), "kolk resumes at the reset") {
		t.Fatalf("automatic policy, no equivalent: %q", out.String())
	}
}
