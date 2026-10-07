package cli

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

func TestLocalEphemeralIsScopedToTheProject(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	first, second := t.TempDir(), t.TempDir()
	t.Chdir(first)
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "off"}); err != nil {
		t.Fatal(err)
	}
	read := func(want string) {
		t.Helper()
		out.Reset()
		if err := a.runConfig(ctx, []string{"get", "local.ephemeral"}); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), want) {
			t.Fatalf("got %q, want %s", out.String(), want)
		}
	}
	read("off")
	t.Chdir(second)
	read("on")
	t.Chdir(first)
	read("off")
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "maybe"}); err == nil {
		t.Fatal("accepted invalid lifetime")
	}
	read("off")
	if err := a.runConfig(ctx, []string{"unset", "local.ephemeral"}); err != nil {
		t.Fatal(err)
	}
	read("on")
}

type localLifetimeProcess struct{ closed atomic.Bool }

func (p *localLifetimeProcess) Close() error { p.closed.Store(true); return nil }

func TestLocaliaPullAndChatShareTheSessionRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pull":
			_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"shared runtime\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, _, _ := pullFixture(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostInstalled, Binary: "/fake/ollama"}
	}
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	p := &localLifetimeProcess{}
	var starts atomic.Int32
	a.localRuntime = &local.HostStarter{Binary: "/fake/ollama", Discover: a.discoverHost,
		Port: func() (int, error) { return port, nil }, Ready: func(context.Context, string) bool { return true },
		Start: func(context.Context, string, []string, []string) (local.Process, error) { starts.Add(1); return p, nil },
	}
	backend := local.NewLazyHostBackend(a.localRuntime)
	defer func() { _ = backend.Close() }()
	ctx := context.Background()
	if err := a.pullLocalModel(ctx, "qwen2.5-coder:7b", true); err != nil {
		t.Fatal(err)
	}
	if p.closed.Load() {
		t.Fatal("pull stopped the session's runtime")
	}
	message, _, err := backend.StreamChat(ctx, "qwen2.5-coder:7b", nil, nil, nil)
	if err != nil || message.Content != "shared runtime" {
		t.Fatalf("chat = %s, %v", message.Content, err)
	}
	if starts.Load() != 1 {
		t.Fatalf("pull and chat started %d servers", starts.Load())
	}
	a.listHostModels = func(_ context.Context, addr, _ string) ([]local.HostModel, error) {
		if addr != strings.TrimPrefix(server.URL, "http://") {
			t.Errorf("model list rediscovered a different server: %s", addr)
		}
		return []local.HostModel{{Name: "qwen2.5-coder:7b"}}, nil
	}
	if got := a.pulledModelNames(ctx); len(got) != 1 {
		t.Fatalf("models = %v", got)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if !p.closed.Load() {
		t.Fatal("session close left its ephemeral runtime running")
	}
}

func TestLocalLifetimeUsesTheCanonicalRepositoryRoot(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "src")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"set", "local.ephemeral", "off"}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out.Reset()
	if err := a.runConfig(ctx, []string{"get", "local.ephemeral"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "off") {
		t.Fatalf("subdirectory lost project setting: %s", out)
	}
}

