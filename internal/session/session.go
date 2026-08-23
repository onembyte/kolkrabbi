// Package session persists conversations to disk so they survive restarts
// and can be resumed. Each session is a single JSON file under the sessions
// directory; file-change checkpoints for a session live in a sibling
// "<id>.ckpt" directory.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type Session struct {
	ID        string             `json:"id"`
	Model     string             `json:"model"`
	Title     string             `json:"title"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Messages  []provider.Message `json:"messages"`

	dir string // where this session is stored; not serialized
}

// New creates a fresh, not-yet-saved session in dir.
func New(dir, model string) *Session {
	b := make([]byte, 2)
	rand.Read(b)
	now := time.Now()
	return &Session{
		ID:        now.Format("20060102-150405") + "-" + hex.EncodeToString(b),
		Model:     model,
		CreatedAt: now,
		dir:       dir,
	}
}

func (s *Session) path() string { return filepath.Join(s.dir, s.ID+".json") }

// CkptDir is where this session's file checkpoints are stored.
func (s *Session) CkptDir() string { return filepath.Join(s.dir, s.ID+".ckpt") }

// Save writes the session atomically (tmp file + rename).
func (s *Session) Save() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// SetTitleFromInput sets a human-readable title from the first user message.
func (s *Session) SetTitleFromInput(input string) {
	if s.Title != "" {
		return
	}
	t := strings.Join(strings.Fields(input), " ")
	if len(t) > 60 {
		t = t[:60] + "…"
	}
	s.Title = t
}

func Load(dir, id string) (*Session, error) {
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	s.dir = dir
	return &s, nil
}

// List returns all sessions in dir, newest first.
func List(dir string) ([]*Session, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		s, err := Load(dir, strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue // skip corrupt files rather than failing the whole list
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Latest returns the most recently updated session, or nil if none exist.
func Latest(dir string) (*Session, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[0], nil
}

// Delete removes a session file and its checkpoint directory.
func Delete(dir, id string) error {
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	// RemoveAll is nil for a path that does not exist, so this only reports a
	// checkpoint directory that really could not be removed — which matters,
	// because a stale .ckpt outlives the session it belonged to.
	return os.RemoveAll(filepath.Join(dir, id+".ckpt"))
}

// Clear removes all sessions and checkpoints in dir.
func Clear(dir string) error {
	all, err := List(dir)
	if err != nil {
		return err
	}
	for _, s := range all {
		if err := Delete(dir, s.ID); err != nil {
			return fmt.Errorf("deleting %s: %w", s.ID, err)
		}
	}
	return nil
}
