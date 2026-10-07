package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/protocol"
)

// The session's runtime starter tidies the installations the installer uses:
// what a killed setup left there goes on the next start, whether or not a
// setup follows.
func TestTheRuntimeStarterTidiesTheInstallations(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	dirs, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	a.bindLocalRuntime(&config.Config{}, dirs, t.TempDir())
	if a.localRuntime == nil || a.localRuntime.Tidy == nil {
		t.Fatal("the runtime starter has no housekeeping")
	}
	stale := filepath.Join(dirs.LocalRuntimeDir(), "installations", ".install-killed")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	a.localRuntime.Tidy(io.Discard)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the starter's housekeeping left %s: %v", stale, err)
	}
}

// stuckLeftover is staging that will not go: a read-only directory inside.
func stuckLeftover(t *testing.T, dir string) string {
	t.Helper()
	locked := filepath.Join(dir, ".install-stuck", "locked")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "part"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if os.Getuid() == 0 {
		t.Skip("root removes read-only directories")
	}
	return filepath.Dir(locked)
}

// The housekeeping's note reaches the writer it is given (the starter's
// output), not the process's stdout.
func TestTheStartNoteReachesTheSurface(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	dirs, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	a.bindLocalRuntime(&config.Config{}, dirs, t.TempDir())
	stuck := stuckLeftover(t, filepath.Join(dirs.LocalRuntimeDir(), "installations"))
	var surface strings.Builder
	a.localRuntime.Tidy(&surface)
	if !strings.Contains(surface.String(), stuck) {
		t.Fatalf("the surface got %q, want the note naming %s", surface.String(), stuck)
	}
	if strings.Contains(out.String(), stuck) {
		t.Fatalf("the note went to stdout: %q", out.String())
	}
}

// stream-json's stdout is the NDJSON stream and nothing else: the local
// runtime's words (setup progress, housekeeping notes) go to stderr.
func TestStreamJSONKeepsTheLocalRuntimesWordsOffStdout(t *testing.T) {
	t.Run("a start's note", func(t *testing.T) {
		a, out, errOut := newTestApp(t, "")
		dirs, err := a.locate()
		if err != nil {
			t.Fatal(err)
		}
		stuck := stuckLeftover(t, filepath.Join(dirs.LocalRuntimeDir(), "installations"))
		_ = a.runDefault(context.Background(), []string{"-p", "hi", "--output-format", "stream-json", "--model", "ollama/qwen2.5-coder:7b", "--permission", "full-auto"})
		if err := protocol.DecodeStream(strings.NewReader(out.String()), protocol.StreamNDJSON, func(protocol.Envelope) error { return nil }); err != nil {
			t.Fatalf("stdout is no longer NDJSON: %v\n%q", err, out.String())
		}
		if !strings.Contains(errOut.String(), stuck) {
			t.Fatalf("the note is nowhere; stderr %q", errOut.String())
		}
	})
	t.Run("setup progress", func(t *testing.T) {
		a, out, errOut := newTestApp(t, "")
		a.installLocalRuntime = func(_ context.Context, _ string, progress func(local.RuntimeProgress)) (local.Host, error) {
			progress(local.RuntimeProgress{Stage: "waiting"})
			return local.Host{}, errors.New("fixture stops here")
		}
		_ = a.runDefault(context.Background(), []string{"-p", "hi", "--output-format", "stream-json", "--model", "ollama/qwen2.5-coder:7b", "--permission", "full-auto"})
		if err := protocol.DecodeStream(strings.NewReader(out.String()), protocol.StreamNDJSON, func(protocol.Envelope) error { return nil }); err != nil {
			t.Fatalf("stdout is no longer NDJSON: %v\n%q", err, out.String())
		}
		if !strings.Contains(errOut.String(), "Preparing Localia") {
			t.Fatalf("setup progress is nowhere; stderr %q", errOut.String())
		}
	})
}
