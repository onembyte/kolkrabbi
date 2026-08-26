package serve

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/devices"
)

// isLoopback reports whether an address reaches only this machine.
//
// Everything it cannot prove is loopback is treated as not loopback. An empty
// host is the case that matters: ":8080" has no host, and no host means every
// interface, so reading it as loopback is how an unauthenticated session ends
// up on the office wifi. The same goes for an address nobody can parse —
// guessing "probably local" about it is the same mistake one step later.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port at all. A bare host is still worth judging; anything else
		// is not loopback.
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// steerRoutes need a device that may act, not merely watch.
//
// Named and tested rather than decided inline, for the same reason openRoutes
// is: adding a write endpoint without listing it here would leave it answerable
// by any paired device, and that failure is silent. Everything authenticated
// and not listed here is readable by any device.
var steerRoutes = map[string]bool{
	"/v1/permissions/resolve": true,
}

// authMiddleware authenticates a request and enforces what its caller may do.
//
// Two kinds of caller. The operator's own `--token` is not tier-limited: it is
// the secret the person running the server chose for themselves, and tiering it
// would be Kolkrabbi restricting its own operator. A device token carries the
// tier it was paired at.
func authMiddleware(token string, exemptRoutes map[string]bool, store *devices.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptRoutes[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		if token == "" {
			// No token is only reachable on loopback — Mux refuses to serve
			// anything else without one — so this is the local case, and a
			// local session must not have to pair with itself.
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"unauthorized","message":"missing or invalid bearer token"}`, http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(authHeader, "Bearer ")

		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		device, ok := deviceOf(store, provided)
		if !ok {
			http.Error(w, `{"error":"forbidden","message":"invalid token"}`, http.StatusForbidden)
			return
		}
		if steerRoutes[r.URL.Path] && device.Tier != devices.TierSteer {
			http.Error(w, `{"error":"forbidden","message":"this device may watch but not act; pair it again or promote it from the machine running the session"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authenticate is a nil-safe wrapper: a server with no device store simply has
// no devices, which is different from a server whose store rejects everything.
func deviceOf(store *devices.Store, token string) (devices.Device, bool) {
	if store == nil {
		return devices.Device{}, false
	}
	return store.Authenticate(token)
}
