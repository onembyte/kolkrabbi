package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func seedSessions(t *testing.T) (paths.Dirs, *session.Session) {
	t.Helper()
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	first := session.New(dirs.Sessions(), "vendor/model")
	first.Title = "add the parser"
	first.SetMessages([]provider.Message{
		{Role: "user", Content: "write a tokenizer for the config format"},
		{Role: "assistant", Content: "done, see internal/config/token.go"},
	})
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	second := session.New(dirs.Sessions(), "vendor/model")
	second.Title = "fix the flaky test"
	second.SetMessages([]provider.Message{{Role: "user", Content: "why does the deploy hang"}})
	if err := second.Save(); err != nil {
		t.Fatal(err)
	}
	return dirs, first
}

func TestSessionsSearchMatchesTitleAndContent(t *testing.T) {
	seedSessions(t)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"sessions", "search", "tokenizer"}); code != ExitOK {
		t.Fatalf("search exit = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "add the parser") {
		t.Fatalf("search output = %q, want the session whose message matched", got)
	}
	if strings.Contains(got, "fix the flaky test") {
		t.Fatalf("search output = %q, want only matches", got)
	}
}

func TestSessionsSearchIsCaseInsensitiveAndSaysWhenNothingMatches(t *testing.T) {
	seedSessions(t)
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "search", "FLAKY"}); code != ExitOK {
		t.Fatal("search must succeed")
	}
	if !strings.Contains(out.String(), "fix the flaky test") {
		t.Fatalf("output = %q, want a case-insensitive match", out.String())
	}

	a, out, _ = newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "search", "nothing-like-this"}); code != ExitOK {
		t.Fatal("a search with no matches is not a failure")
	}
	if !strings.Contains(out.String(), "no session matches") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSessionsRenameReplacesTheTitle(t *testing.T) {
	dirs, first := seedSessions(t)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"sessions", "rename", first.ID, "config", "parser"}); code != ExitOK {
		t.Fatalf("rename exit = %d, stderr = %q", code, errOut.String())
	}
	reloaded, err := session.Load(dirs.Sessions(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title != "config parser" {
		t.Fatalf("title = %q", reloaded.Title)
	}
	if !strings.Contains(out.String(), "config parser") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSessionsForkLeavesTheOriginalUntouched(t *testing.T) {
	dirs, first := seedSessions(t)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"sessions", "fork", first.ID}); code != ExitOK {
		t.Fatalf("fork exit = %d, stderr = %q", code, errOut.String())
	}

	original, err := session.Load(dirs.Sessions(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Title != "add the parser" || len(original.GetMessages()) != 2 {
		t.Fatalf("the original changed: %+v", original)
	}

	all, err := session.List(dirs.Sessions())
	if err != nil {
		t.Fatal(err)
	}
	var fork *session.Session
	for _, candidate := range all {
		if candidate.ID != first.ID && strings.Contains(candidate.Title, "fork") {
			fork = candidate
		}
	}
	if fork == nil {
		t.Fatalf("no fork was created among %d sessions", len(all))
	}
	if len(fork.Messages) != 2 {
		t.Fatalf("fork carries %d messages, want the original's history", len(fork.Messages))
	}
	if !strings.Contains(out.String(), fork.ID) {
		t.Fatalf("output = %q, want the new id so it can be resumed", out.String())
	}
}

func TestSessionsExportRendersAReadableTranscript(t *testing.T) {
	_, first := seedSessions(t)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"# add the parser", "write a tokenizer", "internal/config/token.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("export = %q, want %q", got, want)
		}
	}
}

func TestSessionsExportJSONIsTheStoredRecord(t *testing.T) {
	_, first := seedSessions(t)
	a, out, _ := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"sessions", "export", first.ID, "--json"}); code != ExitOK {
		t.Fatal("export --json must succeed")
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Fatalf("export = %q, want json", out.String())
	}
	if !strings.Contains(out.String(), `"messages"`) {
		t.Fatalf("export = %q", out.String())
	}
}

func TestSessionsSubcommandsRejectAnUnknownID(t *testing.T) {
	seedSessions(t)
	for _, args := range [][]string{
		{"sessions", "rename", "no-such-session", "title"},
		{"sessions", "fork", "no-such-session"},
		{"sessions", "export", "no-such-session"},
	} {
		a, _, errOut := newTestApp(t, "")
		if code := a.main(context.Background(), args); code == ExitOK {
			t.Fatalf("%v succeeded against a session that does not exist", args)
		}
		if errOut.Len() == 0 {
			t.Fatalf("%v failed silently", args)
		}
	}
}

func TestResumePrefersASessionFromThisDirectory(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	here := t.TempDir()

	mine := session.New(dirs.Sessions(), "vendor/model")
	mine.CWD = here
	mine.Title = "this project"
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}
	newer := session.New(dirs.Sessions(), "vendor/model")
	newer.CWD = t.TempDir()
	newer.Title = "another project"
	if err := newer.Save(); err != nil {
		t.Fatal(err)
	}

	t.Chdir(here)
	a, _, _ := newTestApp(t, "")
	resumed, err := a.resolveSession(&options{resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Title != "this project" {
		t.Fatalf("resumed %q, want the session started here", resumed.Title)
	}
}

func TestResumeSaysWhenItReachesIntoAnotherProject(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	only := session.New(dirs.Sessions(), "vendor/model")
	only.CWD = elsewhere
	only.Title = "another project"
	if err := only.Save(); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	a, out, _ := newTestApp(t, "")
	if _, err := a.resolveSession(&options{resume: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), elsewhere) {
		t.Fatalf("output = %q, want the other project named", out.String())
	}
}

func TestStatsSaysWhenItsTotalsAreIncomplete(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	body := `{"kind":"call","turn":"t1","model":"vendor/model","prompt_tokens":10,"cost":0.5}` + "\n" +
		"{not json\n"
	if err := os.WriteFile(dirs.StatsFile(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"stats"}); code != ExitOK {
		t.Fatal("a damaged line must not stop stats from reporting what it can")
	}
	got := out.String()
	if !strings.Contains(got, "vendor/model") {
		t.Fatalf("output = %q, want the readable record counted", got)
	}
	if !strings.Contains(got, "incomplete") {
		t.Fatalf("output = %q, want the totals declared incomplete", got)
	}
}

func TestStatsIsSilentWhenNothingWasSkipped(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dirs.StatsFile(),
		[]byte(`{"kind":"call","turn":"t1","model":"vendor/model","prompt_tokens":10,"cost":0.5}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"stats"}); code != ExitOK {
		t.Fatal("stats must succeed")
	}
	if strings.Contains(out.String(), "incomplete") {
		t.Fatalf("output = %q, want no warning when nothing was lost", out.String())
	}
}
