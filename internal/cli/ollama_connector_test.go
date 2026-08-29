package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func ollamaConnector(t *testing.T, dirs interface{ ConnectorsFile() string }, verified bool) {
	t.Helper()
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "ollama", Plan: "Ollama Pro", Name: "ollama", LoginOwner: "provider-cli", Enabled: true, Verified: verified,
	}); err != nil {
		t.Fatal(err)
	}
}

func ollamaConnectorState(t *testing.T, dirs interface{ ConnectorsFile() string }) (found, verified bool) {
	t.Helper()
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range manifest.Connectors {
		if c.Name == "ollama" {
			return true, c.Verified
		}
	}
	return false, false
}

// E6. The Ollama connector is verified by asking the server, not by a turn:
// `ollama signin` returns as soon as the browser opens, so the login command
// waits for the sign-in to complete and records what the server says.
func TestOllamaLoginVerifiesThroughTheServerNotATurn(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.handover = func(context.Context, string, []string, string) error { return nil }
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}
	}
	polls := 0
	a.signIn = func(context.Context, string) local.SignInState {
		polls++
		if polls < 3 {
			return local.SignInState{Known: true, SignInURL: "https://ollama.com/connect?name=box"}
		}
		return local.SignInState{Known: true, SignedIn: true, Plan: "pro"}
	}
	a.signInBudget = time.Second

	if code := a.main(context.Background(), []string{"plans", "login", "ollama", "Ollama", "Pro"}); code != ExitOK {
		t.Fatalf("plans login exit = %d, stderr = %q", code, errOut.String())
	}
	found, verified := ollamaConnectorState(t, dirs)
	if !found || !verified {
		t.Fatalf("connector found=%v verified=%v after the server confirmed the sign-in", found, verified)
	}
	if !strings.Contains(out.String(), "pro") {
		t.Errorf("output does not name the plan the server reported:\n%s", out.String())
	}
	if strings.Contains(out.String(), "first time ollama answers a turn") {
		t.Errorf("output still promises verification by a turn:\n%s", out.String())
	}
}

func TestOllamaLoginThatStaysSignedOutPrintsTheURLAndStaysUnverified(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.handover = func(context.Context, string, []string, string) error { return nil }
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434"} }
	a.signIn = func(context.Context, string) local.SignInState {
		return local.SignInState{Known: true, SignInURL: "https://ollama.com/connect?name=box"}
	}
	a.signInBudget = 50 * time.Millisecond

	_ = a.main(context.Background(), []string{"plans", "login", "ollama", "Ollama", "Pro"})
	if _, verified := ollamaConnectorState(t, dirs); verified {
		t.Fatal("a sign-in that never completed was recorded as verified")
	}
	if !strings.Contains(out.String(), "https://ollama.com/connect?name=box") {
		t.Errorf("the sign-in URL was not shown:\n%s", out.String())
	}
}

// `ollama signin` needs a server to talk to. Without one the login cannot
// even start, and saying so beats a connector recorded against nothing.
func TestOllamaLoginWithNoServerSaysWhatToStart(t *testing.T) {
	isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	ran := false
	a.handover = func(context.Context, string, []string, string) error { ran = true; return nil }
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostInstalled, Binary: "/opt/ollama"} }

	_ = a.main(context.Background(), []string{"plans", "login", "ollama", "Ollama", "Pro"})
	if ran {
		t.Fatal("signin was run against no server")
	}
	if !strings.Contains(out.String(), "ollama serve") {
		t.Errorf("output does not say how to get a server:\n%s", out.String())
	}
}

// Startup re-reads the truth. A sign-in can lapse and a connector recorded
// verified last month is a claim, so when a server is running the manifest is
// brought in line with what /api/me says — in both directions, and saved only
// on a change.
func TestStartupBringsTheOllamaConnectorInLineWithTheServer(t *testing.T) {
	for _, tc := range []struct{ before, server, after bool }{{false, true, true}, {true, false, false}, {true, true, true}} {
		dirs := isolateConnectorState(t)
		storeFirstRunKey(t)
		ollamaConnector(t, dirs, tc.before)
		a, _, _ := newTestApp(t, "")
		a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434"} }
		a.signIn = func(context.Context, string) local.SignInState {
			return local.SignInState{Known: true, SignedIn: tc.server, Plan: "pro"}
		}
		if _, err := a.newAgent(context.Background(), &options{}); err != nil {
			t.Fatal(err)
		}
		if _, verified := ollamaConnectorState(t, dirs); verified != tc.after {
			t.Errorf("before=%v server=%v: verified=%v, want %v", tc.before, tc.server, verified, tc.after)
		}
	}
}

// The guard that matters: a turn answered by a local model proves nothing
// about a sign-in, and must not verify the connector. A verifier that could
// not tell the two apart would make Ollama Cloud the session default for
// someone who never signed in.
func TestALocalTurnNeverVerifiesTheOllamaConnector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"local answer\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dirs := isolateConnectorState(t)
	storeFirstRunKey(t)
	ollamaConnector(t, dirs, false)
	a, _, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: strings.TrimPrefix(server.URL, "http://")}
	}
	a.signIn = func(context.Context, string) local.SignInState {
		return local.SignInState{Known: true, SignedIn: false}
	}
	agent, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := agent.Routes["ollama"].StreamChat(context.Background(), "qwen2.5-coder:7b", []provider.Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, verified := ollamaConnectorState(t, dirs); verified {
		t.Fatal("a local model answering verified the cloud connector")
	}
}
