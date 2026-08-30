package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/selfupdate"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// replFixture builds a REPL over a scripted provider: no network, no key.
func replFixture(t *testing.T, stdin string, steps ...enginetest.Step) (*app, *engine.Agent, *bytes.Buffer) {
	t.Helper()
	// Isolate first. Without this the app resolves the developer's own
	// ~/.config/kolk, and any setting they happen to have turned on decides the
	// result: `auto_restart_after_update: true` made /update exit the REPL and
	// failed a test that passes on CI, where no config exists. A suite that
	// depends on whose machine it runs on is a suite that reports the wrong
	// thing on exactly one machine — the maintainer's.
	isolateHome(t)
	srv := enginetest.New(steps...)
	t.Cleanup(srv.Close)

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out, in: bufio.NewReader(strings.NewReader(stdin))}
	ag := engine.New(engine.Options{
		Client:     client,
		Model:      "mock/model",
		Sess:       session.New(t.TempDir(), "mock/model"),
		Permission: engine.PermissionFullAuto,
		Out:        io.Discard,
	})
	return a, ag, &out
}

// A piped script whose last line has no trailing newline used to be dropped:
// bufio.ReadString returns the line AND io.EOF together, and the loop returned
// on any error.
func TestReplRunsAFinalLineWithNoTrailingNewline(t *testing.T) {
	a, ag, out := replFixture(t, "/session") // deliberately no \n
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	if !strings.Contains(out.String(), ag.Sess.SessionID()) {
		t.Errorf("the last command was dropped; output was:\n%s", out.String())
	}
}

func TestReplExitsOnEOF(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("empty input should be a clean exit, got %v", err)
	}
}

func TestReplExitsOnSlashExit(t *testing.T) {
	a, ag, out := replFixture(t, "/exit\n/session\n")
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	if strings.Contains(out.String(), "id:    ") {
		t.Errorf("/exit did not stop the loop; /session still ran:\n%s", out.String())
	}
}

func TestReplPrefixesEveryModePromptWithKolk(t *testing.T) {
	for _, mode := range engine.Modes {
		t.Run(mode, func(t *testing.T) {
			a, ag, out := replFixture(t, "/exit\n")
			ag.Mode = mode
			if err := a.repl(context.Background(), ag); err != nil {
				t.Fatalf("repl returned %v", err)
			}
			// Every mode opens a line the same way. The mode itself is in
			// the banner and in /mode, not repeated on every prompt.
			want := "\033[1m❯\033[0m "
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%s prompt = %q, want %q", mode, out.String(), want)
			}
			if strings.Contains(out.String(), "kolk-"+mode+">") {
				t.Fatalf("%s prompt kept the old label: %q", mode, out.String())
			}
		})
	}
}

func TestReplModeChangeKeepsOneMarkerAndReportsTheMode(t *testing.T) {
	a, ag, out := replFixture(t, "/mode chat\n/exit\n")
	ag.Mode = engine.ModeCode
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	got := out.String()
	if strings.Count(got, "\033[1m❯\033[0m ") < 2 {
		t.Fatalf("prompt marker did not survive a mode change: %q", got)
	}
	// The mode still has to be sayable: it moved, it did not disappear.
	if !strings.Contains(got, "mode: chat") {
		t.Fatalf("mode change went unreported: %q", got)
	}
}

func TestReplReportsARealReadError(t *testing.T) {
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out, in: bufio.NewReader(errReader{})}
	_, ag, _ := replFixture(t, "")
	if err := a.repl(context.Background(), ag); err == nil {
		t.Error("a broken stdin must be an error, not a silent clean exit")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReplRunsATurnAndSlashCommands(t *testing.T) {
	a, ag, out := replFixture(t, "hello there\n/mode chat\n/full-auto\n",
		enginetest.Step{Text: "hi back", Cost: 0.001})
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "mode: chat") {
		t.Errorf("/mode chat did not take effect:\n%s", got)
	}
	if !strings.Contains(got, "permission: full-auto") {
		t.Errorf("/full-auto did not take effect:\n%s", got)
	}
	if len(ag.Sess.GetMessages()) < 3 {
		t.Errorf("the turn did not reach the session: %d messages", len(ag.Sess.GetMessages()))
	}
}

func TestSlashUnknownCommandDoesNotExit(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/nonsense") {
		t.Error("an unknown slash command must not quit the REPL")
	}
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("an unknown slash command should point at /help, got %q", out.String())
	}
}

