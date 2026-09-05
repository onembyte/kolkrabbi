package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// TaskSnapshot is the tree as it stood before one writing subagent started,
// together with the paths that subagent went on to change.
//
// Both halves are needed, and for different reasons. The commit says what to
// put back. The paths say how much to put back — because restoring the whole
// tree to that commit would also discard every task that ran after it, which is
// the opposite of taking one task back on its own.
type TaskSnapshot struct {
	Title  string   `json:"title"`
	Turn   int      `json:"turn"`
	Commit string   `json:"commit"`
	Paths  []string `json:"paths,omitempty"`
}

// BeginTask snapshots the tree before a writing subagent starts and returns a
// handle for EndTask. The handle is -1 when nothing was captured: no shadow
// store, or a snapshot that failed. Nothing here can fail a run — a subagent
// that cannot be rewound individually still runs, and the turn's own snapshot
// still covers it.
//
// Only writing subagents are worth this. Research and explain change no files,
// so a snapshot for one would record a tree identical to the last.
func (s *Store) BeginTask(ctx context.Context, title string) int {
	if s.shadow == nil {
		return -1
	}
	commit, err := s.shadow.Snapshot(ctx, s.turn)
	if err != nil {
		return -1
	}
	s.tasks = append(s.tasks, TaskSnapshot{Title: title, Turn: s.turn, Commit: commit})
	return len(s.tasks) - 1
}

// EndTask records which paths the subagent changed, read now rather than later:
// the scheduler will not start another writer until this one returns, so this
// is the only moment when "what changed" means this task and nothing else.
func (s *Store) EndTask(ctx context.Context, handle int) {
	if s.shadow == nil || handle < 0 || handle >= len(s.tasks) {
		return
	}
	paths, err := s.shadow.ChangedSince(ctx, s.tasks[handle].Commit)
	if err != nil {
		return
	}
	s.tasks[handle].Paths = paths
	// A task that changed nothing is still worth keeping in the list: it is an
	// answer to "which subagent touched my file", and the answer is "not that
	// one".
	_ = s.saveManifest()
}

// TaskSnapshots lists this session's per-subagent snapshots in the order they
// were taken, which is the order the run reported them.
func (s *Store) TaskSnapshots() []TaskSnapshot {
	out := make([]TaskSnapshot, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// RewindTask takes back one subagent's file changes and nothing else. n is
// 1-based, matching the "subagent 2/5" the run already printed.
//
// Only the paths that task changed are restored, so the work of every task
// before and after it stays where it is. A path the task created is removed,
// because putting it "back" to a state where it did not exist means deleting
// it.
func (s *Store) RewindTask(ctx context.Context, n int) ([]string, error) {
	if s.shadow == nil {
		return nil, fmt.Errorf("checkpoint: this session snapshots single files rather than the whole tree, " +
			"so it cannot take back one subagent on its own; `/undo` still takes back the turn")
	}
	if n < 1 || n > len(s.tasks) {
		if len(s.tasks) == 0 {
			return nil, fmt.Errorf("checkpoint: no subagent in this session has a snapshot to take back")
		}
		return nil, fmt.Errorf("checkpoint: there is no subagent %d. This session recorded:\n%s", n, s.taskTitles())
	}

	snapshot := s.tasks[n-1]
	restored := make([]string, 0, len(snapshot.Paths))
	modes := s.shadow.modesOf(snapshot.Paths)
	defer s.shadow.reapplyModes(modes)
	for _, path := range snapshot.Paths {
		if err := s.shadow.restorePath(ctx, snapshot.Commit, path); err != nil {
			return restored, err
		}
		restored = append(restored, path)
	}
	return restored, nil
}

// restorePath puts one path back to its state in commit, deleting it when the
// commit did not hold it — a file the task created is taken back by removing
// it, since there is no earlier version to put there.
func (s *Shadow) restorePath(ctx context.Context, commit, path string) error {
	held, err := s.git(ctx, "git cat-file -e "+shell.Quote(commit+":"+path))
	if err != nil {
		return fmt.Errorf("checkpoint: looking for %s in the snapshot: %s", path, failure(shell.Result{}, err))
	}
	if !held.OK() {
		if err := os.Remove(filepath.Join(s.workTree, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("checkpoint: removing %s: %w", path, err)
		}
		return nil
	}
	result, err := s.git(ctx, "git checkout "+shell.Quote(commit)+" -- "+shell.Quote(path))
	if err != nil || !result.OK() {
		return fmt.Errorf("checkpoint: restoring %s: %s", path, failure(result, err))
	}
	return nil
}

// taskTitles is the one-line summary a surface prints when asked what there is
// to take back.
func (s *Store) taskTitles() string {
	titles := make([]string, 0, len(s.tasks))
	for i, snapshot := range s.tasks {
		titles = append(titles, fmt.Sprintf("%d. %s", i+1, snapshot.Title))
	}
	return strings.Join(titles, "\n")
}