func TestLocaliaLoginTargetsTheSamePrivateServerAsVerification(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostInstalled, Binary: "/managed/ollama"}
	}
	a.localRuntime = &local.HostStarter{Discover: a.discoverHost,
		Port: func() (int, error) { return 43212, nil }, Ready: func(context.Context, string) bool { return true },
		Start: func(context.Context, string, []string, []string) (local.Process, error) {
			return &localLifetimeProcess{}, nil
		},
	}
	defer func() { _ = a.localRuntime.Close() }()
	ctx := context.Background()
	addr, err := a.localRuntime.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_HOST", "10.0.0.9:11434")
	out := filepath.Join(t.TempDir(), "login-endpoint")
	t.Setenv("KOLK_TEST_LOGIN_OUT", out)
	a.signIn = func(_ context.Context, got string) local.SignInState {
		if got != addr {
			t.Errorf("verification = %s, want %s", got, addr)
		}
		return local.SignInState{Known: true, SignedIn: true, Plan: "pro"}
	}
	run := func(ctx context.Context, executable string, args []string) error {
		if executable != "/managed/ollama" || len(args) != 1 || args[0] != "signin" {
			t.Errorf("login invocation: %s %v", executable, args)
		}
		return shell.Handover(ctx, "sh", []string{"-c", `printf '%s' "$OLLAMA_HOST" > "$KOLK_TEST_LOGIN_OUT"`}, "")
	}
	selected := provider.Plan{Connector: "ollama", Provider: "ollama", Name: "Ollama Pro"}
	if err := a.runConnectorLoginWith(ctx, dirs.ConnectorsFile(), selected, run); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != addr {
		t.Fatalf("login endpoint = %s, %v; want %s", got, err, addr)
	}
	if os.Getenv("OLLAMA_HOST") != "10.0.0.9:11434" {
		t.Fatal("login changed the parent environment")
	}
}

// A Kolk-managed runtime is not on PATH and never listens on 11434, so "start
// `ollama serve`" cannot help its owner. An idle runtime is started for the
// sign-in and stays the session's; with none yet, setup is the next step.
func TestOllamaLoginStartsAnIdleSessionRuntime(t *testing.T) {
	t.Run("installed", func(t *testing.T) {
		dirs := isolateConnectorState(t)
		a, out, _ := newTestApp(t, "")
		a.discoverHost = func(context.Context) local.Host {
			return local.Host{State: local.HostInstalled, Binary: "/managed/ollama", Managed: true}
		}
		process := &localLifetimeProcess{}
		a.localRuntime = &local.HostStarter{Discover: a.discoverHost,
			Port: func() (int, error) { return 43213, nil }, Ready: func(context.Context, string) bool { return true },
			Start: func(context.Context, string, []string, []string) (local.Process, error) { return process, nil },
		}
		a.signIn = func(_ context.Context, got string) local.SignInState {
			if got != "127.0.0.1:43213" {
				t.Errorf("verification = %s, want the started session runtime", got)
			}
			return local.SignInState{Known: true, SignedIn: true, Plan: "pro"}
		}
		var ran string
		run := func(_ context.Context, executable string, args []string) error {
			ran = executable + " " + strings.Join(args, " ")
			return nil
		}
		selected := provider.Plan{Connector: "ollama", Provider: "ollama", Name: "Ollama Pro"}
		if err := a.runConnectorLoginWith(context.Background(), dirs.ConnectorsFile(), selected, run); err != nil {
			t.Fatal(err)
		}
		if ran != "/managed/ollama signin" {
			t.Fatalf("login ran %q, want the managed binary's signin\n%s", ran, out.String())
		}
		if strings.Contains(out.String(), "ollama serve") || strings.Contains(out.String(), "11434") {
			t.Errorf("login told a managed-runtime user to start the default server:\n%s", out.String())
		}
		if process.closed.Load() {
			t.Fatal("the sign-in stopped the session's runtime before the session ended")
		}
		if err := a.localRuntime.Close(); err != nil || !process.closed.Load() {
			t.Fatalf("session exit left the runtime running: closed=%v err=%v", process.closed.Load(), err)
		}
	})
	t.Run("absent", func(t *testing.T) {
		dirs := isolateConnectorState(t)
		a, out, _ := newTestApp(t, "")
		a.discoverHost = func(context.Context) local.Host { return local.Host{} }
		ran := false
		run := func(context.Context, string, []string) error { ran = true; return nil }
		selected := provider.Plan{Connector: "ollama", Provider: "ollama", Name: "Ollama Pro"}
		if err := a.runConnectorLoginWith(context.Background(), dirs.ConnectorsFile(), selected, run); err != nil {
			t.Fatal(err)
		}
		if ran {
			t.Fatal("signin ran without a server to sign in")
		}
		text := out.String()
		if !strings.Contains(text, "/localia setup") || strings.Contains(text, "ollama serve") || strings.Contains(text, "11434") {
			t.Fatalf("absent-runtime guidance = %q, want /localia setup and no default-server instructions", text)
		}
	})
}

