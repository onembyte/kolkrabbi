package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

type observedWorkBackend struct {
	events []provider.ProgressEvent
	text   string
}

type observedWorkTurn struct {
	events []provider.ProgressEvent
	text   string
}

type scriptedObservedWorkBackend struct {
	mu    sync.Mutex
	turns []observedWorkTurn
}

type localToolScriptBackend struct {
	mu    sync.Mutex
	call  provider.ToolCall
	turns int
}

type retryObservedWorkBackend struct {
	mu       sync.Mutex
	attempts int
}

type failingObservedWorkBackend struct{}

type blockingObservedWorkBackend struct {
	started chan struct{}
	once    sync.Once
}

func bReplay(t *testing.T, b *bus.Bus) []protocol.Envelope {
	t.Helper()
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	return sub.Replay()
}

func replayToolEvents(t *testing.T, b interface {
	Subscribe(uint64) (*bus.Subscription, error)
}, callID string) []protocol.Envelope {
	t.Helper()
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var found []protocol.Envelope
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished:
			var data struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.ID == callID {
				found = append(found, envelope)
			}
		}
	}
	return found
}

func (b *localToolScriptBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns++
	if b.turns == 1 {
		return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{b.call}}, provider.Meta{}, nil
	}
	return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
}

func (b *scriptedObservedWorkBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("engine used StreamChat instead of the observed provider seam")
}

func (b *scriptedObservedWorkBackend) StreamChatObserved(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	b.mu.Lock()
	if len(b.turns) == 0 {
		b.mu.Unlock()
		return provider.Message{}, provider.Meta{}, fmt.Errorf("unexpected observed provider turn")
	}
	turn := b.turns[0]
	b.turns = b.turns[1:]
	b.mu.Unlock()
	for _, event := range turn.events {
		observe(event)
	}
	return provider.Message{Role: "assistant", Content: turn.text}, provider.Meta{Model: model}, nil
}

func (b observedWorkBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("engine used StreamChat instead of the observed provider seam")
}

func (b observedWorkBackend) StreamChatObserved(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	for _, event := range b.events {
		observe(event)
	}
	return provider.Message{Role: "assistant", Content: b.text}, provider.Meta{Model: model}, nil
}

func (b *retryObservedWorkBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("engine used StreamChat instead of the observed provider seam")
}

func (b *retryObservedWorkBackend) StreamChatObserved(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	b.mu.Lock()
	b.attempts++
	attempt := b.attempts
	b.mu.Unlock()
	// Two deltas model the normal streaming shape. Only the first is a
	// work-step, but the retry below must open a new physical observation.
	observe(provider.ProgressEvent{Kind: provider.ProgressMessage, Detail: "thinking"})
	observe(provider.ProgressEvent{Kind: provider.ProgressMessage, Detail: "still thinking"})
	if attempt == 1 {
		return provider.Message{}, provider.Meta{Model: model}, &provider.HTTPError{StatusCode: 429, Message: "try again"}
	}
	return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{Model: model}, nil
}

func (failingObservedWorkBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("engine used StreamChat instead of the observed provider seam")
}

func (failingObservedWorkBackend) StreamChatObserved(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	observe(provider.ProgressEvent{Kind: provider.ProgressError, Detail: "provider exploded", Error: true})
	return provider.Message{}, provider.Meta{Model: model}, fmt.Errorf("provider exploded")
}

func (b *blockingObservedWorkBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("engine used StreamChat instead of the observed provider seam")
}

func (b *blockingObservedWorkBackend) StreamChatObserved(ctx context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	observe(provider.ProgressEvent{Kind: provider.ProgressMessage, Detail: "thinking"})
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return provider.Message{}, provider.Meta{Model: model}, ctx.Err()
}

