package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/internal/term"
)

type verifyOpenRouterFunc func(context.Context, secret.Secret) (provider.KeyStatus, error)

type setCredentialFunc func(
	context.Context,
	string,
	keystore.Ref,
	secret.Secret,
	keystore.WriteMetadata,
) error

func (a *app) initKeyDependencies() {
	if a.verifyOpenRouter == nil {
		a.verifyOpenRouter = provider.OpenRouterVerifier{}.Verify
	}
	if a.setCredential == nil {
		a.setCredential = func(
			ctx context.Context,
			path string,
			ref keystore.Ref,
			value secret.Secret,
			meta keystore.WriteMetadata,
		) error {
			return keystore.NewFileStore(path).SetWithMetadata(ctx, ref, value, meta)
		}
	}
	if a.now == nil {
		a.now = time.Now
	}
}

// runKey is intentionally a small, non-interactive command. A single value
// gets safe shape inference; a provider plus value is the explicit escape for
// new key shapes. A literal dash is the only stdin spelling, so an accidental
// pipe can never silently replace a credential.
func (a *app) runKey(ctx context.Context, args []string) error {
	if len(args) > 2 {
		return usagef("%s", usageLine("key"))
	}
	// A key on the line is refused everywhere: it is in the scrollback, in
	// shell history when kolk was started with it, and in `ps` while it runs.
	// Inside the TUI the hidden read goes through the masked overlay.
	inTUI := a.terminalOwned != nil && a.terminalOwned()
	providerName := ""
	source, raw := "", ""
	switch {
	case len(args) == 0:
		source = "prompt"
	case len(args) == 1 && args[0] == "-":
		source = "stdin"
	case len(args) == 1 && looksLikeAProviderName(args[0]):
		providerName, source = args[0], "prompt"
	case len(args) == 1:
		return refuseKeyOnTheLine("")
	case args[1] == "-":
		providerName, source = args[0], "stdin"
	default:
		return refuseKeyOnTheLine(args[0])
	}
	switch source {
	case "stdin":
		if inTUI {
			return fmt.Errorf("reading a key from stdin needs a terminal this session already owns; run /key and paste it at the hidden prompt, or pipe it in when starting kolk")
		}
		value, err := io.ReadAll(io.LimitReader(a.in, keystore.MaxValueBytes+2))
		if err != nil {
			return fmt.Errorf("reading API key from stdin: %w", secret.ScrubError(err))
		}
		raw = string(value)
	case "prompt":
		const ask = "Paste the API key (it will not be shown): "
		if inTUI {
			if a.readHidden == nil {
				return fmt.Errorf("this session owns the terminal and has no hidden prompt; pipe the key in when starting kolk")
			}
			value, ok := a.readHidden(ctx, ask)
			if !ok {
				return usagef("no key entered")
			}
			raw = value
			break
		}
		value, hidden, err := a.readSecretLine(ask)
		if err != nil {
			return fmt.Errorf("reading API key: %w", secret.ScrubError(err))
		}
		raw = value
		if !hidden {
			source = "stdin" // a pipe, not a person: recorded as such
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return usagef("API key is empty; %s", usageLine("key"))
	}
	if len(raw) > keystore.MaxValueBytes {
		return usagef("API key exceeds the portable %d-byte limit", keystore.MaxValueBytes)
	}

	classification := redact.Classify(raw)
	if classification.Denial != redact.DenyNone {
		return deniedKey(classification.Denial)
	}
	if providerName == "" {
		if classification.Provider == "" {
			return usagef("API key provider is not unambiguous; use `/key <provider> -`")
		}
		providerName = classification.Provider
	}

	ref, err := keystore.NormalizeRef(keystore.Ref{Provider: providerName})
	if err != nil {
		return usagef("invalid provider %q", providerName)
	}
	if classification.Provider != "" && classification.Provider != ref.Provider {
		return usagef("API key shape belongs to %s, not %s", classification.Provider, ref.Provider)
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	if err := a.migrateLegacyCredential(ctx, dirs); err != nil {
		return err
	}

	value := secret.New(raw)
	meta := keystore.WriteMetadata{Source: source}
	detail := "stored, verification unavailable"
	var verificationWarning error
	if ref.Provider == "openrouter" {
		status, verifyErr := a.verifyOpenRouter(ctx, value)
		if verifyErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			verificationWarning = secret.ScrubError(verifyErr)
			detail = "stored, not verified"
		} else {
			meta.Verified = a.now().UTC()
			detail = "verified"
			if status.RemainingUSD != nil {
				detail += fmt.Sprintf(" · $%.2f credits", *status.RemainingUSD)
			}
			detail += " · free tier: " + yesNo(status.FreeTier)
		}
	}

	path := dirs.CredentialsFile()
	if err := a.setCredential(ctx, path, ref, value, meta); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return guided(
			fmt.Sprintf("couldn't save the API key: %s", scrubCredentialError(err, raw)),
			"Try again with: /key",
		)
	}

	if verificationWarning != nil {
		fmt.Fprintf(a.stderr, "warning: couldn't verify the saved %s key: %v\n", ref.Provider, verificationWarning)
	}
	fmt.Fprintf(a.stdout, "%-12s %s   %s\n", ref.Provider, redact.Mask(raw), detail)
	fmt.Fprintf(a.stdout, "saved to     %s   (owner-only plain text)\n", path)
	return nil
}

