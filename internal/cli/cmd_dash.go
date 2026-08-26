package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/dash"
	"github.com/onembyte/kolkrabbi/internal/stats"
)

const defaultDashAddr = "127.0.0.1:0"

// runDash serves the local usage dashboard.
//
// It binds loopback only, and refuses anything else rather than offering a
// flag: this page is a record of everything the user has worked on, and no
// convenience justifies publishing it to a network by accident.
func (a *app) runDash(ctx context.Context, args []string) error {
	addr := defaultDashAddr
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return usagef("usage: kolk dash [--addr 127.0.0.1:0]")
			}
			addr = args[i+1]
			i++
		default:
			return usagef("%s", usageLine("dash"))
		}
	}
	if !dashAddrIsLoopback(addr) {
		return fmt.Errorf("%s is not a loopback address; the dashboard is a record of everything you have worked on and is never served to a network", addr)
	}

	d, err := a.resolve()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{
		Handler:           a.dashHandler(d.Data),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(a.stdout, "kolk dash on http://%s — press Ctrl+C to stop\n", listener.Addr())
	fmt.Fprintln(a.stdout, "nothing leaves this machine; the page is rendered from your local usage log")

	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		_ = server.Close()
		return nil
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// dashHandler reads the usage log per request, so the page is current without
// anything having to watch a file.
func (a *app) dashHandler(dataDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		records, skipped, err := stats.LoadCounted(dataDir)
		if err != nil {
			http.Error(w, "could not read the usage log: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page embeds nothing and loads nothing; say so to the browser too.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(dash.Page(records, skipped)))
	})
	return mux
}

func dashAddrIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
