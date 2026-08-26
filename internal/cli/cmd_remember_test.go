package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRememberWritesToTheUsersOwnNotes(t *testing.T) {
	dirs := isolateConnectorState(t)
	t.Chdir(t.TempDir())
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/remember prefer table-driven tests") {
		t.Fatal("/remember must not exit the session")
	}

	body, err := os.ReadFile(dirs.MemoryFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- prefer table-driven tests") {
		t.Fatalf("memory = %q", body)
	}
	// A note the user cannot find is a note they cannot correct.
	if !strings.Contains(out.String(), dirs.MemoryFile()) {
		t.Fatalf("output = %q, want the file named", out.String())
	}
}

func TestRememberProjectWritesWhereTheAgentReads(t *testing.T) {
	isolateConnectorState(t)
	work := t.TempDir()
	t.Chdir(work)
	if err := os.WriteFile("AGENTS.md", []byte("# rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/remember --project always run make check") {
		t.Fatal("/remember must not exit the session")
	}

	// The existing file the engine loads, not a new one beside it.
	body, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- always run make check") {
		t.Fatalf("AGENTS.md = %q", body)
	}
	if _, err := os.Stat("KOLKRABBI.md"); err == nil {
		t.Fatal("a second memory file was created beside the one already in use")
	}
	if !strings.Contains(out.String(), "project") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRememberAppendsRatherThanReplacing(t *testing.T) {
	isolateConnectorState(t)
	t.Chdir(t.TempDir())
	a, ag, _ := replFixture(t, "")

	a.slash(context.Background(), ag, "/remember first note")
	a.slash(context.Background(), ag, "/remember second note")

	dirs, _ := a.resolve()
	body, err := os.ReadFile(dirs.MemoryFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "first note") || !strings.Contains(string(body), "second note") {
		t.Fatalf("memory = %q, want both notes", body)
	}
}

func TestRememberRefusesAnEmptyNote(t *testing.T) {
	isolateConnectorState(t)
	t.Chdir(t.TempDir())
	a, ag, _ := replFixture(t, "")
	var errOut strings.Builder
	a.stderr = &errOut

	a.slash(context.Background(), ag, "/remember")

	if !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("stderr = %q, want usage rather than an empty line written", errOut.String())
	}
	dirs, _ := a.resolve()
	if _, err := os.Stat(dirs.MemoryFile()); err == nil {
		t.Fatal("an empty note created the memory file")
	}
}