func TestObservedProviderBoundariesAdvanceOnlyTheirOwningSubagent(t *testing.T) {
	first := observedWorkBackend{events: []provider.ProgressEvent{
		{Kind: provider.ProgressMessage, Detail: "first is thinking"},
		{Kind: provider.ProgressToolStarted, ID: "provider_1", Name: "Read first", Detail: "first.txt"},
		{Kind: provider.ProgressToolFinished, ID: "provider_1", Name: "Read first", Detail: "done"},
	}, text: "first done"}
	second := observedWorkBackend{events: []provider.ProgressEvent{
		{Kind: provider.ProgressMessage, Detail: "second is thinking"},
		{Kind: provider.ProgressToolStarted, ID: "provider_2", Name: "Read second", Detail: "second.txt"},
		{Kind: provider.ProgressToolFinished, ID: "provider_2", Name: "Read second", Detail: "done"},
	}, text: "second done"}
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Permission: PermissionFullAuto,
		SubagentBackend: func(_ context.Context, model, _ string, _ string) (ChatBackend, error) {
			switch model {
			case "provider/first":
				return first, nil
			case "provider/second":
				return second, nil
			default:
				return nil, fmt.Errorf("unexpected model %q", model)
			}
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.MaxConcurrentTasks = 2
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "inspect both", []Task{
		{Title: "first", Kind: KindResearch, Model: "provider/first"},
		{Title: "second", Kind: KindResearch, Model: "provider/second"},
	}); err != nil {
		t.Fatal(err)
	}

	firstSteps := seen.task(1)
	secondSteps := seen.task(2)
	assertStatusStep(t, firstSteps, SubagentWorking, "model is responding")
	assertStatusStep(t, firstSteps, SubagentWorking, "provider tool Read first started")
	assertStatusStep(t, firstSteps, SubagentWorking, "provider tool Read first finished")
	assertStatusStep(t, secondSteps, SubagentWorking, "model is responding")
	assertStatusStep(t, secondSteps, SubagentWorking, "provider tool Read second started")
	assertStatusStep(t, secondSteps, SubagentWorking, "provider tool Read second finished")
	for _, status := range firstSteps {
		if strings.Contains(status.Step, "second") {
			t.Fatalf("first task received the second task's provider step: %+v", firstSteps)
		}
	}
	for _, status := range secondSteps {
		if strings.Contains(status.Step, "first") {
			t.Fatalf("second task received the first task's provider step: %+v", secondSteps)
		}
	}
}

