package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// Shadow is a git object store that lives outside the work tree it snapshots.
//
// Every command runs with an explicit GIT_DIR and GIT_WORK_TREE, so the
// project's own .git — its index, HEAD, stash stack and reflog — is never
// touched. That is the whole reason this exists in preference to `git stash`
// or a branch, and `TestShadowNeverTouchesTheUsersOwnGitState` is the test that
// says so. It must not be deleted.
//
// The store is cheap because objects/info/alternates points at the project's
// own object database: a blob git already has is referenced, not rehashed. On
// this repository — 544 files, a 222 MB .git — that is 63 ms for the first
// snapshot, 15 ms for each one after, and a store of about 150 KB.
type Shadow struct {
	dir      string // the shadow GIT_DIR
	workTree string // the project
	sh       shell.Shell
}

// gitEnv keeps every path out of the command line. The shell interprets
// Command, so a project path with a space or a quote in it would be a bug
// waiting for the right directory name; passed as environment it is data.
func (s *Shadow) gitEnv() []string {
	return []string{
		"GIT_DIR=" + s.dir,
		"GIT_WORK_TREE=" + s.workTree,
		// The snapshot is machinery, not authorship. Identity is fixed so it
		// never reads the user's config, prompts, or fails on a machine where
		// user.email was never set.
		"GIT_AUTHOR_NAME=kolk",
		"GIT_AUTHOR_EMAIL=kolk@localhost",
		"GIT_COMMITTER_NAME=kolk",
		"GIT_COMMITTER_EMAIL=kolk@localhost",
	}
}

func (s *Shadow) git(ctx context.Context, command string) (shell.Result, error) {
	return s.sh.Run(ctx, shell.Cmd{Command: command, Dir: s.workTree, Env: s.gitEnv()})
}

// OpenShadow creates or reopens the store for one project.
//
// It returns an error for a directory that is not a git repository, and for any
// git that cannot complete the setup. Both are the same answer to the caller:
// use the copy store instead. There is deliberately no version check — the
// probe is the operation itself, which cannot be wrong about the machine it is
// running on.
func OpenShadow(ctx context.Context, dir, workTree string) (*Shadow, error) {
	projectGit := filepath.Join(workTree, ".git")
	if _, err := os.Stat(projectGit); err != nil {
		return nil, fmt.Errorf("checkpoint: %s is not a git repository", workTree)
	}

	s := &Shadow{dir: dir, workTree: workTree, sh: shell.New()}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	// `git init` is the one command that must not see GIT_WORK_TREE: git
	// refuses a work tree at creation time, and there is nothing to work on
	// yet anyway. It takes the directory as an argument instead.
	initResult, err := s.sh.Run(ctx, shell.Cmd{Command: "git init --quiet --bare .", Dir: dir})
	if err != nil || !initResult.OK() {
		return nil, fmt.Errorf("checkpoint: creating the shadow store: %s", failure(initResult, err))
	}

	// Reuse the project's blobs rather than rehashing the tree. A snapshot of a
	// large checkout is then O(changed), not O(repository).
	altDir := filepath.Join(dir, "objects", "info")
	if err := os.MkdirAll(altDir, 0o700); err != nil {
		return nil, err
	}
	objects := filepath.Join(workTree, ".git", "objects")
	if err := os.WriteFile(filepath.Join(altDir, "alternates"), []byte(objects+"\n"), 0o600); err != nil {
		return nil, err
	}

	// Line endings, long paths and symlinks are the project's business, not
	// ours: a snapshot that rewrote them would restore something the user never
	// wrote. fsmonitor is off because the store has no daemon, and manyFiles
	// keeps the index cheap on a large checkout.
	for _, setting := range []string{
		"core.autocrlf false",
		"core.longpaths true",
		"core.symlinks true",
		"core.fsmonitor false",
		"feature.manyFiles true",
	} {
		if result, err := s.git(ctx, "git config "+setting); err != nil || !result.OK() {
			return nil, fmt.Errorf("checkpoint: configuring the shadow store: %s", failure(result, err))
		}
	}
	return s, nil
}

