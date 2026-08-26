package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/serve"
)

func TestPairingOnLoopbackSaysTheDeviceCannotReachIt(t *testing.T) {
	advice := pairingAdvice(serve.Reachability{Kind: serve.ReachLoopback})

	// Telling someone to pair a phone against a port only this machine can
	// reach is an instruction that cannot be followed.
	if advice == "" {
		t.Fatal("no advice for pairing against a loopback bind")
	}
	lowered := strings.ToLower(advice)
	if !strings.Contains(lowered, "tunnel") && !strings.Contains(lowered, "reach") {
		t.Fatalf("advice = %q, want it to say how the device gets there", advice)
	}
}

func TestPairingOnAReachableBindNeedsNoAdvice(t *testing.T) {
	for _, kind := range []serve.ReachKind{serve.ReachTailscale, serve.ReachNetwork} {
		if advice := pairingAdvice(serve.Reachability{Kind: kind}); advice != "" {
			t.Fatalf("kind %v got unnecessary advice: %q", kind, advice)
		}
	}
}
