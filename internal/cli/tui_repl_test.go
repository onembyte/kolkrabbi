package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func TestTUIReplOwnsAndRestoresOneInteractiveTerminal(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()

	var output bytes.Buffer
	restored := 0
	a := &app{
		stdout: &output, stderr: &output,
		terminalInput: input, terminalOutput: os.Stdout,
		enterRaw: func(got *os.File) (func() error, error) {
			if got != input {
				t.Fatalf("raw input = %v, want test pipe", got)
			}
			return func() error { restored++; return nil }, nil
		},
		terminalSize: func(*os.File) (int, int) { return 72, 14 },
	}
	ag := engine.New(engine.Options{
		Model: "mock/model", Mode: engine.ModeCode, Effort: "standard",
		Sess: session.New(t.TempDir(), "mock/model"), Out: &output,
	})

	writeErr := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("/exit\r"))
		writeErr <- err
	}()
	if err := a.tuiRepl(context.Background(), ag); err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatalf("terminal restore calls = %d, want one", restored)
	}
	got := output.String()
	for _, want := range []string{
		"\x1b[?2004h", "\x1b[?25l", "mode code", "mock/model", "Up arrow recalls history",
		// "twice" alone: the welcome word-wraps at 72 columns, so the phrase may
		// be split across two rows of the raw frame.
		"twice", "\x1b[?25h", "\x1b[?2004l",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output omitted %q: %q", want, got)
		}
	}
	if a.stdout != &output || a.stderr != &output {
		t.Fatal("TUI did not restore app output streams")
	}
	if strings.Contains(got, "kolk — mode:") || strings.Contains(got, "session: ") {
		t.Fatalf("persistent metadata was duplicated in startup transcript: %q", got)
	}
}

func TestTUIWelcomeIsPlainAndReportsAResumedSessionWithoutDuplicateMetadata(t *testing.T) {
	got := tuiWelcome(8)
	for _, want := range []string{"Type a request or /help.", "Up arrow recalls history", "Resumed with 7 messages."} {
		if !strings.Contains(got, want) {
			t.Fatalf("welcome omitted %q: %q", want, got)
		}
	}
	for _, duplicate := range []string{"model:", "mode:", "effort:", "session:"} {
		if strings.Contains(got, duplicate) {
			t.Fatalf("welcome duplicated persistent %s metadata: %q", duplicate, got)
		}
	}
}

func TestTUIEligibilityRequiresRealTerminalFilesAndRawMode(t *testing.T) {
	a := &app{canAnimate: func() bool { return true }}
	if a.canUseTUI() {
		t.Fatal("stream-only app selected the raw TUI")
	}
	a.terminalInput, a.terminalOutput = os.Stdin, os.Stdout
	a.enterRaw = func(*os.File) (func() error, error) { return func() error { return nil }, nil }
	a.terminalSize = func(*os.File) (int, int) { return 80, 24 }
	if !a.canUseTUI() {
		t.Fatal("fully interactive app did not select the TUI")
	}
	a.canAnimate = func() bool { return false }
	if a.canUseTUI() {
		t.Fatal("TERM-dumb or redirected app selected the TUI")
	}
}

func TestTUIStatusUsesSessionTitleEffortModelAndWorkingFolder(t *testing.T) {
	sess := session.New(t.TempDir(), "base/model")
	sess.Title = "continue the purple TUI"
	ag := engine.New(engine.Options{
		Model: "base/model", Mode: engine.ModeCode, Effort: engine.EffortMax,
		Sess: sess, Out: io.Discard,
		Tiers: map[string]string{engine.EffortMax: "frontier/ultra-model"},
	})

	got := tuiStatus(ag, "working", "~/kolkrabbi")
	if got.Session != sess.ID || got.SessionName != sess.Title || got.Folder != "~/kolkrabbi" ||
		got.Model != "frontier/ultra-model" || got.Effort != engine.EffortMax ||
		got.Mode != engine.ModeCode || got.Lifecycle != "working" {
		t.Fatalf("TUI status did not reflect the live agent: %#v", got)
	}
}

func TestCompactWorkingFolderUsesAStableHomeRelativeLabel(t *testing.T) {
	tests := map[string]struct {
		cwd  string
		home string
		want string
	}{
		"project": {cwd: "/Users/franco/kolkrabbi", home: "/Users/franco", want: "~/kolkrabbi"},
		"home":    {cwd: "/Users/franco", home: "/Users/franco", want: "~"},
		"outside": {cwd: "/srv/kolkrabbi", home: "/Users/franco", want: "/srv/kolkrabbi"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := compactWorkingFolder(test.cwd, test.home); got != test.want {
				t.Fatalf("compactWorkingFolder(%q, %q) = %q, want %q", test.cwd, test.home, got, test.want)
			}
		})
	}
}