// Snapshot records the whole work tree as it is now and returns the commit.
//
// It is called once per turn, not once per tool call: a whole-tree snapshot
// already contains every path, so re-taking one per write would multiply the
// cost to record states nothing can address.
func (s *Shadow) Snapshot(ctx context.Context, turn int) (string, error) {
	if result, err := s.git(ctx, "git add -A"); err != nil || !result.OK() {
		return "", fmt.Errorf("checkpoint: staging the snapshot: %s", failure(result, err))
	}
	message := fmt.Sprintf("kolk snapshot: turn %d", turn)
	result, err := s.git(ctx, "git commit --quiet --allow-empty -m "+shell.Quote(message))
	if err != nil || !result.OK() {
		return "", fmt.Errorf("checkpoint: recording the snapshot: %s", failure(result, err))
	}
	head, err := s.git(ctx, "git rev-parse HEAD")
	if err != nil || !head.OK() {
		return "", fmt.Errorf("checkpoint: reading the snapshot: %s", failure(head, err))
	}
	return strings.TrimSpace(head.Output), nil
}

// ChangedSinceSnapshot lists the paths that differ from the last snapshot,
// including every change made outside kolk.
func (s *Shadow) ChangedSinceSnapshot(ctx context.Context) ([]string, error) {
	result, err := s.git(ctx, "git status --porcelain=v1 --untracked-files=all")
	if err != nil || !result.OK() {
		return nil, fmt.Errorf("checkpoint: reading the shadow status: %s", failure(result, err))
	}
	var paths []string
	for _, line := range strings.Split(result.Output, "\n") {
		if len(line) < 4 {
			continue
		}
		// "XY path", and for a rename "XY old -> new"; the new name is ours.
		path := strings.TrimSpace(line[3:])
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// Dir is where the store lives, so a caller can report or delete it.
func (s *Shadow) Dir() string { return s.dir }

func failure(result shell.Result, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case result.Failure != "":
		return result.Failure + ": " + strings.TrimSpace(result.Output)
	default:
		return strings.TrimSpace(result.Output)
	}
}

// The two ways a turn's file state can be captured.
const (
	StrategyCopy   = "copy"   // a backup file per write, before the write
	StrategyShadow = "shadow" // a whole-tree snapshot per turn, in a git store outside the tree
)

// UseShadow attaches a shadow store to s when the project qualifies, and says
// nothing when it does not qualify for a reason the user cannot act on.
//
// There is no git version check here on purpose (item 32): opening the store is
// itself the probe, and a probe cannot be wrong about the machine it runs on.
// Not a repository, no git on PATH, a store that will not open — one answer, so
// that the alternative to snapshotting is never failing the turn.
func (s *Store) UseShadow(ctx context.Context, workTree string) {
	shadow, err := OpenShadow(ctx, filepath.Join(s.dir, "shadow.git"), workTree)
	if err != nil {
		s.fellBack = "Snapshots cover kolk's own edits only: " + reasonOf(err) +
			". `/undo` will not restore changes made by bash."
		return
	}
	s.shadow = shadow
}

// Strategy reports which store is capturing turns.
func (s *Store) Strategy() string {
	if s.shadow != nil {
		return StrategyShadow
	}
	return StrategyCopy
}

// Notice is the one sentence a session prints when it could not snapshot the
// whole tree, or stopped being able to. It is empty when the shadow store is
// working, and it never changes after it is set: a fallback that re-announced
// itself every turn would be noise about a decision already made.
func (s *Store) Notice() string { return s.fellBack }

// snapshotTurn is called from BeginTurn. A failure here is never fatal — it
// costs this session its whole-tree snapshots and nothing else, and the copy
// store carries on underneath.
func (s *Store) snapshotTurn(ctx context.Context, turn int) {
	if s.shadow == nil {
		return
	}
	commit, err := s.shadow.Snapshot(ctx, turn)
	if err != nil {
		s.shadow = nil
		s.fellBack = "Whole-tree snapshots stopped working and this session will not retry them: " +
			reasonOf(err) + ". `/undo` still restores kolk's own edits."
		return
	}
	s.snapshots[turn] = commit
	// The commit is worthless without the index that names it: a snapshot the
	// manifest does not record is one no rewind can ever find.
	if err := s.saveManifest(); err != nil {
		s.shadow = nil
		delete(s.snapshots, turn)
		s.fellBack = "The snapshot index could not be written, so this session will not snapshot " +
			"the whole tree: " + reasonOf(err) + ". `/undo` still restores kolk's own edits."
	}
}

// reasonOf trims the layered prefixes off an error so the sentence a user reads
// is about their machine rather than about our call stack.
func reasonOf(err error) string {
	text := strings.TrimSpace(err.Error())
	text = strings.TrimPrefix(text, "checkpoint: ")
	if line, _, found := strings.Cut(text, "\n"); found {
		text = line
	}
	return strings.TrimSpace(text)
}

// ChangedSince lists what differs between the work tree and one snapshot.
// It is read before a restore, so a rewind can say what it put back.
func (s *Shadow) ChangedSince(ctx context.Context, commit string) ([]string, error) {
	result, err := s.git(ctx, "git diff --name-only "+shell.Quote(commit))
	if err != nil || !result.OK() {
		return nil, fmt.Errorf("checkpoint: comparing against the snapshot: %s", failure(result, err))
	}
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(result.Output, "\n") {
		if path := strings.TrimSpace(line); path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	// A file created since the snapshot is untracked in the shadow index, so
	// `diff` cannot see it, and it is precisely the case /undo must report.
	untracked, err := s.git(ctx, "git ls-files --others --exclude-standard")
	if err != nil || !untracked.OK() {
		return nil, fmt.Errorf("checkpoint: listing new files: %s", failure(untracked, err))
	}
	for _, line := range strings.Split(untracked.Output, "\n") {
		if path := strings.TrimSpace(line); path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// RestoreTo puts the work tree back to one snapshot.
//
// This is the most destructive operation in the package, and two things bound
// it. Everything runs with GIT_WORK_TREE set to the project, so git cannot
// reach outside it whatever the snapshot contains. And `clean` runs without
// `-x`, so a file the project ignores — build output, a local .env — is left
// alone: the snapshot never held it, so putting the tree "back" cannot mean
// deleting it.
func (s *Shadow) RestoreTo(ctx context.Context, commit string) error {
	if result, err := s.git(ctx, "git reset --hard --quiet "+shell.Quote(commit)); err != nil || !result.OK() {
		return fmt.Errorf("checkpoint: restoring the snapshot: %s", failure(result, err))
	}
	if result, err := s.git(ctx, "git clean -fdq"); err != nil || !result.OK() {
		return fmt.Errorf("checkpoint: removing files created since the snapshot: %s", failure(result, err))
	}
	return nil
}

// rewindSnapshot puts the whole work tree back to the snapshot taken at the
// start of one turn, which is what makes `/undo` cover a change kolk never made
// itself.
//
// The paths are read before the restore, not after: afterwards there is by
// construction nothing left to compare.
func (s *Store) rewindSnapshot(ctx context.Context, turn int, commit string) ([]string, error) {
	if s.shadow == nil {
		return nil, fmt.Errorf("checkpoint: turn %d was captured as a whole-tree snapshot, "+
			"and this session can no longer read the store that holds it", turn)
	}
	changed, err := s.shadow.ChangedSince(ctx, commit)
	if err != nil {
		return nil, err
	}
	if err := s.shadow.RestoreTo(ctx, commit); err != nil {
		return nil, err
	}
	delete(s.snapshots, turn)
	if err := s.saveManifest(); err != nil {
		return changed, err
	}
	return changed, nil
}
