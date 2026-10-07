package local

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/lock"
)

// staleStaging leaves what a killed setup leaves: a staging directory holding
// a partial download, which its deferred cleanup never got to remove.
func staleStaging(t *testing.T, dir, name string) string {
	t.Helper()
	stage := filepath.Join(dir, name)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "bundle"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	return stage
}

// Setup removes staging a killed setup left behind, under install.lock, before
// it measures room: those bytes are nobody's once no setup holds the lock, and
// kept they count against every later attempt. Nothing else in the storage
// directory is touched, and a symlink is never followed.
func TestSetupSweepsStagingAKilledSetupLeftBehind(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "darwin/arm64")
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := staleStaging(t, i.Dir, ".install-1234567")
	keep := filepath.Join(i.Dir, "keep-me")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "precious"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(i.Dir, ".install-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the killed setup's staging is still there: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a directory that is not staging was removed: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("a symlink that only reads like staging was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Fatalf("the sweep followed a symlink out of the storage directory: %v", err)
	}
}

// A setup that only reuses the installed runtime sweeps too: the space comes
// back without waiting for the next download.
func TestReusingTheRuntimeSweepsStagingToo(t *testing.T) {
	i, calls, _ := runtimeInstallFixture(t, "darwin/arm64")
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	stale := staleStaging(t, i.Dir, ".install-7654321")
	before := calls.Load()
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("reusing the installed runtime went to the network")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("reuse left the killed setup's staging: %v", err)
	}
}

// While another setup holds install.lock its staging is live, and waiting for
// the lock never touches it; once the lock is ours, what is left is stale.
func TestTheSweepWaitsForTheLock(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "darwin/arm64")
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	other, err := lock.Acquire(context.Background(), filepath.Join(i.Dir, "install.lock"))
	if err != nil {
		t.Fatal(err)
	}
	live := staleStaging(t, i.Dir, ".install-other")
	waiting := make(chan struct{})
	i.Progress = func(p RuntimeProgress) {
		if p.Stage == "waiting" {
			close(waiting)
		}
	}
	done := make(chan error, 1)
	go func() { _, err := i.Ensure(context.Background()); done <- err }()
	<-waiting
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("another setup's staging was removed while it held the lock: %v", err)
	}
	_ = other.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("staging left once the lock was ours: %v", err)
	}
}

// used is what the storage directory holds, for a free-space seam that sees
// the leftover bytes the way a real disk does.
func used(dir string) uint64 {
	var n uint64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += uint64(info.Size())
			}
		}
		return nil
	})
	return n
}

// The point of the sweep: the room check sees the space the killed setup
// held. With only the fresh download's room free once the leftover is gone,
// setup succeeds on the first attempt, not the one after.
func TestTheRoomCheckSeesTheSweptSpace(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "darwin/arm64")
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	staleStaging(t, i.Dir, ".install-killed") // 1 MiB
	const disk = 64<<20 + 512<<10             // room for the download, not for it and the leftover
	i.freeSpace = func(string) (uint64, bool) {
		if held := used(i.Dir); held < disk {
			return disk - held, true
		}
		return 0, true
	}
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatalf("the room check counted the killed setup's bytes: %v", err)
	}
}

// Every leftover goes, not the first: a killed download and a killed retry
// leave two.
func TestSetupSweepsEveryStaleStaging(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "darwin/arm64")
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".install-111", ".install-222", ".install-333"} {
		staleStaging(t, i.Dir, name)
	}
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNoInstallStage(t, i.Dir)
}

// A leftover that will not go is reported by path, and setup carries on:
// housekeeping never blocks a runtime, but a later "not enough disk space"
// must not leave someone guessing what holds it.
func TestALeftoverThatWillNotGoIsReported(t *testing.T) {
	i, _, _ := runtimeInstallFixture(t, "darwin/arm64")
	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stuck := staleStaging(t, i.Dir, ".install-stuck")
	locked := filepath.Join(stuck, "locked")
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
	var notes []string
	i.Progress = func(p RuntimeProgress) {
		if p.Stage == "note" {
			notes = append(notes, p.Note)
		}
	}
	if _, err := i.Ensure(context.Background()); err != nil {
		t.Fatalf("a leftover that will not go stopped setup: %v", err)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], stuck) {
		t.Fatalf("notes = %q, want one naming %s", notes, stuck)
	}
}

