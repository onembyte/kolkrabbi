package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoverRejectsMissingProviderCLI(t *testing.T) {
	err := Handover(context.Background(), "kolk-provider-does-not-exist", nil, "")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Handover error = %v, want missing executable", err)
	}
}

func TestHandoverHonoursCancelledContextBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Handover(ctx, "kolk-provider-does-not-exist", nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("Handover error = %v, want context cancellation", err)
	}
}

// The login handover is the third child path, and the one that gets the
// keyboard: a vendor CLI signing the user in. It must receive the same
// environment the delegated children do -- normal configuration, no
// credential-shaped variables -- because "Kolkrabbi will not see credentials"
// is printed right before it starts, and a child that inherited the parent's
// OPENROUTER_API_KEY would make that line untrue in the other direction.
// GOFLAGS survives to prove this is a denylist, not an empty environment.
func TestHandoverNeverInheritsASentinelSecret(t *testing.T) {
	sentinels := []string{
		"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
		"AWS_SECRET_ACCESS_KEY", "GITHUB_PAT", "KOLK_TEST_SECRET_TOKEN", "SSH_PASSPHRASE",
		"OPENAI_API_KEY_BACKUP", "MY_SECRET_2", "DB_PASSWORD_PROD", "MINIO_ACCESS_KEY", "REGISTRY_AUTHTOKEN",
	}
	for _, name := range sentinels {
		t.Setenv(name, name+"-canary")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	// Handover wires the child to the real stdout, so the proof travels through
	// a file named by an ordinary variable, which the denylist must let through.
	out := filepath.Join(t.TempDir(), "env.txt")
	t.Setenv("KOLK_TEST_HANDOVER_OUT", out)
	var script strings.Builder
	script.WriteString(`printf '%s' "$GOFLAGS" > "$KOLK_TEST_HANDOVER_OUT"; `)
	for _, name := range sentinels {
		fmt.Fprintf(&script, `printf '|%%s' "$%s" >> "$KOLK_TEST_HANDOVER_OUT"; `, name)
	}
	if err := Handover(context.Background(), "sh", []string{"-c", script.String()}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "-mod=mod" + strings.Repeat("|", len(sentinels))
	if string(got) != want {
		t.Fatalf("handover child environment = %q, want %q", got, want)
	}
}
