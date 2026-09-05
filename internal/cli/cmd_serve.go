package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/devices"
	"github.com/onembyte/kolkrabbi/internal/serve"
)

func (a *app) runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	addr := fs.String("addr", "127.0.0.1:4096", "address to listen on (e.g. 127.0.0.1:4096 or unix socket)")
	// Refused when set: see below. It stays a flag so that the refusal, not a
	// generic "flag provided but not defined", is what an old script sees.
	tokenFlag := fs.String("token", "", "refused; set KOLK_AUTH_TOKEN or pair a device with --pair")
	stdio := fs.Bool("stdio", false, "run as stdio event stream instead of HTTP server")
	pair := fs.Bool("pair", false, "open a two-minute window for a new device to pair")
	serveSession := fs.String("session", "", "serve this saved session instead of asking")
	serveNew := fs.Bool("new", false, "serve a new session without asking")

	if err := fs.Parse(args); err != nil {
		return err
	}
	// A bearer token on the command line is in `ps` for every user on the
	// machine and in the shell's history afterwards (V34.1d.4a). The two ways
	// that do not leak already exist, so the flag form is refused, not merely
	// discouraged. The value is not repeated back.
	if *tokenFlag != "" {
		return usagef("kolk serve refuses --token: a bearer token on the command line sits in `ps` and in shell history. " +
			"Set KOLK_AUTH_TOKEN in the environment instead, or pair a device with `kolk serve --pair` and let it hold its own token")
	}
	token := os.Getenv("KOLK_AUTH_TOKEN")

	// Which conversation the server hosts is asked before anything binds: a
	// listener opened first would have to be closed again on a refused answer.
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	sessionID, err := a.pickServedSession(dirs.Sessions(), *serveSession, *serveNew)
	if err != nil {
		return err
	}
	b, err := bus.New(sessionID, bus.Options{})
	if err != nil {
		return fmt.Errorf("initializing event bus: %w", err)
	}
	defer func() { _ = b.Close() }()

	if *stdio {
		return serve.ServeStdio(ctx, os.Stdin, a.stdout, b)
	}

	deviceFile := dirs.DevicesFile()
	deviceStore, err := devices.Load(deviceFile)
	if err != nil {
		return fmt.Errorf("reading paired devices: %w", err)
	}
	pairing := &devices.Pairing{}

	// Built before the socket is opened, so an address that would be reachable
	// from other machines is refused rather than bound and then refused. The
	// window between the two was small and it was the wrong way round.
	srv, err := serve.New(serve.Options{
		Bus:        b,
		Token:      token,
		Addr:       *addr,
		Devices:    deviceStore,
		Pairing:    pairing,
		DeviceFile: deviceFile,
	})
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	l, err := serve.Listen(*addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	defer func() { _ = l.Close() }()

	fmt.Fprintf(a.stdout, "kolk serving on %s\n", l.Addr().String())
	reach := a.printReachability(l.Addr().String())
	if *pair {
		code, expires, err := pairing.Arm()
		if err != nil {
			return fmt.Errorf("opening pairing: %w", err)
		}
		fmt.Fprintf(a.stdout, "pairing code %s — valid until %s, one device, five attempts\n",
			code, expires.Format("15:04:05"))
		fmt.Fprintf(a.stdout, "on the device: POST http://%s/v1/pair {\"code\":\"%s\",\"label\":\"...\"}\n",
			l.Addr().String(), code)
		if advice := pairingAdvice(reach); advice != "" {
			fmt.Fprintf(a.stdout, "\033[2m%s\033[0m\n", advice)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(l)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// printReachability says how the server can be reached, and by whom.
//
// The common failure with a bound port is not insecurity but confusion:
// someone binds every interface, sets a token, and still cannot work out which
// URL to open on their phone. Saying it once, at startup, costs nothing.
func (a *app) printReachability(bound string) serve.Reachability {
	reach := serve.Describe(bound, serve.LocalInterfaces())
	for i, url := range reach.URLs {
		marker := "  "
		if i == 0 && reach.Kind == serve.ReachTailscale {
			marker = "→ "
		}
		fmt.Fprintf(a.stdout, "%s%s\n", marker, url)
	}
	if reach.Note != "" {
		fmt.Fprintf(a.stdout, "\033[2m%s\033[0m\n", reach.Note)
	}
	if reach.Tunnel != "" {
		fmt.Fprintf(a.stdout, "\033[2mfrom elsewhere: %s\033[0m\n", reach.Tunnel)
	}
	return reach
}

// pairingAdvice warns when pairing has been armed on a port the device being
// paired cannot actually reach.
//
// "Pair your phone" against a loopback bind is an instruction nobody can
// follow, and the failure looks like a broken pairing code rather than a
// binding choice.
func pairingAdvice(reach serve.Reachability) string {
	if reach.Kind != serve.ReachLoopback {
		return ""
	}
	return "this port is loopback-only, so the device must reach it through the tunnel above (or re-run with --addr on a reachable address)"
}
