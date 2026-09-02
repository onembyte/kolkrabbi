package keystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

type diskManifest struct {
	Version     int                  `json:"version"`
	Credentials map[string]diskEntry `json:"credentials"`
}

type diskEntry struct {
	Backend  Backend   `json:"backend"`
	Value    string    `json:"value,omitempty"`
	Mask     string    `json:"mask"`
	KeyHash  string    `json:"key_hash"`
	Machine  string    `json:"machine,omitempty"`
	Created  time.Time `json:"created"`
	Verified time.Time `json:"verified,omitempty"`
	Source   string    `json:"source,omitempty"`
	Note     string    `json:"note,omitempty"`
}

// FileStore is the default backend on every OS. Path is normally
// paths.Dirs.CredentialsFile(); taking it explicitly keeps tests and containers
// independent from the process environment.
type FileStore struct {
	Path string

	now      func() time.Time
	hostname func() (string, error)
}

func NewFileStore(path string) *FileStore {
	return &FileStore{Path: path, now: time.Now, hostname: os.Hostname}
}

func (s *FileStore) Name() Backend { return BackendFile }

func (s *FileStore) Available(ctx context.Context) error { return ctx.Err() }

func (s *FileStore) Get(ctx context.Context, ref Ref) (secret.Secret, error) {
	if err := ctx.Err(); err != nil {
		return secret.Secret{}, err
	}
	ref, err := canonicalRef(ref)
	if err != nil {
		return secret.Secret{}, err
	}
	m, err := s.load()
	if err != nil {
		return secret.Secret{}, err
	}
	row, ok := m.Credentials[ref.String()]
	if !ok {
		return secret.Secret{}, fmt.Errorf("%s: %w", ref, ErrNotFound)
	}
	if row.Backend != BackendFile {
		return secret.Secret{}, fmt.Errorf("credential backend for %s: %w", ref, ErrUnavailable)
	}
	return decodeValue(row.Value)
}

func (s *FileStore) Set(ctx context.Context, ref Ref, value secret.Secret) error {
	return s.SetWithMetadata(ctx, ref, value, WriteMetadata{})
}

// SetWithMetadata stores one value and its safe acquisition facts in the same
// locked atomic manifest commit.
func (s *FileStore) SetWithMetadata(ctx context.Context, ref Ref, value secret.Secret, meta WriteMetadata) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := canonicalRef(ref)
	if err != nil {
		return err
	}
	row, err := s.newDiskEntry(value, meta)
	if err != nil {
		return err
	}
	if err := s.ensureDir(); err != nil {
		return err
	}
	held, err := lock.Acquire(ctx, s.Path+".lock")
	if err != nil {
		return err
	}
	defer func() { _ = held.Close() }()

	m, err := s.load()
	if err != nil {
		return err
	}
	m.Credentials[ref.String()] = row
	return s.save(m)
}

func (s *FileStore) newDiskEntry(value secret.Secret, meta WriteMetadata) (diskEntry, error) {
	encoded, err := encodeValue(value)
	if err != nil {
		return diskEntry{}, err
	}
	machine, _ := s.hostname()
	return diskEntry{
		Backend:  BackendFile,
		Value:    encoded,
		Mask:     secret.Redact(value.Reveal()),
		KeyHash:  hashValue(value),
		Machine:  machine,
		Created:  s.now().UTC(),
		Verified: meta.Verified.UTC(),
		Source:   meta.Source,
		Note:     meta.Note,
	}, nil
}

func (s *FileStore) Del(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := canonicalRef(ref)
	if err != nil {
		return err
	}
	if err := s.ensureDir(); err != nil {
		return err
	}
	held, err := lock.Acquire(ctx, s.Path+".lock")
	if err != nil {
		return err
	}
	defer func() { _ = held.Close() }()

	m, err := s.load()
	if err != nil {
		return err
	}
	row, ok := m.Credentials[ref.String()]
	if !ok {
		return nil
	}
	if row.Backend != BackendFile {
		return fmt.Errorf("credential backend for %s: %w", ref, ErrUnavailable)
	}
	delete(m.Credentials, ref.String())
	return s.save(m)
}

func (s *FileStore) Probe(ctx context.Context, ref Ref) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	ref, err := canonicalRef(ref)
	if err != nil {
		return Entry{}, err
	}
	m, err := s.load()
	if err != nil {
		return Entry{}, err
	}
	row, ok := m.Credentials[ref.String()]
	if !ok {
		return Entry{}, fmt.Errorf("%s: %w", ref, ErrNotFound)
	}
	return metadata(ref, row)
}

func (s *FileStore) List(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(m.Credentials))
	for key, row := range m.Credentials {
		ref, err := parseRef(key)
		if err != nil {
			return nil, fmt.Errorf("credential manifest contains an invalid reference: %w", ErrCorrupt)
		}
		entry, err := metadata(ref, row)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.String() < out[j].Ref.String() })
	return out, nil
}

func metadata(ref Ref, row diskEntry) (Entry, error) {
	if row.Backend != BackendFile {
		return Entry{}, fmt.Errorf("credential backend for %s: %w", ref, ErrUnavailable)
	}
	return Entry{
		Ref: ref, Backend: row.Backend, Mask: row.Mask, KeyHash: row.KeyHash,
		Machine: row.Machine, Created: row.Created, Verified: row.Verified,
		Source: row.Source, Note: row.Note,
	}, nil
}

func (s *FileStore) load() (*diskManifest, error) {
	info, err := os.Lstat(s.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return emptyManifest(), nil
	case err != nil:
		return nil, fmt.Errorf("reading credential manifest %s: %w", s.Path, err)
	case info.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("%s is a symlink: %w", s.Path, ErrCorrupt)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file: %w", s.Path, ErrCorrupt)
	}
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return nil, fmt.Errorf("repairing credential permissions on %s: %w", s.Path, err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("reading credential manifest %s: %w", s.Path, err)
	}
	var m diskManifest
	if len(b) == 0 || json.Unmarshal(b, &m) != nil {
		return nil, fmt.Errorf("%s is not valid credential JSON; run `/key` to replace it: %w", s.Path, ErrCorrupt)
	}
	if m.Version != manifestVersion {
		return nil, fmt.Errorf("%s has version %d, want %d: %w", s.Path, m.Version, manifestVersion, ErrVersion)
	}
	if m.Credentials == nil {
		m.Credentials = map[string]diskEntry{}
	}
	return &m, nil
}

func (s *FileStore) save(m *diskManifest) error {
	m.Version = manifestVersion
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding credential manifest: %w", err)
	}
	return atomicfile.Write(s.Path, append(b, '\n'), 0o600)
}

func (s *FileStore) ensureDir() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating credential directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("setting credential directory permissions on %s: %w", dir, err)
	}
	return nil
}

func emptyManifest() *diskManifest {
	return &diskManifest{Version: manifestVersion, Credentials: map[string]diskEntry{}}
}
