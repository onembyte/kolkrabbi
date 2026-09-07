package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// With a screen attached — the TUI receives every subagent's status and
// shows it in its own window — the main transcript gets a summary, not the
// play-by-play: one line when the agents deploy, one when they finish. The
// started lines, the completed milestones and the buffered transcripts stay
// where nothing else shows them: plain output, as before.
func TestALiveSurfaceGetsASummaryNotATranscript(t *testing.T) {
	run := func(live bool) string {
		var out bytes.Buffer
		a := &Agent{Options: Options{
			Mode: ModeAgent, Out: &out, Permission: PermissionFullAuto, MaxConcurrentTasks: 2,
			Root: t.TempDir(),
			SubagentBackend: func(_ context.Context, _, _ string, _ string, _ SubagentCapabilities) (ChatBackend, error) {
				return delayedReportBackend{text: "the whole transcript"}, nil
			},
		}}
		a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
		if live {
			a.Subagents = func(SubagentStatus) {}
		}
		if _, err := a.runTasks(context.Background(), "do both", []Task{
			{Title: "scan the repo", Kind: KindResearch, Model: "provider/fast"},
			{Title: "build it", Kind: KindEdit, Model: "provider/fast"},
		}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	live := run(true)
	for _, want := range []string{"◆ 2 agents deployed (research, edit)", "◆ 2 agents finished: 2 completed"} {
		if !strings.Contains(live, want) {
			t.Errorf("live output lacks %q:\n%s", want, live)
		}
	}
	for _, noise := range []string{"subagent 1/2 started", "subagent 2/2 completed", "scan the repo:"} {
		if strings.Contains(live, noise) {
			t.Errorf("live output still carries %q:\n%s", noise, live)
		}
	}

	plain := run(false)
	for _, want := range []string{"subagent 1/2 started", "subagent 1/2 completed", "subagent 1/2 scan the repo:"} {
		if !strings.Contains(plain, want) {
			t.Errorf("plain output lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "agents deployed") {
		t.Errorf("plain output gained the live summary:\n%s", plain)
	}
}
