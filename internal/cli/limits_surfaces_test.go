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

// /doctor names the limits kolk remembers for this user, with when each lifts;
// with nothing cooling it says so, so an empty section is never a question.
func TestDoctorListsRememberedLimits(t *testing.T) {
	d := isolateHome(t)
	a, stdout, _ := newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); !strings.Contains(out, "\nlimits\n") || !strings.Contains(out, "nothing is cooling") {
		t.Fatalf("doctor with nothing cooling:\n%s", out)
	}

	engine.OpenCooldowns("", d.CooldownsFile()).Mark(provider.Limit{
		Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(45 * time.Minute), Source: "vendor-frame",
	})
	a, stdout, _ = newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	section := stdout.String()[strings.Index(stdout.String(), "\nlimits\n"):]
	for _, want := range []string{"claude", "subscription allowance", "resumes", "vendor-frame"} {
		if !strings.Contains(section, want) {
			t.Fatalf("limits section omits %q:\n%s", want, section)
		}
	}
}

// The status line says when the session's own model or connector is cooling,
// and says nothing when it is not.
func TestStatusLineSaysWhenTheSessionIsCooling(t *testing.T) {
	isolateConnectorState(t)
	_, ag, _ := replFixture(t, "")
	dir := t.TempDir()
	ag.Cooldowns = engine.OpenCooldowns(dir+"/s.cooldowns.json", dir+"/shared.json")
	if got := tuiStatus(ag, "ready", "~").Cooling; got != "" {
		t.Fatalf("Cooling = %q with nothing cooling, want empty", got)
	}
	ag.Cooldowns.Mark(provider.Limit{Kind: provider.LimitEndpointCapacity, Scope: provider.ScopeModel, Connector: "openrouter", Model: ag.SessionModel(), RetryAfter: 10 * time.Minute, Source: "status"})
	got := tuiStatus(ag, "ready", "~").Cooling
	if !strings.HasPrefix(got, "cooling · ") || !strings.Contains(got, "resumes") || !strings.Contains(got, ag.SessionModel()) {
		t.Fatalf("Cooling = %q, want the model, the kind and when it resumes", got)
	}
}

// The status line says when the session itself is paused on a limit, what
// paused it and when it resumes; nothing paused, nothing said.
func TestStatusLineSaysWhenTheSessionIsPaused(t *testing.T) {
	isolateConnectorState(t)
	_, ag, _ := replFixture(t, "")
	if got := tuiStatus(ag, "ready", "~").Paused; got != "" {
		t.Fatalf("Paused = %q with nothing paused, want empty", got)
	}
	ag.Sess.SetPaused(&continuity.Pause{
		Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount), Connector: "claude",
		Model: ag.SessionModel(), Since: time.Now(), ResetAt: time.Now().Add(40 * time.Minute), PendingTurn: "go on",
	})
	got := tuiStatus(ag, "ready", "~").Paused
	if !strings.HasPrefix(got, "paused · ") || !strings.Contains(got, "subscription allowance") || !strings.Contains(got, "resumes") {
		t.Fatalf("Paused = %q, want the reason and when it resumes", got)
	}
}

// /doctor names every session paused on a limit, with the reason, when it
// resumes and the way to resume it now.
func TestDoctorNamesPausedSessions(t *testing.T) {
	d := isolateHome(t)
	sess := session.New(d.Sessions(), "vendor/pinned")
	sess.SetPaused(&continuity.Pause{
		Kind: string(provider.LimitAccountQuota), Scope: string(provider.ScopeAccount), Connector: "openrouter",
		Model: "vendor/pinned", Since: time.Now(), ResetAt: time.Now().Add(2 * time.Hour), PendingTurn: "finish the report",
	})
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	a, stdout, _ := newTestApp(t, "")
	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	section := stdout.String()[strings.Index(stdout.String(), "\nlimits\n"):]
	for _, want := range []string{"paused", sess.SessionID(), "account quota", "resumes", "/resume"} {
		if !strings.Contains(section, want) {
			t.Fatalf("limits section omits %q:\n%s", want, section)
		}
	}
}

// /continue with nothing paused says so; with a pause and no equivalent it
// keeps the pause and says why; a bad number is a usage line. The walk
// itself is the engine's, tested there.
func TestSlashContinueSaysWhatItCannotDo(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/continue") {
		t.Fatal("/continue must not exit the REPL")
	}
	if !strings.Contains(out.String(), "nothing") {
		t.Fatalf("with nothing paused: %q", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/continue zero")
	if !strings.Contains(out.String(), "usage: /continue") {
		t.Fatalf("bad number: %q", out.String())
	}
	out.Reset()
	ag.Switch = func(context.Context, continuity.Candidate) (string, error) { return "", errors.New("unused") }
	ag.Sess.SetPaused(&continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
		Connector: "claude", Model: ag.SessionModel(), Since: time.Now(), ResetAt: time.Now().Add(time.Hour), PendingTurn: "go on"})
	a.slash(context.Background(), ag, "/continue")
	if !strings.Contains(out.String(), "nothing configured can continue") || ag.Sess.Paused() == nil {
		t.Fatalf("with a pause and no equivalent: %q paused=%v", out.String(), ag.Sess.Paused() != nil)
	}
}
