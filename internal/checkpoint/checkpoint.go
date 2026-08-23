// Package checkpoint records the state of files before the agent modifies
// them (via write_file/edit_file), so changes can be rewound turn by turn —
// same idea as Claude Code's checkpoints. It cannot track changes made by
// arbitrary bash commands; only the agent's own file tools are covered.
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

type Entry struct {
	Seq     int       `json:"seq"`
	Turn    int       `json:"turn"`
	Tool    string    `json:"tool"`
	Path    string    `json:"path"`
	Existed bool      `json:"existed"`
	Backup  string    `json:"backup,omitempty"` // filename within the store dir
	Time    time.Time `json:"time"`
}

type manifest struct {
	Turn    int     `json:"turn"`
	Seq     int     `json:"seq"`
	Entries []Entry `json:"entries"`
}

type Store struct {
	dir     string
	turn    int
	seq     int
	entries []Entry
}

// Open creates or reopens a checkpoint store at dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	b, err := os.ReadFile(s.manifestPath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint manifest: %w", err)
	}
	s.turn, s.seq, s.entries = m.Turn, m.Seq, m.Entries
	return s, nil
}

func (s *Store) manifestPath() string { return filepath.Join(s.dir, "manifest.json") }

func (s *Store) saveManifest() error {
	b, err := json.MarshalIndent(manifest{Turn: s.turn, Seq: s.seq, Entries: s.entries}, "", " ")
	if err != nil {
		return err
	}
	// The manifest is the index of every backup: lose it half-written and the
	// backups on disk become unreachable, which is the same as losing them.
	return atomicfile.Write(s.manifestPath(), b, 0o600)
}

// BeginTurn marks the start of a new user turn; subsequent Records belong to it.
func (s *Store) BeginTurn() { s.turn++ }

// Record snapshots the current state of path before it is modified. Call it
// after user confirmation but before the actual write/edit.
func (s *Store) Record(tool, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	content, err := os.ReadFile(abs)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s.seq++
	e := Entry{Seq: s.seq, Turn: s.turn, Tool: tool, Path: abs, Existed: existed, Time: time.Now()}
	if existed {
		e.Backup = fmt.Sprintf("%06d.bak", s.seq)
		if err := os.WriteFile(filepath.Join(s.dir, e.Backup), content, 0o600); err != nil {
			return err
		}
	}
	s.entries = append(s.entries, e)
	return s.saveManifest()
}

// RewindLastTurn restores every file changed in the most recent turn that has
// recorded changes, newest change first, and drops those entries. Returns the
// restored paths, or (nil, nil) if there is nothing to rewind.
func (s *Store) RewindLastTurn() ([]string, error) {
	last := -1
	for _, e := range s.entries {
		if e.Turn > last {
			last = e.Turn
		}
	}
	if last == -1 {
		return nil, nil
	}

	var keep, undo []Entry
	for _, e := range s.entries {
		if e.Turn == last {
			undo = append(undo, e)
		} else {
			keep = append(keep, e)
		}
	}

	var restored []string
	for i := len(undo) - 1; i >= 0; i-- {
		e := undo[i]
		if !e.Existed {
			if err := os.Remove(e.Path); err != nil && !os.IsNotExist(err) {
				return restored, err
			}
		} else {
			data, err := os.ReadFile(filepath.Join(s.dir, e.Backup))
			if err != nil {
				return restored, fmt.Errorf("missing backup for %s: %w", e.Path, err)
			}
			if err := os.MkdirAll(filepath.Dir(e.Path), 0o755); err != nil {
				return restored, err
			}
			if err := os.WriteFile(e.Path, data, 0o644); err != nil {
				return restored, err
			}
		}
		if e.Backup != "" {
			// Best effort: the file is already restored, so a backup that
			// cannot be deleted is litter, not a failed rewind.
			_ = os.Remove(filepath.Join(s.dir, e.Backup))
		}
		restored = append(restored, e.Path)
	}

	s.entries = keep
	return restored, s.saveManifest()
}

// Changes returns a copy of all recorded (not yet rewound) changes, in order.
func (s *Store) Changes() []Entry {
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}
