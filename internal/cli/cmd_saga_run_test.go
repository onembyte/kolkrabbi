package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func sagaFixture(t *testing.T, chapters string) (*app, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	body := "# SAGA: build it\n\n- **Goal**: build it\n- **Status**: in-progress (Chapter 0 / 15)\n" +
		"- **Cumulative Cost**: $0.00 / $5.00 limit\n\n## Chapter Log\n\n" + chapters
	if err := os.WriteFile(filepath.Join(dir, "SAGA.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &app{stdout: &out, stderr: &out}, &out
}

func TestSagaRunWithNoArtifactSaysSo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out}

	if err := a.runSaga(context.Background(), []string{"run"}); err != nil {
		t.Fatalf("runSaga: %v", err)
	}
	if !strings.Contains(out.String(), "no saga") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSagaRunWithNothingLeftToDoSaysSo(t *testing.T) {
	a, out := sagaFixture(t, "### Chapter 1: done already\n- **Status**: completed\n\n")

	if err := a.runSaga(context.Background(), []string{"run"}); err != nil {
		t.Fatalf("runSaga: %v", err)
	}
	// Better than starting a model turn to discover there is nothing to do.
	if !strings.Contains(out.String(), "nothing left") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSagaRunRefusesOutsideAGitRepository(t *testing.T) {
	a, out := sagaFixture(t, "### Chapter 1: work\n- **Status**: pending\n\n")

	err := a.runSaga(context.Background(), []string{"run"})

	// Every chapter ends in a commit. Discovering that after the model has
	// spent a chapter's worth of tokens is the wrong moment to find out.
	if err == nil && !strings.Contains(strings.ToLower(out.String()), "git") {
		t.Fatalf("a saga ran outside a repository: %v / %q", err, out.String())
	}
}

func TestSagaRunIsRegisteredAndDocumented(t *testing.T) {
	var found bool
	for _, c := range commandTable() {
		if c.name == "saga" && strings.Contains(c.args, "run") {
			found = true
		}
	}
	if !found {
		t.Fatal("`kolk saga run` is not in the command table's grammar")
	}
	if _, ok := sessionOnly["saga"]; ok {
		t.Fatal("saga is not session-only; it has a CLI twin")
	}
	var _ engine.ChapterWorker = engine.AgentWorker{}
}
