package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// A key typed after /key sits in the terminal scrollback, the shell's history
// when kolk was started with it, and `ps` while it runs. Outside the TUI --
// where kolk can read it hidden -- the pasted form is refused with the reason
// and the two ways in, and nothing is stored or verified.
func TestKeyOnTheCommandLineIsRefusedOutsideTheTUI(t *testing.T) {
	d := isolateHome(t)
	for _, tc := range []struct {
		args  []string
		piped string
	}{
		{[]string{"key", cliKeyCanary}, "/key -"},
		{[]string{"key", "openrouter", cliKeyCanary}, "/key openrouter -"},
	} {
		args := tc.args
		a, out, errOut := newTestApp(t, "")
		var touched int
		a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
			touched++
			return provider.KeyStatus{}, nil
		}
		a.setCredential = func(context.Context, string, keystore.Ref, secret.Secret, keystore.WriteMetadata) error {
			touched++
			return nil
		}
		if code := runRetiredVerb(t, a, args...); code != ExitUsage {
			t.Fatalf("/key %v exit = %d, want %d (stderr: %s)", args[1:], code, ExitUsage, errOut)
		}
		if touched != 0 {
			t.Errorf("a refused key was verified or stored (%d calls)", touched)
		}
		for _, want := range []string{tc.piped, "hidden"} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("refusal omits %q: %s", want, errOut)
			}
		}
		assertNoCLIKey(t, out.String(), errOut.String())
	}
	if _, err := os.Stat(d.CredentialsFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused key created a manifest: %v", err)
	}
}

// `/key` alone reads the key without echo. Under a test, stdin is a pipe, so
// the read is one line from it and the source is recorded as stdin.
func TestKeyAloneReadsTheKeyHidden(t *testing.T) {
	d := isolateHome(t)
	a, out, errOut := newTestApp(t, cliKeyCanary+"\n")
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) { return provider.KeyStatus{}, nil }
	if code := runRetiredVerb(t, a, "key"); code != ExitOK {
		t.Fatalf("/key exit = %d, stderr: %s", code, errOut)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
	stored, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil || stored.Reveal() != cliKeyCanary {
		t.Fatalf("stored = %v, %v", stored, err)
	}
}

// `/key <provider>` names the provider and still reads the key hidden.
func TestKeyWithAProviderReadsTheKeyHidden(t *testing.T) {
	d := isolateHome(t)
	const key = "0123456789abcdef0123456789abcdef"
	a, out, errOut := newTestApp(t, key+"\n")
	if code := runRetiredVerb(t, a, "key", "mistral"); code != ExitOK {
		t.Fatalf("/key mistral exit = %d, stderr: %s", code, errOut)
	}
	if strings.Contains(out.String(), key) || strings.Contains(errOut.String(), key) {
		t.Fatal("the key was echoed")
	}
	stored, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "mistral"})
	if err != nil || stored.Reveal() != key {
		t.Fatalf("stored = %v, %v", stored, err)
	}
}

// Inside the TUI kolk owns the terminal and cannot yet hide input (V34.1d.4c),
// so the pasted form stays accepted there until it can. This pins the interim
// so that 4c has to flip it on purpose.
func TestKeyOnTheLineIsStillAcceptedInsideTheTUIUntilItCanBeHidden(t *testing.T) {
	isolateHome(t)
	a, out, errOut := newTestApp(t, "")
	a.terminalOwned = func() bool { return true }
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) { return provider.KeyStatus{}, nil }
	if code := runRetiredVerb(t, a, "key", cliKeyCanary); code != ExitOK {
		t.Fatalf("/key <key> in the TUI exit = %d, stderr: %s", code, errOut)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
}
