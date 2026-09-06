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
	"github.com/onembyte/kolkrabbi/internal/devices"
)

// Options holds configuration for the HTTP server.
type Options struct {
	Bus          *bus.Bus
	Token        string
	Addr         string
	PingInterval time.Duration
	Resolver     PermissionResolver
	// Turns is how a remote prompt reaches a session. Nil means this server
	// is not attached to one, which is what `kolk serve` standalone is.
	Turns TurnStarter
	// Devices holds the paired devices. Nil disables device auth and pairing.
	Devices *devices.Store
	// Pairing is the short window during which a new device may be added.
	Pairing *devices.Pairing
	// DeviceFile is where a newly paired device is persisted. Empty keeps it
	// in memory, which only tests want.
	DeviceFile string
}

// openRoutes answer without a credential.
//
// Named rather than inline so that widening it is a visible change to a policy
// with a test on it. Both are here for the same reason: neither says anything
// about the session, and a liveness probe that needs a credential is a liveness
// probe nothing can use. Anything that reveals what the agent is doing, or
// lets someone answer for it, does not belong in this map.
var openRoutes = map[string]bool{
	"/":          true,
	"/v1/health": true,
}

// Mux creates an http.Handler with all endpoints mounted and auth enforced.
func Mux(opts Options) (http.Handler, error) {
	if opts.Bus == nil {
		return nil, errors.New("event bus is required")
	}

	// An empty Addr means the handler is being built without being served —
	// tests, and callers that mount it themselves. Anything that actually
	// listens passes the address it will listen on, and cmd_serve builds the
	// server before it opens the socket so this refusal happens first.
	if opts.Addr != "" && !isLoopback(opts.Addr) && opts.Token == "" {
		return nil, fmt.Errorf("refusing to serve %s without a token: that address is reachable from other machines (use --token, or bind 127.0.0.1)", opts.Addr)
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
	mux.Handle("/v1/turns", turnStartHandler(opts.Turns))

	// Pairing. Not in openRoutes: it is exempt from auth because handing out
	// the first credential is what it does, and it exists only while armed.
	pair := pairHandler(opts.Pairing, opts.Devices, opts.DeviceFile)
	mux.Handle("/v1/pair", pair)

	// The client (plan 26 §5, I26.7): server-rendered, no script, behind the
	// same auth as the API with a device cookie honoured under this prefix.
	mux.Handle("/v1/client", clientPageHandler(opts.Token, opts.Devices))
	mux.Handle("/v1/client/stream", clientStreamHandler(opts.Bus, opts.PingInterval))
	mux.Handle("/v1/client/turn", clientTurnHandler(opts.Token, opts.Devices, opts.Turns))
	mux.Handle("/v1/client/manifest.json", clientManifestHandler())

	guarded := authMiddleware(opts.Token, openRoutes, opts.Devices, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Routed before auth rather than exempted inside it, so the exempt set
		// stays exactly the two routes I26.2 ratcheted it to.
		if r.URL.Path == "/v1/pair" {
			pair.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	}), nil
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
			Addr:              opts.Addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
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
