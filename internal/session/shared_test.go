package session

import (
	"strings"
	"testing"
	"time"
)

func card(id, cwd string, state State) Card {
	return Card{ID: id, CWD: cwd, Updated: time.Now(), State: state}
}

// Two live sessions in one checkout will edit each other's files, and each
// one's /undo will restore over the other's work. What item 27 refuses is
// silence about it.
func TestSharedCheckoutsFindsTwoLiveSessionsInOneDirectory(t *testing.T) {
	shared := SharedCheckouts([]Card{
		card("s_a", "/w/project", StateLive),
		card("s_b", "/w/project", StateLive),
		card("s_c", "/w/other", StateLive),
	})
	if len(shared) != 1 {
		t.Fatalf("found %d shared checkouts, want 1: %#v", len(shared), shared)
	}
	if shared[0].Dir != "/w/project" {
		t.Errorf("directory = %q, want /w/project", shared[0].Dir)
	}
	if len(shared[0].Sessions) != 2 {
		t.Errorf("sessions = %v, want both", shared[0].Sessions)
	}
}

// An idle session is not competing for anything: its process is gone.
func TestSharedCheckoutsIgnoresIdleSessions(t *testing.T) {
	if shared := SharedCheckouts([]Card{
		card("s_a", "/w/project", StateLive),
		card("s_b", "/w/project", StateIdle),
	}); len(shared) != 0 {
		t.Errorf("an idle session was reported as sharing a checkout: %#v", shared)
	}
}

// A session with no recorded directory cannot be said to share one. Guessing
// would produce a warning about nothing, which is how warnings get ignored.
func TestSharedCheckoutsIgnoresSessionsWithNoDirectory(t *testing.T) {
	if shared := SharedCheckouts([]Card{
		card("s_a", "", StateLive),
		card("s_b", "", StateLive),
	}); len(shared) != 0 {
		t.Errorf("sessions with no directory were reported as sharing one: %#v", shared)
	}
}

// Three in one directory is one warning, not three: the thing a person needs
// to know is that this checkout is contended.
func TestSharedCheckoutsReportsOneWarningPerDirectory(t *testing.T) {
	shared := SharedCheckouts([]Card{
		card("s_a", "/w/p", StateLive),
		card("s_b", "/w/p", StateLive),
		card("s_c", "/w/p", StateLive),
	})
	if len(shared) != 1 {
		t.Fatalf("found %d warnings for one directory, want 1", len(shared))
	}
	if len(shared[0].Sessions) != 3 {
		t.Errorf("sessions = %v, want all three", shared[0].Sessions)
	}
}

// The order is fixed so the same situation reads the same way twice.
func TestSharedCheckoutsIsOrdered(t *testing.T) {
	shared := SharedCheckouts([]Card{
		card("s_z", "/w/b", StateLive),
		card("s_y", "/w/b", StateLive),
		card("s_b", "/w/a", StateLive),
		card("s_a", "/w/a", StateLive),
	})
	if len(shared) != 2 {
		t.Fatalf("found %d shared checkouts, want 2", len(shared))
	}
	if shared[0].Dir != "/w/a" || shared[1].Dir != "/w/b" {
		t.Errorf("directories are not sorted: %q then %q", shared[0].Dir, shared[1].Dir)
	}
	if strings.Join(shared[0].Sessions, ",") != "s_a,s_b" {
		t.Errorf("session ids are not sorted: %v", shared[0].Sessions)
	}
}
