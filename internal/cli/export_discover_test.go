package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Adopted from the V43.5 §7 item 5 review (EX2): the Markdown export prints
// the conversation as it stands, and after a compaction that is the shortened
// one. It says so, and where the rest is, instead of looking complete.
func TestAMarkdownExportSaysWhatCompactionShortened(t *testing.T) {
	_, first := seedSessions(t)
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if strings.Contains(out.String(), "--json") {
		t.Fatalf("an export with nothing compacted points elsewhere:\n%s", out.String())
	}
	archive, err := first.ArchiveMessages([]provider.Message{{Role: "user", Content: "the original goal, compacted away"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if !strings.Contains(out.String(), "kolk sessions export "+first.ID+" --json") {
		t.Fatalf("a compacted export does not say where the earlier messages are:\n%s", out.String())
	}
	// An archive that cannot be read is said, not skipped.
	if err := os.WriteFile(archive, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if !strings.Contains(out.String(), "could not be read") {
		t.Fatalf("an unreadable archive went unmentioned:\n%s", out.String())
	}
}

// Adopted from the V43.5 §7 item 5 review (EX1): inside a session nothing
// named export. /session, where a person looks for "this session", says how.
func TestSlashSessionSaysHowToExportIt(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	sess := session.New(t.TempDir(), "mock/model")
	ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode, Sess: sess})
	a.slash(context.Background(), ag, "/session")
	if !strings.Contains(out.String(), "kolk sessions export "+sess.SessionID()) {
		t.Fatalf("/session does not say how to export it:\n%s", out.String())
	}
}

// Adopted from the V43.5 item 5 verification (I5-1): one damaged archive made
// the full export fail, the very export the note pointed to. The export now
// carries every archive it can read and names the ones it cannot.
func TestAFullExportKeepsWhatItCanReadAndNamesTheRest(t *testing.T) {
	_, first := seedSessions(t)
	if _, err := first.ArchiveMessages([]provider.Message{{Role: "user", Content: "READABLE-EVIDENCE"}}); err != nil {
		t.Fatal(err)
	}
	bad, err := first.ArchiveMessages([]provider.Message{{Role: "user", Content: "to be damaged"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID, "--json"}); code != ExitOK {
		t.Fatalf("full export exit = %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "READABLE-EVIDENCE") || !strings.Contains(out.String(), bad) {
		t.Errorf("full export lost the readable archive or did not name the damaged one:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "could not be read") {
		t.Errorf("no warning that the full export is incomplete: %q", errOut.String())
	}
	out.Reset()
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if !strings.Contains(out.String(), "1 compaction archive(s) could not be read") || strings.Contains(out.String(), "tries again") {
		t.Errorf("the Markdown note does not say what --json will hold:\n%s", out.String())
	}
}

// Adopted from the V43.5 item 5 verification (I5-3): an agent's compaction
// does not shorten the conversation the Markdown export prints, so it is not
// described as if it had.
func TestAnAgentsCompactionIsNotCalledTheConversations(t *testing.T) {
	_, first := seedSessions(t)
	child, err := first.ArchiveMessages([]provider.Message{{Role: "tool", Content: "an agent's full evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	first.SetRunState(&continuity.Run{ID: "request", Phase: "tasks", Tasks: []continuity.Task{{Title: "child", Archives: []string{child}}}})
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	got := out.String()
	if strings.Contains(got, "shortened this conversation") || !strings.Contains(got, "Agents in this session compacted their context 1 time(s)") {
		t.Errorf("an agent's compaction reads as the conversation's:\n%s", got)
	}
}

// Adopted from the V43.5 item 5 re-check (J1, J2): a damaged task journal
// withheld the whole full export, and made every agent archive read as the
// conversation's. Both exports now keep what they can read and say what they
// could not.
func TestADamagedJournalWithholdsNothingElse(t *testing.T) {
	dirs, first := seedSessions(t)
	agent, err := first.ArchiveMessages([]provider.Message{{Role: "tool", Content: "an agent's full evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.ArchiveMessages([]provider.Message{{Role: "user", Content: "the conversation before compaction"}}); err != nil {
		t.Fatal(err)
	}
	first.SetRunState(&continuity.Run{ID: "earlier", Phase: "done", Tasks: []continuity.Task{{Title: "child", Archives: []string{agent}}}})
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	first.SetRunState(&continuity.Run{ID: "later", Phase: "done"})
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dirs.Sessions(), first.ID+".executions", fmt.Sprintf("%x.json", sha256.Sum256([]byte("earlier"))))
	if err := os.WriteFile(journal, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID, "--json"}); code != ExitOK {
		t.Fatalf("full export exit = %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "unreadable_executions") || !strings.Contains(out.String(), journal) || !strings.Contains(errOut.String(), "journal") {
		t.Errorf("the damaged journal is not named, or not warned about:\n%s\n%s", out.String(), errOut.String())
	}
	out.Reset()
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID}); code != ExitOK {
		t.Fatalf("export exit = %d", code)
	}
	if got := out.String(); strings.Contains(got, "shortened this conversation 2 time(s)") || !strings.Contains(got, "task journals could not be read") {
		t.Errorf("with the journals unreadable, the note guesses whose archives they are:\n%s", got)
	}
}

// Adopted from the V43.5 item 5 re-check (K1): a store that is not a
// directory stopped the whole full export, which the Markdown note had just
// recommended. A store that cannot be listed is named like a damaged file,
// and everything else is still exported.
func TestAnUnlistableStoreWithholdsNothingElse(t *testing.T) {
	for _, store := range []string{".executions", ".compactions"} {
		dirs, first := seedSessions(t)
		first.SetRunState(&continuity.Run{ID: "earlier", Phase: "done"})
		if err := first.Save(); err != nil {
			t.Fatal(err)
		}
		first.SetRunState(&continuity.Run{ID: "later", Phase: "done"})
		if err := first.Save(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dirs.Sessions(), first.ID+store)
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		a, out, errOut := newTestApp(t, "")
		if code := a.main(context.Background(), []string{"sessions", "export", first.ID, "--json"}); code != ExitOK {
			t.Errorf("%s: full export exit = %d: %s", store, code, errOut.String())
			continue
		}
		if !strings.Contains(out.String(), path) || !strings.Contains(out.String(), "write a tokenizer") || !strings.Contains(errOut.String(), "could not be read") {
			t.Errorf("%s: the store is not named, the messages are missing, or nothing warned:\n%s\n%s", store, out.String(), errOut.String())
		}
	}
}

// A store that is a directory but cannot be listed is named too.
func TestAnUnreadableStoreDirectoryWithholdsNothingElse(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lists any directory")
	}
	dirs, first := seedSessions(t)
	if _, err := first.ArchiveMessages([]provider.Message{{Role: "user", Content: "archived"}}); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dirs.Sessions(), first.ID+".compactions")
	if err := os.Chmod(store, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store, 0o700) })
	a, out, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions", "export", first.ID, "--json"}); code != ExitOK {
		t.Fatalf("full export exit = %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), store) || !strings.Contains(errOut.String(), "could not be read") {
		t.Errorf("the unreadable store is not named:\n%s\n%s", out.String(), errOut.String())
	}
}
