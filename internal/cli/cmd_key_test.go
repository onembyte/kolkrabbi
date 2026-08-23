package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

const cliKeyCanary = "sk-or-v1-0123456789abcdef0123456789abcdef"

func TestKeyCommandInfersVerifiesAndStoresOpenRouter(t *testing.T) {
	d := isolateHome(t)
	a, out, errOut := newTestApp("")
	remaining := 74.5
	var verified int
	a.verifyOpenRouter = func(_ context.Context, key secret.Secret) (provider.KeyStatus, error) {
		verified++
		if key.Reveal() != cliKeyCanary {
			t.Errorf("verifier received %v", key)
		}
		return provider.KeyStatus{RemainingUSD: &remaining}, nil
	}

	if code := a.main(context.Background(), []string{"key", cliKeyCanary}); code != ExitOK {
		t.Fatalf("kolk key exit = %d, stderr:\n%s", code, errOut)
	}
	if verified != 1 {
		t.Errorf("verification calls = %d, want 1", verified)
	}
	got := out.String()
	for _, want := range []string{"openrouter", "sk-or-v1-…cdef", "verified", "$74.50 credits", "free tier: no", d.CredentialsFile(), "plain text"} {
		if !strings.Contains(got, want) {
			t.Errorf("success output omits %q:\n%s", want, got)
		}
	}
	assertNoCLIKey(t, out.String(), errOut.String())

	stored, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Reveal() != cliKeyCanary {
		t.Errorf("stored credential = %v", stored)
	}
	entry, err := keystore.NewFileStore(d.CredentialsFile()).Probe(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source != "paste" || entry.Verified.IsZero() {
		t.Errorf("stored metadata = %+v", entry)
	}
	if _, err := os.Stat(d.Config); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("napkin key command touched the config directory: %v", err)
	}
}

func TestKeyCommandStoresWhenVerificationIsOffline(t *testing.T) {
	d := isolateHome(t)
	a, out, errOut := newTestApp("")
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
		return provider.KeyStatus{}, fmt.Errorf("offline while checking %s", cliKeyCanary)
	}

	if code := a.main(context.Background(), []string{"key", cliKeyCanary}); code != ExitOK {
		t.Fatalf("kolk key exit = %d, stderr:\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "stored, not verified") || !strings.Contains(errOut.String(), "warning:") {
		t.Errorf("offline output:\nstdout: %s\nstderr: %s", out, errOut)
	}
	if _, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "openrouter"}); err != nil {
		t.Errorf("offline verification discarded the key: %v", err)
	}
	entry, err := keystore.NewFileStore(d.CredentialsFile()).Probe(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Verified.IsZero() {
		t.Errorf("failed verification recorded a timestamp: %+v", entry)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
}

func TestKeyCommandDenialsAndUnknownShapesHaveNoSideEffects(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"subscription", "sk-ant-oat01-0123456789abcdef", "subscription token"},
		{"github", "ghp_0123456789abcdefghijklmnopqrstuvwxyz", "GitHub token"},
		{"unknown", "0123456789abcdef0123456789abcdef", "kolk key <provider> -"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := isolateHome(t)
			a, _, errOut := newTestApp("")
			var verified, stored int
			a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
				verified++
				return provider.KeyStatus{}, nil
			}
			a.setCredential = func(context.Context, string, keystore.Ref, secret.Secret, keystore.WriteMetadata) error {
				stored++
				return nil
			}
			if code := a.main(context.Background(), []string{"key", tt.key}); code != ExitUsage {
				t.Errorf("exit = %d, want %d", code, ExitUsage)
			}
			if verified != 0 || stored != 0 {
				t.Errorf("denied input made %d verification and %d store calls", verified, stored)
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Errorf("error omits %q: %s", tt.want, errOut)
			}
			if _, err := os.Stat(d.CredentialsFile()); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("denied input created a manifest: %v", err)
			}
		})
	}
}