func TestObservedProviderBoundariesAdvanceTheMainLedger(t *testing.T) {
	b := newTestBus(t)
	backend := observedWorkBackend{events: []provider.ProgressEvent{
		{Kind: provider.ProgressMessage, Detail: "thinking"},
		{Kind: provider.ProgressMessage, Detail: "still thinking"},
		{Kind: provider.ProgressToolStarted, ID: "provider_1", Name: "Read", Detail: "README.md"},
		{Kind: provider.ProgressToolFinished, ID: "provider_1", Name: "Read", Detail: "done"},
	}, text: "done"}
	a := &Agent{Options: Options{
		Backend: backend, Mode: ModeAgent, Model: "provider/main", Effort: EffortHigh, Out: io.Discard, Bus: b,
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.resetMainWork()
	if _, _, err := a.streamChatObserved(context.Background(), activityPlanning, "provider/main", nil, nil, nil,
		a.mainProviderProgress("provider/main", EffortHigh)); err != nil {
		t.Fatal(err)
	}

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var work []protocol.WorkUpdatedData
	for _, envelope := range sub.Replay() {
		if envelope.Type != protocol.EventWorkUpdated {
			continue
		}
		var data protocol.WorkUpdatedData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatal(err)
		}
		work = append(work, data)
	}
	if len(work) != 3 {
		t.Fatalf("main work = %+v, want response plus provider tool start/finish", work)
	}
	for index, data := range work {
		if data.Role != protocol.WorkRoleMain || data.ID != a.lastTurnID || data.ChildTurn != "" ||
			data.Index != 0 || data.Total != 0 || data.Phase != protocol.WorkPhaseProvider ||
			data.Sequence != uint64(index+1) {
			t.Fatalf("main work[%d] = %+v", index, data)
		}
	}
	if work[0].Step != "model is responding" || work[1].Step != "provider tool Read started" ||
		work[2].Step != "provider tool Read finished" {
		t.Fatalf("main steps = %+v", work)
	}
}

func TestObservedProviderRetryGetsOneWorkStepPerPhysicalAttempt(t *testing.T) {
	b := newTestBus(t)
	backend := &retryObservedWorkBackend{}
	a := &Agent{Options: Options{
		Backend: backend, Bus: b, Mode: ModeAgent, Model: "provider/retry", Effort: EffortHigh, Out: io.Discard,
		RetryWait: func(context.Context, time.Duration) error { return nil },
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.resetMainWork()
	if _, _, err := a.streamChatObserved(context.Background(), activityPlanning, "provider/retry", nil, nil, nil,
		a.mainProviderProgress("provider/retry", EffortHigh)); err != nil {
		t.Fatal(err)
	}
	work := mainWorkEvents(t, b)
	if len(work) != 2 {
		t.Fatalf("retry work updates = %+v, want one per physical attempt", work)
	}
	for index, event := range work {
		if event.Phase != protocol.WorkPhaseProvider || event.Step != "model is responding" ||
			event.Sequence != uint64(index+1) {
			t.Fatalf("retry work[%d] = %+v", index, event)
		}
	}
}

func TestFailedSubagentEmitsOneTerminalWorkAndFinishedEvent(t *testing.T) {
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Permission: PermissionFullAuto,
		SubagentBackend: func(context.Context, string, string, string) (ChatBackend, error) {
			return failingObservedWorkBackend{}, nil
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "fail once", []Task{{
		Title: "fail", Kind: KindResearch, Model: "provider/failing",
	}}); err != nil {
		t.Fatal(err)
	}
	statuses := seen.task(1)
	var terminal []SubagentStatus
	for _, status := range statuses {
		if status.State == SubagentFailed && status.Phase == SubagentPhaseComplete {
			terminal = append(terminal, status)
		}
	}
	if len(terminal) != 1 || !strings.Contains(terminal[0].Step, "provider exploded") {
		t.Fatalf("terminal child statuses = %+v", terminal)
	}

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	terminalWork, finished := 0, 0
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventWorkUpdated:
			var data protocol.WorkUpdatedData
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.ID == terminal[0].ID && data.Role == protocol.WorkRoleSubagent &&
				data.State == protocol.WorkStateFailed && data.Phase == protocol.WorkPhaseComplete {
				terminalWork++
			}
		case protocol.EventSubagentFinished:
			var data protocol.SubagentFinishedData
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.ID == terminal[0].ID {
				if data.OK {
					t.Fatalf("failed child finished as ok: %+v", data)
				}
				finished++
			}
		}
	}
	if terminalWork != 1 || finished != 1 {
		t.Fatalf("failed child terminal work/finished = %d/%d, want 1/1", terminalWork, finished)
	}
}

func TestCancelledAgentTurnEmitsOneTerminalWorkAndCancelledEvent(t *testing.T) {
	backend := &blockingObservedWorkBackend{started: make(chan struct{})}
	b := newTestBus(t)
	a := New(Options{
		Backend: backend, Bus: b, Mode: ModeAgent, Model: "provider/cancel", Effort: EffortMedium,
		Out: io.Discard, Sess: enginetest.NewFakeSession("s_test", "provider/cancel"),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.RunTurn(ctx, "cancel while planning") }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("planner did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled RunTurn = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled RunTurn did not return")
	}

	terminalWork, cancelled, finished := 0, 0, 0
	for _, envelope := range bReplay(t, b) {
		switch envelope.Type {
		case protocol.EventWorkUpdated:
			var data protocol.WorkUpdatedData
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Role == protocol.WorkRoleMain && data.Phase == protocol.WorkPhaseComplete {
				if data.State != protocol.WorkStateFailed || data.Step != "cancelled" {
					t.Fatalf("cancelled terminal work = %+v", data)
				}
				terminalWork++
			}
		case protocol.EventTurnCancelled:
			cancelled++
		case protocol.EventTurnFinished:
			finished++
		}
	}
	if terminalWork != 1 || cancelled != 1 || finished != 0 {
		t.Fatalf("cancelled terminal work/cancelled/finished = %d/%d/%d, want 1/1/0", terminalWork, cancelled, finished)
	}
}

func TestProviderProgressStepScrubsControlsAndBoundsHostileDetail(t *testing.T) {
	key := "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	step := providerProgressStep(provider.ProgressEvent{
		Kind:   provider.ProgressToolFinished,
		Name:   "\x1b[31m" + key + strings.Repeat(" name", 80),
		Detail: key + "\x1b]8;;https://example.invalid\a" + strings.Repeat(" detail", 80),
		Error:  true,
	})
	if strings.Contains(step, key) || strings.Contains(step, "\x1b") {
		t.Fatalf("hostile provider detail reached a work step: %q", step)
	}
	if len([]rune(step)) > maxSubagentStepRunes {
		t.Fatalf("provider work step is %d runes, over %d: %q", len([]rune(step)), maxSubagentStepRunes, step)
	}
	if !strings.Contains(step, "redacted") || !strings.Contains(step, "provider tool") {
		t.Fatalf("provider work step lost its safe diagnostic: %q", step)
	}
}

