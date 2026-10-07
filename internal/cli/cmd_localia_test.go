package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

func fakeHardware() local.Hardware {
	const gib = 1 << 30
	return local.Hardware{
		Accelerators: []local.Accelerator{{
			Vendor: "amd", Name: "card0",
			VRAM:          local.Capacity{Bytes: 16 * gib, Known: true},
			AvailableVRAM: local.Capacity{Bytes: 15 * gib, Known: true},
		}, {
			Vendor: "nvidia", Name: "card1",
		}},
		SystemRAM: local.Capacity{Bytes: 32 * gib, Known: true},
		DiskFree:  local.Capacity{Bytes: 200 * gib, Known: true},
	}
}

func TestLocaliaReportsHardwareAndStorage(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia"); code != ExitOK {
		t.Fatalf("localia exit = %d, stderr = %q", code, errOut.String())
	}

	got := out.String()
	for _, want := range []string{"32.0 GiB", "200.0 GiB", "card0", "card1", "amd", "nvidia"} {
		if !strings.Contains(got, want) {
			t.Fatalf("localia output = %q, want %q", got, want)
		}
	}
	// A card Kolkrabbi could not measure must say so rather than read as 0 B.
	if !strings.Contains(got, "unknown") {
		t.Fatalf("localia output = %q, want the unmeasured card marked unknown", got)
	}
}

// The store a pull lands in is the user's own Ollama's (option E), and the
// report names it — read from the environment, so a test never names the
// developer's real home.
func TestLocaliaNamesTheStoreItPullsInto(t *testing.T) {
	isolateConnectorState(t)
	store := filepath.Join(t.TempDir(), "ollama-store")
	t.Setenv("OLLAMA_MODELS", store)
	a, out, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia"); code != ExitOK {
		t.Fatal("localia must succeed")
	}
	if !strings.Contains(out.String(), store) || !strings.Contains(out.String(), "your Ollama's") {
		t.Fatalf("localia output = %q, want the host store named as the user's own", out.String())
	}
}

func TestLocaliaSaysNothingIsPulledYet(t *testing.T) {
	isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia"); code != ExitOK {
		t.Fatal("localia must succeed with no models installed")
	}
	if !strings.Contains(out.String(), "nothing pulled yet") {
		t.Fatalf("localia output = %q, want it to say nothing is pulled", out.String())
	}
}

func TestSlashLocaliaMirrorsTheCommand(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if a.slash(context.Background(), ag, "/localia") {
		t.Fatal("/localia must not exit the session")
	}
	if !strings.Contains(out.String(), "32.0 GiB") {
		t.Fatalf("slash localia output = %q", out.String())
	}
}

