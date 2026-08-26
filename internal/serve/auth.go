package serve

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
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

// authMiddleware enforces bearer token authentication on protected routes.
func authMiddleware(token string, exemptRoutes map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if exemptRoutes[path] {
			next.ServeHTTP(w, r)
			return
		}

		if token == "" {
			// If no token is configured, allow requests (e.g. local loopback dev)
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"unauthorized","message":"missing or invalid bearer token"}`, http.StatusUnauthorized)
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, `{"error":"forbidden","message":"invalid token"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