func TestKolkToolRequestedDataScrubsSecretArgumentsBeforePublication(t *testing.T) {
	key := "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	data := kolkToolRequestedData(provider.ToolCall{
		ID: "call_secret", Function: provider.FunctionCall{
			Name: "bash", Arguments: `{"command":"printf '` + key + `'"}`,
		},
	}, toolWorkOwner{taskID: "k_01ARYZ6S41TSV4RRFFQ69G5FAW", childTurn: "t_01ARYZ6S41TSV4RRFFQ69G5FAV"})
	if strings.Contains(data.Arguments, key) || !strings.Contains(data.Arguments, "redacted") {
		t.Fatalf("secret survived tool-request construction: %+v", data)
	}
}

func TestSubagentToolWorkScrubsSecretArgumentsOutputAndStatus(t *testing.T) {
	key := "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	description := "inspect " + key + "\x1b[31m" + strings.Repeat(" detail", 40)
	args, err := json.Marshal(map[string]string{
		"command":     "printf '%s' '" + key + "'",
		"description": description,
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := &localToolScriptBackend{call: provider.ToolCall{
		ID: "call_secret", Function: provider.FunctionCall{Name: "bash", Arguments: string(args)},
	}}
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Root: t.TempDir(), Permission: PermissionFullAuto,
		SubagentBackend: func(context.Context, string, string, string) (ChatBackend, error) { return backend, nil },
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "inspect secret", []Task{{
		Title: "inspect safely", Kind: KindResearch, Model: "provider/secret",
	}}); err != nil {
		t.Fatal(err)
	}
	for _, envelope := range replayToolEvents(t, b, "call_secret") {
		if strings.Contains(string(envelope.Data), key) {
			t.Fatalf("secret reached durable %s event: %s", envelope.Type, envelope.Data)
		}
	}
	for _, status := range seen.task(1) {
		if status.Phase != SubagentPhaseTool {
			continue
		}
		if strings.Contains(status.Step, key) || strings.Contains(status.Step, "\x1b") ||
			len([]rune(status.Step)) > maxSubagentStepRunes {
			t.Fatalf("unsafe tool work status: %+v", status)
		}
	}
}

func TestSubagentToolOutputKeepsItsExistingBoundOutsideTheStatusRow(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	body := "OPENROUTER_API_KEY=sk-or-v1-0123456789abcdef0123456789abcdef\n" + strings.Repeat("x", 13_000)
	if err := os.WriteFile(filepath.Join(root, "secrets.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &localToolScriptBackend{call: provider.ToolCall{
		ID: "call_long", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"secrets.txt"}`},
	}}
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Root: root, Permission: PermissionFullAuto,
		SubagentBackend: func(context.Context, string, string, string) (ChatBackend, error) { return backend, nil },
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "read secret", []Task{{
		Title: "read safely", Kind: KindResearch, Model: "provider/read",
	}}); err != nil {
		t.Fatal(err)
	}
	var output protocol.ToolOutputData
	for _, envelope := range replayToolEvents(t, b, "call_long") {
		if envelope.Type == protocol.EventToolOutput {
			if err := json.Unmarshal(envelope.Data, &output); err != nil {
				t.Fatal(err)
			}
		}
	}
	// tools.maxOutput is 12,000 bytes; its line-paging explanation and a
	// redaction sentinel add a small, deliberate tail to the durable frame.
	if len(output.Output) == 0 || len(output.Output) > 12_200 || !strings.Contains(output.Output, "truncated") {
		t.Fatalf("tool output lost its established bound: %d bytes: %.200q", len(output.Output), output.Output)
	}
	if strings.Contains(output.Output, "sk-or-v1-0123456789") || !strings.Contains(output.Output, "redacted") {
		t.Fatalf("tool output was not scrubbed: %.200q", output.Output)
	}
	for _, status := range seen.task(1) {
		if status.Phase == SubagentPhaseTool && (strings.Contains(status.Step, strings.Repeat("x", 40)) ||
			strings.Contains(status.Step, "sk-or-v1-0123456789")) {
			t.Fatalf("tool output leaked into live status: %+v", status)
		}
	}
}

func TestOrchestratedPlannerAndSynthesisUseTheObservedProviderSeam(t *testing.T) {
	srv := enginetest.New()
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	main := &scriptedObservedWorkBackend{turns: []observedWorkTurn{
		{events: []provider.ProgressEvent{{Kind: provider.ProgressMessage, Detail: "planning"}},
			text: `[{"title":"read one","kind":"research"},{"title":"read two","kind":"research"}]`},
		{events: []provider.ProgressEvent{
			{Kind: provider.ProgressMessage, Detail: "synthesizing"},
			{Kind: provider.ProgressToolStarted, ID: "provider_synth", Name: "Read", Detail: "report"},
			{Kind: provider.ProgressToolFinished, ID: "provider_synth", Name: "Read", Detail: "done"},
		}, text: "all done"},
	}}
	a.Backend = main
	a.SubagentBackend = func(context.Context, string, string, string) (ChatBackend, error) {
		return observedWorkBackend{text: "task done"}, nil
	}
	b := newTestBus(t)
	a.Bus = b

	if err := a.RunTurn(context.Background(), "inspect both"); err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, event := range mainWorkEvents(t, b) {
		if event.Phase == protocol.WorkPhaseProvider {
			steps = append(steps, event.Step)
		}
	}
	for _, want := range []string{
		"model is responding", "provider tool Read started", "provider tool Read finished",
	} {
		if !slices.Contains(steps, want) {
			t.Fatalf("provider work steps = %v, want %q", steps, want)
		}
	}
}

func TestSubagentLocalToolLifecycleIsCorrelatedOrderedAndVisible(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: "call_list",
			Function: provider.FunctionCall{
				Name: "list_dir", Arguments: `{}`,
			},
		}}},
		enginetest.Step{Text: "listed"},
	)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Root = t.TempDir()
	a.MaxConcurrentTasks = 1
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "list it", []Task{{
		Title: "list the project", Kind: KindResearch, Model: "mock/model",
	}}); err != nil {
		t.Fatal(err)
	}
	statuses := seen.task(1)
	if len(statuses) == 0 {
		t.Fatal("subagent emitted no statuses")
	}
	child := statuses[0].ChildTurn
	taskID := statuses[0].ID
	assertStatusStep(t, statuses, SubagentWorking, "Listing directory")
	assertStatusStep(t, statuses, SubagentWorking, "finished list_dir")

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var types []protocol.EventType
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished:
			var correlation struct {
				ID        string `json:"id"`
				TaskID    string `json:"task_id"`
				ChildTurn string `json:"child_turn"`
			}
			if err := json.Unmarshal(envelope.Data, &correlation); err != nil {
				t.Fatal(err)
			}
			if correlation.ID != "call_list" || correlation.TaskID != taskID || correlation.ChildTurn != child {
				t.Fatalf("tool event %q correlation = %+v, want call/task/child %q/%q/%q",
					envelope.Type, correlation, "call_list", taskID, child)
			}
			types = append(types, envelope.Type)
		}
	}
	want := []protocol.EventType{
		protocol.EventToolRequested, protocol.EventToolStarted,
		protocol.EventToolOutput, protocol.EventToolFinished,
	}
	if !slices.Equal(types, want) {
		t.Fatalf("tool lifecycle = %v, want %v", types, want)
	}
}

func TestDirectAgentToolLifecycleIsMainOwnedAndVisible(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"list the project","kind":"research"}]`},
		enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: "call_direct",
			Function: provider.FunctionCall{
				Name: "list_dir", Arguments: `{}`,
			},
		}}},
		enginetest.Step{Text: "listed"},
	)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Root = t.TempDir()
	b := newTestBus(t)
	a.Bus = b

	if err := a.RunTurn(context.Background(), "list it"); err != nil {
		t.Fatal(err)
	}
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var types []protocol.EventType
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished:
			var correlation struct {
				ID        string `json:"id"`
				TaskID    string `json:"task_id"`
				ChildTurn string `json:"child_turn"`
			}
			if err := json.Unmarshal(envelope.Data, &correlation); err != nil {
				t.Fatal(err)
			}
			if correlation.ID != "call_direct" || correlation.TaskID != "" || correlation.ChildTurn != "" {
				t.Fatalf("main tool event %q correlation = %+v", envelope.Type, correlation)
			}
			types = append(types, envelope.Type)
		}
	}
	want := []protocol.EventType{
		protocol.EventToolRequested, protocol.EventToolStarted,
		protocol.EventToolOutput, protocol.EventToolFinished,
	}
	if !slices.Equal(types, want) {
		t.Fatalf("tool lifecycle = %v, want %v", types, want)
	}
	var steps []string
	for _, event := range mainWorkEvents(t, b) {
		if event.Phase == protocol.WorkPhaseTool {
			steps = append(steps, event.Step)
		}
	}
	if !slices.ContainsFunc(steps, func(step string) bool { return strings.Contains(step, "Listing directory") }) ||
		!slices.Contains(steps, "finished list_dir") {
		t.Fatalf("main tool work steps = %v", steps)
	}
}