func TestLocaliaNeedsNoGpuOrOllama(t *testing.T) {
	// The default probe must run on a machine with neither, and still print a
	// usable report rather than failing.
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")

	if code := runRetiredVerb(t, a, "localia"); code != ExitOK {
		t.Fatalf("localia exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "system RAM") {
		t.Fatalf("localia output = %q", out.String())
	}
}

func TestLocaliaModelsListsTheCatalogWithSizes(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")

	if code := runRetiredVerb(t, a, "localia", "models"); code != ExitOK {
		t.Fatalf("localia models exit = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"qwen2.5-coder:7b", "Q4_K_M", "GiB", "estimate"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestLocaliaPlanShowsEveryNumberTheDecisionRestedOn(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia", "plan", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatalf("localia plan exit = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"qwen2.5-coder:7b", "download", "needs", "available", "reserved", "gpu"} {
		if !strings.Contains(strings.ToLower(got), want) {
			t.Fatalf("plan output = %q, want %q", got, want)
		}
	}
	// Planning is not pulling. Nothing may be downloaded by looking.
	if strings.Contains(strings.ToLower(got), "downloading") {
		t.Fatalf("plan output = %q, want no download to have started", got)
	}
}

func TestLocaliaPlanRefusesWithItsReason(t *testing.T) {
	isolateConnectorState(t)
	a, _, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware {
		tiny := fakeHardware()
		tiny.SystemRAM = local.Capacity{Bytes: 2 << 30, Known: true}
		tiny.Accelerators = nil
		return tiny
	}

	if code := runRetiredVerb(t, a, "localia", "plan", "phi4:14b"); code == ExitOK {
		t.Fatal("a model that cannot fit must not report a plan")
	}
	if !strings.Contains(errOut.String(), "GiB") {
		t.Fatalf("stderr = %q, want the sizes that caused the refusal", errOut.String())
	}
}

func TestLocaliaPlanUsesTheConfiguredHeadroom(t *testing.T) {
	dirs := isolateConnectorState(t)
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetLocal(cfg, "local.gpu_mode", "cpu"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetLocal(cfg, "local.reserved_ram_bytes", "28GiB"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
		t.Fatal(err)
	}
	a, _, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	// 32 GiB with 28 reserved leaves 4, which cannot hold a 14B model.
	if code := runRetiredVerb(t, a, "localia", "plan", "phi4:14b"); code == ExitOK {
		t.Fatalf("configured headroom was ignored; stderr = %q", errOut.String())
	}
}

func TestLocaliaPlanReservesHeadroomByDefault(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia", "plan", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatalf("plan exit = %d, stderr = %q", code, errOut.String())
	}
	// A default of zero reserved would let a plan consume every byte on the
	// machine, which is not a machine that survives the model running.
	if strings.Contains(out.String(), "after 0 B reserved") {
		t.Fatalf("plan output = %q, want documented default headroom", out.String())
	}
}

func TestLocaliaPlanHonoursADeliberateZeroReserve(t *testing.T) {
	dirs := isolateConnectorState(t)
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetLocal(cfg, "local.reserved_ram_bytes", "0"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetLocal(cfg, "local.gpu_mode", "cpu"); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := runRetiredVerb(t, a, "localia", "plan", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatal("plan must succeed")
	}
	if !strings.Contains(out.String(), "after 0 B reserved") {
		t.Fatalf("plan output = %q, want a chosen zero to be respected", out.String())
	}
}

func pullFixture(t *testing.T, stdin string) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, stdin)
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
	return a, out, errOut
}

func TestLocaliaPullAsksBeforeDownloadingAnything(t *testing.T) {
	a, out, _ := pullFixture(t, "n\n")

	if code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatal("declining a pull is a normal outcome, not a failure")
	}
	got := out.String()
	if !strings.Contains(got, "4.6 GiB") {
		t.Fatalf("output = %q, want the download size before the question", got)
	}
	if !strings.Contains(strings.ToLower(got), "[y/n]") {
		t.Fatalf("output = %q, want an explicit question", got)
	}
	if !strings.Contains(got, "nothing was downloaded") {
		t.Fatalf("output = %q, want the outcome stated", got)
	}
}

func TestLocaliaPullTreatsSilenceAsNo(t *testing.T) {
	// A closed stdin must never be read as approval for a multi-gigabyte
	// download.
	a, out, _ := pullFixture(t, "")

	if code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatal("an unanswered question is a decline, not an error")
	}
	if !strings.Contains(out.String(), "nothing was downloaded") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestLocaliaPullRefusesBeforeAskingWhenTheModelCannotFit(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "y\n")
	a.probeHardware = func(context.Context, string) local.Hardware {
		cramped := fakeHardware()
		cramped.DiskFree = local.Capacity{Bytes: 5 << 30, Known: true}
		return cramped
	}

	if code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:14b"); code == ExitOK {
		t.Fatal("a model that cannot fit must not be offered")
	}
	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want no question for a model that cannot fit", out.String())
	}
	if !strings.Contains(errOut.String(), "GiB") {
		t.Fatalf("stderr = %q, want the sizes behind the refusal", errOut.String())
	}
}

func TestLocaliaPullReportsNativeSetupFailure(t *testing.T) {
	a, _, errOut := pullFixture(t, "y\n")

	code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b")
	if code == ExitOK {
		t.Fatal("a failed native setup must fail the pull")
	}
	if !strings.Contains(errOut.String(), "native runtime setup is disabled in this fixture") {
		t.Fatalf("stderr = %q, want the setup failure", errOut.String())
	}
}

