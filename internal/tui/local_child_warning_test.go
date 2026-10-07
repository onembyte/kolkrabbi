package tui

import (
	"sync/atomic"
	"testing"
	"time"
)

func awaitLocalWarning(t *testing.T, r *Runtime, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if got := r.Snapshot().Status.LocalWarning; got == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("local warning = %q, want %q", r.Snapshot().Status.LocalWarning, want)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestLocalChildWarningSurvivesRemoteParentStatusUntilCPUIsChosen(t *testing.T) {
	r := NewRuntime(RuntimeOptions{
		Status: Status{Model: "remote/model", Mode: "agent"},
		WarningForModel: func(model string) string {
			if model == "ollama/qwen2.5-coder:7b" {
				return "warning: local child would use CPU"
			}
			return ""
		},
	})
	r.SetAgentStatus(AgentStatus{ID: "child", Model: "ollama/qwen2.5-coder:7b", State: "working", Sequence: 1})
	awaitLocalWarning(t, r, "warning: local child would use CPU")
	r.SetStatus(Status{Model: "remote/model", Mode: "agent", Lifecycle: "ready"})
	if got := r.Snapshot().Status.LocalWarning; got == "" {
		t.Fatal("remote parent refresh dismissed the child CPU warning")
	}
	r.SetStatus(Status{Model: "remote/model", LocalWarningAcknowledged: true})
	if got := r.Snapshot().Status.LocalWarning; got != "" {
		t.Fatalf("explicit CPU selection did not dismiss child warning: %q", got)
	}
}

func TestStaleLocalChildCannotRaiseAWarning(t *testing.T) {
	var probes atomic.Int32
	started := make(chan struct{}, 1)
	r := NewRuntime(RuntimeOptions{Status: Status{Model: "remote/model"}, WarningForModel: func(string) string {
		probes.Add(1)
		started <- struct{}{}
		return "warning: CPU"
	}})
	r.SetAgentStatus(AgentStatus{ID: "child", Model: "remote/model", State: "done", Sequence: 2})
	r.SetAgentStatus(AgentStatus{ID: "child", Model: "ollama/qwen2.5-coder:7b", State: "working", Sequence: 1})
	select {
	case <-started:
		t.Fatal("stale child started a placement probe")
	case <-time.After(100 * time.Millisecond):
	}
	if probes.Load() != 0 || r.Snapshot().Status.LocalWarning != "" {
		t.Fatalf("stale child raised a warning: probes=%d, status=%+v", probes.Load(), r.Snapshot().Status)
	}
}

func TestRepeatedLocalChildStepsProbePlacementOncePerRun(t *testing.T) {
	var probes atomic.Int32
	r := NewRuntime(RuntimeOptions{Status: Status{Model: "remote/model"}, WarningForModel: func(string) string {
		probes.Add(1)
		return "warning: CPU"
	}})
	for sequence := uint64(1); sequence <= 3; sequence++ {
		r.SetAgentStatus(AgentStatus{ID: "child", Model: "ollama/qwen2.5-coder:7b", State: "working", Sequence: sequence})
	}
	awaitLocalWarning(t, r, "warning: CPU")
	if probes.Load() != 1 {
		t.Fatalf("one child re-probed placement %d times", probes.Load())
	}
}

func TestSlowPlacementProbeDoesNotBlockChildStatus(t *testing.T) {
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r := NewRuntime(RuntimeOptions{Status: Status{Model: "remote/model"}, WarningForModel: func(string) string {
		close(started)
		<-release
		return "warning: CPU"
	}})
	go func() {
		r.SetAgentStatus(AgentStatus{ID: "child", Model: "ollama/qwen2.5-coder:7b", State: "working", Sequence: 1})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(300 * time.Millisecond):
		close(release)
		t.Fatal("hardware probe blocked the child's status callback")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("child placement probe never started")
	}
	close(release)
	awaitLocalWarning(t, r, "warning: CPU")
}