func TestSubagentDoomLoopSkipDoesNotPretendTheToolRan(t *testing.T) {
	call := func(id string) enginetest.Step {
		return enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: id, Function: provider.FunctionCall{Name: "list_dir", Arguments: `{}`},
		}}}
	}
	srv := enginetest.New(call("call_1"), call("call_2"), call("call_3"), enginetest.Step{Text: "stopped repeating"})
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Root = t.TempDir()
	a.MaxConcurrentTasks = 1
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "list it", []Task{{
		Title: "list the project", Kind: KindResearch, Model: "mock/model",
	}}); err != nil {
		t.Fatal(err)
	}
	assertStatusStep(t, seen.task(1), SubagentWorking, "skipped list_dir: repeated call")

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	byID := map[string][]protocol.EventType{}
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished:
			var data struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			byID[data.ID] = append(byID[data.ID], envelope.Type)
		}
	}
	full := []protocol.EventType{protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished}
	if !slices.Equal(byID["call_1"], full) || !slices.Equal(byID["call_2"], full) {
		t.Fatalf("executed tool lifecycles = %+v, want first two %v", byID, full)
	}
	if want := []protocol.EventType{protocol.EventToolRequested, protocol.EventToolOutput}; !slices.Equal(byID["call_3"], want) {
		t.Fatalf("skipped tool lifecycle = %v, want %v", byID["call_3"], want)
	}
}

