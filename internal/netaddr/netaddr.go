// Package netaddr answers one question: does this bind address reach only this
// machine?
//
// It exists because the question was being answered twice — once in
// internal/serve and once for `kolk dash` — and the two copies did not agree.
// The serve copy treated an empty host as loopback, so `--addr :8080` bound
// every interface and was served without a token until I26.1; the dash copy
// got the same case right. A security predicate implemented twice is a
// predicate that will be wrong in one place, and the wrong place is the one
// nobody is looking at.
package netaddr

import (
	"net"
	"strings"
)

// IsLoopback reports whether a listen address reaches only this machine.
//
// Everything it cannot prove is loopback is false. An empty host means every
// interface, and an address nobody can parse is not something to guess about —
// both are the unsafe direction of the ambiguity, so both are refused.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port at all. Judge the bare host; anything stranger is not
		// loopback.
		host = addr
	}
	host = strings.TrimSpace(host)
	switch host {
	case "":
		return false
	case "localhost":
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
