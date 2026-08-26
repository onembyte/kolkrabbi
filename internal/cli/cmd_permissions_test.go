package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestPermissionsShowsAllThreeAndMarksTheCurrentOne(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	ag.Permission = engine.PermissionAsk

	if a.slash(context.Background(), ag, "/permissions") {
		t.Fatal("/permissions must not exit the session")
	}

	got := out.String()
	for _, tier := range []string{"ask", "auto-approve", "full-auto"} {
		if !strings.Contains(got, tier) {
			t.Fatalf("output = %q, want every tier listed", got)
		}
	}
	// The current one has to be identifiable at a glance, or the list is just
	// documentation.
	if !strings.Contains(got, "→ ask") && !strings.Contains(got, "* ask") {
		t.Fatalf("output = %q, want the active tier marked", got)
	}
	// Each tier must say what it actually does, including what it still refuses.
	if !strings.Contains(got, "refus") {
		t.Fatalf("output = %q, want the floor mentioned", got)
	}
}

func TestPermissionsSelectsATier(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/permissions full-auto") {
		t.Fatal("/permissions must not exit the session")
	}
	if ag.Permission != engine.PermissionFullAuto {
		t.Fatalf("permission = %q", ag.Permission)
	}
	if !strings.Contains(out.String(), "full-auto") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestEachTierHasItsOwnCommand(t *testing.T) {
	isolateConnectorState(t)
	for command, want := range map[string]engine.Permission{
		"/ask":          engine.PermissionAsk,
		"/auto-approve": engine.PermissionAutoApprove,
		"/full-auto":    engine.PermissionFullAuto,
	} {
		a, ag, out := replFixture(t, "")
		if a.slash(context.Background(), ag, command) {
			t.Fatalf("%s must not exit the session", command)
		}
		if ag.Permission != want {
			t.Fatalf("%s set %q, want %q", command, ag.Permission, want)
		}
		if !strings.Contains(out.String(), string(want)) {
			t.Fatalf("%s said %q", command, out.String())
		}
	}
}

func TestFullAutoSaysWhatItStillRefuses(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/full-auto")

	// Switching to the most permissive tier is the moment to say that it is
	// not unlimited.
	got := strings.ToLower(out.String())
	if !strings.Contains(got, "still") || !strings.Contains(got, "refus") {
		t.Fatalf("output = %q, want the floor stated when entering full-auto", out.String())
	}
}

func TestUnknownTierIsRejectedWithTheChoices(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	var errOut strings.Builder
	a.stderr = &errOut

	a.slash(context.Background(), ag, "/permissions yolo")

	if ag.Permission == "yolo" {
		t.Fatal("an unknown tier was accepted")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ask") || !strings.Contains(combined, "full-auto") {
		t.Fatalf("output = %q, want the valid choices listed", combined)
	}
}

func TestYoloIsGone(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/yolo")

	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("output = %q, want /yolo to no longer exist", out.String())
	}
}