func TestTUIStatusReResolvesModelWhenEffortChanges(t *testing.T) {
	sess := session.New(t.TempDir(), "base/model")
	ag := engine.New(engine.Options{
		Model: "base/model", Mode: engine.ModeCode, Effort: engine.EffortMedium,
		Sess: sess, Out: io.Discard,
		Tiers: map[string]string{
			engine.EffortHigh: "frontier/high-model",
			"quick":           "legacy/quick-model",
		},
	})

	// 1. Initial medium effort inherits base model
	status1 := tuiStatus(ag, "ready", "~/kolkrabbi")
	if status1.Effort != engine.EffortMedium || status1.Model != "base/model" {
		t.Fatalf("initial status = %#v, want effort medium and model base/model", status1)
	}

	// 2. Set effort to high via numeric alias "3"
	if err := ag.SetEffort("3"); err != nil {
		t.Fatal(err)
	}
	status2 := tuiStatus(ag, "ready", "~/kolkrabbi")
	if status2.Effort != engine.EffortHigh || status2.Model != "frontier/high-model" {
		t.Fatalf("status after /effort 3 = %#v, want effort high and model frontier/high-model", status2)
	}

	// 3. Set effort to low via legacy alias "quick"
	if err := ag.SetEffort("quick"); err != nil {
		t.Fatal(err)
	}
	status3 := tuiStatus(ag, "ready", "~/kolkrabbi")
	if status3.Effort != engine.EffortLow || status3.Model != "legacy/quick-model" {
		t.Fatalf("status after /effort quick = %#v, want effort low and model legacy/quick-model", status3)
	}
}

// The status line already says what mode, effort and model a session is on.
// What it cannot say is how to change any of them, and a first session is
// exactly where that matters: the three dials that make kolk feel different
// are invisible until someone reads the docs, which is one place too far.
func TestANewSessionIsToldHowToChangeTheThreeDials(t *testing.T) {
	got := tuiWelcome(0)
	for _, want := range []string{"/mode", "/effort", "/model", "picker"} {
		if !strings.Contains(got, want) {
			t.Errorf("a new session's welcome never mentions %s: %q", want, got)
		}
	}
	if strings.Count(got, "\n") > 3 {
		t.Errorf("the welcome grew past three lines, which is a wall, not an orientation: %q", got)
	}
}

// A resumed session has already met the dials. Repeating them every time turns
// an orientation into noise, and noise is what people learn to skip.
func TestAResumedSessionIsNotReorientated(t *testing.T) {
	got := tuiWelcome(8)
	for _, unwanted := range []string{"/mode", "/effort", "/model"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a resumed session was re-orientated with %s: %q", unwanted, got)
		}
	}
}

