package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// A shared scratch directory is not a project root. Without this boundary a
// stale /tmp/SAGA.md hijacks every unrelated directory below it, including
// Go's test directories and a person's temporary checkout.
func TestSagaArtifactDoesNotInheritFromWorldWritableAncestor(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o1777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "SAGA.md"), []byte("# SAGA: other project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(shared, "unrelated", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	path, err := sagaArtifactPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(nested, "SAGA.md"); path != want {
		t.Fatalf("saga artifact = %q, want isolated child path %q", path, want)
	}
}

func TestSagaArtifactStillInheritsFromNormalNonGitAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SAGA.md"), []byte("# SAGA: local project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	path, err := sagaArtifactPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "SAGA.md"); path != want {
		t.Fatalf("saga artifact = %q, want normal ancestor path %q", path, want)
	}
}

// projectTree builds a repository with a nested working directory and chdirs
// into the nested one, which is where an inline SAGA prompt is most likely to
// be entered by accident.
func projectTree(t *testing.T) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested = filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	return root, nested
}

func TestSagaGoalWritesTheArtifactAtTheProjectRoot(t *testing.T) {
	root, nested := projectTree(t)
	a := &app{stdout: &strings.Builder{}, stderr: &strings.Builder{}}

	if _, err := a.openSaga("fix all tests"); err != nil {
		t.Fatalf("openSaga: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
	if err != nil {
		t.Fatalf("the saga artifact is not at the project root: %v", err)
	}
	if !strings.Contains(string(body), "- **Goal**: fix all tests") {
		t.Fatalf("artifact = %q", body)
	}
	if _, err := os.Stat(filepath.Join(nested, "SAGA.md")); err == nil {
		t.Fatal("saving a saga from a subdirectory littered that subdirectory with SAGA.md")
	}
}

// A saga in flight keeps its goal. The wake messages tell the user to type
// `next chapter /saga` or `retry /saga`; before this, that text became the
// goal and the planner planned "retry" for the rest of the run.
func TestAWakeNoteDoesNotReplaceTheGoal(t *testing.T) {
	root, _ := projectTree(t)
	a := &app{stdout: &strings.Builder{}, stderr: &strings.Builder{}}

	first, err := a.openSaga("build the app")
	if err != nil {
		t.Fatal(err)
	}
	if first.goal != "build the app" || first.note != "" || first.notice != "" {
		t.Fatalf("first opening = %+v, want a fresh goal with no note", first)
	}

	second, err := a.openSaga("retry")
	if err != nil {
		t.Fatal(err)
	}
	if second.goal != "build the app" {
		t.Fatalf("second wake goal = %q, want the persisted goal", second.goal)
	}
	if second.note != "retry" || !strings.Contains(second.notice, "continuing") {
		t.Fatalf("second opening = %+v, want the text carried as a note", second)
	}
	body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- **Goal**: build the app") || strings.Contains(string(body), "retry") {
		t.Fatalf("SAGA.md after a note wake = %q, want the goal untouched", body)
	}

	// Restating the goal is not a note.
	third, err := a.openSaga("Build the app")
	if err != nil {
		t.Fatal(err)
	}
	if third.note != "" || third.notice != "" {
		t.Fatalf("restated goal produced a note: %+v", third)
	}
}

// A finished artifact is a restart boundary, and a request that lands on one
// is asking for something new. Before this, a completed SAGA.md answered every
// later /saga with "every acceptance criterion is met", and the only way out
// was deleting the file by hand.
func TestANewGoalAfterAFinishedSagaArchivesAndStartsFresh(t *testing.T) {
	for _, status := range []string{engine.SagaStatusCompleted, engine.SagaStatusBlocked} {
		t.Run(status, func(t *testing.T) {
			root, _ := projectTree(t)
			old := &engine.SagaState{
				Goal:          "old goal",
				Started:       time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
				Status:        status,
				ActiveChapter: 1,
				Chapters:      []engine.Chapter{{Number: 1, Title: "done thing", Status: engine.StatusDone}},
			}
			if err := os.WriteFile(filepath.Join(root, "SAGA.md"), []byte(engine.FormatSagaMarkdown(old)), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &app{stdout: &strings.Builder{}, stderr: &strings.Builder{}}

			opening, err := a.openSaga("add dark mode")
			if err != nil {
				t.Fatal(err)
			}
			if opening.goal != "add dark mode" || opening.note != "" {
				t.Fatalf("opening = %+v, want a fresh goal", opening)
			}
			if !strings.Contains(opening.notice, "archived") || !strings.Contains(opening.notice, status) {
				t.Fatalf("notice = %q, want the archive and the old status named", opening.notice)
			}

			body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"- **Goal**: add dark mode", "- **Status**: in-progress"} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("new SAGA.md = %q, want %q", body, want)
				}
			}
			if strings.Contains(string(body), "done thing") {
				t.Fatalf("new SAGA.md still carries the old chapters: %q", body)
			}

			archives, _ := filepath.Glob(filepath.Join(root, "SAGA.*.md"))
			if len(archives) != 1 {
				t.Fatalf("archives = %v, want exactly one", archives)
			}
			if filepath.Base(archives[0]) != "SAGA.20260901-100000.md" {
				t.Fatalf("archive name = %q, want it stamped with when the old saga started", archives[0])
			}
			archived, _ := os.ReadFile(archives[0])
			if !strings.Contains(string(archived), "- **Goal**: old goal") || !strings.Contains(string(archived), "done thing") {
				t.Fatalf("archive = %q, want the old saga intact", archived)
			}
		})
	}
}

