package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

const firstRunStoredKey = "sk-or-v1-0123456789abcdef0123456789abcdef"

func storeFirstRunKey(t *testing.T) paths.Dirs {
	t.Helper()
	d := isolateHome(t)
	store := keystore.NewFileStore(d.CredentialsFile())
	if err := store.Set(context.Background(), keystore.Ref{Provider: "openrouter"}, secret.New(firstRunStoredKey)); err != nil {
		t.Fatal(err)
	}
	return d
}

func writeCorruptCredentialManifest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not credential json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStoredCredentialBuildsComputedDefaultAgent(t *testing.T) {
	storeFirstRunKey(t)
	a, out, errOut := newTestApp(t, "")

	ag, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ag.Client.Key().Reveal(); got != firstRunStoredKey {
		t.Errorf("provider received %q, want the stored credential", got)
	}
	if ag.Model != defaultModel {
		t.Errorf("model = %q, want computed default %q", ag.Model, defaultModel)
	}
	if ag.Mode != engine.ModeCode {
		t.Errorf("mode = %q, want computed default %q", ag.Mode, engine.ModeCode)
	}
	if ag.Effort != engine.EffortMedium {
		t.Errorf("effort = %q, want computed default %q", ag.Effort, engine.EffortMedium)
	}
	if strings.Contains(out.String()+errOut.String(), firstRunStoredKey) {
		t.Fatal("constructing an agent printed the stored credential")
	}
}

func TestModeAgentFlagRunsTheOrchestratedPipeline(t *testing.T) {
	d := storeFirstRunKey(t)
	srv := enginetest.New(
		enginetest.Step{Text: `["inspect the request", "prepare the answer"]`},
		enginetest.Step{Text: "Inspection complete."},
		enginetest.Step{Text: "Answer prepared."},
		enginetest.Step{Text: "The public agent route completed."},
	)
	defer srv.Close()
	if err := config.Save(d.ConfigFile(), &config.Config{BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	// Also through the environment, because isolateHome points that at a
	// closed port so a stray provider call cannot reach the real API, and the
	// environment beats the saved config by design.
	t.Setenv("OPENROUTER_BASE_URL", srv.URL)

	a, out, errOut := newTestApp(t, "")
	args := []string{"--mode", "agent", "-p", "inspect and answer"}
	if code := a.main(context.Background(), args); code != ExitOK {
		t.Fatalf("kolk %v exit = %d, stderr:\n%s", args, code, errOut)
	}
	for _, want := range []string{"plan (2 tasks)", "subagent 1/2", "subagent 2/2", "The public agent route completed."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("agent output missing %q:\n%s", want, out)
		}
	}
	if len(srv.Tools) != 4 {
		t.Fatalf("model calls = %d, want planner + two subagents + synthesis", len(srv.Tools))
	}
	if srv.Tools[0] != 0 || srv.Tools[1] == 0 || srv.Tools[2] == 0 || srv.Tools[3] != 0 {
		t.Errorf("tool schemas by role = %v, want none/tools/tools/none", srv.Tools)
	}
}

func TestEnvironmentCredentialWinsWithoutReadingCorruptManifest(t *testing.T) {
	d := isolateHome(t)
	writeCorruptCredentialManifest(t, d.CredentialsFile())
	const envKey = "sk-or-v1-fedcba9876543210fedcba9876543210"
	t.Setenv("OPENROUTER_API_KEY", envKey)
	a, _, _ := newTestApp(t, "")

	ag, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatalf("environment resolution touched the corrupt store: %v", err)
	}
	if got := ag.Client.Key().Reveal(); got != envKey {
		t.Errorf("provider key = %q, want environment override", got)
	}
}

func TestCorruptManifestIsNotReportedAsMissingCredential(t *testing.T) {
	d := isolateHome(t)
	writeCorruptCredentialManifest(t, d.CredentialsFile())
	a, _, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), nil); code != ExitError {
		t.Errorf("exit = %d, want %d", code, ExitError)
	}
	got := errOut.String()
	if strings.Contains(got, "kolk needs an API key") {
		t.Errorf("corrupt store masqueraded as a missing credential:\n%s", got)
	}
	if !strings.Contains(got, d.CredentialsFile()) || !strings.Contains(got, "credential") {
		t.Errorf("corrupt-store error is not actionable:\n%s", got)
	}
}

func TestCanceledCredentialReadStopsTheRun(t *testing.T) {
	isolateHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, _, errOut := newTestApp(t, "")

	if code := a.main(ctx, nil); code != ExitInterrupt {
		t.Errorf("exit = %d, want %d", code, ExitInterrupt)
	}
	if got := errOut.String(); got != "(interrupted)\n" {
		t.Errorf("cancelled lookup printed %q", got)
	}
}

type observedFirstRunRequest struct {
	Authorization string
	Model         string
	Path          string
}

func TestStoredCredentialCompletesOfflineDefaultTurn(t *testing.T) {
	d := isolateHome(t)
	store := keystore.NewFileStore(d.CredentialsFile())
	if err := store.Set(context.Background(), keystore.Ref{Provider: "openrouter"}, secret.New(firstRunStoredKey)); err != nil {
		t.Fatal(err)
	}

	observed := make(chan observedFirstRunRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		observed <- observedFirstRunRequest{
			Authorization: r.Header.Get("Authorization"),
			Model:         request.Model,
			Path:          r.URL.Path,
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"model\":\"openrouter/auto\",\"choices\":[{\"delta\":{\"content\":\"purple octopus online\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	if err := config.Save(d.ConfigFile(), &config.Config{BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	// Also through the environment, because isolateHome points that at a
	// closed port so a stray provider call cannot reach the real API, and the
	// environment beats the saved config by design.
	t.Setenv("OPENROUTER_BASE_URL", srv.URL)
	a, out, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"-p", "hello"}); code != ExitOK {
		t.Fatalf("exit = %d, stderr:\n%s", code, errOut)
	}

	request := <-observed
	if request.Authorization != "Bearer "+firstRunStoredKey {
		t.Errorf("authorization = %q", request.Authorization)
	}
	if request.Model != defaultModel {
		t.Errorf("request model = %q, want %q", request.Model, defaultModel)
	}
	if request.Path != "/chat/completions" {
		t.Errorf("request path = %q", request.Path)
	}
	if !strings.Contains(out.String(), "purple octopus online") {
		t.Errorf("fixture response missing from stdout:\n%s", out)
	}
	if strings.Contains(out.String()+errOut.String(), firstRunStoredKey) {
		t.Fatal("the model turn printed the stored credential")
	}

	entries, err := os.ReadDir(d.Sessions())
	if err != nil {
		t.Fatal(err)
	}
	var sessionFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			sessionFiles = append(sessionFiles, entry.Name())
		}
	}
	if len(sessionFiles) != 1 {
		t.Fatalf("session JSON files = %d, want 1", len(sessionFiles))
	}
	if err := filepath.Walk(d.Sessions(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), firstRunStoredKey) {
			t.Errorf("session/checkpoint file %s contains the stored credential", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
