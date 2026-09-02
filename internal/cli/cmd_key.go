package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/secret"
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
	if len(args) != 1 && len(args) != 2 {
		return usagef("%s", usageLine("key"))
	}

	providerName := ""
	explicitProvider := len(args) == 2
	input := args[0]
	if explicitProvider {
		providerName = args[0]
		input = args[1]
	}

	source := "paste"
	raw := input
	if input == "-" {
		// A session reads the terminal from its own goroutine, so reading it
		// here would compete for the user's keystrokes and look like a hang.
		if a.terminalOwned != nil && a.terminalOwned() {
			// There is no outside-session `kolk key` to fall back to any more
			// (docs/plan/09, 2026-09-02), so the advice has to be something
			// that works from here: paste it, or pipe it into the session.
			return fmt.Errorf("reading a key from stdin needs a terminal this session already owns; paste the key after /key instead, or pipe it in when starting kolk")
		}
		source = "stdin"
		value, err := io.ReadAll(io.LimitReader(a.in, keystore.MaxValueBytes+2))
		if err != nil {
			return fmt.Errorf("reading API key from stdin: %w", secret.ScrubError(err))
		}
		raw = string(value)
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
	if source == "paste" && strings.TrimSpace(os.Getenv("CI")) != "" {
		safeCommand := "/key -"
		if explicitProvider {
			safeCommand = "/key " + ref.Provider + " -"
		}
		return usagef("refusing an API key in process arguments while CI is set; use `%s`", safeCommand)
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
			"Try again with: /key <API_KEY>",
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