func TestSubagentToolErrorFinishesFalseAndNamesTheFailure(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{ToolCalls: []provider.ToolCall{{
			ID: "call_missing", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"missing.txt"}`},
		}}},
		enginetest.Step{Text: "could not read it"},
	)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.Root = t.TempDir()
	a.MaxConcurrentTasks = 1
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "read it", []Task{{
		Title: "read the missing file", Kind: KindResearch, Model: "mock/model",
	}}); err != nil {
		t.Fatal(err)
	}
	assertStatusStep(t, seen.task(1), SubagentWorking, "failed read_file")

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var finished *protocol.ToolFinishedData
	for _, envelope := range sub.Replay() {
		if envelope.Type != protocol.EventToolFinished {
			continue
		}
		var data protocol.ToolFinishedData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.ID == "call_missing" {
			finished = &data
		}
	}
	if finished == nil || finished.OK {
		t.Fatalf("missing-tool terminal event = %+v, want ok:false", finished)
	}
}

func TestConcurrentSubagentToolLifecyclesKeepTheirExactOwner(t *testing.T) {
	first := &localToolScriptBackend{call: provider.ToolCall{
		// Provider-local call IDs need not be unique across concurrent child
		// processes. The task and child coordinates, not the call ID alone,
		// are what make each durable lifecycle attributable.
		ID: "call_shared", Function: provider.FunctionCall{Name: "list_dir", Arguments: `{}`},
	}}
	second := &localToolScriptBackend{call: provider.ToolCall{
		ID: "call_shared", Function: provider.FunctionCall{Name: "list_dir", Arguments: `{}`},
	}}
	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Root: t.TempDir(), Permission: PermissionFullAuto,
		SubagentBackend: func(_ context.Context, model, _ string, _ string) (ChatBackend, error) {
			switch model {
			case "provider/first":
				return first, nil
			case "provider/second":
				return second, nil
			default:
				return nil, fmt.Errorf("unexpected model %q", model)
			}
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.MaxConcurrentTasks = 2
	b := newTestBus(t)
	a.Bus = b
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "list both", []Task{
		{Title: "first", Kind: KindResearch, Model: "provider/first"},
		{Title: "second", Kind: KindResearch, Model: "provider/second"},
	}); err != nil {
		t.Fatal(err)
	}
	wantOwner := map[string]struct{}{
		seen.task(1)[0].ID + "/" + seen.task(1)[0].ChildTurn: {},
		seen.task(2)[0].ID + "/" + seen.task(2)[0].ChildTurn: {},
	}
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	byOwner := map[string][]protocol.EventType{}
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished:
			var data struct {
				ID        string `json:"id"`
				TaskID    string `json:"task_id"`
				ChildTurn string `json:"child_turn"`
			}
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.ID == "call_shared" {
				owner := data.TaskID + "/" + data.ChildTurn
				if _, ok := wantOwner[owner]; !ok {
					t.Fatalf("tool event %s has unknown or incomplete owner %q", envelope.Type, owner)
				}
				byOwner[owner] = append(byOwner[owner], envelope.Type)
			}
		}
	}
	full := []protocol.EventType{protocol.EventToolRequested, protocol.EventToolStarted, protocol.EventToolOutput, protocol.EventToolFinished}
	for owner, got := range byOwner {
		if !slices.Equal(got, full) {
			t.Fatalf("tool owner %s lifecycle = %v, want %v", owner, got, full)
		}
	}
	if len(byOwner) != len(wantOwner) {
		t.Fatalf("local tool lifecycles = %+v, want both children", byOwner)
	}
	for index := 1; index <= 2; index++ {
		var toolSteps []SubagentStatus
		for _, status := range seen.task(index) {
			if status.Phase == SubagentPhaseTool {
				toolSteps = append(toolSteps, status)
			}
		}
		if len(toolSteps) != 2 || toolSteps[0].Step != "running Listing directory — ." ||
			toolSteps[1].Step != "finished list_dir" {
			t.Fatalf("task %d tool ledger = %+v, want exactly start and finish", index, toolSteps)
		}
		if toolSteps[0].ID != toolSteps[1].ID || toolSteps[0].ChildTurn != toolSteps[1].ChildTurn ||
			toolSteps[1].Sequence != toolSteps[0].Sequence+1 {
			t.Fatalf("task %d tool ledger lost identity or order: %+v", index, toolSteps)
		}
	}
}
