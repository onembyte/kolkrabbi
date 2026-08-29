package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
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

func TestLocaliaNamesTheDirectoryItManages(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatal("localia must succeed")
	}
	if !strings.Contains(out.String(), dirs.LocalModelsDir()) {
		t.Fatalf("localia output = %q, want the managed model directory named", out.String())
	}
}

func TestLocaliaSaysNoModelIsInstalledYet(t *testing.T) {
	isolateConnectorState(t)
	a, out, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatal("localia must succeed with no models installed")
	}
	if !strings.Contains(out.String(), "no local model") {
		t.Fatalf("localia output = %q, want it to say nothing is installed", out.String())
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

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatalf("localia exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "system RAM") {
		t.Fatalf("localia output = %q", out.String())
	}
}

func TestLocaliaModelsListsTheCatalogWithSizes(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"localia", "models"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "plan", "qwen2.5-coder:7b"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "plan", "phi4:14b"}); code == ExitOK {
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
	if code := a.main(context.Background(), []string{"localia", "plan", "phi4:14b"}); code == ExitOK {
		t.Fatalf("configured headroom was ignored; stderr = %q", errOut.String())
	}
}

func TestLocaliaPlanReservesHeadroomByDefault(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia", "plan", "qwen2.5-coder:7b"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "plan", "qwen2.5-coder:7b"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"}); code != ExitOK {
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

	if code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:14b"}); code == ExitOK {
		t.Fatal("a model that cannot fit must not be offered")
	}
	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want no question for a model that cannot fit", out.String())
	}
	if !strings.Contains(errOut.String(), "GiB") {
		t.Fatalf("stderr = %q, want the sizes behind the refusal", errOut.String())
	}
}

func TestLocaliaPullSaysWhatIsMissingWhenApproved(t *testing.T) {
	a, _, errOut := pullFixture(t, "y\n")

	code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"})
	if code == ExitOK {
		t.Fatal("with no Ollama installed the pull cannot succeed")
	}
	if !strings.Contains(errOut.String(), "ollama.com") {
		t.Fatalf("stderr = %q, want the install line", errOut.String())
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
	if code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"}); code != ExitOK {
		t.Fatalf("pull exit = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{"pulling manifest", "100%", "success", "ollama/qwen2.5-coder:7b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
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
	if code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"}); code != ExitOK {
		t.Fatalf("pull exit = %d, stderr = %q", code, errOut.String())
	}
	if started != 1 || stopped != 1 {
		t.Fatalf("started %d, stopped %d; want the server up for the pull and down after", started, stopped)
	}
}

func TestLocaliaPullYesSkipsTheQuestion(t *testing.T) {
	a, out, _ := pullFixture(t, "")

	_ = a.main(context.Background(), []string{"localia", "pull", "--yes", "qwen2.5-coder:7b"})
	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want --yes to answer it", out.String())
	}
}

func TestLocaliaPullWritesNothingWhenDeclined(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "n\n")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	_ = a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"})
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
	code := a.main(context.Background(), []string{"localia", "pull", "qwen2.5-coder:7b"})

	if strings.Contains(strings.ToLower(out.String()), "[y/n]") {
		t.Fatalf("output = %q, want no prompt while the session owns the keyboard", out.String())
	}
	if code == ExitOK {
		t.Fatal("the pull must not proceed unconfirmed")
	}
	if !strings.Contains(errOut.String(), "kolk localia pull") {
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
	_ = a.main(context.Background(), []string{"localia", "pull", "--yes", "qwen2.5-coder:7b"})
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

	_ = a.main(context.Background(), []string{"localia"})

	// nvidia-smi against a wedged driver is a known hang. Unknown is a valid
	// answer; a frozen session is not.
	if !hasDeadline {
		t.Fatal("the hardware probe runs without a deadline")
	}
	if until := time.Until(deadline); until <= 0 || until > time.Minute {
		t.Fatalf("probe deadline is %s away, want a short bound", until)
	}
}
