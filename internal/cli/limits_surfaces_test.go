package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
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
