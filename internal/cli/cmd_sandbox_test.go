package cli

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestSandboxIsOffByDefaultAndSaysSo(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/sandbox") {
		t.Fatal("/sandbox must not exit the session")
	}
	if ag.Sandbox != nil {
		t.Fatal("sandbox must be off by default")
	}
	got := out.String()
	if !strings.Contains(got, "sandbox: off") || !strings.Contains(got, "/sandbox on") {
		t.Fatalf("output = %q, want the state and how to change it", got)
	}
}

// Turning it on where nothing can enforce it is refused at the command, with
// the reason, and toggles nothing. Fail closed, but at the moment of the ask.
func TestSandboxOnIsRefusedWhenNoMechanismExists(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("seatbelt exists here; the refusal is exercised where no mechanism does, and the success below")
	}
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/sandbox on")
	if ag.Sandbox != nil {
		t.Fatal("/sandbox on must toggle nothing when it cannot be established")
	}
	got := out.String()
	if !strings.Contains(got, "cannot enable the sandbox") || !strings.Contains(got, "no sandbox mechanism") {
		t.Fatalf("output = %q, want the refusal and its reason", got)
	}
}

func TestSandboxOffIsExplicitAndIdempotent(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/sandbox off")
	if ag.Sandbox != nil {
		t.Fatal("sandbox should be off")
	}
	if !strings.Contains(out.String(), "sandbox → off") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSandboxRejectsAnythingButOnOrOff(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/sandbox auto")
	got := out.String()
	if !strings.Contains(got, "on") || !strings.Contains(got, "off") || strings.Contains(got, "→") {
		t.Fatalf("output = %q, want usage naming on and off, and no toggle", got)
	}
}

// Default-off weakens plan 13's "--yolo inside a sandbox" pairing. The
// mitigation is a single nudge when full-auto is chosen -- once, never twice,
// and never a silent switch.
func TestFullAutoSuggestsTheSandboxExactlyOnce(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/full-auto")
	a.slash(context.Background(), ag, "/full-auto")
	if n := strings.Count(out.String(), "/sandbox on"); n != 1 {
		t.Fatalf("nudge appeared %d times, want exactly once:\n%s", n, out.String())
	}
	if ag.Sandbox != nil {
		t.Fatal("a nudge must never turn the sandbox on by itself")
	}
}

func TestConfigSetSandboxAcceptsOnlyOnOrOff(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/config set sandbox auto")
	if !strings.Contains(out.String(), "use on or off") {
		t.Fatalf("output = %q, want a refusal naming on and off", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/config set sandbox on")
	if !strings.Contains(out.String(), "sandbox → on") {
		t.Fatalf("output = %q", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/config get sandbox")
	if !strings.Contains(out.String(), "on") {
		t.Fatalf("output = %q", out.String())
	}
}

// Where Seatbelt exists, /sandbox on takes effect and names the mechanism.
func TestSandboxOnSucceedsWhereSeatbeltExists(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt is macOS")
	}
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/sandbox on")
	if ag.Sandbox == nil {
		t.Fatalf("/sandbox on did not take effect:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "sandbox → on (seatbelt)") {
		t.Fatalf("output = %q, want the mechanism named", out.String())
	}
	if ag.Sandbox.Root != ag.Root {
		t.Fatalf("policy root %q is not the jail root %q", ag.Sandbox.Root, ag.Root)
	}
}
