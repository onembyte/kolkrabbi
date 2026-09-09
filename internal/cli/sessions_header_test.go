package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/session"
)

// `kolk sessions` lists from the headers a save leaves beside each transcript
// (OPTIMIZATION_PLAN.md O6). Proved the only way that cannot be faked: with a
// transcript no decoder could read. Before O6 this listing decoded every one.
func TestSessionsListsFromHeadersWithoutDecodingTranscripts(t *testing.T) {
	dirs, first := seedSessions(t)
	for _, id := range []string{first.ID} {
		if err := os.WriteFile(filepath.Join(dirs.Sessions(), id+".json"), []byte("{ not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, out, errOut := newTestApp(t, "")
	a.dirs = dirs

	if code := a.main(context.Background(), []string{"sessions", "--all"}); code != ExitOK {
		t.Fatalf("sessions exit = %d, stderr = %q", code, errOut.String())
	}
	listing := out.String()
	if !strings.Contains(listing, "add the parser") {
		t.Fatalf("listing lost the session whose transcript it did not read:\n%s", listing)
	}
	// And the count comes from the header, not from a decode.
	if !strings.Contains(listing, "msgs:2") {
		t.Fatalf("listing = %q, want the header's message count", listing)
	}
}

// Searching asks the header first: a title match must not decode anything.
func TestSessionsSearchMatchesATitleWithoutDecoding(t *testing.T) {
	dirs, first := seedSessions(t)
	if err := os.WriteFile(filepath.Join(dirs.Sessions(), first.ID+".json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp(t, "")
	a.dirs = dirs

	if code := a.main(context.Background(), []string{"sessions", "search", "--all", "parser"}); code != ExitOK {
		t.Fatalf("search exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), first.ID) {
		t.Fatalf("search = %q, want the title match", out.String())
	}
}

// Doctor is what puts a header back, and what writes the first one for a
// session saved by a kolk that had none.
func TestDoctorRepairsASessionHeader(t *testing.T) {
	dirs, first := seedSessions(t)
	if err := os.Remove(filepath.Join(dirs.Sessions(), first.ID+".meta.json")); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")
	a.dirs = dirs

	if err := a.runDoctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "header") {
		t.Fatalf("doctor said nothing about the headers:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dirs.Sessions(), first.ID+".meta.json")); err != nil {
		t.Fatalf("doctor did not write the missing header: %v", err)
	}
	// And what it wrote is what the transcript says.
	all, err := session.List(dirs.Sessions())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.ID == first.ID && m.MessageCount != 2 {
			t.Fatalf("repaired header = %+v, want the transcript's two messages", m)
		}
	}
}
