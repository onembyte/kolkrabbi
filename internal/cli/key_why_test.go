package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// Plan 05 §1: `kolk key --why [provider]` renders the chain — the flag link
// that is empty by design, KOLK_API_KEY, the provider's own variable, the
// store — with the first hit and what it shadowed, and never a value.
func TestKeyWhyRendersTheChainWithTheFirstHit(t *testing.T) {
	d := isolateHome(t)
	if err := keystore.NewFileStore(d.CredentialsFile()).Set(context.Background(), keystore.Ref{Provider: "openrouter"}, secret.New("sk-or-v1-"+strings.Repeat("s", 24))); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-"+strings.Repeat("e", 24))
	a, out, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "key", "--why"); code != ExitOK {
		t.Fatalf("key --why exit = %d: %s", code, out.String())
	}
	text := out.String()
	for _, want := range []string{"0  flag", "none", "1  KOLK_API_KEY", "absent", "2  OPENROUTER_API_KEY", "hit", "3  store", "shadowed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("--why omits %q:\n%s", want, text)
		}
	}
	// Masks only — the prefix and the last four, the way /doctor shows a key —
	// never the value itself.
	if strings.Contains(text, "sk-or-v1-"+strings.Repeat("e", 24)) || strings.Contains(text, "sk-or-v1-"+strings.Repeat("s", 24)) {
		t.Fatalf("--why printed a value:\n%s", text)
	}
}

// A store that is locked or unavailable stops the chain with its own remedy;
// it never reads as "no key".
func TestALockedStoreIsNamedNotMistakenForNoKey(t *testing.T) {
	advice := keyStoreAdvice(errors.New("wrapped: " + keystore.ErrLocked.Error()))
	if advice != "" {
		t.Fatalf("an unrelated error got store advice: %q", advice)
	}
	for err, want := range map[error]string{
		keystore.ErrLocked:      "locked",
		keystore.ErrTimeout:     "did not answer",
		keystore.ErrUnavailable: "unavailable",
	} {
		if got := keyStoreAdvice(err); !strings.Contains(got, want) || strings.Contains(got, "no key") {
			t.Fatalf("advice for %v = %q, want %q and never 'no key'", err, got, want)
		}
	}
}