func TestTUIModelRowsUnifySharedSubscriptionModels(t *testing.T) {
	bin := t.TempDir()
	codex := filepath.Join(bin, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")
	a, ag, _ := replFixture(t, "")
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	a.listHostModels = func(context.Context, string, string) ([]local.HostModel, error) { return nil, nil }
	a.probeHardware = func(context.Context, string) local.Hardware { return local.Hardware{} }
	a.pulledNames = func() map[string]bool { return map[string]bool{} }

	rows := tuiModels(context.Background(), a, ag)
	want := map[string]int{
		"gpt-5.6-sol": 0, "gpt-5.6-terra": 0, "gpt-5.6-luna": 0, "gpt-5.6-pro": 0,
	}
	for _, row := range rows {
		if row.Cost != tui.CostSubscription {
			continue
		}
		if _, ok := want[row.ID]; ok {
			want[row.ID]++
			if strings.Contains(row.Name, "ChatGPT Plus") || strings.Contains(row.Name, "ChatGPT Pro") {
				t.Errorf("subscription limits leaked into model row %q: %q", row.ID, row.Name)
			}
			if strings.Contains(row.Name, row.ID) {
				t.Errorf("model row repeats canonical ID %q in its supplementary label: %q", row.ID, row.Name)
			}
		}
	}
	for model, count := range want {
		if count != 1 {
			t.Errorf("TUI rows contain %d subscription rows for %q, want exactly one: %+v", count, model, rows)
		}
	}

	entries := tuiModelPickEntries(context.Background(), a, ag)
	for model := range want {
		count := 0
		for _, entry := range entries {
			if entry.ID == model {
				count++
				if len(entry.Efforts) == 0 {
					t.Errorf("picker entry %q lost its effort dial", model)
				}
			}
		}
		if count != 1 {
			t.Errorf("picker entries contain %d rows for %q, want exactly one: %+v", count, model, entries)
		}
	}
}

func TestTUISubscriptionModelGroupsCollapseOnlySelectableSharedModels(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex", "gemini"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	a, _, _ := replFixture(t, "")
	groups := tuiSubscriptionModelGroups(a)
	counts := make(map[string]int)
	planCounts := make(map[string]int)
	for _, group := range groups {
		key := group[0].Connector + "/" + group[0].Model
		counts[key]++
		planCounts[key] = len(group)
		for _, plan := range group {
			if plan.Access != "provider CLI" {
				t.Errorf("selector included unusable subscription row: %+v", plan)
			}
		}
	}
	for _, key := range []string{
		"codex/gpt-5.6-sol", "codex/gpt-5.6-terra", "codex/gpt-5.6-luna",
	} {
		if counts[key] != 1 || planCounts[key] != 2 {
			t.Errorf("group %q: rows=%d plans=%d, want one row carrying two plans", key, counts[key], planCounts[key])
		}
	}
	if counts["gemini/gemini-2.5-pro"] != 0 {
		t.Errorf("unsupported Gemini subscription appeared in selectable groups: %+v", groups)
	}
}

func TestPreferredTUISubscriptionPlanFollowsTheUsableLogin(t *testing.T) {
	var terra []provider.PlanModel
	for _, plan := range provider.PlanModels("") {
		if plan.Provider == "openai" && plan.Model == "gpt-5.6-terra" {
			terra = append(terra, plan)
		}
	}
	if len(terra) != 2 {
		t.Fatalf("terra catalog rows = %d, want Plus and Pro", len(terra))
	}

	tests := []struct {
		name     string
		manifest provider.ConnectorManifest
		wantPlan string
		usable   bool
	}{
		{name: "signed out", wantPlan: "ChatGPT Plus"},
		{
			name: "plus login",
			manifest: provider.ConnectorManifest{Connectors: []provider.Connector{
				{Provider: "openai", Plan: "ChatGPT Plus", Name: "codex", Enabled: true},
			}},
			wantPlan: "ChatGPT Plus", usable: true,
		},
		{
			name: "pro login",
			manifest: provider.ConnectorManifest{Connectors: []provider.Connector{
				{Provider: "openai", Plan: "ChatGPT Pro", Name: "codex", Enabled: true},
			}},
			wantPlan: "ChatGPT Pro", usable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, usable := preferredTUISubscriptionPlan(terra, test.manifest)
			if got.Plan != test.wantPlan || usable != test.usable {
				t.Fatalf("preferred plan = %q, usable=%t; want %q, usable=%t", got.Plan, usable, test.wantPlan, test.usable)
			}
		})
	}
}

// A bare /config opens the searchable overlay rather than falling through to
// the plain-text dump `kolk config` prints — the literal ask this leaf exists
// to answer.
func TestBareSlashConfigOpensTheSearchableOverlay(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	screen := tui.NewRuntime(tui.RuntimeOptions{})

	done := make(chan bool, 1)
	go func() {
		done <- tuiConfigPickerCommand(context.Background(), screen, a, "/config")
	}()

	deadline := time.Now().Add(2 * time.Second)
	for screen.ConfigPicker() == nil {
		if time.Now().After(deadline) {
			t.Fatal("bare /config never opened the overlay")
		}
		time.Sleep(time.Millisecond)
	}
	screen.HandleKey(tui.Key{Kind: tui.KeyEscape})

	select {
	case shown := <-done:
		if !shown {
			t.Fatal("tuiConfigPickerCommand reported bare /config as not its own")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("closing the overlay never unblocked the caller")
	}
}

// `/config get|set|unset <key>` is already past the picker — the CLI's own
// command runs it, exactly as it does today. If the picker claimed this too,
// typing a value would fight the overlay for the same keystrokes.
func TestSlashConfigWithArgumentsIsNotThePickers(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	screen := tui.NewRuntime(tui.RuntimeOptions{})
	for _, prompt := range []string{"/config get effort", "/config set effort high", "/model"} {
		if tuiConfigPickerCommand(context.Background(), screen, a, prompt) {
			t.Fatalf("%q was claimed by the config picker", prompt)
		}
	}
}

// A status port nothing feeds looks empty forever and looks correct. This
// asserts the typed engine update reaches the runtime method that owns the
// screen lock; calling Controller directly would race concurrent subagents.
func TestAgentLifecycleIsWiredThroughTheRuntime(t *testing.T) {
	source, err := os.ReadFile("tui_repl.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "ag.Subagents = func(status engine.SubagentStatus)") ||
		!strings.Contains(string(source), "screen.SetAgentStatus(tui.AgentStatus{") {
		t.Error("the typed subagent lifecycle is not attached through the race-safe runtime boundary")
	}
}
