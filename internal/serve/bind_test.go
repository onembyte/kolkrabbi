package serve

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

func testBus(t *testing.T) *bus.Bus {
	t.Helper()
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAWildcardBindIsNotLoopback(t *testing.T) {
	// ":8080" has no host, and no host means every interface. Reading that as
	// loopback is how an unauthenticated session ends up on the office wifi.
	for _, addr := range []string{":8080", ":0", "0.0.0.0:8080", "[::]:8080", "192.168.1.5:8080"} {
		if isLoopback(addr) {
			t.Fatalf("%q was treated as loopback", addr)
		}
	}
}

func TestRealLoopbackIsStillLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1:0"} {
		if !isLoopback(addr) {
			t.Fatalf("%q was not treated as loopback", addr)
		}
	}
}

func TestServingWideOpenWithoutATokenIsRefused(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080"} {
		_, err := Mux(Options{Bus: testBus(t), Addr: addr})
		if err == nil {
			t.Fatalf("%q was served with no token", addr)
		}
		// The refusal has to say what to do, or it reads as a bug in kolk.
		if !strings.Contains(err.Error(), "token") {
			t.Fatalf("%q refused with %q, want the token named", addr, err)
		}
	}
}

func TestAnUnparseableAddressIsRefusedRatherThanGuessed(t *testing.T) {
	// Guessing "probably loopback" about an address nobody can parse is the
	// same mistake as the empty host, one step later.
	_, err := Mux(Options{Bus: testBus(t), Addr: "not a real address"})
	if err == nil {
		t.Fatal("an unparseable address was served with no token")
	}
}

func TestLoopbackStillNeedsNoToken(t *testing.T) {
	// The common case must stay frictionless: a local session should not have
	// to invent a secret to talk to its own dashboard.
	if _, err := Mux(Options{Bus: testBus(t), Addr: "127.0.0.1:8080"}); err != nil {
		t.Fatalf("loopback required a token: %v", err)
	}
	if _, err := Mux(Options{Bus: testBus(t)}); err != nil {
		t.Fatalf("an unset address required a token: %v", err)
	}
}

func TestAWideOpenBindWithATokenIsAllowed(t *testing.T) {
	if _, err := Mux(Options{Bus: testBus(t), Addr: ":8080", Token: "s3cret"}); err != nil {
		t.Fatalf("a token was not enough: %v", err)
	}
}
