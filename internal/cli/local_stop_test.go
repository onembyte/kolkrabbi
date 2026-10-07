package cli

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
)

type stopTestProcess struct{ pid int }

func (p stopTestProcess) Pid() int     { return p.pid }
func (p stopTestProcess) Close() error { return nil }

func TestLocaliaStopIsExplicitAndTheLifetimeChangeExplainsIt(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	project := t.TempDir()
	t.Chdir(project)
	dirs, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	var signals atomic.Int32
	h := &local.HostStarter{
		Binary: "/fixture/ollama", Persistent: true, Project: project,
		StateDir: filepath.Join(dirs.LocalRuntimeDir(), "projects"),
		Port:     func() (int, error) { return 43567, nil },
		Ready:    func(context.Context, string) bool { return true },
		Identity: func(context.Context, int) (string, error) { return "original-ollama", nil },
		EndpointOwner: func(_ context.Context, pid int, addr string) (bool, error) {
			return pid == 9001 && addr == "127.0.0.1:43567", nil
		},
		Signal: func(context.Context, int) error { signals.Add(1); return nil },
		DetachedStart: func(context.Context, string, []string, []string) (local.Process, error) {
			return stopTestProcess{pid: 9001}, nil
		},
	}
	a.localRuntime = h
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "off"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "on"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/localia stop") || signals.Load() != 0 {
		t.Fatalf("off→on hint or implicit-stop policy wrong: %q; signals=%d", out.String(), signals.Load())
	}
	out.Reset()
	if err := a.runLocalia(ctx, []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stopped") || signals.Load() != 1 {
		t.Fatalf("explicit stop: output=%q signals=%d", out.String(), signals.Load())
	}
	if err := a.runLocalia(ctx, []string{"stop"}); err == nil || signals.Load() != 1 {
		t.Fatalf("repeat stop = %v, signals=%d", err, signals.Load())
	}
	if err := a.runLocalia(ctx, []string{"stop", "other"}); err == nil || signals.Load() != 1 {
		t.Fatalf("extra args accepted: %v", err)
	}
	if err := h.Close(); err != nil || signals.Load() != 1 {
		t.Fatalf("close signalled kept runtime: %v / %d", err, signals.Load())
	}
}

func TestLifetimeHintFindsKeptRuntimeBesideUserDefaultServer(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	project := t.TempDir()
	t.Chdir(project)
	dirs, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dirs.LocalRuntimeDir(), "projects")
	first := &local.HostStarter{Binary: "/fixture/ollama", Persistent: true, Project: project, StateDir: state,
		Port:     func() (int, error) { return 43568, nil },
		Ready:    func(context.Context, string) bool { return true },
		Identity: func(context.Context, int) (string, error) { return "original-ollama", nil },
		DetachedStart: func(context.Context, string, []string, []string) (local.Process, error) {
			return stopTestProcess{pid: 9002}, nil
		},
	}
	ctx := context.Background()
	if _, err := first.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	a.localRuntime = &local.HostStarter{Project: project, StateDir: state,
		Discover: func(context.Context) local.Host {
			return local.Host{State: local.HostRunning, Addr: local.DefaultHostAddr}
		},
		Ready:         func(context.Context, string) bool { return true },
		Identity:      func(context.Context, int) (string, error) { return "original-ollama", nil },
		EndpointOwner: func(context.Context, int, string) (bool, error) { return true, nil },
	}
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "off"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "on"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/localia stop") || !strings.Contains(out.String(), "127.0.0.1:43568") {
		t.Fatalf("default user server hid kept runtime stop hint: %q", out.String())
	}
	a.localRuntime.Discover = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	out.Reset()
	if err := a.runLocalia(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "kept running") || !strings.Contains(out.String(), "/localia stop") {
		t.Fatalf("Localia status omitted the verified kept-runtime action: %q", out.String())
	}
	// The recorded process can remain alive while another Ollama takes its
	// old port. Then /localia stop refuses, so this hint must not promise it.
	a.localRuntime.EndpointOwner = func(context.Context, int, string) (bool, error) { return false, nil }
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "off"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "on"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "/localia stop") {
		t.Fatalf("foreign listener was offered as Kolk's stoppable runtime: %q", out.String())
	}
	out.Reset()
	if err := a.runLocalia(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "/localia stop") {
		t.Fatalf("Localia status offered a foreign listener as stoppable: %q", out.String())
	}
}
