package cli

import (
	"context"
	"errors"
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
)

func TestLocalStartupAndResumeNeedNoRemoteCredential(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	t.Setenv("OPENROUTER_BASE_URL", provider.DefaultBaseURL)
	var installs atomic.Int32
	a.installLocalRuntime = func(context.Context, string, func(local.RuntimeProgress)) (local.Host, error) {
		installs.Add(1)
		return local.Host{}, errors.New("unexpected install")
	}
	ag, err := a.newAgent(context.Background(), &options{model: "ollama/custom:tiny", debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if ag.Client != nil || ag.SessionModel() != "ollama/custom:tiny" || ag.Routes["ollama"] == nil {
		t.Fatalf("local startup is not independent: %s, %v", ag.SessionModel(), ag.Client)
	}
	if installs.Load() != 0 {
		t.Fatal("startup installed without a local request")
	}
	a.pulledNames = func() map[string]bool { return map[string]bool{"custom:tiny": true, "gpt-oss:120b-cloud": true} }
	if _, ok := rowByID(tuiModels(context.Background(), a, ag), "ollama/custom:tiny"); !ok {
		t.Fatal("keyless model picker lost cached models")
	}
	cloud, ok := rowByID(tuiModels(context.Background(), a, ag), "ollama/gpt-oss:120b-cloud")
	if !ok || cloud.Cost == "local" || strings.Contains(cloud.Name, "runs on this machine") {
		t.Fatalf("unverified cached source labelled local: %+v", cloud)
	}
	if err := a.runModels(context.Background(), nil); err != nil {
		t.Fatalf("keyless /models: %v", err)
	}
	if err := a.printSessionModelCatalog(context.Background(), ag, "custom"); err != nil {
		t.Fatalf("keyless /model: %v", err)
	}
	if err := ag.Sess.Save(); err != nil {
		t.Fatal(err)
	}
	id := ag.Sess.SessionID()
	if a.sessionHold != nil {
		a.sessionHold.Close()
		a.sessionHold = nil
	}
	ag.Close()
	a.joinBackground()
	if a.debugLog != nil {
		a.debugLog.Close()
	}
	a.backgroundCtx, a.backgroundCancel = nil, nil
	resumed, err := a.newAgent(context.Background(), &options{session: id})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.SessionModel() != "ollama/custom:tiny" || resumed.Client != nil {
		t.Fatal("local resume required a remote provider")
	}
	before := resumed.SessionModel()
	if _, err := a.switchModel(context.Background(), resumed, "remote/model"); err == nil {
		t.Fatal("remote switch accepted missing credentials")
	}
	if resumed.SessionModel() != before {
		t.Fatal("failed remote switch changed local selection")
	}
}

func TestLocalStartupKeepsExplicitEndpointForRemoteSwitch(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	t.Setenv("OPENROUTER_BASE_URL", provider.DefaultBaseURL)
	endpoint := "http://127.0.0.1:45671/v1"
	ag, err := a.newAgent(context.Background(), &options{model: "ollama/custom:tiny", baseURL: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	if _, err := a.switchModel(context.Background(), ag, "vendor/model"); err != nil {
		t.Fatal(err)
	}
	client, ok := ag.SessionBackend().(*provider.Client)
	if !ok || client.BaseURL != endpoint || client.HasKey() {
		t.Fatalf("explicit endpoint lost: %#v", ag.SessionBackend())
	}
}

func TestRemoteCatalogOutageKeepsLocalModelsVisible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	a, out, _ := newTestApp(t, "")
	t.Setenv("OPENROUTER_BASE_URL", server.URL)
	a.pulledNames = func() map[string]bool { return map[string]bool{"custom:tiny": true} }
	ag, err := a.newAgent(context.Background(), &options{model: "ollama/custom:tiny", baseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	if _, err := a.switchModel(context.Background(), ag, "vendor/model"); err != nil {
		t.Fatal(err)
	}
	for _, list := range []func() error{
		func() error { return a.runModels(context.Background(), nil) },
		func() error { return a.printSessionModelCatalog(context.Background(), ag, "") },
	} {
		out.Reset()
		if err := list(); err == nil {
			t.Fatal("remote outage was hidden")
		}
		if !strings.Contains(out.String(), "ollama/custom:tiny") {
			t.Fatalf("remote failure hid local cache: %s", out)
		}
	}
}

func TestNativeSetupSelectionAndPullShareSessionOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pull":
			w.Write([]byte("{\"status\":\"success\"}\n"))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"local answer\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out, _ := newTestApp(t, "")
	t.Setenv("OPENROUTER_BASE_URL", provider.DefaultBaseURL)
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
	var installs, starts atomic.Int32
	a.installLocalRuntime = func(_ context.Context, dir string, progress func(local.RuntimeProgress)) (local.Host, error) {
		installs.Add(1)
		if !strings.Contains(dir, "local-runtime") {
			t.Errorf("storage=%s", dir)
		}
		for _, stage := range []string{"waiting", "checking", "downloading", "extracting", "ready"} {
			progress(local.RuntimeProgress{Stage: stage, Version: "v99.0.0", Completed: 100, Total: 100})
		}
		return local.Host{State: local.HostInstalled, Managed: true, Binary: "/fixture/ollama", Version: "v99.0.0"}, nil
	}
	ag, err := a.newAgent(context.Background(), &options{model: "ollama/qwen2.5-coder:7b"})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	p := &localLifetimeProcess{}
	a.localRuntime.Port = func() (int, error) { return port, nil }
	a.localRuntime.Ready = func(context.Context, string) bool { return true }
	a.localRuntime.Start = func(_ context.Context, binary string, _ []string, _ []string) (local.Process, error) {
		if binary != "/fixture/ollama" {
			t.Errorf("binary=%s", binary)
		}
		starts.Add(1)
		return p, nil
	}
	a.warmHost = func(context.Context, modelWarmer, string) {}
	// Opening status and the picker remains read-only, even with no runtime.
	if err := a.runLocalia(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	a.hostModelRows(context.Background(), provider.ConnectorManifest{}, map[string]bool{"custom:tiny": true})
	if installs.Load() != 0 || starts.Load() != 0 {
		t.Fatal("status/picker caused setup")
	}
	if _, err := a.switchModel(context.Background(), ag, "ollama/qwen2.5-coder:7b"); err != nil {
		t.Fatal(err)
	}
	if err := a.runLocaliaWith(context.Background(), ag, []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	if err := a.pullLocalModel(context.Background(), "qwen2.5-coder:7b", true); err != nil {
		t.Fatal(err)
	}
	answer, _, err := ag.Routes["ollama"].StreamChat(context.Background(), "qwen2.5-coder:7b", nil, nil, nil)
	if err != nil || answer.Content != "local answer" {
		t.Fatalf("chat=%q, %v", answer.Content, err)
	}
	if installs.Load() != 1 || starts.Load() != 1 || p.closed.Load() {
		t.Fatalf("ownership: installs=%d starts=%d closed=%v", installs.Load(), starts.Load(), p.closed.Load())
	}
	if !strings.Contains(out.String(), "Checksum verified") || !strings.Contains(out.String(), "runtime ready") {
		t.Fatalf("setup progress missing: %s", out)
	}
	if err := ag.Close(); err != nil {
		t.Fatal(err)
	}
	if !p.closed.Load() {
		t.Fatal("session exit left its new runtime running")
	}
}

func TestFailedNativeSetupKeepsPreviousSelection(t *testing.T) {
	storeFirstRunKey(t)
	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	before := ag.SessionModel()
	a.installLocalRuntime = func(context.Context, string, func(local.RuntimeProgress)) (local.Host, error) {
		return local.Host{}, context.Canceled
	}
	if _, err := a.switchModel(context.Background(), ag, "ollama/custom:tiny"); !errors.Is(err, context.Canceled) {
		t.Fatalf("setup=%v", err)
	}
	if ag.SessionModel() != before || ag.Sess.ModelName() != before {
		t.Fatal("canceled setup replaced model selection")
	}
}

func TestIncompleteLocalSelectionDoesNotProvision(t *testing.T) {
	storeFirstRunKey(t)
	a, _, _ := newTestApp(t, "")
	var installs atomic.Int32
	a.installLocalRuntime = func(context.Context, string, func(local.RuntimeProgress)) (local.Host, error) {
		installs.Add(1)
		return local.Host{}, errors.New("unexpected install")
	}
	ag, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	for _, ref := range []string{"ollama/", "ollama/ ", "ollama/name:", "ollama/name/", "ollama/model\nnext"} {
		if _, err := a.switchModel(context.Background(), ag, ref); err == nil {
			t.Errorf("accepted %q", ref)
		}
		if _, err := a.newAgent(context.Background(), &options{model: ref}); err == nil {
			t.Errorf("startup accepted %q", ref)
		}
	}
	if installs.Load() != 0 {
		t.Fatalf("invalid IDs invoked installer %d times", installs.Load())
	}
}

func TestCachedCustomModelsRemainVisibleBeforeRuntimeSetup(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	rows := a.hostModelRows(context.Background(), provider.ConnectorManifest{}, map[string]bool{"my-namespace/custom:tiny": true, "qwen2.5-coder:7b": true})
	row, ok := rowByID(rows, "ollama/my-namespace/custom:tiny")
	if !ok || !strings.Contains(row.Name, "sets up Localia") {
		t.Fatalf("custom cached model missing: %+v", row)
	}
	row, ok = rowByID(rows, "ollama/qwen2.5-coder:14b")
	if !ok || !strings.Contains(row.Name, "not pulled") {
		t.Fatalf("7b cache invented 14b weights: %+v", row)
	}
	// Read-only discovery does not create managed runtime storage.
	dirs, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirs.LocalRuntimeDir()); !os.IsNotExist(err) {
		t.Fatal("picker created runtime storage")
	}
}

// A companion download is its own progress line, named for the bundle, not a
// second "Runtime" run the throttle hides behind the first one's 100%. A note
// (a companion that could not be added) is shown, not dropped.
func TestNativeSetupProgressNamesTheCompanionAndShowsNotes(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	a.installLocalRuntime = func(_ context.Context, _ string, progress func(local.RuntimeProgress)) (local.Host, error) {
		for _, bundle := range []string{"", "rocm"} {
			for _, done := range []int64{0, 50, 100} {
				progress(local.RuntimeProgress{Stage: "downloading", Version: "v99.0.0", Bundle: bundle, Completed: done << 20, Total: 100 << 20})
			}
		}
		progress(local.RuntimeProgress{Stage: "note", Note: "the rocm bundle for AMD GPU card0 could not be added (offline); using the installed runtime without it"})
		progress(local.RuntimeProgress{Stage: "ready", Version: "v99.0.0"})
		return local.Host{State: local.HostInstalled, Managed: true, Binary: "/fixture/ollama", Version: "v99.0.0"}, nil
	}
	var out strings.Builder
	if _, err := a.provisionLocalRuntime(context.Background(), dirs, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Runtime 0%", "Runtime 50%", "Runtime 100%", "ROCm bundle 0%", "ROCm bundle 50%", "ROCm bundle 100%", "! the rocm bundle for AMD GPU card0 could not be added"} {
		if !strings.Contains(text, want) {
			t.Errorf("progress lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "Runtime 100%") != 1 {
		t.Errorf("the companion was reported as a second Runtime download:\n%s", text)
	}
}

// /localia status says what setup would still add for this machine's
// accelerator, and names hardware no official bundle serves, instead of
// leaving a user to wonder why their GPU sits idle.
func TestLocaliaStatusNamesMissingCompanionsAndUnsupportedAccelerators(t *testing.T) {
	for _, c := range []struct {
		name string
		host local.Host
		want []string
	}{
		{"missing companion", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"},
			[]string{"ROCm bundle for AMD GPU card0", "next local setup, pull or model start adds it"}},
		{"unsupported accelerator", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", AcceleratorNote: "this Jetson reports L4T R32, which no official Ollama JetPack bundle supports; Ollama may not use its GPU"},
			[]string{"! this Jetson reports L4T R32"}},
		{"absent with companion", local.Host{State: local.HostAbsent, MissingCompanion: "JetPack 6 bundle for NVIDIA Jetson, L4T R36"},
			[]string{"JetPack 6 bundle for NVIDIA Jetson, L4T R36"}},
		{"running without companion", local.Host{State: local.HostRunning, Managed: true, Addr: "127.0.0.1:43216", MissingCompanion: "ROCm bundle for AMD GPU card0"},
			[]string{"ROCm bundle for AMD GPU card0", "running runtime started without it"}},
		{"remembered failure", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "native runtime SHA-256 verification failed"},
			[]string{"earlier attempt failed (native runtime SHA-256 verification failed)", "`/localia setup` retries it"}},
		{"running after a remembered failure", local.Host{State: local.HostRunning, Managed: true, Addr: "127.0.0.1:43219", MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "native runtime SHA-256 verification failed"},
			[]string{"earlier attempt failed (native runtime SHA-256 verification failed)", "running runtime started without it", "After `/localia setup`, its next start retries"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, out, _ := newTestApp(t, "")
			a.discoverHost = func(context.Context) local.Host { return c.host }
			// Discovery's own notes come from the installer (tested in
			// internal/local); this is what the session's starter reports.
			a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return c.host }}
			if err := a.runLocalia(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			_, runtime, _ := strings.Cut(out.String(), "RUNTIME")
			for _, want := range c.want {
				if !strings.Contains(runtime, want) {
					t.Errorf("RUNTIME section lacks %q:\n%s", want, runtime)
				}
			}
			// A running runtime is reused as it is, and a remembered failure is
			// skipped: neither is added to by the next setup, pull or start.
			if (c.host.State == local.HostRunning || c.host.CompanionFailure != "") && strings.Contains(runtime, "next local setup") {
				t.Errorf("status promises setup adds the bundle:\n%s", runtime)
			}
			if c.host.CompanionFailure != "" && strings.Contains(runtime, "adds it the next time it starts one") {
				t.Errorf("status promises setup adds the bundle to a running runtime:\n%s", runtime)
			}
		})
	}
}

// An explicit /localia setup is the retry a remembered failure withholds: it
// forgets remembered failures before anything else, even with a runtime up.
func TestLocaliaSetupForgetsRememberedFailures(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	installations := filepath.Join(dirs.LocalRuntimeDir(), "installations")
	if err := os.MkdirAll(installations, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(installations, "failed-v99.12.3-linux-amd64-abc-rocm-def.json")
	if err := os.WriteFile(marker, []byte(`{"error":"native runtime SHA-256 verification failed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: "127.0.0.1:43217", Managed: true}
	}}
	if err := a.setupLocalRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("an explicit setup kept the remembered failure: %v", err)
	}
}

// /doctor tells the same truth as /localia: a Kolk-managed runtime is not a
// server "kolk never stops", and a missing accelerator bundle or an unused GPU
// is exactly what a health check should name.
func TestDoctorDescribesKolksRuntimeAndItsAccelerator(t *testing.T) {
	for _, c := range []struct {
		name      string
		host      local.Host
		want, not []string
	}{
		{"managed with missing bundle", local.Host{State: local.HostRunning, Managed: true, Addr: "127.0.0.1:43220", Version: "0.34.4", MissingCompanion: "ROCm bundle for AMD GPU card0"},
			[]string{"Kolk's runtime", "ROCm bundle for AMD GPU card0"}, []string{"never stops it"}},
		{"user's own server", local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.34.4"},
			[]string{"never stops it"}, []string{"Kolk's runtime", "accelerator:"}},
		{"unserved accelerator", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", AcceleratorNote: "AMD GPU card0: Ollama publishes no ROCm bundle for linux/arm64, so it runs on the CPU"},
			[]string{"! AMD GPU card0"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, out, _ := newTestApp(t, "")
			a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return c.host }}
			a.doctorLocalModels(context.Background())
			text := out.String()
			for _, want := range c.want {
				if !strings.Contains(text, want) {
					t.Errorf("doctor lacks %q:\n%s", want, text)
				}
			}
			for _, not := range c.not {
				if strings.Contains(text, not) {
					t.Errorf("doctor says %q:\n%s", not, text)
				}
			}
		})
	}
}

// Picking a model is consent to what the pick does, so the row says it: a
// managed runtime missing its accelerator bundle is downloaded again with it,
// and a first setup names the bundle it adds.
func TestPickerRowsSayWhatAPickDownloads(t *testing.T) {
	for _, c := range []struct {
		name string
		host local.Host
		want string
	}{
		{"managed missing bundle", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"},
			"downloads Ollama again with its ROCm bundle for AMD GPU card0 when picked"},
		{"failed before", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "native runtime SHA-256 verification failed"},
			"starts ollama when picked"},
		{"absent with bundle", local.Host{State: local.HostAbsent, MissingCompanion: "ROCm bundle for AMD GPU card0"},
			"sets up Localia with its ROCm bundle for AMD GPU card0 when picked"},
		{"user's own", local.Host{State: local.HostInstalled, Binary: "/usr/local/bin/ollama"}, "starts ollama when picked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _, _ := newTestApp(t, "")
			a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return c.host }}
			a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
			rows := a.hostModelRows(context.Background(), provider.ConnectorManifest{}, map[string]bool{"qwen2.5-coder:7b": true})
			var name string
			for _, row := range rows {
				if row.ID == "ollama/qwen2.5-coder:7b" {
					name = row.Name
				}
			}
			if !strings.Contains(name, c.want) {
				t.Fatalf("row = %q; want %q", name, c.want)
			}
		})
	}
}

// The local catalog tells a reader that Cloud tags are pulled too, without a
// fit plan, since the list above them holds only models that run here.
func TestLocalCatalogMentionsCloudTags(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if err := a.printLocalCatalog(""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/localia pull --yes <tag>-cloud") {
		t.Fatalf("catalog does not mention Cloud tags:\n%s", out.String())
	}
}

// Every local row says what its pick does to the runtime, not-pulled rows
// included; a platform without managed setup is told to install Ollama; and
// a remembered failure still names the download a new release would bring.
func TestEveryPickerRowSaysWhatThePickSetsUp(t *testing.T) {
	for _, c := range []struct {
		name      string
		host      local.Host
		pulled    bool
		supported bool
		want      string
	}{
		{"not pulled, absent with bundle", local.Host{State: local.HostAbsent, MissingCompanion: "ROCm bundle for AMD GPU card0"}, false, true,
			"not pulled: /localia pull qwen2.5-coder:7b · sets up Localia with its ROCm bundle for AMD GPU card0 when picked"},
		{"not pulled, incomplete tree", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}, false, true,
			"not pulled: /localia pull qwen2.5-coder:7b · downloads Ollama again with its ROCm bundle for AMD GPU card0 when picked"},
		{"failed before", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "SHA-256"}, true, true,
			"starts ollama when picked; a new Ollama release downloads it again with its ROCm bundle for AMD GPU card0"},
		{"no managed setup here", local.Host{State: local.HostAbsent}, true, false,
			"needs Ollama installed first"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _, _ := newTestApp(t, "")
			a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return c.host }}
			a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
			a.managedSetupSupported = func() bool { return c.supported }
			rows := a.hostModelRows(context.Background(), provider.ConnectorManifest{}, map[string]bool{"qwen2.5-coder:7b": c.pulled})
			var name string
			for _, row := range rows {
				if row.ID == "ollama/qwen2.5-coder:7b" {
					name = row.Name
				}
			}
			if !strings.Contains(name, c.want) {
				t.Fatalf("row = %q; want %q", name, c.want)
			}
		})
	}
}

// A runtime an earlier persistent setting left running is described as such,
// not as "stops when this session closes" or "not running".
func TestStatusAndDoctorNameAKeptRuntime(t *testing.T) {
	kept := local.Host{State: local.HostRunning, Managed: true, KeptRunning: true, Addr: "127.0.0.1:43210", Version: "0.34.4"}
	a, out, _ := newTestApp(t, "")
	a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return kept }}
	if err := a.runLocalia(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	a.doctorLocalModels(context.Background())
	text := out.String()
	if strings.Count(text, "kept running from an earlier `local.ephemeral off`") != 2 || strings.Contains(text, "stops when this session closes") {
		t.Fatalf("status and doctor for a kept runtime:\n%s", text)
	}
}

// /localia setup with a runtime running forgets the remembered failure, so
// the next start retries; it says so, and status stops blaming the failure.
func TestLocaliaSetupWithARunningRuntimeSaysWhatItLacks(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	incomplete := local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/standard/ollama",
		MissingCompanion: "ROCm bundle for AMD GPU card0", CompanionFailure: "native runtime SHA-256 verification failed"}
	a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return incomplete },
		Port: func() (int, error) { return 43221, nil }, Ready: func(context.Context, string) bool { return true },
		Start: func(context.Context, string, []string, []string) (local.Process, error) {
			return &localLifetimeProcess{}, nil
		},
	}
	defer func() { _ = a.localRuntime.Close() }()
	if _, err := a.localRuntime.EnsureInstalled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.setupLocalRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "running runtime started without it, and Kolk adds it the next time it starts one") {
		t.Fatalf("setup with a running runtime did not say what it lacks:\n%s", out.String())
	}
	out.Reset()
	if err := a.runLocalia(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "earlier attempt failed") {
		t.Fatalf("status still blames a failure setup forgot:\n%s", out.String())
	}
}

// The plain /model listing says what a pick does in the same words as the
// picker: for a tree missing its bundle, a pick downloads the runtime again.
func TestModelListingSaysWhatAPickDoesLikeThePicker(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host {
		return local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", MissingCompanion: "ROCm bundle for AMD GPU card0"}
	}}
	a.printHostModels(context.Background(), "", "")
	text := out.String()
	if !strings.Contains(text, "downloads Ollama again with its ROCm bundle for AMD GPU card0 when picked") || strings.Contains(text, "pick a pulled one and start it") {
		t.Fatalf("listing disagrees with the picker:\n%s", text)
	}
}

// A fit plan never places a model on a GPU the runtime cannot use: an AMD
// card with no ROCm bundle for it is left out, as the status note says, both
// for an installed runtime and before the first one is set up.
func TestFitPlanLeavesOutAnUnservedGPU(t *testing.T) {
	const note = "AMD GPU card0: Ollama publishes no ROCm bundle for linux/arm64, so it runs on the CPU"
	for _, c := range []struct {
		name string
		host local.Host
	}{
		{"installed", local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", UnservedVendor: "amd", AcceleratorNote: note}},
		{"no runtime yet", local.Host{UnservedVendor: "amd", AcceleratorNote: note}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, out, _ := newTestApp(t, "")
			a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
			a.localRuntime = &local.HostStarter{Discover: func(context.Context) local.Host { return c.host }}
			if err := a.printLocalPlan(context.Background(), "qwen2.5-coder:7b"); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "card0") {
				t.Fatalf("plan places the model on the AMD GPU the runtime cannot use:\n%s", out.String())
			}
		})
	}
}

// A recorded runtime that runs but does not answer is named on every
// surface, with what this session does about it, never as "not running".
func TestStatusDoctorAndPickerNameAStalledRuntime(t *testing.T) {
	const stalled = "Kolk's runtime for this project (process 910001) is running but not answering at 127.0.0.1:43210"
	for _, c := range []struct {
		name         string
		persistent   bool
		advice, pick string
	}{
		{"ephemeral", false, "this session starts its own", "starts ollama when picked"},
		{"persistent", true, "local models fail until it answers", "Kolk's runtime is not answering"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, out, _ := newTestApp(t, "")
			host := local.Host{State: local.HostInstalled, Managed: true, Binary: "/managed/ollama", StalledRuntime: stalled}
			a.localRuntime = &local.HostStarter{Persistent: c.persistent, Discover: func(context.Context) local.Host { return host }}
			if err := a.runLocalia(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			a.doctorLocalModels(context.Background())
			text := out.String()
			if strings.Count(text, stalled) != 2 || strings.Count(text, c.advice) != 2 {
				t.Errorf("status and doctor do not both name the stall and %q:\n%s", c.advice, text)
			}
			if strings.Contains(text, "/managed/ollama, not running") {
				t.Errorf("doctor calls a stalled runtime not running:\n%s", text)
			}
			if suffix := a.pickSuffix(a.localHost(context.Background())); !strings.Contains(suffix, c.pick) {
				t.Errorf("picker suffix = %q; want %q", suffix, c.pick)
			}
		})
	}
}
