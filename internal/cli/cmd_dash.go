package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/dash"
	"github.com/onembyte/kolkrabbi/internal/netaddr"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/stats"
)

const defaultDashAddr = "127.0.0.1:0"

// startDashInSession serves the dashboard in the background and returns at
// once.
//
// A slash command runs on the turn goroutine, so serving until cancelled here
// would freeze the session behind a web server the user cannot see: the prompt
// simply stops responding until they interrupt it. The server instead outlives
// the command and the session keeps working.
func (a *app) startDashInSession(addr string) error {
	if a.dashURL != "" {
		// A second /dash is a request to find the first one, not to start
		// another server on another port.
		fmt.Fprintf(a.stdout, "/dash is already running on %s\n", a.dashURL)
		return nil
	}
	listener, server, err := a.dashListener(addr)
	if err != nil {
		return err
	}
	a.dashURL = "http://" + listener.Addr().String()
	go func() { _ = server.Serve(listener) }()
	fmt.Fprintf(a.stdout, "/dash on %s — it stays up for this session\n", a.dashURL)
	fmt.Fprintln(a.stdout, "nothing leaves this machine; the page is rendered from your local usage log")
	return nil
}

// dashListener validates the address and prepares the server both entry points
// use, so the loopback rule cannot be true in one and forgotten in the other.
func (a *app) dashListener(addr string) (net.Listener, *http.Server, error) {
	if !dashAddrIsLoopback(addr) {
		return nil, nil, fmt.Errorf("%s is not a loopback address; the dashboard is a record of everything you have worked on and is never served to a network", addr)
	}
	d, err := a.resolve()
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	return listener, &http.Server{
		Handler:           a.dashHandler(d.Data),
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
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
		cards, shared := a.sessionCards(r.Context(), dataDir)
		_, _ = w.Write([]byte(dash.Page(records, skipped, cards, shared)))
	})
	return mux
}

// dashAddrIsLoopback reports whether the dashboard would be reachable from
// anywhere but this machine. Shared with `kolk serve` through internal/netaddr:
// this copy was right and serve's was not, which is the argument for one copy.
func dashAddrIsLoopback(addr string) bool { return netaddr.IsLoopback(addr) }

// dashAddrFrom reads the optional address flag.
func dashAddrFrom(args []string) (string, error) {
	addr := defaultDashAddr
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return "", usagef("usage: /dash [--addr 127.0.0.1:0]")
			}
			addr = args[i+1]
			i++
		default:
			return "", usagef("%s", usageLine("dash"))
		}
	}
	return addr, nil
}

// sessionCards gathers what the page renders.
//
// The gathering lives here rather than in internal/dash because each source was
// built with its own cost decision — a header-only session read, an advisory
// lock probed without taking it, a 64 KiB journal tail read only for live
// sessions, and one pass over the usage log for the sessions being shown — and
// re-reading them from inside a template would undo all four.
func (a *app) sessionCards(ctx context.Context, dataDir string) ([]dash.SessionCard, []dash.SharedCheckout) {
	sessionsDir := filepath.Join(dataDir, "sessions")
	overview, err := session.Overview(sessionsDir)
	if err != nil || len(overview) == 0 {
		return nil, nil
	}

	wanted := make(map[string]bool, len(overview))
	for _, card := range overview {
		wanted[card.ID] = true
	}
	costs, err := stats.CostForSessions(dataDir, wanted)
	if err != nil {
		costs = nil
	}

	cards := make([]dash.SessionCard, 0, len(overview))
	for _, card := range overview {
		live := card.State == session.StateLive
		view := dash.SessionCard{
			ID:    card.ID,
			Name:  card.Name(),
			Model: card.Model,
			CWD:   card.CWD,
			Live:  live,
		}
		if cost, recorded := costs[card.ID]; recorded {
			view.Cost, view.CostKnown = cost, true
		}
		// Only live sessions: an idle one cannot be blocked, so there is
		// nothing to look for and nothing to pay for looking.
		if live {
			if blocked, waiting := session.BlockedOn(sessionsDir, card.ID); waiting {
				view.BlockedOn = blocked.Tool
			}
			// What source control is doing in a live session's tree; a
			// directory git will not speak for gets no line.
			if card.CWD != "" {
				if branch, dirty, ok := shell.RepoState(ctx, card.CWD); ok {
					view.Branch, view.Dirty, view.VCSKnown = branch, dirty, true
				}
			}
		}
		cards = append(cards, view)
	}

	var shared []dash.SharedCheckout
	for _, s := range session.SharedCheckouts(overview) {
		shared = append(shared, dash.SharedCheckout{Dir: s.Dir, Sessions: s.Sessions})
	}
	return cards, shared
}