// A start sweeps too, since a complete runtime or the user's own Ollama never
// reaches setup. It never waits on a setup in progress, never creates the
// storage directory, and never sweeps through a symlinked one.
func TestAStartSweepsWithoutWaitingOnASetup(t *testing.T) {
	t.Run("free", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "installations")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		stale := staleStaging(t, dir, ".install-killed")
		(&RuntimeInstaller{Dir: dir}).SweepStale()
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("the start left the killed setup's staging: %v", err)
		}
	})
	t.Run("a setup in progress", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "installations")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		other, err := lock.Acquire(context.Background(), filepath.Join(dir, "install.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer other.Close()
		live := staleStaging(t, dir, ".install-live")
		swept := make(chan struct{})
		go func() { (&RuntimeInstaller{Dir: dir}).SweepStale(); close(swept) }()
		select {
		case <-swept:
		case <-time.After(2 * time.Second):
			t.Fatal("the start waited on a setup in progress")
		}
		if _, err := os.Stat(live); err != nil {
			t.Fatalf("the start removed a live setup's staging: %v", err)
		}
	})
	t.Run("no storage yet", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "installations")
		(&RuntimeInstaller{Dir: dir}).SweepStale()
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("the start created the storage directory: %v", err)
		}
	})
	t.Run("a symlinked storage directory", func(t *testing.T) {
		real := t.TempDir()
		stale := staleStaging(t, real, ".install-killed")
		link := filepath.Join(t.TempDir(), "installations")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		(&RuntimeInstaller{Dir: link}).SweepStale()
		if _, err := os.Stat(stale); err != nil {
			t.Fatalf("the start swept through a symlinked storage directory: %v", err)
		}
	})
}

// The starter runs its housekeeping on a start, before discovery, so the
// paths that never provision (a complete runtime, the user's own Ollama) run
// it as well.
func TestAStarterTidiesBeforeItStarts(t *testing.T) {
	for _, host := range []Host{
		{State: HostInstalled, Managed: true, Binary: "/opt/ollama"},
		{State: HostRunning, Addr: "127.0.0.1:11434"},
	} {
		f := newStarterFixture(0, 43111)
		var order []string
		f.starter.Tidy = func(io.Writer) { order = append(order, "tidy") }
		f.starter.Discover = func(context.Context) Host { order = append(order, "discover"); return host }
		if _, err := f.starter.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
		_ = f.starter.Close()
		if len(order) < 2 || order[0] != "tidy" || order[1] != "discover" {
			t.Fatalf("host %v: order %v, want tidy before discovery", host.State, order)
		}
	}
}

// The start's sweep holds install.lock while it removes anything: a check that
// let the lock go first would let another setup take it, stage a download and
// lose it. A leftover that will not go is reported from inside the sweep, and
// the lock is held right there.
func TestTheStartSweepHoldsTheLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "installations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stuck := staleStaging(t, dir, ".install-stuck")
	locked := filepath.Join(stuck, "locked")
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
	checked := false
	i := &RuntimeInstaller{Dir: dir, Progress: func(p RuntimeProgress) {
		if p.Stage != "note" {
			return
		}
		checked = true
		if held, err := lock.Held(filepath.Join(dir, "install.lock")); err != nil || !held {
			t.Errorf("the sweep ran without install.lock held (%v, %v)", held, err)
		}
	}}
	i.SweepStale()
	if !checked {
		t.Fatal("the leftover that will not go was not reported")
	}
}

// Housekeeping runs once per start, on the sign-in path too, and writes where
// the starter writes: SetOutput moves it with everything else.
func TestAStarterTidiesOncePerStartIntoItsOwnOutput(t *testing.T) {
	for _, installed := range []bool{false, true} {
		f := newStarterFixture(0, 43111)
		surface := &strings.Builder{}
		f.starter.SetOutput(surface)
		var tidies int
		var wrote io.Writer
		f.starter.Tidy = func(out io.Writer) { tidies++; wrote = out }
		f.starter.Discover = func(context.Context) Host { return Host{State: HostInstalled, Managed: true, Binary: "/opt/ollama"} }
		for range 5 {
			var err error
			if installed {
				_, err = f.starter.EnsureInstalled(context.Background())
			} else {
				_, err = f.starter.Ensure(context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		_ = f.starter.Close()
		if tidies != 1 {
			t.Errorf("sign-in path %v: %d tidies for one start, want 1", installed, tidies)
		}
		if wrote != io.Writer(surface) {
			t.Errorf("sign-in path %v: housekeeping wrote to %T, not the starter's output", installed, wrote)
		}
	}
}

// A starter built without an output still tidies: housekeeping that writes a
// note must not panic on the nil writer a bare HostStarter has.
func TestAStarterWithoutAnOutputStillTidies(t *testing.T) {
	f := newStarterFixture(0, 43111)
	f.starter.Out = nil
	tidied := false
	f.starter.Tidy = func(out io.Writer) { tidied = true; _, _ = io.WriteString(out, "  ! a note\n") }
	f.starter.Discover = func(context.Context) Host { return Host{State: HostInstalled, Managed: true, Binary: "/opt/ollama"} }
	if _, err := f.starter.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = f.starter.Close()
	if !tidied {
		t.Fatal("no housekeeping ran")
	}
}