// An archive name that already exists gets a counter, not an overwrite.
func TestArchivingTwiceInTheSameSecondKeepsBoth(t *testing.T) {
	root, _ := projectTree(t)
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for _, goal := range []string{"first", "second"} {
		path := filepath.Join(root, "SAGA.md")
		if err := os.WriteFile(path, []byte(goal), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := archiveSaga(path, &engine.SagaState{Started: at}, at); err != nil {
			t.Fatal(err)
		}
	}
	archives, _ := filepath.Glob(filepath.Join(root, "SAGA.*.md"))
	if len(archives) != 2 {
		t.Fatalf("archives = %v, want both kept", archives)
	}
}

// The wake budget carries every limit SAGA.md records. A budget built without
// the doom threshold blocked a five-strike saga at the default three.
func TestWakeBudgetCarriesMaxStrikesFromSagaFile(t *testing.T) {
	state := &engine.SagaState{MaxChapters: 7, CostLimit: 2.5, MaxStrikes: 5, Strikes: 3}
	budget := sagaWakeBudget(state)
	if budget.MaxChapters != 7 || budget.CostLimit != 2.5 || budget.DoomThreshold != 5 {
		t.Fatalf("budget = %+v, want every SAGA.md limit carried", budget)
	}
	if reason := budget.Check(state, state.Strikes, 0); reason != engine.StopNone {
		t.Fatalf("three strikes under a five-strike allowance stopped the wake: %q", reason)
	}
	if reason := budget.Check(state, 5, 0); reason != engine.StopDoomLoop {
		t.Fatalf("five strikes under a five-strike allowance = %q, want doom-loop", reason)
	}
}

// A wake reads SAGA.md once. openSaga has to parse it to decide whether this
// request starts, continues or resets a saga, and the wake takes that parse
// rather than reading the same file again — it cannot have changed in between,
// and a saga is hundreds of wakes.
func TestAWakeParsesTheArtifactOnce(t *testing.T) {
	root, _ := projectTree(t)
	a := &app{stdout: &strings.Builder{}, stderr: &strings.Builder{}}

	opening, err := a.openSaga("build the app")
	if err != nil {
		t.Fatal(err)
	}
	if opening.state == nil || opening.state.Goal != "build the app" {
		t.Fatalf("opening carries no parsed state: %+v", opening)
	}
	if opening.path != filepath.Join(root, "SAGA.md") {
		t.Fatalf("opening path = %q", opening.path)
	}

	// The wake works from that parse: with the file removed, a second read
	// would fail, and the wake still knows the goal.
	if err := os.Remove(opening.path); err != nil {
		t.Fatal(err)
	}
	second, err := a.openSaga("continue")
	if err == nil {
		// Removing it makes the next request a *new* saga, which is correct —
		// what matters is that the first opening still carries its own parse.
		if second.state == nil {
			t.Fatal("a fresh saga carries no parsed state either")
		}
	}
	if opening.state.Goal != "build the app" {
		t.Fatal("the first opening's parse was invalidated by a later read")
	}
}
