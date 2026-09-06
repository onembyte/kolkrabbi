package engine

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

func TestTaskLevelChoosesItsOwnEffort(t *testing.T) {
	tests := []struct {
		name          string
		level         Level
		sessionEffort string
		want          string
	}{
		{name: "trivial is cheap", level: LevelTrivial, sessionEffort: EffortHigh, want: EffortLow},
		{name: "routine is balanced", level: LevelRoutine, sessionEffort: EffortLow, want: EffortMedium},
		{name: "hard gets the full reasoning budget", level: LevelHard, sessionEffort: EffortLow, want: EffortMax},
		{name: "unstated preserves the user choice", level: LevelUnstated, sessionEffort: EffortHigh, want: EffortHigh},
		{name: "unknown is treated as unstated", level: Level("surprising"), sessionEffort: EffortLow, want: EffortLow},
		{name: "invalid session effort is safe", level: LevelUnstated, sessionEffort: "surprising", want: EffortMedium},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := effortForTask(tc.level, tc.sessionEffort); got != tc.want {
				t.Fatalf("effortForTask(%q, %q) = %q, want %q", tc.level, tc.sessionEffort, got, tc.want)
			}
		})
	}
}

func TestTrivialSubagentUsesLowRoundCeilingInsideAHighEffortSession(t *testing.T) {
	steps := make([]enginetest.Step, MaxRoundsFor(ModeCode, EffortHigh))
	for i := range steps {
		steps[i] = enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("call-%d", i),
			Function: provider.FunctionCall{
				Name:      "read_file",
				Arguments: fmt.Sprintf(`{"path":"missing-%d"}`, i),
			},
		}}}
	}
	srv := enginetest.New(steps...)
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh
	agent.MaxConcurrentTasks = 1

	outcomes, err := agent.runTasks(context.Background(), "mechanical work", []Task{{
		Title: "mechanical", Kind: KindResearch, Level: LevelTrivial, Model: "mock/model",
	}})
	if err != nil {
		t.Fatalf("runTasks: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != statusIncomplete {
		t.Fatalf("outcomes = %+v, want one round-limited task", outcomes)
	}
	if got, want := len(srv.Requests), MaxRoundsFor(ModeCode, EffortLow); got != want {
		t.Fatalf("provider calls = %d, want low-effort ceiling %d", got, want)
	}
}

func TestToolExecutionUsesTheExplicitEffortDeadline(t *testing.T) {
	agent := &Agent{Options: Options{Effort: EffortMax}}
	var deadline time.Time
	guard := func(ctx context.Context, _ io.Writer) tools.Guard {
		deadline, _ = ctx.Deadline()
		return func(tools.Request) bool { return false }
	}
	call := provider.ToolCall{Function: provider.FunctionCall{
		Name:      "bash",
		Arguments: `{"command":"true","description":"deadline probe"}`,
	}}
	started := time.Now()
	_, _ = agent.executeToolWith(context.Background(), call, io.Discard, EffortLow, guard, false, agent.Root)

	if deadline.IsZero() {
		t.Fatal("bash guard received a context without a deadline")
	}
	if got := deadline.Sub(started); got < 29*time.Second || got > 31*time.Second {
		t.Fatalf("bash deadline = %s, want the explicit low-effort timeout near 30s", got)
	}
}

func TestEachSubagentBackendReceivesTheTaskEffort(t *testing.T) {
	srv := enginetest.New()
	defer srv.Close()
	agent, _, _, recorder := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh
	agent.MaxConcurrentTasks = 1

	var mu sync.Mutex
	got := make(map[string]string)
	agent.Root = t.TempDir()
	agent.SubagentBackend = func(_ context.Context, model, mode, effort string, _ SubagentCapabilities) (ChatBackend, error) {
		if mode != ModeCode {
			t.Errorf("backend mode = %q, want %q", mode, ModeCode)
		}
		mu.Lock()
		got[model] = effort
		mu.Unlock()
		return recordingBackend{}, nil
	}

	tasks := []Task{
		{Title: "mechanical", Kind: KindResearch, Level: LevelTrivial, Model: "model/trivial"},
		{Title: "ordinary", Kind: KindResearch, Level: LevelRoutine, Model: "model/routine"},
		{Title: "subtle", Kind: KindResearch, Level: LevelHard, Model: "model/hard"},
		{Title: "unspecified", Kind: KindResearch, Level: LevelUnstated, Model: "model/unstated"},
	}
	if _, err := agent.runTasks(context.Background(), "work", tasks); err != nil {
		t.Fatalf("runTasks: %v", err)
	}

	want := map[string]string{
		"model/trivial":  EffortLow,
		"model/routine":  EffortMedium,
		"model/hard":     EffortMax,
		"model/unstated": EffortHigh,
	}
	mu.Lock()
	defer mu.Unlock()
	for model, effort := range want {
		if got[model] != effort {
			t.Errorf("backend effort for %s = %q, want %q", model, got[model], effort)
		}
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.Calls) != len(tasks) {
		t.Fatalf("recorded %d subagent calls, want %d", len(recorder.Calls), len(tasks))
	}
	for i, call := range recorder.Calls {
		if call.Role != "subagent" {
			t.Errorf("record %d role = %q, want subagent", i, call.Role)
		}
		if call.Effort != want[tasks[i].Model] {
			t.Errorf("record %d effort = %q, want %q", i, call.Effort, want[tasks[i].Model])
		}
	}
}
