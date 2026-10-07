package cli

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Adopted from the V43.5 §7 item 4 review: a restart after /update or a
// mid-session sign-in replaces this process with exec, so the deferred
// cleanup of the run never happens. A runtime this session started must be
// stopped before that, or it runs on with no owner; a kept one is left, as
// at any exit.
func TestARestartReleasesTheRuntimeThisSessionStarted(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		a, _, _ := newTestApp(t, "")
		p := &restartProcess{}
		a.localRuntime = &local.HostStarter{Binary: "/fake/ollama", Persistent: persistent,
			Discover: func(context.Context) local.Host {
				return local.Host{State: local.HostInstalled, Binary: "/fake/ollama"}
			},
			Port:          func() (int, error) { return 11500, nil },
			Ready:         func(context.Context, string) bool { return true },
			Start:         func(context.Context, string, []string, []string) (local.Process, error) { return p, nil },
			DetachedStart: func(context.Context, string, []string, []string) (local.Process, error) { return p, nil },
			Identity:      func(context.Context, int) (string, error) { return "fake", nil },
			StateDir:      t.TempDir(), Project: t.TempDir(),
		}
		if _, err := a.localRuntime.Ensure(context.Background()); err != nil {
			t.Fatalf("persistent=%v: start: %v", persistent, err)
		}
		stoppedAtExec, execed := false, false
		a.restartInto = "kolk v9.9.9"
		a.executablePath = func() (string, error) { return "/fake/kolk", nil }
		a.replaceSelf = func(string, []string, []string) error {
			execed, stoppedAtExec = true, p.closed.Load()
			return nil
		}
		ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode, Sess: session.New(t.TempDir(), "mock/model")})
		a.finishSession(context.Background(), ag)
		if !execed {
			t.Fatalf("persistent=%v: the restart never ran", persistent)
		}
		if stoppedAtExec == persistent {
			t.Errorf("persistent=%v: runtime stopped before exec = %v, want %v", persistent, stoppedAtExec, !persistent)
		}
	}
}

// restartProcess reports a process id, as a persistent runtime must, and
// counts how often it was stopped.
type restartProcess struct {
	localLifetimeProcess
	closes atomic.Int32
}

func (p *restartProcess) Close() error { p.closes.Add(1); return p.localLifetimeProcess.Close() }

func (*restartProcess) Pid() int { return 4242 }

// A route kolk started (a host server behind a model prefix) is released
// before exec as well, and when exec fails the run's own release that follows
// frees nothing a second time. The session on disk already holds the last
// turn: a finished turn saves.
type closingRoute struct {
	provider.Client
	closes atomic.Int32
}

func (r *closingRoute) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{Role: "assistant"}, provider.Meta{}, nil
}

func (r *closingRoute) Close() error { r.closes.Add(1); return nil }

func TestARestartReleasesTheRunFirstAndOnlyOnce(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "the answer before the restart"})
	defer srv.Close()
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	route := &closingRoute{}
	ag := engine.New(engine.Options{Client: provider.NewCompatibleClient(srv.URL), Model: "mock/model", Mode: engine.ModeCode,
		Sess: sess, SaveInterval: time.Hour, Out: &strings.Builder{}, Routes: map[string]engine.ChatBackend{"host/": route}})
	if err := ag.RunTurn(context.Background(), "a question before the restart"); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	p := &restartProcess{}
	a.localRuntime = &local.HostStarter{Binary: "/fake/ollama",
		Discover: func(context.Context) local.Host {
			return local.Host{State: local.HostInstalled, Binary: "/fake/ollama"}
		},
		Port:  func() (int, error) { return 11500, nil },
		Ready: func(context.Context, string) bool { return true },
		Start: func(context.Context, string, []string, []string) (local.Process, error) { return p, nil },
	}
	if _, err := a.localRuntime.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, routeClosed := false, false
	a.restartInto = "kolk v9.9.9"
	a.executablePath = func() (string, error) { return "/fake/kolk", nil }
	a.replaceSelf = func(string, []string, []string) error {
		routeClosed = route.closes.Load() == 1
		if loaded, err := session.Load(dir, sess.ID); err == nil {
			for _, m := range loaded.GetMessages() {
				saved = saved || strings.Contains(m.Content, "the answer before the restart")
			}
		}
		return errors.New("exec failed")
	}
	a.finishSession(context.Background(), ag)
	if !routeClosed || !saved {
		t.Errorf("before exec: route closed=%v, last turn on disk=%v; want both", routeClosed, saved)
	}
	// exec failed, so run's deferred release follows; it must not free twice.
	if backendErr, runtimeErr := a.releaseRun(ag); backendErr != nil || runtimeErr != nil || p.closes.Load() != 1 || route.closes.Load() != 1 {
		t.Errorf("second release: %v %v; runtime stopped %d times, route closed %d times; want once each",
			backendErr, runtimeErr, p.closes.Load(), route.closes.Load())
	}
}
