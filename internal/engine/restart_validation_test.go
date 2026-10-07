package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Refusals must happen before provider work and preserve the real durable
// journal. Main-task state was previously checked only for child tasks.
func TestRestartRefusesInvalidExecutionBeforeProviderWork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*continuity.Run)
	}{
		{"main state", func(r *continuity.Run) { r.Main.State = "unknown" }},
		{"main state case", func(r *continuity.Run) { r.Main.State = "SETTLED" }},
		{"main rounds", func(r *continuity.Run) { r.Main.Rounds = -1 }},
		{"format", func(r *continuity.Run) { r.Version = 99 }},
		{"phase", func(r *continuity.Run) { r.Phase = "unknown" }},
		{"mode", func(r *continuity.Run) { r.Mode = "unknown" }},
		{"child state", func(r *continuity.Run) { r.Tasks[0].State = "unknown" }},
		{"child rounds", func(r *continuity.Run) { r.Tasks[0].Rounds = -1 }},
		{"child title", func(r *continuity.Run) { r.Tasks[0].Title = "" }},
		{"child model", func(r *continuity.Run) { r.Tasks[0].Model = "" }},
		{"settled without outcome", func(r *continuity.Run) { r.Tasks[0].State = continuity.TaskSettled }},
		{"queued with outcome", func(r *continuity.Run) { r.Tasks[0].Status = "done" }},
		{"unknown outcome", func(r *continuity.Run) { r.Tasks[0].State = continuity.TaskSettled; r.Tasks[0].Status = "unknown" }},
		{"negative dependency", func(r *continuity.Run) { r.Tasks[0].Needs = []int{-1} }},
		{"self dependency", func(r *continuity.Run) { r.Tasks[0].Needs = []int{0} }},
		{"future dependency", func(r *continuity.Run) { r.Tasks[0].Needs = []int{1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			run := nativeRun(t, root, "direct")
			run.Tasks = []continuity.Task{{Title: "saved task", Model: "mock/model", State: continuity.TaskQueued}}
			tc.change(run)
			dir, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
			before, err := json.Marshal(sess.RunState())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			ag := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
				calls++
				return provider.Message{Role: "assistant", Content: "WRONG: work ran"}, nil
			})))
			defer ag.Close()
			pending, ok := ag.Resume()
			if !ok {
				t.Fatal("missing recovery claim")
			}
			err = ag.RunTurn(context.Background(), pending)
			if err == nil || calls != 0 {
				t.Fatalf("invalid saved execution admitted work: err=%v calls=%d", err, calls)
			}
			loaded, err := session.Load(dir, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(loaded.RunState())
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("refusal changed saved work:\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}