func TestSlashModeSwitchesToAgent(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/mode agent") {
		t.Fatal("/mode agent must not exit the REPL")
	}
	if ag.Mode != engine.ModeAgent {
		t.Fatalf("mode = %q, want %q", ag.Mode, engine.ModeAgent)
	}
	if !strings.Contains(out.String(), "mode: agent") {
		t.Fatalf("mode switch was not reported: %q", out.String())
	}
}

func TestSlashHelpListsAllReleaseModes(t *testing.T) {
	a, ag, out := replFixture(t, "")
	a.slash(context.Background(), ag, "/help")
	if !strings.Contains(out.String(), "/mode <chat|code|agent>") {
		t.Fatalf("slash help does not list all three release modes: %q", out.String())
	}
	if !strings.Contains(out.String(), "agent = orchestrated") {
		t.Fatalf("slash help does not explain agent mode: %q", out.String())
	}
	// All three tiers have to be discoverable from help, not just the one a
	// user already knows to ask for.
	for _, tier := range []string{"/permissions", "/ask", "/auto-approve", "/full-auto"} {
		if !strings.Contains(out.String(), tier) {
			t.Fatalf("slash help does not list %s: %q", tier, out.String())
		}
	}
	if !strings.Contains(out.String(), "/model [id]") || !strings.Contains(out.String(), "list available models") {
		t.Fatalf("slash help does not describe model listing and switching: %q", out.String())
	}
	if !strings.Contains(out.String(), "/update") {
		t.Fatalf("slash help does not list the update command: %q", out.String())
	}
	if !strings.Contains(out.String(), "↑ last message") || !strings.Contains(out.String(), "twice exits") {
		t.Fatalf("slash help does not explain TUI history/interrupt keys: %q", out.String())
	}
}

func TestSlashAutoApproveControlsTheLiveSession(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Permission = engine.PermissionAsk

	if a.slash(context.Background(), ag, "/auto-approve") {
		t.Fatal("/auto-approve must not exit the REPL")
	}
	if ag.Permission != engine.PermissionAutoApprove {
		t.Fatalf("permission = %q", ag.Permission)
	}
	// The tier has to state what it still asks about, or a user reads
	// "auto-approve" as "approves everything".
	if !strings.Contains(out.String(), "still asks") {
		t.Fatalf("auto-approve did not say what it still asks about: %q", out.String())
	}
	if !strings.Contains(out.String(), "this process only") {
		t.Fatalf("tier change does not explain its scope: %q", out.String())
	}

	if a.slash(context.Background(), ag, "/ask") {
		t.Fatal("/ask must not exit the REPL")
	}
	if ag.Permission != engine.PermissionAsk {
		t.Fatalf("permission = %q", ag.Permission)
	}
}

func TestSlashPermissionsRejectsAnUnknownTierWithoutChangingState(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Permission = engine.PermissionAsk

	if a.slash(context.Background(), ag, "/permissions forever") {
		t.Fatal("an invalid tier must not exit the REPL")
	}
	if ag.Permission != engine.PermissionAsk {
		t.Fatal("an invalid tier changed the current state")
	}
	// Refusing without showing the choices leaves the user guessing.
	if got := out.String(); !strings.Contains(got, "full-auto") {
		t.Fatalf("an invalid tier did not list the choices: %q", got)
	}
}

func TestSlashUpdateReportsRestartAndKeepsSessionAlive(t *testing.T) {
	a, ag, out := replFixture(t, "")
	a.currentVersion = func() string { return "1.0.0" }
	calls := 0
	a.update = func(context.Context) (selfupdate.Result, error) {
		calls++
		if got := out.String(); got != "Current version: 1.0.0\nChecking for updates to latest version...\n" {
			t.Fatalf("pre-update output = %q", got)
		}
		return selfupdate.Result{
			Current: "1.0.0", Latest: "1.2.3", Updated: true, Path: "/usr/local/bin/kolk",
		}, nil
	}
	if a.slash(context.Background(), ag, "/update") {
		t.Fatal("/update must not exit the REPL")
	}
	for _, want := range []string{
		"Kolk updated successfully (1.0.0 → 1.2.3)",
		"Installed to: /usr/local/bin/kolk",
		"Restart kolk to use 1.2.3",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("calls = %d, output omitted %q: %q", calls, want, out.String())
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, output = %q", calls, out.String())
	}
}

