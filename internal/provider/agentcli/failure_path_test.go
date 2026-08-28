package agentcli

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func oneTurn(t *testing.T, backend *ClaudeBackend, text string) (provider.Message, error) {
	t.Helper()
	msg, _, err := backend.StreamChat(context.Background(), "opus",
		[]provider.Message{{Role: "user", Content: text}}, nil, nil)
	return msg, err
}

// B12.15b. The vendor CLI is uninstalled underneath a live session — a real
// case on a machine where `claude` came from a package manager. The turn must
// fail with a sentence naming the missing binary, not with a raw exec error
// that leaves the reader guessing which of two providers went away.
func TestAMissingVendorCLIIsNamedRatherThanRaw(t *testing.T) {
	backend := &ClaudeBackend{start: func(context.Context, string, []string) (lineProcess, error) {
		return nil, &exec.Error{Name: "claude", Err: exec.ErrNotFound}
	}}

	_, err := oneTurn(t, backend, "hi")
	if err == nil {
		t.Fatal("a turn with no CLI installed succeeded, want a failure")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error %q does not name the missing program", err)
	}
	// exec.ErrNotFound has to survive: the surface distinguishes "not installed"
	// from "installed and refused", and it cannot if the cause is flattened
	// into a string.
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("error %q loses exec.ErrNotFound, so nothing above can tell this from a refusal", err)
	}
}

// B12.15a. A plan login that expired mid-session. The CLI starts fine and then
// refuses the turn, which is a different failure from not being installed: the
// session must survive it, because signing in again in another terminal and
// retrying is exactly what the user will do.
func TestAnExpiredPlanLoginLeavesTheSessionUsable(t *testing.T) {
	starts := 0
	backend := &ClaudeBackend{start: func(context.Context, string, []string) (lineProcess, error) {
		starts++
		if starts == 1 {
			return &fakeLineProcess{lines: [][]byte{
				[]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Invalid API key · Please run /login"}`),
			}}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("signed back in")}, nil
	}}

	_, err := oneTurn(t, backend, "before")
	if err == nil {
		t.Fatal("a turn against an expired login succeeded, want a failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "login") {
		t.Errorf("error %q does not tell the user to sign in again", err)
	}

	// The session is not poisoned. A backend that refuses every later turn
	// because one failed would make signing in again pointless.
	msg, err := oneTurn(t, backend, "after")
	if err != nil {
		t.Fatalf("the session never recovered after a re-login: %v", err)
	}
	if !strings.Contains(msg.Content, "signed back in") {
		t.Errorf("second turn = %q, want the answer from the re-authenticated CLI", msg.Content)
	}
}

// The retry above must not replay a turn the user already saw part of. A stream
// that dies half-way is a different failure from one that never began, and
// printing the first half twice is worse than the error.
func TestAHalfStreamedTurnIsNotRetried(t *testing.T) {
	starts := 0
	backend := &ClaudeBackend{start: func(context.Context, string, []string) (lineProcess, error) {
		starts++
		if starts == 1 {
			return &fakeLineProcess{lines: [][]byte{
				[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"half an answ"}]}}`),
			}}, nil
		}
		return &fakeLineProcess{lines: claudeTurnFrames("a whole second answer")}, nil
	}}

	var seen strings.Builder
	_, _, err := backend.StreamChat(context.Background(), "opus",
		[]provider.Message{{Role: "user", Content: "hi"}}, nil, func(token string) { seen.WriteString(token) })
	if err == nil {
		t.Fatal("a turn whose stream died succeeded, want a failure")
	}
	if strings.Contains(seen.String(), "a whole second answer") {
		t.Fatalf("the turn was retried after streaming %q, so the user saw two answers to one question", seen.String())
	}
	if starts != 1 {
		t.Fatalf("started %d processes, want 1: a half-streamed turn must not be replayed", starts)
	}
}
