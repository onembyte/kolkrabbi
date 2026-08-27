package netaddr

import "testing"

func TestLoopbackAddressesAreRecognised(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:8080", "127.0.0.1:0", "localhost:8080", "[::1]:8080", "127.0.0.53:53",
	} {
		if !IsLoopback(addr) {
			t.Fatalf("%q was not recognised as loopback", addr)
		}
	}
}

func TestEverythingElseIsNot(t *testing.T) {
	// The empty host is the one that matters: ":8080" has no host, and no host
	// means every interface. Reading it as loopback is how an unauthenticated
	// server ends up on the office wifi, which is exactly what happened in
	// internal/serve before I26.1.
	for _, addr := range []string{
		":8080", ":0", "", "0.0.0.0:8080", "[::]:8080", "192.168.1.5:8080",
		"example.com:8080", "not an address", "8080",
	} {
		if IsLoopback(addr) {
			t.Fatalf("%q was treated as loopback", addr)
		}
	}
}

func TestAHostnameThatMerelyStartsWithLocalhostIsNot(t *testing.T) {
	// "localhost.evil.com" resolves wherever its owner points it.
	for _, addr := range []string{"localhost.evil.com:8080", "notlocalhost:8080"} {
		if IsLoopback(addr) {
			t.Fatalf("%q was treated as loopback", addr)
		}
	}
}
