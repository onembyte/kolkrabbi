// Package secret holds API keys without leaking them.
//
// The threat here is not an attacker. It is a stack trace pasted into a GitHub
// issue, a --verbose log shared in a Discord thread, a session transcript
// committed to a repository. Keys escape through helpfulness, and every one of
// those paths is some code printing a struct it did not think contained a
// secret.
//
// So the defence is a type that cannot be printed by accident:
//
//	fmt.Println(key)          // sk-or-v1-…f4a2
//	fmt.Printf("%v", key)     // sk-or-v1-…f4a2
//	fmt.Printf("%+v", key)    // sk-or-v1-…f4a2
//	fmt.Printf("%#v", key)    // sk-or-v1-…f4a2
//	log.Printf("%s", cfg)     // sk-or-v1-…f4a2  (nested in any struct)
//	json.Marshal(key)         // "sk-or-v1-…f4a2"
//	key.Reveal()              // sk-or-v1-abcdef…f4a2  — the only way, and it is greppable
//
// The one place this defence historically fails is HTTP. A Secret that redacts
// perfectly still leaks the moment it becomes an Authorization header, because
// %+v on an *http.Request prints Header, and Header is a plain map of strings.
// That is why AuthTransport exists and why it works on a clone: the request the
// caller holds never contains the token at all.
package secret

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/redact"
)

// Secret is an API key. Its zero value is "no key", which is a valid state:
// kolk must run without one until the moment it needs to call a model.
type Secret struct {
	v string
}

// New wraps a raw key. Surrounding whitespace is trimmed, because the single
// most common way to enter one is pasting, and a trailing newline from a
// heredoc or a copied line produces a 401 with no explanation.
func New(raw string) Secret {
	value := strings.TrimSpace(raw)
	redact.Register(value)
	return Secret{v: value}
}

// Reveal returns the real key. It is deliberately the only way to get it, and
// deliberately named so that `grep -rn Reveal` is a complete audit of every
// place in the tree where a key becomes an ordinary string.
func (s Secret) Reveal() string { return s.v }

// IsZero reports whether there is no key.
func (s Secret) IsZero() bool { return s.v == "" }

// String is the redacted form. Every other formatting path routes here.
func (s Secret) String() string { return Redact(s.v) }

// GoString covers %#v, which otherwise prints the struct's fields.
func (s Secret) GoString() string { return Redact(s.v) }

// Format covers every verb, including %+v and %#v on a struct that merely
// contains a Secret. Without this, one flag on one Printf undoes the type.
func (s Secret) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v', 's', 'q':
		if verb == 'q' {
			fmt.Fprintf(f, "%q", Redact(s.v))
			return
		}
		fmt.Fprint(f, Redact(s.v))
	default:
		fmt.Fprintf(f, "%%!%c(secret.Secret=%s)", verb, Redact(s.v))
	}
}

// MarshalJSON writes the redacted form.
//
// This is asymmetric on purpose: a Secret that wanders into a debug dump, an
// event payload or a session transcript must come out redacted, and storing one
// deliberately is a different, explicit act that calls Reveal. Safe by default;
// unsafe only where someone typed the word.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(Redact(s.v)) }

// UnmarshalJSON reads a raw key, so a credentials file round-trips into a
// Secret rather than a plain string that can be printed.
func (s *Secret) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*s = New(raw)
	return nil
}

// Redact renders a key so it can be recognised but not used.
//
// It keeps a prefix, because the prefix is what tells a person which key this
// is — sk-or-v1 is OpenRouter, sk-ant is Anthropic — and a suffix, because that
// is how you check it is the one you meant to paste. A short string reveals
// nothing at all: with fewer than 12 characters there is no way to show both
// ends without showing most of it.
func Redact(key string) string {
	if strings.TrimSpace(key) == "" {
		return "(none)"
	}
	return redact.Mask(key)
}

// Scrub replaces every key-shaped substring in arbitrary text.
//
// This runs over anything kolk persists or transmits that a model or a shell
// command could have put a key into: tool output, session transcripts, error
// messages, bus events. `echo $OPENROUTER_API_KEY` is a command a model will
// eventually run, and its output goes straight into a transcript.
func Scrub(text string) string {
	return redact.Scrub(text)
}

// ScrubError wraps an error so its message is scrubbed when printed. Provider
// errors quote response bodies, and a misconfigured gateway will happily echo
// the Authorization header it received straight back.
func ScrubError(err error) error {
	if err == nil {
		return nil
	}
	return scrubbedError{err}
}

type scrubbedError struct{ err error }

func (e scrubbedError) Error() string { return Scrub(e.err.Error()) }
func (e scrubbedError) Unwrap() error { return e.err }
