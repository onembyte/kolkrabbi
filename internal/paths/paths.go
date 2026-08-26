// Package paths resolves where kolk keeps things.
//
// It is the only package in the tree permitted to call os.UserHomeDir or
// os.UserConfigDir — enforced by internal/arch — because a directory decided in
// two places is a directory that eventually differs in two places, and the
// thing that differs is the user's data.
//
// The split is config / data / cache, and it matters more than it looks:
//
//   - Config is what a person edits and might commit to a dotfiles repo.
//   - Data is state they would be upset to lose and must never commit:
//     sessions, checkpoints, the stats log, credentials. Credentials are
//     state, not config — which on Windows is the difference between
//     %LocalAppData% and %AppData%, and %AppData% roams to a domain profile
//     server.
//   - Cache is anything kolk can rebuild by asking the network again.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// app is the single directory name every platform appends. One constant, so a
// typo cannot silently create a second home for someone's sessions.
const app = "kolk"

// Environment variables that override everything, on every platform. These
// exist so a test, a container or a second profile never has to touch $HOME.
const (
	EnvConfigDir = "KOLK_CONFIG_DIR"
	EnvDataDir   = "KOLK_DATA_DIR"
	EnvCacheDir  = "KOLK_CACHE_DIR"
)

// Dirs is one resolved set of directories.
type Dirs struct {
	Config string // settings a person edits
	Data   string // state they would be upset to lose, and must never commit
	Cache  string // anything kolk can rebuild from the network
}

// UserHomeDir is the platform-owned home-directory seam for callers that
// need a display path without duplicating OS discovery outside this package.
func UserHomeDir() (string, error) { return os.UserHomeDir() }

// Resolve computes the directories for the current user and environment.
//
// It fails only when there is no home directory AND no override, which on a
// real machine means something is badly wrong — and is still reported rather
// than papered over with a relative path that would scatter state into
// whatever directory kolk happened to be run from.
func Resolve() (Dirs, error) {
	home, homeErr := UserHomeDir()
	d := resolve(os.Getenv, home)

	if d.Config == "" || d.Data == "" || d.Cache == "" {
		if homeErr != nil {
			return Dirs{}, fmt.Errorf("cannot locate your home directory: %w\n"+
				"set %s, %s and %s to choose explicitly", homeErr, EnvConfigDir, EnvDataDir, EnvCacheDir)
		}
		return Dirs{}, fmt.Errorf("cannot locate your home directory; set %s, %s and %s to choose explicitly",
			EnvConfigDir, EnvDataDir, EnvCacheDir)
	}
	return d, nil
}

// override returns a cleaned KOLK_*_DIR value, or "" if it is unset or blank.
// A relative override is resolved against the working directory, so
// KOLK_DATA_DIR=./state does what it looks like it does.
func override(getenv func(string) string, key string) string {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return ""
	}
	if abs, err := filepath.Abs(v); err == nil {
		return abs
	}
	return filepath.Clean(v)
}

// ConfigFile is the settings file a person may edit or symlink into dotfiles.
func (d Dirs) ConfigFile() string { return filepath.Join(d.Config, "config.json") }

// Sessions holds one JSON file per conversation.
func (d Dirs) Sessions() string { return filepath.Join(d.Data, "sessions") }

// Session is one conversation's file.
func (d Dirs) Session(id string) string { return filepath.Join(d.Sessions(), id+".json") }

// Checkpoints is where a session's file backups live, so a turn can be undone.
func (d Dirs) Checkpoints(id string) string { return filepath.Join(d.Sessions(), id+".ckpt") }

// StatsFile is the append-only local usage log. Nothing in it ever leaves the
// machine, which is a promise the location is part of keeping.
func (d Dirs) StatsFile() string { return filepath.Join(d.Data, "stats.jsonl") }

// CredentialsFile holds API keys when no OS keychain is in use.
//
// It lives in Data, not Config: a key is state, not a setting. Someone who
// symlinks their config directory into a public dotfiles repo must not thereby
// publish their key, and on Windows this is the difference between
// %LocalAppData% and a %AppData% that roams to a domain profile server.
func (d Dirs) CredentialsFile() string { return filepath.Join(d.Data, "credentials.json") }

// ConnectorsFile stores only provider-owned connector metadata, never secrets.
func (d Dirs) ConnectorsFile() string { return filepath.Join(d.Data, "connectors.json") }

// MemoryFile is the user's own standing notes, applied to every project. It
// lives with configuration because it is a preference, not session state.
func (d Dirs) MemoryFile() string { return filepath.Join(d.Config, "memory.md") }

// LocalModelsDir stores models and managed local-runtime state.
func (d Dirs) LocalModelsDir() string { return filepath.Join(d.Data, "local-models") }

// LocalRuntimeDir stores versioned runtime binaries owned by Kolk.
func (d Dirs) LocalRuntimeDir() string { return filepath.Join(d.Data, "local-runtime") }

// CatalogFile is the cached model catalog: rebuildable, so it lives in Cache.
func (d Dirs) CatalogFile() string { return filepath.Join(d.Cache, "models.json") }

// EnsureConfig creates the config directory. 0700 because the prototype wrote
// keys here and old installs still do.
func (d Dirs) EnsureConfig() error { return mkdir(d.Config) }

// EnsureCache creates the cache directory.
func (d Dirs) EnsureCache() error { return mkdir(d.Cache) }

// EnsureData creates the data directory and drops a .gitignore in it.
//
// The .gitignore is not paranoia: KOLK_DATA_DIR makes it legal to point state
// at a directory inside a repository, and the failure mode of getting that
// wrong is a published API key.
func (d Dirs) EnsureData() error {
	if err := mkdir(d.Data); err != nil {
		return err
	}
	p := filepath.Join(d.Data, ".gitignore")
	if _, err := os.Lstat(p); err == nil {
		return nil // already there; never overwrite what the user may have edited
	}
	return atomicfile.Write(p, []byte(gitignore), 0o600)
}

const gitignore = `# kolk keeps state here. None of it belongs in a repository.
credentials.json
connectors.json
local-models/
local-runtime/
sessions/
stats.jsonl
dash.db
dash.db-wal
dash.db-shm
`

func mkdir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	return nil
}

// String renders the three directories for `kolk doctor` and bug reports.
func (d Dirs) String() string {
	return fmt.Sprintf("config: %s\ndata:   %s\ncache:  %s", d.Config, d.Data, d.Cache)
}
