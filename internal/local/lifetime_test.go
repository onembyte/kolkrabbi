package local

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func persistentFixture(t *testing.T, dir, project string, starts *atomic.Int32) (*HostStarter, *startedProcess) {
	t.Helper()
	p := &startedProcess{pid: 4242}
	h := &HostStarter{Binary: "/fake/ollama", Persistent: true, StateDir: dir, Project: project,
		Port:     func() (int, error) { return 43210, nil },
		Ready:    func(context.Context, string) bool { return true },
		Identity: func(context.Context, int) (string, error) { return "same-process", nil },
		DetachedStart: func(context.Context, string, []string, []string) (Process, error) {
			starts.Add(1)
			return p, nil
		},
		Start: func(context.Context, string, []string, []string) (Process, error) {
			t.Error("persistent server used session-owned launch")
			return nil, errors.New("wrong launch")
		},
	}
	return h, p
}

func TestPersistentRuntimeSurvivesCloseAndIsReusedConcurrently(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	first, p := persistentFixture(t, dir, project, &starts)
	addr, err := first.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if p.closed.Load() {
		t.Fatal("persistent server stopped at session close")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _ := persistentFixture(t, dir, project, &starts)
			got, err := h.Ensure(context.Background())
			if err != nil || got != addr {
				t.Errorf("reuse = %s, %v", got, err)
			}
			_ = h.Close()
		}()
	}
	wg.Wait()
	if starts.Load() != 1 {
		t.Fatalf("launched %d servers", starts.Load())
	}
}

func TestPersistentRuntimeStaleIdentityAndProjectIsolation(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	first, p := persistentFixture(t, dir, project, &starts)
	if _, err := first.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	stale, _ := persistentFixture(t, dir, project, &starts)
	stale.Identity = func(context.Context, int) (string, error) { return "new-process", nil }
	if _, err := stale.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	other, _ := persistentFixture(t, dir, t.TempDir(), &starts)
	if _, err := other.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 3 {
		t.Fatalf("stale/other project reused wrong process: %d starts", starts.Load())
	}
	if p.closed.Load() {
		t.Fatal("stale registry caused a process kill")
	}
}

func TestPersistentRuntimePublishFailureStopsOnlyNewProcess(t *testing.T) {
	var starts atomic.Int32
	h, p := persistentFixture(t, t.TempDir(), t.TempDir(), &starts)
	start := h.DetachedStart
	h.DetachedStart = func(ctx context.Context, binary string, args, env []string) (Process, error) {
		process, err := start(ctx, binary, args, env)
		// Refuse publication only after discovery and launch succeeded.
		if err := os.Mkdir(h.recordPath(), 0o700); err != nil {
			t.Fatal(err)
		}
		return process, err
	}
	_, err := h.Ensure(context.Background())
	if err == nil {
		t.Fatal("accepted unreadable record")
	}
	if starts.Load() != 1 || !p.closed.Load() {
		t.Fatal("failed startup left a persistent process")
	}
}

