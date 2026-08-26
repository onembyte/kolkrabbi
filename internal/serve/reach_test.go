package serve

import (
	"strings"
	"testing"
)

func ifaces(pairs ...string) []Interface {
	out := make([]Interface, 0, len(pairs))
	for _, pair := range pairs {
		name, addr, _ := strings.Cut(pair, "=")
		out = append(out, Interface{Name: name, Addrs: []string{addr}})
	}
	return out
}

func TestLoopbackSaysNothingElseCanReachIt(t *testing.T) {
	got := Describe("127.0.0.1:4096", ifaces("lo=127.0.0.1", "eth0=192.168.1.5"))

	if got.Kind != ReachLoopback {
		t.Fatalf("kind = %v", got.Kind)
	}
	if len(got.URLs) != 1 || got.URLs[0] != "http://127.0.0.1:4096" {
		t.Fatalf("urls = %v", got.URLs)
	}
	// Someone who binds loopback should be told that is what they got, not
	// left wondering why their phone cannot connect.
	if !strings.Contains(strings.ToLower(got.Note), "this machine") {
		t.Fatalf("note = %q", got.Note)
	}
}

func TestATailscaleAddressComesFirst(t *testing.T) {
	got := Describe("0.0.0.0:4096", ifaces(
		"lo=127.0.0.1",
		"eth0=192.168.1.5",
		"tailscale0=100.101.102.103",
	))

	if got.Kind != ReachTailscale {
		t.Fatalf("kind = %v", got.Kind)
	}
	// It is the one address that works from anywhere, so it is the one to try.
	if len(got.URLs) == 0 || got.URLs[0] != "http://100.101.102.103:4096" {
		t.Fatalf("urls = %v, want the tailscale address first", got.URLs)
	}
	if !strings.Contains(got.URLs[1], "192.168.1.5") {
		t.Fatalf("urls = %v, want the LAN address kept", got.URLs)
	}
}

func TestTailscaleIsRecognisedByItsAddressRange(t *testing.T) {
	// The interface is not always called tailscale0 — utun on macOS, ts0 on
	// some setups. 100.64/10 is the range it is assigned from.
	got := Describe("0.0.0.0:4096", ifaces("utun3=100.90.80.70"))

	if got.Kind != ReachTailscale {
		t.Fatalf("kind = %v", got.Kind)
	}
	if got.URLs[0] != "http://100.90.80.70:4096" {
		t.Fatalf("urls = %v", got.URLs)
	}
}

func TestTailscaleIsAlsoRecognisedByInterfaceName(t *testing.T) {
	got := Describe("0.0.0.0:4096", ifaces("tailscale0=10.20.30.40"))
	if got.Kind != ReachTailscale {
		t.Fatalf("kind = %v", got.Kind)
	}
}

func TestAPlainLanBindWarnsPlainly(t *testing.T) {
	got := Describe("0.0.0.0:4096", ifaces("lo=127.0.0.1", "eth0=192.168.1.5"))

	if got.Kind != ReachNetwork {
		t.Fatalf("kind = %v", got.Kind)
	}
	if got.URLs[0] != "http://192.168.1.5:4096" {
		t.Fatalf("urls = %v", got.URLs)
	}
	// The failure mode here is someone not realising the office wifi can see
	// it, so the note has to say so in words, not in a URL.
	lowered := strings.ToLower(got.Note)
	if !strings.Contains(lowered, "network") || !strings.Contains(lowered, "reach") {
		t.Fatalf("note = %q, want it to say who can reach this", got.Note)
	}
}

func TestBindingOneAddressAdvertisesOnlyThatAddress(t *testing.T) {
	got := Describe("192.168.1.5:4096", ifaces("lo=127.0.0.1", "eth0=192.168.1.5", "tailscale0=100.1.2.3"))

	// Listing an address the socket is not bound to sends someone to a port
	// that will not answer.
	if len(got.URLs) != 1 || got.URLs[0] != "http://192.168.1.5:4096" {
		t.Fatalf("urls = %v, want only the bound address", got.URLs)
	}
}

func TestNoisyAddressesAreLeftOut(t *testing.T) {
	got := Describe("0.0.0.0:4096", ifaces(
		"eth0=192.168.1.5",
		"eth0=169.254.10.10",
		"eth0=fe80::1",
		"lo=127.0.0.1",
	))

	// Link-local addresses are not somewhere anyone types into a phone.
	for _, url := range got.URLs {
		if strings.Contains(url, "169.254") || strings.Contains(url, "fe80") {
			t.Fatalf("urls = %v, want link-local left out", got.URLs)
		}
	}
	if len(got.URLs) != 1 {
		t.Fatalf("urls = %v", got.URLs)
	}
}

func TestAnIPv6AddressIsBracketed(t *testing.T) {
	got := Describe("[::]:4096", ifaces("eth0=2001:db8::1"))

	if len(got.URLs) != 1 || got.URLs[0] != "http://[2001:db8::1]:4096" {
		t.Fatalf("urls = %v, want a bracketed host", got.URLs)
	}
}

func TestIPv6LoopbackIsStillLoopback(t *testing.T) {
	got := Describe("[::1]:4096", ifaces("lo=::1"))
	if got.Kind != ReachLoopback {
		t.Fatalf("kind = %v", got.Kind)
	}
	if got.URLs[0] != "http://[::1]:4096" {
		t.Fatalf("urls = %v", got.URLs)
	}
}

func TestAWildcardWithNothingToShowStillSaysSomething(t *testing.T) {
	got := Describe("0.0.0.0:4096", nil)

	// A machine with no usable address still bound the port. Printing nothing
	// reads as a crash.
	if got.Note == "" {
		t.Fatal("no note for a bind with no addresses")
	}
}

func TestTheSSHTunnelIsOfferedForLoopback(t *testing.T) {
	got := Describe("127.0.0.1:4096", ifaces("lo=127.0.0.1"))

	// The honest remote answer for a loopback bind, and it needs nothing from
	// Kolkrabbi at all.
	if !strings.Contains(got.Tunnel, "ssh -L") || !strings.Contains(got.Tunnel, "4096") {
		t.Fatalf("tunnel = %q", got.Tunnel)
	}
}
