package serve

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/devices"
	"github.com/onembyte/kolkrabbi/internal/netaddr"
)

// isLoopback reports whether an address reaches only this machine.
//
// Delegated to internal/netaddr so `kolk serve` and `kolk dash` cannot drift
// apart again: they answered this question separately, and only one of them
// handled an empty host, which is how a wildcard bind was served without a
// token until I26.1.
func isLoopback(addr string) bool { return netaddr.IsLoopback(addr) }

// steerRoutes need a device that may act, not merely watch.
//
// Named and tested rather than decided inline, for the same reason openRoutes
// is: adding a write endpoint without listing it here would leave it answerable
// by any paired device, and that failure is silent. Everything authenticated
// and not listed here is readable by any device.
var steerRoutes = map[string]bool{
	"/v1/client/turn":         true,
	"/v1/permissions/resolve": true,
	"/v1/turns":               true,
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
		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			// The client routes, and only they, accept the device cookie the
			// pairing form set: a browser cannot send a bearer from a form or
			// a stream, and the cookie never leaves the /v1 path.
			if c, err := r.Cookie(clientCookie); err == nil && c.Value != "" && strings.HasPrefix(r.URL.Path, clientPrefix) {
				provided = c.Value
			} else {
				http.Error(w, `{"error":"unauthorized","message":"missing or invalid bearer token"}`, http.StatusUnauthorized)
				return
			}
		}

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
