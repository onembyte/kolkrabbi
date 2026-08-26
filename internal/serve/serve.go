package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/bus"
)

// Options holds configuration for the HTTP server.
type Options struct {
	Bus          *bus.Bus
	Token        string
	Addr         string
	PingInterval time.Duration
	Resolver     PermissionResolver
}

// Mux creates an http.Handler with all endpoints mounted and auth enforced.
func Mux(opts Options) (http.Handler, error) {
	if opts.Bus == nil {
		return nil, errors.New("event bus is required")
	}

	if opts.Addr != "" && !isLoopback(opts.Addr) && opts.Token == "" {
		return nil, errors.New("binding to non-loopback address requires a non-empty bearer token")
	}

	mux := http.NewServeMux()

	// Hello / version info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"kolkrabbi","version":"%s","status":"ok"}`+"\n", buildinfo.Get().Version)
	})

	// Health check
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	// SSE events endpoint
	mux.Handle("/v1/events", sseHandler(opts.Bus, opts.PingInterval))

	// Permission resolution
	mux.Handle("/v1/permissions/resolve", permissionResolveHandler(opts.Resolver))

	exempt := map[string]bool{
		"/":          true,
		"/v1/health": true,
	}

	return authMiddleware(opts.Token, exempt, mux), nil
}

// Server is the HTTP server lifecycle wrapper.
type Server struct {
	httpServer *http.Server
}

// New creates a new Server.
func New(opts Options) (*Server, error) {
	handler, err := Mux(opts)
	if err != nil {
		return nil, err
	}
	return &Server{
		httpServer: &http.Server{
			Addr:    opts.Addr,
			Handler: handler,
		},
	}, nil
}

// Serve starts serving traffic on listener l.
func (s *Server) Serve(l net.Listener) error {
	return s.httpServer.Serve(l)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