func TestKeyCommandRefusesArgvInCIAndAcceptsStdin(t *testing.T) {
	d := isolateHome(t)
	t.Setenv("CI", "1")
	a, _, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"key", cliKeyCanary}); code != ExitUsage {
		t.Fatalf("positional key in CI exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut.String(), "kolk key -") {
		t.Errorf("CI refusal omits safe stdin form: %s", errOut)
	}
	if _, err := os.Stat(d.CredentialsFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("CI refusal created a manifest: %v", err)
	}

	a, out, errOut := newTestApp(cliKeyCanary + "\n")
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
		return provider.KeyStatus{}, nil
	}
	if code := a.main(context.Background(), []string{"key", "-"}); code != ExitOK {
		t.Fatalf("stdin key in CI exit = %d, stderr: %s", code, errOut)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
	entry, err := keystore.NewFileStore(d.CredentialsFile()).Probe(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source != "stdin" {
		t.Errorf("stdin source = %q", entry.Source)
	}
}

func TestKeyCommandCIGuidancePreservesAnExplicitProvider(t *testing.T) {
	isolateHome(t)
	t.Setenv("CI", "true")
	a, _, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"key", "mistral", "0123456789abcdef0123456789abcdef"}); code != ExitUsage {
		t.Fatalf("explicit positional key in CI exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut.String(), "kolk key mistral -") {
		t.Errorf("CI guidance would lose the explicit provider: %s", errOut)
	}
}

func TestKeyCommandExplicitProviderIsTheUnknownShapeEscape(t *testing.T) {
	d := isolateHome(t)
	const key = "0123456789abcdef0123456789abcdef"
	a, out, errOut := newTestApp(key + "\n")
	var verified int
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
		verified++
		return provider.KeyStatus{}, nil
	}
	if code := a.main(context.Background(), []string{"key", "mistral", "-"}); code != ExitOK {
		t.Fatalf("explicit provider exit = %d, stderr: %s", code, errOut)
	}
	if verified != 0 {
		t.Errorf("Mistral key triggered %d OpenRouter requests", verified)
	}
	if !strings.Contains(out.String(), "mistral") || !strings.Contains(out.String(), "stored, verification unavailable") {
		t.Errorf("explicit provider output: %s", out)
	}
	stored, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "mistral"})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Reveal() != key {
		t.Errorf("stored credential = %v", stored)
	}
}

func TestKeyCommandStoreFailureNamesAWorkingRecoveryWithoutLeaking(t *testing.T) {
	isolateHome(t)
	a, out, errOut := newTestApp("")
	a.verifyOpenRouter = func(context.Context, secret.Secret) (provider.KeyStatus, error) {
		return provider.KeyStatus{}, nil
	}
	a.setCredential = func(context.Context, string, keystore.Ref, secret.Secret, keystore.WriteMetadata) error {
		return fmt.Errorf("disk failure involving %s", cliKeyCanary)
	}
	if code := a.main(context.Background(), []string{"key", cliKeyCanary}); code != ExitError {
		t.Fatalf("store failure exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(errOut.String(), "couldn't save") || !strings.Contains(errOut.String(), "kolk key <API_KEY>") {
		t.Errorf("store recovery is incomplete: %s", errOut)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
}

func TestKeyCommandStoreFailureScrubsAnUnknownShape(t *testing.T) {
	isolateHome(t)
	const unknownKey = "0123456789abcdef0123456789abcdef"
	a, out, errOut := newTestApp(unknownKey + "\n")
	a.setCredential = func(context.Context, string, keystore.Ref, secret.Secret, keystore.WriteMetadata) error {
		return fmt.Errorf("disk rejected %s", unknownKey)
	}
	if code := a.main(context.Background(), []string{"key", "mistral", "-"}); code != ExitError {
		t.Fatalf("store failure exit = %d, want %d", code, ExitError)
	}
	if strings.Contains(out.String(), unknownKey) || strings.Contains(errOut.String(), unknownKey) {
		t.Errorf("store failure leaked unknown-shaped credential:\nstdout: %s\nstderr: %s", out, errOut)
	}
}

func TestKeyCommandUsageDoesNotReadOrWrite(t *testing.T) {
	isolateHome(t)
	for _, args := range [][]string{{"key"}, {"key", "one", "two", "three"}} {
		a, _, _ := newTestApp("")
		if code := a.main(context.Background(), args); code != ExitUsage {
			t.Errorf("kolk %v exit = %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestLegacyConfigSetKeyIsAHardRedirectWithoutSideEffects(t *testing.T) {
	d := isolateHome(t)
	a, out, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-key", cliKeyCanary}); code != ExitUsage {
		t.Fatalf("legacy set-key exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut.String(), "kolk key <API_KEY>") {
		t.Errorf("legacy redirect omits the supported command: %s", errOut)
	}
	assertNoCLIKey(t, out.String(), errOut.String())
	for _, path := range []string{d.ConfigFile(), d.CredentialsFile()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("legacy redirect touched %s: %v", path, err)
		}
	}
}

func assertNoCLIKey(t *testing.T, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(value, cliKeyCanary) {
			t.Errorf("CLI output leaked the credential:\n%s", value)
		}
	}
}
