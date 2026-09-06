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
	if len(args) >= 1 && args[0] == "--why" {
		return a.runKeyWhy(ctx, args[1:])
	}
	if len(args) >= 2 && args[0] == "--backend" {
		return a.runKeyBackend(ctx, args[1], args[2:])
	}
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

// runKeyWhy is `kolk key --why [provider]` (plan 05 §1): the chain rendered
// link by link — the flag that is empty by design, KOLK_API_KEY, the
// provider's own variable, the store — with the first hit and what it
// shadowed, masks only, never a value.
func (a *app) runKeyWhy(ctx context.Context, args []string) error {
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	providers := []string{"openrouter"}
	if len(args) > 0 {
		providers = []string{strings.ToLower(strings.TrimSpace(args[0]))}
	} else {
		providers = append(providers, provider.KeyedVendors()...)
	}
	store := routedStore(dirs.CredentialsFile(), a.keychainSpawner)
	for _, name := range providers {
		res, err := keystore.Resolve(ctx, keystore.Ref{Provider: name, Profile: "default"}, os.Getenv, store)
		fmt.Fprintf(a.stdout, "%s\n", name)
		for _, link := range res.Trace {
			detail := link.Detail
			if detail != "" {
				detail = "  " + detail
			}
			fmt.Fprintf(a.stdout, "  %d  %-20s %-9s%s\n", link.Rank, link.Name, link.Outcome, detail)
		}
		switch {
		case err == nil:
			fmt.Fprintf(a.stdout, "  → %s\n", res.Source)
		case errors.Is(err, keystore.ErrNotFound):
			fmt.Fprintln(a.stdout, "  → no credential; add one with `kolk key "+name+"`")
		default:
			fmt.Fprintf(a.stdout, "  → %v\n", err)
			if advice := keyStoreAdvice(err); advice != "" {
				fmt.Fprintf(a.stdout, "    %s\n", advice)
			}
		}
		if res.Warning != "" {
			fmt.Fprintf(a.stdout, "  ! %s\n", res.Warning)
		}
	}
	return nil
}

// keychainNotice is the one claim kolk makes for the keychain, exactly
// (plan 05 §3.1), said once when a credential first moves there.
const keychainNotice = "The OS keychain encrypts the credential at rest — against a stolen disk, a backup, a synced dotfiles directory. It gives you nothing against code running as you, which can read it back with no prompt."

// runKeyBackend is `kolk key --backend keychain|file [provider]` (plan 05
// §3.6): read the old copy, write the new one, read it back and compare,
// update the manifest, then delete the old — never delete-then-write. A
// failed last delete leaves an orphan that is said aloud, because a silently
// orphaned credential is one nobody rotates.
func (a *app) runKeyBackend(ctx context.Context, backendName string, rest []string) error {
	target := keystore.Backend(strings.ToLower(strings.TrimSpace(backendName)))
	if target != keystore.BackendFile && target != keystore.BackendKeychain {
		return usagef("kolk key --backend <keychain|file> [provider]")
	}
	providerName := "openrouter"
	if len(rest) > 0 {
		providerName = strings.ToLower(strings.TrimSpace(rest[0]))
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	routed := routedStore(dirs.CredentialsFile(), a.keychainSpawner)
	ref := keystore.Ref{Provider: providerName, Profile: "default"}
	entry, err := routed.Manifest.Probe(ctx, ref)
	if err != nil {
		return fmt.Errorf("no stored credential for %s to move: %w", providerName, secret.ScrubError(err))
	}
	if entry.Backend == target {
		fmt.Fprintf(a.stdout, "%s is already kept in the %s\n", ref, target)
		return nil
	}
	from, ok := routed.Backends[entry.Backend]
	if !ok {
		return fmt.Errorf("%s is kept in %s, which this machine cannot open", ref, entry.Backend)
	}
	to := routed.Backends[target]
	if err := to.Available(ctx); err != nil {
		return fmt.Errorf("the %s cannot be used here: %w", target, err)
	}
	value, err := from.Get(ctx, ref)
	if err != nil {
		if advice := keyStoreAdvice(err); advice != "" {
			return fmt.Errorf("%w\n  %s", secret.ScrubError(err), advice)
		}
		return secret.ScrubError(err)
	}
	if target == keystore.BackendKeychain {
		fmt.Fprintln(a.stderr, "◆ "+keychainNotice)
	}
	// The new backend writes, proves by reading back, and records the route
	// itself; the file backend's Set does the same for the file.
	if err := to.Set(ctx, ref, value); err != nil {
		return fmt.Errorf("writing %s to the %s: %w", ref, target, secret.ScrubError(err))
	}
	back, err := to.Get(ctx, ref)
	if err != nil || back.Reveal() != value.Reveal() {
		return fmt.Errorf("the %s did not read back what was written for %s; the old copy in %s is untouched", target, ref, entry.Backend)
	}
	if err := deleteOrphan(ctx, from, ref); err != nil {
		fmt.Fprintf(a.stderr, "warning: an orphaned copy of %s remains in %s — remove it with `kolk key clean`: %v\n", ref, entry.Backend, secret.ScrubError(err))
	}
	fmt.Fprintf(a.stdout, "%s moved to the %s\n", ref, target)
	return nil
}

// deleteOrphan removes the old backend's copy after the manifest has moved
// on: the file's copy went with the row it overwrote; the keychain's item is
// deleted by account.
func deleteOrphan(ctx context.Context, from keystore.Store, ref keystore.Ref) error {
	if orphan, ok := from.(interface {
		DeleteItem(context.Context, keystore.Ref) error
	}); ok {
		return orphan.DeleteItem(ctx, ref)
	}
	return nil
}
