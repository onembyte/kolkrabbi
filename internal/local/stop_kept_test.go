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

func stopFixture(t *testing.T) (*HostStarter, *HostStarter, *atomic.Int32) {
	t.Helper()
	var starts atomic.Int32
	first, _ := persistentFixture(t, t.TempDir(), t.TempDir(), &starts)
	if _, err := first.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, _ := persistentFixture(t, first.StateDir, first.Project, &starts)
	var signals atomic.Int32
	next.EndpointOwner = func(context.Context, int, string) (bool, error) { return true, nil }
	next.Signal = func(context.Context, int) error { signals.Add(1); return nil }
	return first, next, &signals
}

func TestStopKeptRuntimeChecksIdentityAddressAndRemovesRecord(t *testing.T) {
	first, next, signals := stopFixture(t)
	if err := next.StopKept(context.Background()); err != nil {
		t.Fatal(err)
	}
	if signals.Load() != 1 {
		t.Fatalf("signalled %d times, want once", signals.Load())
	}
	if _, err := os.Stat(first.recordPath()); !os.IsNotExist(err) {
		t.Fatalf("record remains after stop: %v", err)
	}
	if err := next.StopKept(context.Background()); err == nil || signals.Load() != 1 {
		t.Fatalf("second stop = %v, signals=%d; want no target and no second signal", err, signals.Load())
	}
}

func TestStopKeptRuntimeRefusesUnverifiedTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testing.T, *HostStarter)
	}{
		{"reused PID", func(_ *testing.T, h *HostStarter) {
			h.Identity = func(context.Context, int) (string, error) { return "another-process", nil }
		}},
		{"other listener", func(_ *testing.T, h *HostStarter) {
			h.EndpointOwner = func(context.Context, int, string) (bool, error) { return false, nil }
		}},
		{"no Ollama heartbeat", func(_ *testing.T, h *HostStarter) {
			h.Ready = func(context.Context, string) bool { return false }
		}},
		{"another project", func(t *testing.T, h *HostStarter) {
			b, err := os.ReadFile(h.recordPath())
			if err != nil {
				t.Fatal(err)
			}
			var record runtimeRecord
			if err := json.Unmarshal(b, &record); err != nil {
				t.Fatal(err)
			}
			record.Project = t.TempDir()
			b, _ = json.Marshal(record)
			if err := os.WriteFile(h.recordPath(), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"default port", func(t *testing.T, h *HostStarter) {
			b, err := os.ReadFile(h.recordPath())
			if err != nil {
				t.Fatal(err)
			}
			var record runtimeRecord
			if err := json.Unmarshal(b, &record); err != nil {
				t.Fatal(err)
			}
			record.Addr = DefaultHostAddr
			b, _ = json.Marshal(record)
			if err := os.WriteFile(h.recordPath(), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, next, signals := stopFixture(t)
			tc.edit(t, next)
			if err := next.StopKept(context.Background()); err == nil || signals.Load() != 0 {
				t.Fatalf("unsafe stop = %v, signals=%d", err, signals.Load())
			}
		})
	}
}

func TestStopKeptRuntimeRefusesIdentityChangeDuringProbe(t *testing.T) {
	first, next, signals := stopFixture(t)
	var checks atomic.Int32
	next.Identity = func(context.Context, int) (string, error) {
		if checks.Add(1) == 1 {
			return "same-process", nil
		}
		return "replacement-process", nil
	}
	if err := next.StopKept(context.Background()); err == nil || signals.Load() != 0 {
		t.Fatalf("changed process was signalled: %v / %d", err, signals.Load())
	}
	if _, err := os.Stat(first.recordPath()); err != nil {
		t.Fatalf("refusal removed record: %v", err)
	}
}

func TestStopKeptRuntimePreservesRecordWhenSignalFails(t *testing.T) {
	first, next, _ := stopFixture(t)
	next.Signal = func(context.Context, int) error { return errors.New("signal refused") }
	if err := next.StopKept(context.Background()); err == nil || !strings.Contains(err.Error(), "signal refused") {
		t.Fatalf("signal error was hidden: %v", err)
	}
	if _, err := os.Stat(first.recordPath()); err != nil {
		t.Fatalf("failed stop removed record: %v", err)
	}
}

func TestConcurrentStopKeptRuntimeSignalsOnce(t *testing.T) {
	_, first, signals := stopFixture(t)
	second, _ := persistentFixture(t, first.StateDir, first.Project, new(atomic.Int32))
	second.EndpointOwner, second.Signal = first.EndpointOwner, first.Signal
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, h := range []*HostStarter{first, second} {
		wg.Add(1)
		go func(h *HostStarter) { defer wg.Done(); results <- h.StopKept(context.Background()) }(h)
	}
	wg.Wait()
	close(results)
	var success int
	for err := range results {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "no kept") {
			t.Errorf("unexpected stop result: %v", err)
		}
	}
	if success != 1 || signals.Load() != 1 {
		t.Fatalf("successes=%d signals=%d; want exactly one", success, signals.Load())
	}
}
