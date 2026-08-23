package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// ErrNotFound is returned when no key is stored for a provider.
var ErrNotFound = errors.New("no key stored")

// Store keeps keys for named providers.
//
// It is an interface because the file store is the default, not the only
// option: an OS keychain is an opt-in alternative, and a test wants neither.
// The default is a 0600 file rather than the keychain, deliberately — a
// keychain prompt on first run is exactly the setup questionnaire the product
// exists to avoid.
type Store interface {
	Get(provider string) (Secret, error)
	Set(provider string, key Secret) error
	Delete(provider string) error
	Providers() ([]string, error)

	// Name describes where keys are kept, for `kolk key` and `kolk doctor`.
	Name() string
}

// FileStore keeps keys in one JSON file, 0600, inside the data directory.
type FileStore struct {
	Path string

	mu sync.Mutex
}

// NewFileStore returns a store backed by the given file.
func NewFileStore(path string) *FileStore { return &FileStore{Path: path} }

func (s *FileStore) Name() string { return s.Path }

// manifest is the on-disk shape. Keys are stored raw — that is the entire job —
// so this type is never logged, never embedded in another struct, and never
// printed. Everything that leaves this file does so as a Secret.
type manifest struct {
	Version int               `json:"version"`
	Keys    map[string]string `json:"keys"`
}

// load reads the manifest. A missing file is an empty manifest: no credentials
// yet is the normal state of a fresh install, not an error.
func (s *FileStore) load() (*manifest, error) {
	// Lstat first, and refuse a symlink rather than following it. A symlinked
	// credentials file is either something the user did not intend or something
	// pointed at kolk deliberately; either way, writing a key through it sends
	// the key somewhere kolk did not choose.
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &manifest{Version: 1, Keys: map[string]string{}}, nil
	case err != nil:
		return nil, err
	case info.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("%s is a symlink; kolk will not read or write credentials through one", s.Path)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", s.Path)
	}

	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return &manifest{Version: 1, Keys: map[string]string{}}, nil
	}

	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		// Never quote the file's contents in the error. It is the one file in
		// the tree whose contents must not reach a terminal.
		return nil, fmt.Errorf("%s is not valid JSON; delete it and run `kolk key` again", s.Path)
	}
	if m.Keys == nil {
		m.Keys = map[string]string{}
	}
	return &m, nil
}

func (s *FileStore) save(m *manifest) error {
	m.Version = 1
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// 0600, and atomic: a credentials file truncated by a crash reads to the
	// user as kolk losing their key.
	return atomicfile.Write(s.Path, append(b, '\n'), 0o600)
}

// Get returns the key stored for a provider.
func (s *FileStore) Get(provider string) (Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return Secret{}, err
	}
	raw, ok := m.Keys[normalise(provider)]
	if !ok || strings.TrimSpace(raw) == "" {
		return Secret{}, fmt.Errorf("%s: %w", provider, ErrNotFound)
	}
	return New(raw), nil
}

// Set stores a key, replacing any previous one for that provider.
func (s *FileStore) Set(provider string, key Secret) error {
	if key.IsZero() {
		return fmt.Errorf("refusing to store an empty key for %s", provider)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return err
	}
	m.Keys[normalise(provider)] = key.Reveal()
	return s.save(m)
}

// Delete removes a provider's key. Deleting one that is not there is not an
// error: `kolk logout` should succeed on a machine that was never logged in.
func (s *FileStore) Delete(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return err
	}
	delete(m.Keys, normalise(provider))
	return s.save(m)
}

// Providers lists the providers with a stored key, sorted.
func (s *FileStore) Providers() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m.Keys))
	for name, raw := range m.Keys {
		if strings.TrimSpace(raw) != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// normalise makes provider names case- and whitespace-insensitive, so
// `kolk key OpenRouter …` and `kolk key openrouter …` are the same account
// rather than two half-configured ones.
func normalise(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