// E10. An approved pull goes through the host's own API and is watched.
func TestLocaliaPullStreamsThroughTheHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n{\"status\":\"pulling sha256:abc\",\"total\":10,\"completed\":10}\n{\"status\":\"success\"}\n"))
	}))
	t.Cleanup(server.Close)
	a, out, errOut := pullFixture(t, "y\n")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: strings.TrimPrefix(server.URL, "http://"), Version: "0.33.1"}
	}
	if code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatalf("pull exit = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{"pulling manifest", "100%", "success", "ollama/qwen2.5-coder:7b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// A Cloud row's pull fetches only a manifest, so it has no fit to plan, but
// it is still asked for and goes through the session's own server. That is
// the one pull a Kolk-managed runtime can do: its `ollama` is not on PATH and
// does not listen on the default port.
// ollamaPullServer answers /api/show with a remote host for the named Cloud
// models, as Ollama does, and records every /api/pull body.
func ollamaPullServer(t *testing.T, cloud ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var pulled []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/show":
			for _, name := range cloud {
				if strings.Contains(string(body), `"`+name+`"`) {
					_, _ = w.Write([]byte(`{"remote_host":"https://ollama.com:443","capabilities":["completion","tools"]}`))
					return
				}
			}
			_, _ = w.Write([]byte(`{"capabilities":["completion"]}`))
		case "/api/pull":
			pulled = append(pulled, string(body))
			_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n{\"status\":\"success\"}\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &pulled
}

func TestLocaliaPullsACloudManifestWithoutAFitPlan(t *testing.T) {
	for _, answer := range []string{"y\n", "n\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			server, recorded := ollamaPullServer(t, "gpt-oss:120b-cloud")
			isolateConnectorState(t)
			a, out, errOut := newTestApp(t, answer)
			// Unknown hardware refuses every local pull; a Cloud manifest
			// does not depend on it.
			a.probeHardware = func(context.Context, string) local.Hardware { return local.Hardware{} }
			a.discoverHost = func(context.Context) local.Host {
				return local.Host{State: local.HostRunning, Addr: strings.TrimPrefix(server.URL, "http://"), Version: "0.34.4", Managed: true}
			}

			if code := runRetiredVerb(t, a, "localia", "pull", "gpt-oss:120b-cloud"); code != ExitOK {
				t.Fatalf("cloud pull exit = %d, stderr = %q", code, errOut.String())
			}
			got := out.String()
			if !strings.Contains(got, "ollama.com") || !strings.Contains(strings.ToLower(got), "[y/n]") {
				t.Fatalf("output = %q, want where it runs and an explicit question", got)
			}
			if answer == "n\n" {
				if len(*recorded) != 0 || !strings.Contains(got, "nothing was downloaded") {
					t.Fatalf("a declined cloud pull reached the server: %v\n%s", *recorded, got)
				}
				return
			}
			if pulled := *recorded; len(pulled) != 1 || !strings.Contains(pulled[0], `"gpt-oss:120b-cloud"`) {
				t.Fatalf("pulls = %v, want one pull of the exact cloud tag", pulled)
			}
			if !strings.Contains(got, "ollama/gpt-oss:120b-cloud") {
				t.Fatalf("output = %q, want the model id to select", got)
			}
		})
	}
}

// A name that merely ends in -cloud proves nothing: the server's /api/show
// remote host is the proof, as for the picker's Cloud rows. A picker id pasted
// with its ollama/ prefix names the same model.
func TestLocaliaPullNeedsTheServersCloudProof(t *testing.T) {
	cases := []struct{ arg, wire string }{
		{"big:70b-cloud", ""},
		{"ollama/gpt-oss:120b-cloud", `"gpt-oss:120b-cloud"`},
		{"ollama/qwen2.5-coder:7b", `"qwen2.5-coder:7b"`},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			server, recorded := ollamaPullServer(t, "gpt-oss:120b-cloud")
			a, out, errOut := pullFixture(t, "")
			a.discoverHost = func(context.Context) local.Host {
				return local.Host{State: local.HostRunning, Addr: strings.TrimPrefix(server.URL, "http://"), Version: "0.34.4"}
			}
			code := runRetiredVerb(t, a, "localia", "pull", "--yes", c.arg)
			pulled := *recorded
			if c.wire == "" {
				if code == ExitOK || len(pulled) != 0 {
					t.Fatalf("an unproven -cloud name was pulled: exit=%d pulls=%v\n%s", code, pulled, out.String())
				}
				if !strings.Contains(errOut.String(), "not an Ollama Cloud model") {
					t.Fatalf("stderr = %q, want the refusal to say why", errOut.String())
				}
				return
			}
			if code != ExitOK || len(pulled) != 1 || !strings.Contains(pulled[0], c.wire) || strings.Contains(pulled[0], "ollama/") {
				t.Fatalf("exit=%d pulls=%v, want one pull of %s\nstderr=%s", code, pulled, c.wire, errOut.String())
			}
			if strings.Contains(out.String(), "ollama/ollama/") {
				t.Fatalf("output doubled the route prefix:\n%s", out.String())
			}
		})
	}
}

