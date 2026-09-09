package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// scriptedBackend answers with a prepared message per call and repeats the last
// one forever, so a test can say "fifty tool rounds, then an answer" without
// counting the calls the engine makes around the loop (the title call, mainly).
type scriptedBackend struct {
	steps []provider.Message
	calls int
}

func (b *scriptedBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	msg := b.steps[len(b.steps)-1]
	if b.calls < len(b.steps) {
		msg = b.steps[b.calls]
	}
	b.calls++
	if onToken != nil && msg.Content != "" {
		onToken(msg.Content)
	}
	return msg, provider.Meta{Model: "mock/model"}, nil
}

// readingRounds scripts n tool rounds that each read a different file, then a
// plain answer. Different arguments each round on purpose: identical calls are
// what the doom-loop guard is for, and it would end the turn early.
func readingRounds(t *testing.T, n int) (*scriptedBackend, string) {
	t.Helper()
	dir := resolvedTempDir(t)
	steps := make([]provider.Message, 0, n+1)
	for i := range n {
		name := filepath.Join(dir, fmt.Sprintf("note-%02d.txt", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("line %d\n", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, provider.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Reading note %d.", i),
			ToolCalls: []provider.ToolCall{{
				ID: fmt.Sprintf("call_%02d", i),
				Function: provider.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"` + jsonEsc(name) + `"}`,
				},
			}},
		})
	}
	steps = append(steps, provider.Message{Role: "assistant", Content: "All read."})
	return &scriptedBackend{steps: steps}, dir
}

func coalescingAgent(t *testing.T, backend engine.ChatBackend, root string, opts func(*engine.Options)) (*engine.Agent, *enginetest.FakeSession, *strings.Builder) {
	t.Helper()
	sess := enginetest.NewFakeSession("s_o3", "mock/model")
	var out strings.Builder
	options := engine.Options{
		Backend: backend, Model: "mock/model", Mode: engine.ModeCode,
		Effort: engine.EffortMax, Permission: engine.PermissionFullAuto,
		Sess: sess, Root: root, Out: &out,
		// Frozen: the save interval must never elapse during the loop, so what
		// the count measures is the coalescing rule and not how fast the
		// machine running the test happens to be.
		Clock: enginetest.FakeClock(time.Unix(1_760_000_000, 0), 0),
	}
	if opts != nil {
		opts(&options)
	}
	return engine.New(options), sess, &out
}

// The headline of OPTIMIZATION_PLAN.md O3: a fifty-round turn used to rewrite
// the whole session about a hundred times — once per model round and once per
// tool round — each with a file fsync and a directory fsync. The target is ten
// writes or fewer for the same turn.
func TestAFiftyRoundTurnWritesTheSessionAtMostTenTimes(t *testing.T) {
	backend, root := readingRounds(t, 50)
	agent, sess, _ := coalescingAgent(t, backend, root, nil)

	if err := agent.RunTurn(context.Background(), "read all the notes"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if backend.calls < 51 {
		t.Fatalf("the model was called %d times, so the turn did not run fifty rounds", backend.calls)
	}
	durable, interim := sess.SaveCounts()
	t.Logf("50-round turn: %d durable + %d interval = %d writes (was ~101 before O3)",
		durable, interim, durable+interim)
	if total := durable + interim; total > 10 {
		t.Errorf("a 50-round turn wrote the session %d times (%d durable, %d interval), want at most 10",
			total, durable, interim)
	}
	// Coalescing that never writes is not coalescing, it is data loss: the
	// turn's own boundary must have landed.
	if durable == 0 {
		t.Error("no durable save in a completed turn; the turn-end boundary did not flush")
	}
}

// The last message of a turn must be on disk when the turn returns, whatever
// the interval said a moment earlier.
func TestTheTurnBoundaryFlushesWhatTheLoopCoalesced(t *testing.T) {
	backend, root := readingRounds(t, 3)
	agent, sess, _ := coalescingAgent(t, backend, root, nil)

	if err := agent.RunTurn(context.Background(), "read three notes"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	messages := sess.GetMessages()
	if len(messages) == 0 || messages[len(messages)-1].Content != "All read." {
		t.Fatalf("last message = %+v, want the final answer", messages[len(messages)-1])
	}
	if durable, _ := sess.SaveCounts(); durable == 0 {
		t.Error("the turn ended without a durable save")
	}
}

// The rollback switch named in the plan: a non-positive interval restores
// save-on-every-call, so a session that cannot afford to coalesce need not.
func TestANonPositiveSaveIntervalWritesOnEveryCall(t *testing.T) {
	backend, root := readingRounds(t, 10)
	agent, sess, _ := coalescingAgent(t, backend, root, func(o *engine.Options) {
		o.SaveInterval = -1
	})

	if err := agent.RunTurn(context.Background(), "read ten notes"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	durable, interim := sess.SaveCounts()
	// One user message, ten assistant messages, ten tool rounds: the old
	// behaviour, which is more than twenty writes.
	if durable < 21 {
		t.Errorf("durable saves = %d, want the pre-O3 count of at least 21", durable)
	}
	if interim != 0 {
		t.Errorf("interval saves = %d, want none when coalescing is off", interim)
	}
}

// A round that changed the user's files is never coalesced: the transcript is
// written before the tool touches the tree, and again after the result, so it
// is never staler than the files it describes.
func TestARoundThatWritesAFileSavesDurablyTwice(t *testing.T) {
	root := resolvedTempDir(t)
	target := filepath.Join(root, "made.txt")
	backend := &scriptedBackend{steps: []provider.Message{
		{Role: "assistant", Content: "Writing it.", ToolCalls: []provider.ToolCall{{
			ID: "call_w",
			Function: provider.FunctionCall{
				Name:      "write_file",
				Arguments: `{"path":"` + jsonEsc(target) + `","content":"hi\n"}`,
			},
		}}},
		{Role: "assistant", Content: "Done."},
	}}
	agent, sess, _ := coalescingAgent(t, backend, root, nil)

	if err := agent.RunTurn(context.Background(), "write a file"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the tool did not write the file: %v", err)
	}
	durable, interim := sess.SaveCounts()
	// Once before the write (the tool call), once for the round's result, once
	// at the turn boundary. The interval never gets a look in.
	if durable < 3 {
		t.Errorf("durable saves = %d, want at least three around a file-writing round", durable)
	}
	if interim != 0 {
		t.Errorf("interval saves = %d, want none in a round that changed the tree", interim)
	}
}

// watchingDecider answers every question and remembers what was already on
// disk when it was asked.
type watchingDecider struct {
	sess             *enginetest.FakeSession
	durable, interim int
	asked            bool
	answer           bool
}

func (d *watchingDecider) Confirm(context.Context, engine.Confirmation) bool {
	d.durable, d.interim = d.sess.SaveCounts()
	d.asked = true
	return d.answer
}

// A permission prompt is a boundary. The person about to answer one is the
// longest pause in a turn, and a transcript that is only in memory while a
// human thinks is a transcript a closed laptop takes with it.
func TestThePromptIsAskedOnlyAfterTheTranscriptIsOnDisk(t *testing.T) {
	root := resolvedTempDir(t)
	backend := &scriptedBackend{steps: []provider.Message{
		{Role: "assistant", Content: "Running it.", ToolCalls: []provider.ToolCall{{
			ID:       "call_ask",
			Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"echo hi","description":"say hi"}`},
		}}},
		{Role: "assistant", Content: "Done."},
	}}
	sess := enginetest.NewFakeSession("s_prompt", "mock/model")
	decider := &watchingDecider{sess: sess, answer: false}
	var out strings.Builder
	agent := engine.New(engine.Options{
		Backend: backend, Model: "mock/model", Mode: engine.ModeCode,
		Effort: engine.EffortMax,
		// The tier that asks, which is the default a session starts with.
		Permission: engine.PermissionAsk,
		Sess:       sess, Root: root, Out: &out, Decider: decider,
		Clock: enginetest.FakeClock(time.Unix(1_760_000_000, 0), 0),
	})

	if err := agent.RunTurn(context.Background(), "run a command"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if !decider.asked {
		t.Fatal("the command never reached a prompt, so this test proves nothing")
	}
	if decider.durable == 0 {
		t.Errorf("the question was asked with %d durable and %d interval saves behind it; want the transcript flushed first",
			decider.durable, decider.interim)
	}
}
