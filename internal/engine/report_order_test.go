package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type delayedReportBackend struct {
	delay time.Duration
	text  string
}

func (b delayedReportBackend) StreamChat(ctx context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	select {
	case <-time.After(b.delay):
		return provider.Message{Role: "assistant", Content: b.text}, provider.Meta{Model: model}, nil
	case <-ctx.Done():
		return provider.Message{}, provider.Meta{Model: model}, ctx.Err()
	}
}

func TestConcurrentTaskMilestonesStayChronologicalWhileReportsFlushInPlanOrder(t *testing.T) {
	var out bytes.Buffer
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: &out, Permission: PermissionFullAuto, MaxConcurrentTasks: 2,
		Root: t.TempDir(),
		SubagentBackend: func(_ context.Context, model, _ string, _ string, _ SubagentCapabilities) (ChatBackend, error) {
			switch model {
			case "provider/slow":
				return delayedReportBackend{delay: 50 * time.Millisecond, text: "slow report"}, nil
			case "provider/fast":
				return delayedReportBackend{text: "fast report"}, nil
			default:
				return nil, nil
			}
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	if _, err := a.runTasks(context.Background(), "compare", []Task{
		{Title: "slow", Kind: KindResearch, Model: "provider/slow"},
		{Title: "fast", Kind: KindResearch, Model: "provider/fast"},
	}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	fastMilestone := strings.Index(got, "subagent 2/2 completed: fast")
	slowMilestone := strings.Index(got, "subagent 1/2 completed: slow")
	if fastMilestone < 0 || slowMilestone < 0 || fastMilestone > slowMilestone {
		t.Fatalf("completion milestones lost chronological order:\n%s", got)
	}
	slowReport := strings.Index(got, "subagent 1/2 slow:")
	fastReport := strings.Index(got, "subagent 2/2 fast:")
	if slowReport < 0 || fastReport < 0 || slowReport > fastReport {
		t.Fatalf("buffered reports did not flush in plan order:\n%s", got)
	}
}