// scrubCredentialError protects both the provider shapes known globally and
// the exact value accepted through the explicit-provider escape. The latter
// matters for a new provider whose format is not in keyshapes.json yet.
func scrubCredentialError(err error, raw string) string {
	message := secret.Scrub(err.Error())
	return strings.ReplaceAll(message, raw, redact.Mask(raw))
}

func deniedKey(kind redact.Denial) error {
	switch kind {
	case redact.DenyClaudeSubscription:
		return usagef("that is a Claude subscription token, not an API key; Kolkrabbi will not store it")
	case redact.DenyGitHub:
		return usagef("that is a GitHub token; Kolkrabbi will not store it as a model API key")
	case redact.DenyAWS:
		return usagef("that is an AWS credential; Kolkrabbi will not store it as a model API key")
	case redact.DenySlack:
		return usagef("that is a Slack token; Kolkrabbi will not store it as a model API key")
	case redact.DenyPrivateKey:
		return usagef("that contains a private key; Kolkrabbi will not store it")
	default:
		return usagef("that credential shape is not accepted")
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// looksLikeAProviderName tells `/key mistral` from `/key <a pasted key>`: a
// valid keystore provider name, short, and not something the shape table
// recognises as a credential. Anything else is treated as a key, which is the
// safe side -- a key mistaken for a provider name would be echoed back.
func looksLikeAProviderName(arg string) bool {
	if len(arg) > 24 {
		return false
	}
	if c := redact.Classify(arg); c.Provider != "" || c.Denial != redact.DenyNone {
		return false
	}
	_, err := keystore.NormalizeRef(keystore.Ref{Provider: arg})
	return err == nil
}

// refuseKeyOnTheLine is the refusal for a pasted key, naming the ways in and
// never the key. With a provider given, the piped form keeps it.
func refuseKeyOnTheLine(providerName string) error {
	piped := "/key -"
	prompt := "/key"
	if providerName != "" {
		piped = "/key " + providerName + " -"
		prompt = "/key " + providerName
	}
	return usagef("kolk does not take a key on the command line: it stays in the scrollback, in shell history and in `ps`. "+
		"Run `%s` and paste it at the hidden prompt, or pipe it in with `%s`", prompt, piped)
}

// readSecretLine reads a credential from the person at the terminal with echo
// off, or one line from stdin when stdin is a pipe. The prompt goes to stderr
// so stdout stays scriptable. hidden reports which of the two happened.
func (a *app) readSecretLine(prompt string) (value string, hidden bool, err error) {
	if term.IsStdinTerminal() {
		fmt.Fprint(a.stderr, prompt)
		line, err := term.ReadPassword(os.Stdin)
		fmt.Fprintln(a.stderr)
		return strings.TrimSpace(line), true, err
	}
	line, err := a.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	return strings.TrimSpace(line), false, nil
}
