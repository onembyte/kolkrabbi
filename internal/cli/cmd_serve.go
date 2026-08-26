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
	"github.com/onembyte/kolkrabbi/internal/xid"
)

func (a *app) runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	addr := fs.String("addr", "127.0.0.1:4096", "address to listen on (e.g. 127.0.0.1:4096 or unix socket)")
	token := fs.String("token", os.Getenv("KOLK_AUTH_TOKEN"), "bearer auth token (or KOLK_AUTH_TOKEN env)")
	stdio := fs.Bool("stdio", false, "run as stdio event stream instead of HTTP server")
	pair := fs.Bool("pair", false, "open a two-minute window for a new device to pair")

	if err := fs.Parse(args); err != nil {
		return err
	}

	sessionID := xid.New(xid.Session)
	b, err := bus.New(sessionID, bus.Options{})
	if err != nil {
		return fmt.Errorf("initializing event bus: %w", err)
	}
	defer func() { _ = b.Close() }()

	if *stdio {
		return serve.ServeStdio(ctx, os.Stdin, a.stdout, b)
	}

	d, err := a.resolve()
	if err != nil {
		return err
	}
	deviceFile := d.DevicesFile()
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
		Token:      *token,
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
	if *pair {
		code, expires, err := pairing.Arm()
		if err != nil {
			return fmt.Errorf("opening pairing: %w", err)
		}
		fmt.Fprintf(a.stdout, "pairing code %s — valid until %s, one device, five attempts\n",
			code, expires.Format("15:04:05"))
		fmt.Fprintf(a.stdout, "on the device: POST http://%s/v1/pair {\"code\":\"%s\",\"label\":\"...\"}\n",
			l.Addr().String(), code)
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
