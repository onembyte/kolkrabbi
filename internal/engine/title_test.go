package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type titlingBackend struct {
	calls int
	reply string
	err   error
}

func (b *titlingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	if b.err != nil {
		return provider.Message{}, provider.Meta{}, b.err
	}
	return provider.Message{Role: "assistant", Content: b.reply}, provider.Meta{}, nil
}

func twoTurns() []provider.Message {
	return []provider.Message{
		{Role: "user", Content: "the config parser drops trailing commas"},
		{Role: "assistant", Content: "found it in token.go"},
		{Role: "user", Content: "fix it and add a test"},
		{Role: "assistant", Content: "done"},
	}
}

func TestSessionIsNamedAfterEnoughHasHappened(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(twoTurns())
	backend := &titlingBackend{reply: "fix trailing comma parsing"}
	agent := &Agent{Options: Options{Sess: session, Backend: backend, Out: &strings.Builder{}}}

	agent.titleSessionIfNeeded(context.Background())

	if session.SessionTitle() != "fix trailing comma parsing" {
		t.Fatalf("title = %q", session.SessionTitle())
	}
}

func TestASessionIsNamedOnlyOnce(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(twoTurns())
	backend := &titlingBackend{reply: "fix trailing comma parsing"}
	agent := &Agent{Options: Options{Sess: session, Backend: backend, Out: &strings.Builder{}}}

	for range 3 {
		agent.titleSessionIfNeeded(context.Background())
	}

	// A title that keeps changing under the user is worse than a mediocre one
	// that stays put, and each change costs a model call.
	if backend.calls != 1 {
		t.Fatalf("named %d times, want once", backend.calls)
	}
}

func TestAShortSessionIsNotNamedYet(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages([]provider.Message{{Role: "user", Content: "hello"}})
	backend := &titlingBackend{reply: "something"}
	agent := &Agent{Options: Options{Sess: session, Backend: backend, Out: &strings.Builder{}}}

	agent.titleSessionIfNeeded(context.Background())

	if backend.calls != 0 {
		t.Fatal("a session with one turn was named before there was anything to name")
	}
}

func TestAFailedNamingIsSilentAndHarmless(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(twoTurns())
	var out strings.Builder
	agent := &Agent{Options: Options{
		Sess: session, Out: &out,
		Backend: &titlingBackend{err: context.DeadlineExceeded},
	}}

	agent.titleSessionIfNeeded(context.Background())

	// Naming is a nicety nobody asked for; complaining about it would be noise.
	if out.String() != "" {
		t.Fatalf("out = %q, want silence", out.String())
	}
}

func TestNamingNeverOverwritesAChosenTitle(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(twoTurns())
	// A fake marks itself titled once, which stands in for a user rename.
	session.SetAutoTitle("already named by a person")
	backend := &titlingBackend{reply: "a model's idea"}
	agent := &Agent{Options: Options{Sess: session, Backend: backend, Out: &strings.Builder{}}}

	agent.titleSessionIfNeeded(context.Background())

	if session.SessionTitle() != "already named by a person" {
		t.Fatalf("title = %q, want the existing one kept", session.SessionTitle())
	}
}
