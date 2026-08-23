package keystore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

const legacyConfigKey = "sk-or-v1-fedcba9876543210fedcba9876543210"

func legacyConfigFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "config.json")
	manifestPath := filepath.Join(dir, "data", "credentials.json")
	body, err := os.ReadFile(filepath.Join("testdata", "v0-config-with-key.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, manifestPath
}

func TestLegacyConfigKeyMigratesOnceWithoutLosingSettings(t *testing.T) {
	configPath, manifestPath := legacyConfigFixture(t)
	store := NewFileStore(manifestPath)

	moved, err := store.MigrateLegacyConfig(context.Background(), configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("MigrateLegacyConfig reported no migration")
	}
	got, err := store.Get(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reveal() != legacyConfigKey {
		t.Errorf("manifest credential = %v", got)
	}
	entry, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source != "legacy-config" || !entry.Verified.IsZero() {
		t.Errorf("migration metadata = %+v", entry)
	}

	rewritten, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), legacyConfigKey) || strings.Contains(string(rewritten), "api_key") {
		t.Errorf("rewritten config still contains credential material:\n%s", rewritten)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &settings); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model", "base_url", "tiers", "future_setting"} {
		if _, ok := settings[name]; !ok {
			t.Errorf("migration lost setting %q: %s", name, rewritten)
		}
	}

	before, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	moved, err = store.MigrateLegacyConfig(context.Background(), configPath)
	if err != nil || moved {
		t.Fatalf("second migration = moved %v, err %v; want no-op", moved, err)
	}
	after, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if after.Created != before.Created || after.KeyHash != before.KeyHash {
		t.Errorf("idempotent migration rewrote the manifest: before=%+v after=%+v", before, after)
	}
}

func TestLegacyMigrationNeverOverwritesADifferentManifestKey(t *testing.T) {
	configPath, manifestPath := legacyConfigFixture(t)
	store := NewFileStore(manifestPath)
	const currentKey = "sk-or-v1-0123456789abcdef0123456789abcdef"
	if err := store.Set(context.Background(), Ref{Provider: "openrouter"}, secret.New(currentKey)); err != nil {
		t.Fatal(err)
	}

	moved, err := store.MigrateLegacyConfig(context.Background(), configPath)
	if moved || !errors.Is(err, ErrMigrationConflict) {
		t.Fatalf("migration = moved %v, err %v; want ErrMigrationConflict", moved, err)
	}
	got, getErr := store.Get(context.Background(), Ref{Provider: "openrouter"})
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Reveal() != currentKey {
		t.Errorf("migration overwrote the current manifest credential: %v", got)
	}
	legacy, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(legacy), legacyConfigKey) {
		t.Error("conflict discarded the only copy of the legacy credential")
	}
}

func TestLegacyMigrationRemovesAnAlreadyCopiedKeyWithoutRewritingIt(t *testing.T) {
	configPath, manifestPath := legacyConfigFixture(t)
	store := NewFileStore(manifestPath)
	created := time.Date(2025, time.December, 1, 2, 3, 4, 0, time.UTC)
	store.now = func() time.Time { return created }
	if err := store.Set(context.Background(), Ref{Provider: "openrouter"}, secret.New(legacyConfigKey)); err != nil {
		t.Fatal(err)
	}

	store.now = func() time.Time { return created.Add(24 * time.Hour) }
	moved, err := store.MigrateLegacyConfig(context.Background(), configPath)
	if err != nil || !moved {
		t.Fatalf("migration = moved %v, err %v", moved, err)
	}
	entry, err := store.Probe(context.Background(), Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Created.Equal(created) {
		t.Errorf("already-copied credential was overwritten: Created = %v, want %v", entry.Created, created)
	}
	configBody, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configBody), "api_key") {
		t.Errorf("duplicate legacy field was not removed: %s", configBody)
	}
}

func TestCanceledLegacyMigrationTouchesNothing(t *testing.T) {
	configPath, manifestPath := legacyConfigFixture(t)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	moved, err := NewFileStore(manifestPath).MigrateLegacyConfig(ctx, configPath)
	if moved || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration = moved %v, err %v", moved, err)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Error("canceled migration rewrote config")
	}
	if _, statErr := os.Stat(manifestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("canceled migration created a manifest: %v", statErr)
	}
}

func TestLegacyMigrationCopiesBeforeAConfigRewriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission bits do not make a directory unwritable")
	}
	configPath, manifestPath := legacyConfigFixture(t)
	configDir := filepath.Dir(configPath)
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDir, 0o700) })

	moved, err := NewFileStore(manifestPath).MigrateLegacyConfig(context.Background(), configPath)
	if moved || err == nil {
		t.Fatalf("migration = moved %v, err %v; want a config rewrite failure", moved, err)
	}
	stored, getErr := NewFileStore(manifestPath).Get(context.Background(), Ref{Provider: "openrouter"})
	if getErr != nil {
		t.Fatalf("credential was not copied before rewrite: %v", getErr)
	}
	if stored.Reveal() != legacyConfigKey {
		t.Errorf("copied credential = %v", stored)
	}
	legacy, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(legacy), legacyConfigKey) {
		t.Error("failed rewrite removed the source credential")
	}
}

func TestLegacyMigrationPreservesAConfigSymlink(t *testing.T) {
	configPath, manifestPath := legacyConfigFixture(t)
	target := filepath.Join(filepath.Dir(configPath), "dotfiles-config.json")
	if err := os.Rename(configPath, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), configPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	moved, err := NewFileStore(manifestPath).MigrateLegacyConfig(context.Background(), configPath)
	if err != nil || !moved {
		t.Fatalf("symlink migration = moved %v, err %v", moved, err)
	}
	info, err := os.Lstat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("migration replaced the user's config symlink")
	}
	targetBody, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(targetBody), "api_key") || strings.Contains(string(targetBody), legacyConfigKey) {
		t.Errorf("symlink target retained credential material: %s", targetBody)
	}
}
