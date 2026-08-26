package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/serve"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:4096", "address to listen on")
	token := flag.String("token", os.Getenv("KOLKD_AUTH_TOKEN"), "bearer auth token")
	stdio := flag.Bool("stdio", false, "run over stdio")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("kolkd %s\n", buildinfo.Get().Version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessionID := xid.New(xid.Session)
	b, err := bus.New(sessionID, bus.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kolkd: event bus error: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = b.Close() }()

	if *stdio {
		if err := serve.ServeStdio(ctx, os.Stdin, os.Stdout, b); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "kolkd: stdio stream error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	l, err := serve.Listen(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kolkd: listen error on %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer func() { _ = l.Close() }()

	srv, err := serve.New(serve.Options{
		Bus:   b,
		Token: *token,
		Addr:  *addr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "kolkd: server error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("kolkd %s listening on %s\n", buildinfo.Get().Version, l.Addr().String())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(l)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "kolkd: serve error: %v\n", err)
			os.Exit(1)
		}
	}
}
