package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSlashDashDoesNotBlockTheSession(t *testing.T) {
	isolateConnectorState(t)
	a, ag, _ := replFixture(t, "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.slash(context.Background(), ag, "/dash --addr 127.0.0.1:0")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("/dash never returned; the session is frozen until the user interrupts it")
	}
}

func TestSecondSlashDashPointsAtTheFirst(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/dash --addr 127.0.0.1:0")
	first := a.dashURL
	if first == "" {
		t.Fatal("the first /dash did not record a url")
	}
	a.slash(context.Background(), ag, "/dash --addr 127.0.0.1:0")

	if a.dashURL != first {
		t.Fatalf("a second /dash started another server: %q then %q", first, a.dashURL)
	}
	if !strings.Contains(out.String(), "already running") {
		t.Fatalf("output = %q, want the running url pointed at", out.String())
	}
}

func TestSlashDashStillRefusesANonLoopbackAddress(t *testing.T) {
	isolateConnectorState(t)
	a, ag, _ := replFixture(t, "")
	var errOut strings.Builder
	a.stderr = &errOut

	a.slash(context.Background(), ag, "/dash --addr 0.0.0.0:8080")

	// The rule has to hold in both entry points, which is why they share one
	// listener helper.
	if !strings.Contains(errOut.String(), "loopback") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if a.dashURL != "" {
		t.Fatal("a refused address still started a server")
	}
}