// A check that could not be made is not a verdict. A signed-out server, a
// failing one and a pressed Esc each say what happened, and nothing is pulled.
func TestLocaliaCloudPullSaysWhyTheCheckFailed(t *testing.T) {
	var pulls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/api/pull":
			pulls.Add(1)
			_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
		case strings.Contains(string(body), `"locked:cloud"`):
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		case strings.Contains(string(body), `"broken:cloud"`):
			http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
		case strings.Contains(string(body), `"garbled:cloud"`):
			_, _ = w.Write([]byte(`not json`))
		default:
			<-r.Context().Done()
		}
	}))
	t.Cleanup(server.Close)
	running := func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: strings.TrimPrefix(server.URL, "http://"), Version: "0.34.4"}
	}
	for _, c := range []struct{ name, want string }{
		{"locked:cloud", "/plans login ollama"},
		{"broken:cloud", "could not confirm"},
		{"garbled:cloud", "could not confirm"},
	} {
		a, _, errOut := pullFixture(t, "")
		a.discoverHost = running
		if code := runRetiredVerb(t, a, "localia", "pull", "--yes", c.name); code == ExitOK {
			t.Fatalf("%s: an unconfirmed pull succeeded", c.name)
		}
		if got := errOut.String(); !strings.Contains(got, c.want) || strings.Contains(got, "is not an Ollama Cloud model") {
			t.Errorf("%s: stderr = %q, want %q and no verdict about the model", c.name, got, c.want)
		}
	}

	a, out, _ := pullFixture(t, "")
	a.discoverHost = running
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	err := a.pullLocalModel(ctx, "slow:cloud", true)
	shown := out.String() + fmt.Sprint(err)
	if !errors.Is(err, context.Canceled) || strings.Contains(shown, "is not an Ollama Cloud model") || strings.Contains(shown, "could not confirm") {
		t.Fatalf("an Esc during the check = %v; want the cancellation alone, neither a verdict nor a failed check\n%s", err, out.String())
	}
	if pulls.Load() != 0 {
		t.Fatalf("%d pulls after unconfirmed checks", pulls.Load())
	}
}

