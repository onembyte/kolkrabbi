package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
)

func diffFixture(t *testing.T) (*app, *checkpoint.Store, string, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	t.Chdir(work)
	store, err := checkpoint.Open(filepath.Join(t.TempDir(), "ckpt"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &app{stdout: &out, stderr: &out}, store, work, &out
}

func recordEdit(t *testing.T, store *checkpoint.Store, path, after string) {
	t.Helper()
	store.BeginTurn(context.Background())
	if err := store.Record("edit_file", path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiffShowsWhatTheSessionChanged(t *testing.T) {
	a, store, work, out := diffFixture(t)
	path := filepath.Join(work, "a.go")
	if err := os.WriteFile(path, []byte("const Port = 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordEdit(t, store, path, "const Port = 9090\n")

	a.printSessionDiff(store, "")

	got := out.String()
	if !strings.Contains(got, "-const Port = 8080") || !strings.Contains(got, "+const Port = 9090") {
		t.Fatalf("diff = %q", got)
	}
	if !strings.Contains(got, "a.go") {
		t.Fatalf("diff did not name the file:\n%s", got)
	}
}

func TestDiffIsAgainstTheStartOfTheSessionNotTheLastEdit(t *testing.T) {
	a, store, work, out := diffFixture(t)
	path := filepath.Join(work, "a.go")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordEdit(t, store, path, "two\n")
	recordEdit(t, store, path, "three\n")

	a.printSessionDiff(store, "")

	// "What has this session done to my file" is the question. Against the
	// previous edit it would answer "what did the last turn do".
	got := out.String()
	if !strings.Contains(got, "-one") || !strings.Contains(got, "+three") {
		t.Fatalf("diff = %q, want the whole session's change", got)
	}
	if strings.Contains(got, "two") {
		t.Fatalf("diff = %q, want the intermediate state gone", got)
	}
}

func TestACreatedFileIsShownAsNew(t *testing.T) {
	a, store, work, out := diffFixture(t)
	path := filepath.Join(work, "new.go")
	store.BeginTurn(context.Background())
	if err := store.Record("write_file", path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.printSessionDiff(store, "")

	got := out.String()
	if !strings.Contains(strings.ToLower(got), "new file") {
		t.Fatalf("a created file was not marked new:\n%s", got)
	}
	if !strings.Contains(got, "+package main") {
		t.Fatalf("a created file showed no content:\n%s", got)
	}
}

func TestDiffCanBeNarrowedToOneFile(t *testing.T) {
	a, store, work, out := diffFixture(t)
	for _, name := range []string{"a.go", "b.go"} {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		recordEdit(t, store, path, "after "+name+"\n")
	}

	a.printSessionDiff(store, "b.go")

	got := out.String()
	if !strings.Contains(got, "b.go") {
		t.Fatalf("asked for b.go, got:\n%s", got)
	}
	if strings.Contains(got, "after a.go") {
		t.Fatalf("showed a file that was not asked for:\n%s", got)
	}
}

func TestAFileRevertedByHandShowsNoDiff(t *testing.T) {
	a, store, work, out := diffFixture(t)
	path := filepath.Join(work, "a.go")
	if err := os.WriteFile(path, []byte("same\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordEdit(t, store, path, "changed\n")
	if err := os.WriteFile(path, []byte("same\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.printSessionDiff(store, "")

	// The session touched it; the file is as it was. Printing an empty diff
	// under a heading reads as a bug.
	got := strings.ToLower(out.String())
	if !strings.Contains(got, "unchanged") && !strings.Contains(got, "no change") {
		t.Fatalf("output = %q, want it to say the file is back to where it started", out.String())
	}
}

func TestDiffWithNothingToShowSaysSo(t *testing.T) {
	a, store, _, out := diffFixture(t)

	a.printSessionDiff(store, "")

	if !strings.Contains(out.String(), "no file changes") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAskingForAFileTheSessionNeverTouched(t *testing.T) {
	a, store, work, out := diffFixture(t)
	path := filepath.Join(work, "a.go")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordEdit(t, store, path, "y\n")

	a.printSessionDiff(store, "other.go")

	if !strings.Contains(out.String(), "other.go") {
		t.Fatalf("output = %q, want it to name what was asked for", out.String())
	}
}

// A backup exists so that /undo can put the bytes back. It does not exist so
// that /diff can print them: the file the session edited may be the .env, and
// the diff of an .env is two secrets, the old one and the new one. Every line
// /diff renders goes through the same scrubber the transcript does.
func TestDiffNeverPrintsASecretFromABackupOrTheWorkingFile(t *testing.T) {
	work := t.TempDir()
	store, err := checkpoint.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(work, ".env")
	if err := os.WriteFile(env, []byte("DATABASE_PASSWORD=hunter2correcthorsebattery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.BeginTurn(context.Background())
	if err := store.Record("edit_file", env); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("DATABASE_PASSWORD=newpasswordalsolongenough99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(work, "secrets.yaml")
	store.BeginTurn(context.Background())
	if err := store.Record("write_file", created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("api_key: 9f8e7d6c5b4a39281706abcdef012345\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a, stdout, _ := newTestApp(t, "")
	a.printSessionDiff(store, "")
	out := stdout.String()
	for _, secret := range []string{"hunter2correcthorse", "newpasswordalsolongenough99", "9f8e7d6c5b4a39281706abcdef012345"} {
		if strings.Contains(out, secret) {
			t.Fatalf("/diff printed a secret (%s):\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "DATABASE_PASSWORD") || !strings.Contains(out, "api_key") {
		t.Fatalf("/diff scrubbed the names too, so the diff is unreadable:\n%s", out)
	}
}
