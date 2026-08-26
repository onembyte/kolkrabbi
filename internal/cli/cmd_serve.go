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
	"github.com/onembyte/kolkrabbi/internal/serve"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

func (a *app) runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	addr := fs.String("addr", "127.0.0.1:4096", "address to listen on (e.g. 127.0.0.1:4096 or unix socket)")
	token := fs.String("token", os.Getenv("KOLK_AUTH_TOKEN"), "bearer auth token (or KOLK_AUTH_TOKEN env)")
	stdio := fs.Bool("stdio", false, "run as stdio event stream instead of HTTP server")

	if err := fs.Parse(args); err != nil {
		return err
	}

	sessionID := xid.New(xid.Session)
	b, err := bus.New(sessionID, bus.Options{})
	if err != nil {
		return fmt.Errorf("initializing event bus: %w", err)
	}
	defer b.Close()

	if *stdio {
		return serve.ServeStdio(ctx, os.Stdin, a.stdout, b)
	}

	l, err := serve.Listen(*addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	defer l.Close()

	srv, err := serve.New(serve.Options{
		Bus:   b,
		Token: *token,
		Addr:  *addr,
	})
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	fmt.Fprintf(a.stdout, "kolk serving on %s\n", l.Addr().String())

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