// The question covers everything a yes does. A pull goes through a running
// server, so with an idle runtime a yes also starts it, and with none, where
// Kolk can install one, a yes also sets it up. Where it cannot, or where a
// server already runs, the question promises nothing extra.
func TestLocaliaPullNamesRuntimeSetupBeforeTheQuestion(t *testing.T) {
	states := []struct {
		name      string
		host      local.Host
		supported bool
		want      string
	}{
		{"absent", local.Host{State: local.HostAbsent}, true, "a yes also sets up Ollama"},
		{"installed", local.Host{State: local.HostInstalled, Binary: "/opt/ollama"}, true, "a yes also starts Ollama"},
		{"running", local.Host{State: local.HostRunning, Addr: "127.0.0.1:1"}, true, ""},
		{"absent-with-companion", local.Host{State: local.HostAbsent, MissingCompanion: "ROCm bundle for AMD GPU card0"}, true,
			"a yes also sets up Ollama (official build, no Docker or sudo) with its ROCm bundle for AMD GPU card0"},
		{"installed-missing-companion", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, true,
			"a yes also downloads Ollama again with its ROCm bundle for AMD GPU card0, beside the installed runtime"},
		{"users-own-installed", local.Host{State: local.HostInstalled, Binary: "/usr/local/bin/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, true, "a yes also starts Ollama"},
		{"installed-companion-failed-before", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "native runtime SHA-256 verification failed"}, true,
			"a yes also starts Ollama; adding its ROCm bundle for AMD GPU card0 failed before"},
	}
	for _, state := range states {
		for _, model := range []string{"qwen2.5-coder:7b", "gpt-oss:120b-cloud"} {
			t.Run(state.name+"/"+model, func(t *testing.T) {
				a, out, errOut := pullFixture(t, "n\n")
				a.discoverHost = func(context.Context) local.Host { return state.host }
				// A session's starter reports the host, notes included, as it
				// does for a pull inside a session.
				a.localRuntime = &local.HostStarter{Discover: a.discoverHost}
				a.managedSetupSupported = func() bool { return state.supported }
				if code := runRetiredVerb(t, a, "localia", "pull", model); code != ExitOK {
					t.Fatalf("declined pull exit = %d, stderr = %q", code, errOut.String())
				}
				before, _, asked := strings.Cut(out.String(), "[y/N]")
				if !asked {
					t.Fatalf("no question asked:\n%s", out.String())
				}
				setup := strings.Contains(before, "setup:")
				if state.want == "" && setup {
					t.Errorf("the question promises setup that a yes will not do:\n%s", before)
				}
				if state.want != "" && !strings.Contains(before, state.want) {
					t.Errorf("the question hides %q:\n%s", state.want, before)
				}
				if strings.Contains(before, "manifest only") {
					t.Errorf("the question claims a manifest-only download:\n%s", before)
				}
			})
		}
	}
}

// With no Ollama and no managed setup for this platform (Windows), a yes
// could only fail, so the pull refuses before asking and names the install,
// as the picker row does ("needs Ollama installed first").
func TestLocaliaPullWithNoOllamaAndNoSetupRefusesBeforeAsking(t *testing.T) {
	for _, model := range []string{"qwen2.5-coder:7b", "gpt-oss:120b-cloud"} {
		t.Run(model, func(t *testing.T) {
			a, out, errOut := pullFixture(t, "y\n")
			a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
			a.localRuntime = &local.HostStarter{Discover: a.discoverHost, Provision: func(context.Context, io.Writer) (local.Host, error) {
				t.Error("a platform without managed setup tried to set one up")
				return local.Host{}, errors.New("unreachable")
			}}
			a.managedSetupSupported = func() bool { return false }
			if code := runRetiredVerb(t, a, "localia", "pull", model); code == ExitOK {
				t.Fatalf("a pull with nothing to pull into succeeded:\n%s", out.String())
			}
			if strings.Contains(out.String(), "[y/N]") {
				t.Errorf("asked a question whose yes can only fail:\n%s", out.String())
			}
			hint := local.Host{}.InstallHint()
			if text := out.String() + errOut.String(); !strings.Contains(text, hint) || strings.Contains(text, "check the network") {
				t.Errorf("refusal does not name the install %q, or blames the network:\n%s", hint, text)
			}
		})
	}
}

// A persistent session waits on the project's one runtime. While it runs but
// does not answer, every start, setup or download fails at once, so no
// surface promises one: the pull refuses before its question, the accelerator
// line promises no bundle, and the listing promises no pick. An ephemeral
// session starts its own beside it, so there the same promises hold.
func TestAStalledPersistentRuntimePromisesNothing(t *testing.T) {
	const stalled = "Kolk's runtime for this project (process 910001) is running but not answering at 127.0.0.1:43210"
	host := local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama",
		MissingCompanion: "ROCm bundle for AMD GPU card0", StalledRuntime: stalled}
	for _, persistent := range []bool{true, false} {
		for _, model := range []string{"qwen2.5-coder:7b", "gpt-oss:120b-cloud"} {
			t.Run(fmt.Sprintf("persistent=%v/%s", persistent, model), func(t *testing.T) {
				a, out, errOut := pullFixture(t, "n\n")
				a.discoverHost = func(context.Context) local.Host { return host }
				a.localRuntime = &local.HostStarter{Persistent: persistent, Discover: a.discoverHost}
				code := runRetiredVerb(t, a, "localia", "pull", model)
				text := out.String() + errOut.String()
				asked := strings.Contains(out.String(), "[y/N]")
				if persistent && (code == ExitOK || asked || strings.Contains(text, "a yes also") || !strings.Contains(text, stalled)) {
					t.Fatalf("a pull that can only fail asked or promised (exit %d):\n%s", code, text)
				}
				if !persistent && (code != ExitOK || !asked || !strings.Contains(out.String(), "a yes also downloads Ollama again")) {
					t.Fatalf("an ephemeral session, which starts its own, did not ask (exit %d):\n%s", code, text)
				}
			})
		}
	}

	for _, persistent := range []bool{true, false} {
		a, out, _ := newTestApp(t, "")
		a.localRuntime = &local.HostStarter{Persistent: persistent, Discover: func(context.Context) local.Host { return host }}
		if err := a.runLocalia(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		a.printHostModels(context.Background(), "", "")
		text := out.String()
		promises := strings.Contains(text, "the next local setup, pull or model start adds it") || strings.Contains(text, "can still pick")
		if persistent && (promises || !strings.Contains(text, "nothing adds it while Kolk's runtime is not answering")) {
			t.Errorf("persistent status or listing promises a start:\n%s", text)
		}
		if !persistent && !strings.Contains(text, "the next local setup, pull or model start adds it") {
			t.Errorf("ephemeral status lost the bundle its own start adds:\n%s", text)
		}
	}
}

// An installed, idle Ollama is started for the pull and stopped after it —
// the one command that earns a server, and only for as long as it takes.
func TestLocaliaPullStartsAnIdleOllamaAndStopsItAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	t.Cleanup(server.Close)
	a, _, errOut := pullFixture(t, "y\n")
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostInstalled, Binary: "/opt/ollama"} }
	started, stopped := 0, 0
	a.startHost = func(context.Context, local.Host) (string, func(), error) {
		started++
		return strings.TrimPrefix(server.URL, "http://"), func() { stopped++ }, nil
	}
	if code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b"); code != ExitOK {
		t.Fatalf("pull exit = %d, stderr = %q", code, errOut.String())
	}
	if started != 1 || stopped != 1 {
		t.Fatalf("started %d, stopped %d; want the server up for the pull and down after", started, stopped)
	}
}