func TestSlashUpdateUnchangedDoesNotRequestRestart(t *testing.T) {
	a, ag, out := replFixture(t, "")
	a.currentVersion = func() string { return "1.2.3" }
	a.update = func(context.Context) (selfupdate.Result, error) {
		return selfupdate.Result{Current: "1.2.3", Latest: "1.2.3"}, nil
	}
	if a.slash(context.Background(), ag, "/update") {
		t.Fatal("unchanged /update must not exit the REPL")
	}
	if !strings.Contains(out.String(), "Kolk is up to date (1.2.3)") || strings.Contains(strings.ToLower(out.String()), "restart") {
		t.Fatalf("unchanged output = %q", out.String())
	}
}

func TestSlashUpdateFailureAndArgumentsKeepSessionAlive(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		a, ag, out := replFixture(t, "")
		a.currentVersion = func() string { return "1.2.3" }
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{}, errors.New("network unavailable")
		}
		if a.slash(context.Background(), ag, "/update") {
			t.Fatal("failed /update must not exit the REPL")
		}
		if !strings.Contains(out.String(), "update failed: network unavailable") {
			t.Fatalf("failure output = %q", out.String())
		}
		for _, want := range []string{"Current version: 1.2.3", "Checking for updates to latest version..."} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("failure output omitted %q: %q", want, out.String())
			}
		}
	})

	t.Run("argument", func(t *testing.T) {
		a, ag, out := replFixture(t, "")
		calls := 0
		a.update = func(context.Context) (selfupdate.Result, error) {
			calls++
			return selfupdate.Result{}, nil
		}
		if a.slash(context.Background(), ag, "/update now") {
			t.Fatal("invalid /update must not exit the REPL")
		}
		if calls != 0 || !strings.Contains(out.String(), "usage: /update") {
			t.Fatalf("calls = %d, output = %q", calls, out.String())
		}
	})
}

func TestSlashUpdateUsesActiveContext(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.update = func(got context.Context) (selfupdate.Result, error) {
		if !errors.Is(got.Err(), context.Canceled) {
			t.Fatalf("updater context error = %v, want cancelled", got.Err())
		}
		return selfupdate.Result{}, got.Err()
	}
	if a.slash(ctx, ag, "/update") {
		t.Fatal("cancelled /update must not exit the REPL")
	}
}

