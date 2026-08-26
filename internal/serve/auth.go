package serve

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// isLoopback checks whether an address string is bound to loopback.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
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
