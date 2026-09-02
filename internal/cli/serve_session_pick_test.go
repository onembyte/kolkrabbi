package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/session"
)

func savedSessions(t *testing.T, a *app) string {
	t.Helper()
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	dir := d.Sessions()
	for i, title := range []string{"older work", "newest work"} {
		s := session.New(dir, "test/model")
		s.Title = title
		s.CWD = "/somewhere/" + title
		s.UpdatedAt = time.Date(2026, 9, 1+i, 12, 0, 0, 0, time.UTC)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The server hosts a session, so which one is a question — and the list is
// newest first, offers a new session as the last entry, and is answered by
// number.
func TestServeAsksWhichSessionToHost(t *testing.T) {
	a, out, _ := newTestApp(t, "1\n")
	a.isStdinPiped = func() bool { return false }
	dir := savedSessions(t, a)

	id, err := a.pickServedSession(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "which session should this server host?") {
		t.Fatalf("no prompt:\n%s", got)
	}
	if !strings.Contains(got, "newest work") || !strings.Contains(got, "older work") {
		t.Fatalf("the list is incomplete:\n%s", got)
	}
	if !strings.Contains(got, "3. new session") {
		t.Fatalf("no new-session option:\n%s", got)
	}
	// 1 is the newest, because that is the one you were last in.
	if !strings.Contains(got, "serving session "+id) || !strings.Contains(got, "newest work") {
		t.Fatalf("choice 1 did not serve the newest session (%s):\n%s", id, got)
	}
	saved, err := session.Load(dir, id)
	if err != nil || saved.Title != "newest work" {
		t.Fatalf("served %s (%v), want the newest saved session", id, err)
	}
}

// Choosing the last entry, or answering nothing, serves a new session.
func TestServeCanStartANewSession(t *testing.T) {
	for name, stdin := range map[string]string{"explicit": "3\n", "empty answer": "\n"} {
		t.Run(name, func(t *testing.T) {
			a, out, _ := newTestApp(t, stdin)
			a.isStdinPiped = func() bool { return false }
			dir := savedSessions(t, a)
			id, err := a.pickServedSession(dir, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "serving a new session "+id) {
				t.Fatalf("did not serve a new session:\n%s", out.String())
			}
			if _, err := session.Load(dir, id); err == nil {
				t.Fatal("the new session id belongs to a saved session")
			}
		})
	}
}

// An answer outside the list is refused rather than defaulted: serving the
// wrong conversation to a client is not a mistake to make for someone.
func TestServeRefusesAChoiceThatIsNotOnTheList(t *testing.T) {
	for _, answer := range []string{"9\n", "0\n", "-1\n", "banana\n"} {
		a, _, _ := newTestApp(t, answer)
		a.isStdinPiped = func() bool { return false }
		dir := savedSessions(t, a)
		if _, err := a.pickServedSession(dir, "", false); err == nil {
			t.Fatalf("%q was accepted", strings.TrimSpace(answer))
		}
	}
}

// Scripts are never asked. --session names one, --new says so, and a piped
// stdin gets what serving always did, with the flags named.
func TestServeNeverBlocksAScript(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.isStdinPiped = func() bool { return true }
	dir := savedSessions(t, a)

	id, err := a.pickServedSession(dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stdin is not a terminal") || !strings.Contains(out.String(), "--session <id>") {
		t.Fatalf("a piped stdin was not told how to choose:\n%s", out.String())
	}
	if _, err := session.Load(dir, id); err == nil {
		t.Fatal("a piped stdin silently served a saved session")
	}

	// --session names one exactly.
	saved, err := session.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, bout, _ := newTestApp(t, "")
	if got, err := b.pickServedSession(dir, saved[0].ID, false); err != nil || got != saved[0].ID {
		t.Fatalf("--session = %q, %v; want %s", got, err, saved[0].ID)
	} else if !strings.Contains(bout.String(), "serving session "+saved[0].ID) {
		t.Fatalf("--session said nothing:\n%s", bout.String())
	}
	if _, err := b.pickServedSession(dir, "s_nosuchsession", false); err == nil {
		t.Fatal("--session accepted an id with no saved session")
	}
	if _, err := b.pickServedSession(dir, saved[0].ID, true); err == nil {
		t.Fatal("--session and --new together were accepted")
	}

	// A machine with nothing saved is not asked either. Its own directory,
	// rather than whichever one the isolation happened to leave in the
	// environment — the point of the case is that it is empty.
	c, cout, _ := newTestApp(t, "")
	c.isStdinPiped = func() bool { return false }
	if _, err := c.pickServedSession(t.TempDir(), "", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cout.String(), "no saved sessions on this machine") {
		t.Fatalf("an empty machine was prompted:\n%s", cout.String())
	}
}