func TestSlashModelListsTheActiveProviderCatalog(t *testing.T) {
	isolateHome(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[`+
			`{"id":"z/model","name":"Zulu","context_length":32000,"pricing":{"prompt":"0.000001","completion":"0.000002"}},`+
			`{"id":"a/free","name":"Alpha","context_length":1000000,"pricing":{"prompt":"0","completion":"0"}}]}`)
	}))
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	a, out, errOut := newTestApp(t, "")
	ag := engine.New(engine.Options{
		Client: client, Model: "current/model", Sess: session.New(t.TempDir(), "current/model"), Out: io.Discard,
	})

	if a.slash(context.Background(), ag, "/model") {
		t.Fatal("/model must not exit the REPL")
	}
	got := out.String()
	if !strings.Contains(got, "current model: current/model") {
		t.Fatalf("/model omitted the current model: %q", got)
	}
	free, paid := strings.Index(got, "a/free"), strings.Index(got, "z/model")
	if free < 0 || paid < 0 || free > paid {
		t.Fatalf("/model catalog is missing or unsorted: %q", got)
	}
	for _, want := range []string{"ctx 1000000", "free", "$1.00 in / $2.00 out per 1M tokens"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/model catalog omitted %q: %q", want, got)
		}
	}
	if requests.Load() != 1 || errOut.Len() != 0 {
		t.Fatalf("catalog requests = %d, stderr = %q", requests.Load(), errOut.String())
	}
}

func TestSlashModelDirectSwitchDoesNotFetchCatalog(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	a, ag, out := replFixture(t, "")
	ag.Client = client

	if a.slash(context.Background(), ag, "/model vendor/new") {
		t.Fatal("/model <id> must not exit the REPL")
	}
	if ag.Model != "vendor/new" || ag.Sess.ModelName() != "vendor/new" {
		t.Fatalf("direct model switch = (%q, %q)", ag.Model, ag.Sess.ModelName())
	}
	if requests.Load() != 0 {
		t.Fatalf("direct model switch made %d catalog requests", requests.Load())
	}
	if !strings.Contains(out.String(), "model set to vendor/new") {
		t.Fatalf("direct switch was not reported: %q", out.String())
	}
}

func TestSlashModelCatalogFailureKeepsTheSession(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	a, out, errOut := newTestApp(t, "")
	ag := engine.New(engine.Options{
		Client: client, Model: "current/model", Sess: session.New(t.TempDir(), "current/model"), Out: io.Discard,
	})

	if a.slash(context.Background(), ag, "/model") {
		t.Fatal("catalog failure must not exit the REPL")
	}
	if ag.Model != "current/model" || ag.Sess.ModelName() != "current/model" {
		t.Fatal("catalog failure changed the current model")
	}
	if !strings.Contains(out.String(), "current model: current/model") ||
		!strings.Contains(errOut.String(), "could not list models") {
		t.Fatalf("catalog failure output = stdout %q, stderr %q", out.String(), errOut.String())
	}
}

func TestSlashEffortSwitchesAndReportsActiveModel(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Tiers = map[string]string{
		engine.EffortHigh: "frontier/high-model",
	}

	// 1. Bare /effort displays current effort
	if a.slash(context.Background(), ag, "/effort") {
		t.Fatal("/effort must not exit REPL")
	}
	if !strings.Contains(out.String(), "effort: medium (low|medium|high|max)") {
		t.Errorf("bare /effort output = %q", out.String())
	}

	// 2. Set effort to high via numeric alias "3"
	out.Reset()
	if a.slash(context.Background(), ag, "/effort 3") {
		t.Fatal("/effort 3 must not exit REPL")
	}
	if ag.Effort != engine.EffortHigh {
		t.Errorf("ag.Effort = %q, want %q", ag.Effort, engine.EffortHigh)
	}
	if !strings.Contains(out.String(), "effort: high → frontier/high-model") {
		t.Errorf("switch output = %q", out.String())
	}
}

func TestSlashModelResolvesAliases(t *testing.T) {
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/model sonnet") {
		t.Fatal("/model sonnet must not exit REPL")
	}
	if ag.Model != "anthropic/claude-3-7-sonnet" {
		t.Errorf("ag.Model = %q, want anthropic/claude-3-7-sonnet", ag.Model)
	}
	if !ag.PinnedModel {
		t.Error("explicit /model did not set ag.PinnedModel")
	}
	if !strings.Contains(out.String(), "model set to anthropic/claude-3-7-sonnet") {
		t.Errorf("output = %q", out.String())
	}

	out.Reset()
	if a.slash(context.Background(), ag, "/model flash") {
		t.Fatal("/model flash must not exit REPL")
	}
	if ag.Model != "google/gemini-2.5-flash" {
		t.Errorf("ag.Model = %q, want google/gemini-2.5-flash", ag.Model)
	}
	if !strings.Contains(out.String(), "model set to google/gemini-2.5-flash") {
		t.Errorf("output = %q", out.String())
	}
}

// The ceiling is enforced in the engine, but a limit nobody can see is one
// people find out about by being surprised. Entering agent mode says what an
// orchestrated run may reach.
func TestAgentModeSaysWhichModelsTheRunMayUse(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Model = "claude-sonnet"
	if a.slash(context.Background(), ag, "/mode agent") {
		t.Fatal("/mode must not exit the REPL")
	}
	got := out.String()
	// Stated as what is refused, which is the part that is guaranteed. Naming
	// the cheaper rungs would predict a routing decision the router does not
	// yet make on a plan session, where slots still resolve gateway models.
	if !strings.Contains(got, "capped at claude-sonnet") {
		t.Errorf("agent mode did not say what it is capped at:\n%s", got)
	}
	for _, blocked := range []string{"claude-opus", "claude-fable"} {
		if !strings.Contains(got, blocked) {
			t.Errorf("agent mode did not say %s is out of reach:\n%s", blocked, got)
		}
	}
	if !strings.Contains(got, "out of reach") {
		t.Errorf("the message does not say the stronger models are refused:\n%s", got)
	}
}

// Nothing is claimed for a model kolk has not ranked, and nothing is said when
// the user already picked the cheapest rung.
func TestNoCeilingLineWhenThereIsNothingToSay(t *testing.T) {
	// An unranked model, and the strongest rung: nothing is out of reach in
	// either case, so there is nothing true to say.
	for _, model := range []string{"mock/model", "claude-fable"} {
		a, ag, out := replFixture(t, "")
		ag.Model = model
		a.slash(context.Background(), ag, "/mode agent")
		if strings.Contains(out.String(), "capped at") {
			t.Errorf("model %q produced a ceiling line it cannot support:\n%s", model, out.String())
		}
	}
}

// Code mode is not orchestrated, so the line would be noise.
func TestNoCeilingLineOutsideAgentMode(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Model = "claude-sonnet"
	a.slash(context.Background(), ag, "/mode code")
	if strings.Contains(out.String(), "capped at") {
		t.Errorf("code mode printed the agent ceiling line:\n%s", out.String())
	}
}
