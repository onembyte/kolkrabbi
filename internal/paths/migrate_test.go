package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyLayout builds a prototype-era install: sessions and the usage log
// sitting in the config directory, where they used to live.
func legacyLayout(t *testing.T) Dirs {
	t.Helper()
	base := t.TempDir()
	d := Dirs{
		Config: filepath.Join(base, "config"),
		Data:   filepath.Join(base, "data"),
		Cache:  filepath.Join(base, "cache"),
	}
	if err := os.MkdirAll(filepath.Join(d.Config, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d.Config, "sessions", "s_one.json"), `{"id":"s_one"}`)
	write(t, filepath.Join(d.Config, "sessions", "s_two.json"), `{"id":"s_two"}`)
	if err := os.MkdirAll(filepath.Join(d.Config, "sessions", "s_one.ckpt"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(d.Config, "sessions", "s_one.ckpt", "manifest.json"), `[]`)
	write(t, filepath.Join(d.Config, "stats.jsonl"), "{\"kind\":\"usage\"}\n")
	return d
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateMovesPrototypeState(t *testing.T) {
	d := legacyLayout(t)

	moved, err := d.Migrate()
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(moved) != 2 {
		t.Errorf("moved = %v, want both sessions and the usage log", moved)
	}

	// Everything arrived, including the checkpoint directory inside sessions.
	for _, p := range []string{
		filepath.Join(d.Sessions(), "s_one.json"),
		filepath.Join(d.Sessions(), "s_two.json"),
		filepath.Join(d.Sessions(), "s_one.ckpt", "manifest.json"),
		d.StatsFile(),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing after migration: %s", p)
		}
	}
	// And the old copies are gone, so there is one history, not two.
	for _, p := range []string{
		filepath.Join(d.Config, "sessions"),
		filepath.Join(d.Config, "stats.jsonl"),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("still present in the old location: %s", p)
		}
	}
}

func TestMigrateIsANoOpTheSecondTime(t *testing.T) {
	d := legacyLayout(t)
	if _, err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	moved, err := d.Migrate()
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(moved) != 0 {
		t.Errorf("second run moved %v; migration must happen once", moved)
	}
}

func TestMigrateOnAFreshInstallDoesNothing(t *testing.T) {
	base := t.TempDir()
	d := Dirs{Config: filepath.Join(base, "c"), Data: filepath.Join(base, "d"), Cache: filepath.Join(base, "x")}
	moved, err := d.Migrate()
	if err != nil {
		t.Fatalf("Migrate on a fresh install: %v", err)
	}
	if len(moved) != 0 {
		t.Errorf("moved %v on a machine with nothing to move", moved)
	}
}

// The one case that could destroy something irreplaceable: state exists in both
// places. Refuse and report, rather than guessing which history is real.
func TestMigrateRefusesToOverwrite(t *testing.T) {
	d := legacyLayout(t)
	write(t, filepath.Join(d.Sessions(), "s_new.json"), `{"id":"s_new"}`)

	moved, err := d.Migrate()
	if err == nil {
		t.Fatal("Migrate must report a collision, not merge two histories")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("the error should say the state exists in both places: %v", err)
	}

	// The new location is untouched and the old one is still there to rescue.
	if _, statErr := os.Stat(filepath.Join(d.Sessions(), "s_new.json")); statErr != nil {
		t.Error("the existing session was destroyed by a refused migration")
	}
	if _, statErr := os.Stat(filepath.Join(d.Config, "sessions", "s_one.json")); statErr != nil {
		t.Error("the old session was removed even though the migration was refused")
	}
	// The usage log has no collision, so it should still have moved.
	if len(moved) != 1 || moved[0] != "usage log" {
		t.Errorf("moved = %v; a collision on one item must not block the others", moved)
	}
}

func TestMigrateIsANoOpWhenConfigAndDataAreTheSame(t *testing.T) {
	base := t.TempDir()
	d := Dirs{Config: base, Data: base, Cache: base}
	write(t, filepath.Join(base, "stats.jsonl"), "{}\n")

	moved, err := d.Migrate()
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("moved %v; there is nowhere to move to", moved)
	}
	if _, err := os.Stat(filepath.Join(base, "stats.jsonl")); err != nil {
		t.Error("the usage log was destroyed by a migration with nothing to do")
	}
}

// copyTree is the cross-filesystem path. It is exercised directly because the
// only way to reach it in an ordinary test run is a rename failure.
func TestCopyTreePreservesContentAndModes(t *testing.T) {
	base := t.TempDir()
	from := filepath.Join(base, "from")
	to := filepath.Join(base, "to")
	write(t, filepath.Join(from, "a.json"), "one")
	write(t, filepath.Join(from, "sub", "b.json"), "two")

	if err := copyTree(from, to); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"a.json": "one", "sub/b.json": "two"} {
		b, err := os.ReadFile(filepath.Join(to, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", rel, b, want)
		}
	}
}

// A symlink in the state directory was not put there by kolk, so following it
// would move a file the user never meant to move.
func TestCopyTreeRefusesASymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real.json")
	write(t, target, "{}")
	link := filepath.Join(base, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := copyTree(link, filepath.Join(base, "copied.json"))
	if err == nil {
		t.Fatal("copyTree followed a symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("the error should name the problem: %v", err)
	}
}
