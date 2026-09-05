package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A base URL of the form https://token@host is how a credential ends up in
// shell history, in ps, in config in clear, and -- because net/http sends
// userinfo as Basic auth -- on the wire to whatever host the URL names. kolk
// refuses it wherever a base URL can arrive, and says why and what to do.
func TestABaseURLCarryingCredentialsIsRefusedBeforeAnyClientExists(t *testing.T) {
	d := storeFirstRunKey(t)
	for _, endpoint := range []string{
		"http://sk-token-here@host.invalid/v1",
		"https://user:password@host.invalid/v1",
		"https://user:password@openrouter.ai/api/v1",
	} {
		client, err := providerClientForEndpoint(context.Background(), endpoint, d.CredentialsFile())
		if err == nil {
			t.Fatalf("%s was accepted; client BaseURL=%q", endpoint, client.BaseURL)
		}
		if strings.Contains(err.Error(), "password") && strings.Contains(err.Error(), "user:password") {
			t.Fatalf("the refusal echoes the credential: %v", err)
		}
		if !strings.Contains(err.Error(), "/key") {
			t.Fatalf("the refusal does not say where a credential belongs: %v", err)
		}
	}
}

// The saved setting is the durable one: refused at the moment of saving, so a
// credential never lands in config.toml at all.
func TestConfigSetBaseURLRefusesACredentialedURL(t *testing.T) {
	d := isolateHome(t)
	a, _, errOut := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-base-url", "https://sk-token-here@host.invalid/v1"); code == ExitOK {
		t.Fatal("a base URL carrying a credential was saved")
	}
	if !strings.Contains(errOut.String(), "/key") {
		t.Fatalf("the refusal does not say where a credential belongs: %s", errOut)
	}
	if body, err := os.ReadFile(d.ConfigFile()); err == nil && strings.Contains(string(body), "sk-token-here") {
		t.Fatalf("the credential reached the config file: %s", body)
	}
}

// /doctor reads OPENROUTER_BASE_URL straight from the environment; with a
// credential in it, the report must refuse rather than print the URL.
func TestDoctorRefusesACredentialedBaseURLInsteadOfPrintingIt(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENROUTER_BASE_URL", "https://sk-token-here@host.invalid/v1")
	a, out, _ := newTestApp(t, "")
	a.doctorNetwork(context.Background())
	if strings.Contains(out.String(), "sk-token-here") {
		t.Fatalf("/doctor printed the credential:\n%s", out)
	}
	if !strings.Contains(out.String(), "refus") {
		t.Fatalf("/doctor did not say it refused the URL:\n%s", out)
	}
}