func TestPersistentRuntimeConcurrentFirstUseStartsOnce(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _ := persistentFixture(t, dir, project, &starts)
			if _, err := h.Ensure(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if starts.Load() != 1 {
		t.Fatalf("first use started %d runtimes", starts.Load())
	}
}

func TestAdoptedServerIsNeverStopped(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Discover = func(context.Context) Host { return Host{State: HostRunning, Addr: DefaultHostAddr} }
	if addr, err := f.starter.Ensure(context.Background()); err != nil || addr != DefaultHostAddr {
		t.Fatalf("adopt = %s, %v", addr, err)
	}
	_ = f.starter.Close()
	if f.starts.Load() != 0 || f.process.closed.Load() {
		t.Fatal("adopted server acquired ownership")
	}
}

func TestPersistentRuntimeNeverDuplicatesALiveUnreadyServer(t *testing.T) {
	var starts atomic.Int32
	dir, project := t.TempDir(), t.TempDir()
	first, p := persistentFixture(t, dir, project, &starts)
	if _, err := first.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, _ := persistentFixture(t, dir, project, &starts)
	next.Ready = func(context.Context, string) bool { return false }
	if _, err := next.Ensure(context.Background()); err == nil {
		t.Fatal("unready live runtime was accepted")
	}
	if starts.Load() != 1 || p.closed.Load() {
		t.Fatal("unready runtime was replaced or stopped")
	}
}

func TestPersistentFollowersFollowAReplacementRuntime(t *testing.T) {
	for _, surface := range []string{"request", "status"} {
		t.Run(surface, func(t *testing.T) {
			dir, project := t.TempDir(), t.TempDir()
			var starts atomic.Int32
			first, _ := persistentFixture(t, dir, project, &starts)
			ctx := context.Background()
			if _, err := first.Ensure(ctx); err != nil {
				t.Fatal(err)
			}
			follower, _ := persistentFixture(t, dir, project, &starts)
			if _, err := follower.Ensure(ctx); err != nil {
				t.Fatal(err)
			}
			// The original process exited and a different session replaced it.
			replacement, _ := persistentFixture(t, dir, project, &starts)
			replacement.Identity = func(context.Context, int) (string, error) { return "replacement-process", nil }
			replacement.Port = func() (int, error) { return 43211, nil }
			want, err := replacement.Ensure(ctx)
			if err != nil {
				t.Fatal(err)
			}
			follower.Identity = replacement.Identity
			var got string
			if surface == "request" {
				got, err = follower.Ensure(ctx)
			} else {
				got = follower.Host(ctx).Addr
			}
			if err != nil || got != want {
				t.Fatalf("follower stayed on %q; replacement is %q, error %v", got, want, err)
			}
			if starts.Load() != 2 {
				t.Fatalf("follower launched another server: %d starts", starts.Load())
			}
		})
	}
}

func TestPersistentRuntimeRejectsForeignRecordsBeforeProbing(t *testing.T) {
	for _, mutate := range []func(*runtimeRecord){
		func(r *runtimeRecord) { r.Addr = "10.0.0.1:43210" },
		func(r *runtimeRecord) { r.Addr = DefaultHostAddr },
		func(r *runtimeRecord) { r.Project = "/different-project" },
		func(r *runtimeRecord) { r.PID = 0 },
		func(r *runtimeRecord) { r.Identity = "" },
	} {
		var starts atomic.Int32
		h, _ := persistentFixture(t, t.TempDir(), t.TempDir(), &starts)
		r := runtimeRecord{Project: h.Project, Addr: "127.0.0.1:43210", PID: 4242, Identity: "same-process"}
		mutate(&r)
		b, _ := json.Marshal(r)
		if err := os.WriteFile(h.recordPath(), b, 0o600); err != nil {
			t.Fatal(err)
		}
		h.Identity = func(context.Context, int) (string, error) {
			t.Error("foreign record reached process probe")
			return "", nil
		}
		if addr, err := h.reusable(context.Background()); err != nil || addr != "" {
			t.Fatalf("reuse = %s, %v", addr, err)
		}
	}
}

// A project switched back to ephemeral still has its earlier persistent
// runtime running. A session reuses it instead of starting a second server,
// says it was kept, and leaves it running: nothing signals a process another
// session started.
func TestEphemeralSessionReusesAPersistentRuntimeLeftRunning(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	persistent, kept := persistentFixture(t, dir, project, &starts)
	addr, err := persistent.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = persistent.Close()

	var sessionStarts atomic.Int32
	ephemeral, _ := persistentFixture(t, dir, project, &starts)
	ephemeral.Persistent = false
	ephemeral.Start = func(context.Context, string, []string, []string) (Process, error) {
		sessionStarts.Add(1)
		return &startedProcess{pid: 5151}, nil
	}
	got, err := ephemeral.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != addr || sessionStarts.Load() != 0 {
		t.Fatalf("ephemeral session addr=%s, started %d servers; want the kept runtime at %s reused", got, sessionStarts.Load(), addr)
	}
	if host := ephemeral.Host(context.Background()); !host.KeptRunning || !host.Managed {
		t.Fatalf("host = %+v; want a managed runtime marked as kept running", host)
	}
	if err := ephemeral.Close(); err != nil || kept.closed.Load() {
		t.Fatalf("the ephemeral session stopped a runtime it did not start: closed=%v err=%v", kept.closed.Load(), err)
	}
}

// A kept runtime whose process is still Kolk's but no longer answers (hung,
// or stopped with SIGSTOP) is never an ephemeral session's to stop, and the
// session does not wait on it either: it starts its own beside it, says why,
// and leaves the kept one alone. Before a start, Host names the stall rather
// than calling the runtime installed and idle.
func TestEphemeralSessionStartsItsOwnBesideAStalledKeptRuntime(t *testing.T) {
	for _, when := range []string{"before reuse", "mid-session"} {
		t.Run(when, func(t *testing.T) {
			dir, project := t.TempDir(), t.TempDir()
			var starts atomic.Int32
			persistent, kept := persistentFixture(t, dir, project, &starts)
			keptAddr, err := persistent.Ensure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_ = persistent.Close()

			ephemeral, _ := persistentFixture(t, dir, project, &starts)
			ephemeral.Persistent = false
			out := &strings.Builder{}
			ephemeral.Out = out
			ephemeral.Port = func() (int, error) { return 43211, nil }
			var stalled atomic.Bool
			ephemeral.Ready = func(_ context.Context, addr string) bool { return addr != keptAddr || !stalled.Load() }
			own := &startedProcess{pid: 5151}
			ephemeral.Start = func(context.Context, string, []string, []string) (Process, error) { return own, nil }
			if when == "mid-session" {
				if got, err := ephemeral.Ensure(context.Background()); err != nil || got != keptAddr {
					t.Fatalf("reuse = %q, %v; want the kept runtime", got, err)
				}
			}
			stalled.Store(true)

			if host := ephemeral.Host(context.Background()); host.State == HostRunning || !strings.Contains(host.StalledRuntime, "process 4242") {
				t.Fatalf("host = %+v; want the stalled kept runtime named, not reported running or idle", host)
			}
			got, err := ephemeral.Ensure(context.Background())
			if err != nil || got != "127.0.0.1:43211" {
				t.Fatalf("ephemeral session = %q, %v; want its own runtime beside the stalled one", got, err)
			}
			if !strings.Contains(out.String(), "not answering") || !strings.Contains(out.String(), "starts its own") {
				t.Errorf("the session did not say why it started its own:\n%s", out.String())
			}
			if host := ephemeral.Host(context.Background()); host.KeptRunning || host.Addr != got {
				t.Fatalf("host = %+v; want the session's own runtime, not the kept one", host)
			}
			if err := ephemeral.Close(); err != nil || !own.closed.Load() || kept.closed.Load() {
				t.Fatalf("close: own stopped=%v kept stopped=%v err=%v; want only the session's own stopped", own.closed.Load(), kept.closed.Load(), err)
			}
		})
	}
}

// A persistent session keeps its one runtime per project: a stalled one is
// an error, never a second server, and Host names the stall.
func TestPersistentSessionNamesAStalledRuntime(t *testing.T) {
	var starts atomic.Int32
	dir, project := t.TempDir(), t.TempDir()
	first, _ := persistentFixture(t, dir, project, &starts)
	if _, err := first.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, _ := persistentFixture(t, dir, project, &starts)
	next.Ready = func(context.Context, string) bool { return false }
	if host := next.Host(context.Background()); host.State == HostRunning || !strings.Contains(host.StalledRuntime, "not answering") {
		t.Fatalf("host = %+v; want the stall named", host)
	}
	if _, err := next.Ensure(context.Background()); err == nil || starts.Load() != 1 {
		t.Fatalf("stalled persistent runtime: err=%v starts=%d; want an error and no second server", err, starts.Load())
	}

	// Mid-session too: a session using it neither starts another nor moves
	// quietly to a user's own server, as the status line says.
	reused, _ := persistentFixture(t, dir, project, &starts)
	var stall atomic.Bool
	reused.Ready = func(context.Context, string) bool { return !stall.Load() }
	if _, err := reused.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	stall.Store(true)
	reused.Discover = func(context.Context) Host { return Host{State: HostRunning, Addr: DefaultHostAddr, Version: "0.34.4"} }
	if addr, err := reused.Ensure(context.Background()); err == nil || starts.Load() != 1 {
		t.Fatalf("a stalled persistent runtime was swapped for %q (starts=%d)", addr, starts.Load())
	}
}

// Host and Ensure race on an ephemeral session beside a stalled kept runtime.
// Both hold the starter's lock, so under -race there is no data race, and the
// session ends with exactly one server of its own at one address.
func TestHostAndEnsureRaceBesideAStalledKeptRuntime(t *testing.T) {
	dir, project := t.TempDir(), t.TempDir()
	var starts atomic.Int32
	persistent, kept := persistentFixture(t, dir, project, &starts)
	keptAddr, err := persistent.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = persistent.Close()

	ephemeral, _ := persistentFixture(t, dir, project, &starts)
	ephemeral.Persistent = false
	ephemeral.Out = &strings.Builder{}
	ephemeral.Port = func() (int, error) { return 43211, nil }
	ephemeral.Ready = func(_ context.Context, addr string) bool { return addr != keptAddr }
	var own atomic.Int32
	ephemeral.Start = func(context.Context, string, []string, []string) (Process, error) {
		own.Add(1)
		return &startedProcess{pid: 5151}, nil
	}
	var wg sync.WaitGroup
	addrs := make(chan string, 16)
	for n := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if n%2 == 0 {
				_ = ephemeral.Host(context.Background())
				return
			}
			addr, err := ephemeral.Ensure(context.Background())
			if err != nil {
				t.Error(err)
			}
			addrs <- addr
		}()
	}
	wg.Wait()
	close(addrs)
	for addr := range addrs {
		if addr != "127.0.0.1:43211" {
			t.Errorf("ensure returned %q; want the session's own runtime", addr)
		}
	}
	if own.Load() != 1 {
		t.Errorf("started %d servers of its own; want one", own.Load())
	}
	if err := ephemeral.Close(); err != nil || kept.closed.Load() {
		t.Fatalf("close = %v, kept stopped=%v; want the kept runtime left alone", err, kept.closed.Load())
	}
}