func TestLocaliaPullYesSkipsTheQuestion(t *testing.T) {
	a, out, _ := pullFixture(t, "")

	_ = runRetiredVerb(t, a, "localia", "pull", "--yes", "qwen2.5-coder:7b")
	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want --yes to answer it", out.String())
	}
}

func TestLocaliaPullWritesNothingWhenDeclined(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "n\n")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	_ = runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b")
	if _, err := os.Stat(dirs.LocalModelsDir()); err == nil {
		t.Fatal("declining a pull created the managed model directory")
	}
}

func TestLocaliaPullDoesNotPromptWhileKolkrabbiOwnsTheTerminal(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
	a.terminalOwned = func() bool { return true }

	// Reading stdin here would fight the session's own reader for the user's
	// keystrokes, exactly as a provider login would.
	code := runRetiredVerb(t, a, "localia", "pull", "qwen2.5-coder:7b")

	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want no prompt while the session owns the keyboard", out.String())
	}
	if code == ExitOK {
		t.Fatal("the pull must not proceed unconfirmed")
	}
	if !strings.Contains(errOut.String(), "/localia pull") {
		t.Fatalf("stderr = %q, want the command to run in a separate terminal", errOut.String())
	}
}

func TestLocaliaPullWithYesStillWorksInSession(t *testing.T) {
	isolateConnectorState(t)
	a, _, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
	a.terminalOwned = func() bool { return true }

	// An explicit --yes needs no keyboard, so it is allowed to proceed to the
	// point where it reports what is actually missing.
	_ = runRetiredVerb(t, a, "localia", "pull", "--yes", "qwen2.5-coder:7b")
	if strings.Contains(errOut.String(), "separate terminal") {
		t.Fatalf("stderr = %q, want --yes to bypass the prompt entirely", errOut.String())
	}
}

func TestHardwareProbeIsBounded(t *testing.T) {
	isolateConnectorState(t)
	a, _, _ := newTestApp(t, "")
	var deadline time.Time
	var hasDeadline bool
	a.probeHardware = func(ctx context.Context, _ string) local.Hardware {
		deadline, hasDeadline = ctx.Deadline()
		return fakeHardware()
	}

	_ = runRetiredVerb(t, a, "localia")

	// nvidia-smi against a wedged driver is a known hang. Unknown is a valid
	// answer; a frozen session is not.
	if !hasDeadline {
		t.Fatal("the hardware probe runs without a deadline")
	}
	if until := time.Until(deadline); until <= 0 || until > time.Minute {
		t.Fatalf("probe deadline is %s away, want a short bound", until)
	}
}