func TestSessionClosesLocalRuntimeDiscoveredAfterStartup(t *testing.T) {
	storeFirstRunKey(t)
	a, _, _ := newTestApp(t, "/quit\n")
	var installed atomic.Bool
	a.discoverHost = func(context.Context) local.Host {
		if installed.Load() {
			return local.Host{State: local.HostInstalled, Binary: "/fake/ollama"}
		}
		return local.Host{}
	}
	p := &localLifetimeProcess{}
	// runDefault calls this after constructing the agent, before its REPL.
	// Reproduce an installation appearing after the route map was built.
	a.isStdinPiped = func() bool {
		installed.Store(true)
		a.localRuntime.Start = func(context.Context, string, []string, []string) (local.Process, error) { return p, nil }
		a.localRuntime.Ready = func(context.Context, string) bool { return true }
		a.localRuntime.Port = func() (int, error) { return 43111, nil }
		if _, _, err := a.startHostFor(context.Background(), a.localHost(context.Background())); err != nil {
			t.Fatal(err)
		}
		return false
	}
	if err := a.runDefault(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !p.closed.Load() {
		t.Fatal("session leaked a runtime that appeared after its route map was built")
	}
}

// A sign-in talks to a server; it never becomes a runtime download nobody was
// asked about. An installed tree that lacks its accelerator bundle is started
// as it is for the sign-in, and completing it is left to setup or a pull,
// which say so first.
func TestOllamaLoginNeverDownloadsARuntime(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	incomplete := local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/standard/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}
	var provisions atomic.Int32
	var started string
	a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return incomplete },
		Port: func() (int, error) { return 43215, nil }, Ready: func(context.Context, string) bool { return true },
		Start: func(_ context.Context, binary string, _ []string, _ []string) (local.Process, error) {
			started = binary
			return &localLifetimeProcess{}, nil
		},
		Provision: func(context.Context, io.Writer) (local.Host, error) {
			provisions.Add(1)
			return local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/rocm/ollama"}, nil
		},
	}
	defer func() { _ = a.localRuntime.Close() }()
	a.signIn = func(context.Context, string) local.SignInState { return local.SignInState{Known: true, SignedIn: true} }
	ran := ""
	run := func(_ context.Context, executable string, _ []string) error { ran = executable; return nil }
	selected := provider.Plan{Connector: "ollama", Provider: "ollama", Name: "Ollama Pro"}
	if err := a.runConnectorLoginWith(context.Background(), dirs.ConnectorsFile(), selected, run); err != nil {
		t.Fatal(err)
	}
	if provisions.Load() != 0 {
		t.Fatalf("the sign-in ran runtime setup %d times\n%s", provisions.Load(), out.String())
	}
	if started != "/managed/standard/ollama" || ran != "/managed/standard/ollama" {
		t.Fatalf("started %q and signed in with %q; want the installed runtime as it is", started, ran)
	}
}

// The picker prints `/plans login ollama "Ollama Pro"`; pasted, the quoted
// plan name selects the plan. A name that matches nothing lists the plans.
func TestPlansLoginAcceptsTheQuotedPlanThePickerPrints(t *testing.T) {
	isolateConnectorState(t)
	a, _, errOut := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "plans", "login", "ollama", `"Ollama`, `Pro"`); code != ExitOK || strings.Contains(errOut.String(), "no exact provider CLI plan") {
		t.Fatalf("pasted quoted plan: exit=%d stderr=%q", code, errOut.String())
	}
	errOut.Reset()
	if code := runRetiredVerb(t, a, "plans", "login", "ollama", "Platinum"); code == ExitOK || !strings.Contains(errOut.String(), "Ollama Pro") {
		t.Fatalf("unknown plan: exit=%d stderr=%q; want the available plans named", code, errOut.String())
	}
}
