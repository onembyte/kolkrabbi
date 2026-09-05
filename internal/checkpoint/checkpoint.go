// Package checkpoint records the state of files before the agent modifies
// them (via write_file/edit_file), so changes can be rewound turn by turn —
// same idea as Claude Code's checkpoints. It cannot track changes made by
// arbitrary bash commands; only the agent's own file tools are covered.
package checkpoint

import (
	"context"
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
	// Mode is the permission bits the file had when it was recorded, so a
	// restore that has to recreate it (the file was removed since) gives it
	// back its own mode rather than inventing one. Zero in manifests written
	// before this field existed; the restore then keeps whatever mode the file
	// has now, and only invents 0644 when there is no file at all.
	Mode os.FileMode `json:"mode,omitempty"`
}

type manifest struct {
	Turn    int     `json:"turn"`
	Seq     int     `json:"seq"`
	Entries []Entry `json:"entries"`
	// Snapshots maps a turn to the shadow commit taken at its start. A turn
	// appears here or in Entries, never both: which one says how it was
	// captured, so a session that gained or lost `git` half-way through still
	// rewinds each turn the way that turn was recorded.
	Snapshots map[int]string `json:"snapshots,omitempty"`
	// Tasks are the per-subagent snapshots, so a rewind survives the session
	// that took them: the commits live in the shadow store either way, and a
	// list nothing records is a list no rewind can find.
	Tasks []TaskSnapshot `json:"tasks,omitempty"`
}

type Store struct {
	dir     string
	turn    int
	seq     int
	entries []Entry

	// shadow is the whole-tree snapshot store, when the project qualifies for
	// one. nil means this session captures turns by copying files before they
	// are written, which misses everything `bash` does.
	shadow    *Shadow
	fellBack  string
	snapshots map[int]string
	// tasks are the per-subagent snapshots of this session, in the order the
	// run took them (A33.8).
	tasks []TaskSnapshot
}

// Open creates or reopens a checkpoint store at dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, snapshots: map[int]string{}}
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
	s.turn, s.seq, s.entries, s.snapshots, s.tasks = m.Turn, m.Seq, m.Entries, m.Snapshots, m.Tasks
	if s.snapshots == nil {
		s.snapshots = map[int]string{}
	}
	return s, nil
}

func (s *Store) manifestPath() string { return filepath.Join(s.dir, "manifest.json") }

func (s *Store) saveManifest() error {
	b, err := json.MarshalIndent(manifest{Turn: s.turn, Seq: s.seq, Entries: s.entries, Snapshots: s.snapshots, Tasks: s.tasks}, "", " ")
	if err != nil {
		return err
	}
	// The manifest is the index of every backup: lose it half-written and the
	// backups on disk become unreachable, which is the same as losing them.
	return atomicfile.Write(s.manifestPath(), b, 0o600)
}

// BeginTurn marks the start of a new user turn; subsequent Records belong to it.
// BeginTurn opens a new turn and, when a shadow store is attached, snapshots
// the whole work tree as it is before anything in this turn runs. Per turn is
// the cadence item 32 chose: a whole-tree snapshot already contains every path,
// so re-taking one per tool call would multiply the cost to record states
// nothing can address.
func (s *Store) BeginTurn(ctx context.Context) {
	s.turn++
	s.snapshotTurn(ctx, s.turn)
}

// Record snapshots the current state of path before it is modified. Call it
// after user confirmation but before the actual write/edit.
func (s *Store) Record(tool, path string) error {
	// Under the shadow strategy the turn's opening snapshot already contains
	// every path, so copying one file before it is written records a state
	// nothing can address — and would make the same turn recoverable two ways,
	// which is how the two stores could disagree.
	if s.shadow != nil {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	content, err := os.ReadFile(abs)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var mode os.FileMode
	if existed {
		if info, err := os.Stat(abs); err == nil {
			mode = info.Mode().Perm()
		}
	}
	s.seq++
	e := Entry{Seq: s.seq, Turn: s.turn, Tool: tool, Path: abs, Existed: existed, Time: time.Now(), Mode: mode}
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
func (s *Store) RewindLastTurn(ctx context.Context) ([]string, error) {
	last := -1
	for _, e := range s.entries {
		if e.Turn > last {
			last = e.Turn
		}
	}
	for turn := range s.snapshots {
		if turn > last {
			last = turn
		}
	}
	if last == -1 {
		return nil, nil
	}
	if commit, ok := s.snapshots[last]; ok {
		return s.rewindSnapshot(ctx, last, commit)
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
			if err := writeRestored(e.Path, data, e.Mode); err != nil {
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

// ChangedPaths lists each file this session touched, once, in the order it was
// first touched.
//
// A file edited three times is one changed file. First-touched order is the
// order someone will look for it: it is the order things happened in.
func (s *Store) ChangedPaths() []string {
	seen := make(map[string]bool, len(s.entries))
	paths := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		if seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		paths = append(paths, e.Path)
	}
	return paths
}

// Original returns a file's contents as they were before this session first
// touched it, and whether it existed then.
//
// The *first* backup, not the most recent one: a session diff answers "what has
// this session done to my file", and against the previous edit it would answer
// "what did the last turn do", which is a different and much less useful
// question.
func (s *Store) Original(path string) ([]byte, bool, error) {
	for _, e := range s.entries {
		if e.Path != path {
			continue
		}
		if !e.Existed {
			return nil, false, nil
		}
		content, err := os.ReadFile(filepath.Join(s.dir, e.Backup))
		if err != nil {
			return nil, true, fmt.Errorf("missing backup for %s: %w", path, err)
		}
		return content, true, nil
	}
	return nil, false, fmt.Errorf("%s was not changed by this session", path)
}

// writeRestored puts a backup's bytes back at path with the mode the file was
// recorded with. A recorded mode wins over whatever the path has now; without
// one (an older manifest) the current mode is kept, and 0644 is used only when
// the file has to be created from nothing. The chmod after the write matters
// for the recorded case: writing into an existing file keeps its inode and so
// its current mode, which is not necessarily the recorded one.
func writeRestored(path string, data []byte, recorded os.FileMode) error {
	perm := recorded
	if perm == 0 {
		perm = 0o644
		if info, err := os.Stat(path); err == nil {
			perm = info.Mode().Perm()
		}
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	if recorded != 0 {
		return os.Chmod(path, recorded)
	}
	return nil
}
