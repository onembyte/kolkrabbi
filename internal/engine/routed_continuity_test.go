package engine_test

// Adopted from the V43.5 review (EG2), priorities 4 and 5 together: a routed four-task plan on a
// discovered menu hits a usage limit mid-run, is restarted from disk,
// resumed under a refreshed menu that lost one rung, and compacts a resumed
// child after a context overflow. Every promise is asserted on the durable
// session, not on the Agent that produced it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

const p45Marker = "P45_MIDDLE_EVIDENCE_ONLY_IN_ARCHIVE"

type p45Reg struct {
	t        *testing.T
	mu       sync.Mutex
	phase    int
	calls    map[string]int    // model|title -> calls
	opened   map[string]string // title -> "model@effort" of each open, appended
	refuse   map[string]bool
	root     string
	readArg  string
	writeArg string
	t1Start  chan struct{}
	limitHit chan struct{}
	requests map[string][][]provider.Message // model|title -> requests
}

func (r *p45Reg) bump(model, title string, msgs []provider.Message) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := model + "|" + title
	r.calls[key]++
	r.requests[key] = append(r.requests[key], append([]provider.Message(nil), msgs...))
	return r.calls[key]
}

func (r *p45Reg) count(model, title string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[model+"|"+title]
}

func taskTitle(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == "user" && strings.HasPrefix(m.Content, "Your task: ") {
			return strings.TrimPrefix(m.Content, "Your task: ")
		}
	}
	return "?"
}

type p45Child struct {
	model string
	reg   *p45Reg
}

func (c *p45Child) ContextWindow(string) int { return 0 }

func (c *p45Child) StreamChat(ctx context.Context, wire string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	r := c.reg
	title := taskTitle(msgs)
	n := r.bump(c.model, title, msgs)
	ok := func(text string) (provider.Message, provider.Meta, error) {
		return provider.Message{Role: "assistant", Content: text}, provider.Meta{Model: wire, Cost: 0.01}, nil
	}
	call := func(id, name, args string) (provider.Message, provider.Meta, error) {
		return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: args}}}}, provider.Meta{Model: wire, Cost: 0.01}, nil
	}
	switch r.phase {
	case 1:
		switch {
		case c.model == "small" && title == "T1 mechanical" && n == 1:
			close(r.t1Start)
			<-r.limitHit
			time.Sleep(60 * time.Millisecond) // the sibling's limit is noted by now
			return call("t1-write", "write_file", r.writeArg)
		case c.model == "mid" && title == "T2 routine" && n == 1:
			return call("t2-read", "read_file", r.readArg)
		case c.model == "mid" && title == "T2 routine" && n == 2:
			<-r.t1Start
			close(r.limitHit)
			return provider.Message{}, provider.Meta{Model: wire}, &provider.HTTPError{StatusCode: http.StatusTooManyRequests,
				Message: "You have reached your usage limit for this window", RetryAfter: 30 * time.Minute}
		}
	case 2:
		switch {
		case c.model == "small" && title == "T1 mechanical" && n == 2:
			return ok("T1 done: renamed")
		case c.model == "sel" && title == "T2 routine" && n == 1:
			return provider.Message{}, provider.Meta{Model: wire}, &provider.HTTPError{StatusCode: http.StatusBadRequest,
				Message: "context_length_exceeded"}
		case c.model == "sel" && title == "T2 routine" && n == 2:
			return ok("T2 done: scanned")
		case c.model == "sel" && title == "T3 hard" && n == 1:
			return ok("T3 done: designed")
		case c.model == "sel" && title == "T4 unstated" && n == 1:
			sys := msgs[0].Content
			for _, want := range []string{"T1 done", "T2 done", "T3 done"} {
				if !strings.Contains(sys, want) {
					r.t.Errorf("T4 briefing lacks %q", want)
				}
			}
			return ok("T4 done: wrapped up")
		}
	}
	r.t.Errorf("unexpected child call phase=%d model=%s title=%q n=%d", r.phase, c.model, title, n)
	return provider.Message{}, provider.Meta{}, errors.New("unexpected call")
}

type p45Main struct {
	reg   *p45Reg
	mu    sync.Mutex
	calls int
	synth []provider.Message
}

func (m *p45Main) StreamChat(_ context.Context, wire string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.mu.Unlock()
	if n == 1 {
		return provider.Message{Role: "assistant", Content: `[
 {"title":"T1 mechanical","kind":"boilerplate","level":"trivial","needs":[]},
 {"title":"T2 routine","kind":"research","level":"routine","needs":[]},
 {"title":"T3 hard","kind":"research","level":"hard","needs":[]},
 {"title":"T4 unstated","kind":"explain","needs":[1,2,3]}]`}, provider.Meta{Model: wire, Cost: 0.01}, nil
	}
	m.mu.Lock()
	m.synth = msgs
	m.mu.Unlock()
	return provider.Message{Role: "assistant", Content: "all four done"}, provider.Meta{Model: wire, Cost: 0.01}, nil
}

func TestARoutedPlanPausesRestartsResumesAndCompacts(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	evidence := filepath.Join(root, "evidence.txt")
	if err := os.WriteFile(evidence, []byte(strings.Repeat("first line of evidence\n", 300)+p45Marker+"\n"+strings.Repeat("last line of evidence\n", 300)), 0o600); err != nil {
		t.Fatal(err)
	}
	written := filepath.Join(root, "t1.txt")
	readArg, _ := json.Marshal(map[string]string{"path": evidence})
	writeArg, _ := json.Marshal(map[string]string{"path": written, "content": "written once"})
	reg := &p45Reg{t: t, phase: 1, calls: map[string]int{}, opened: map[string]string{}, refuse: map[string]bool{},
		root: root, readArg: string(readArg), writeArg: string(writeArg), t1Start: make(chan struct{}), limitHit: make(chan struct{}),
		requests: map[string][][]provider.Message{}}
	menu := func(ceiling, _ string) engine.Roster {
		rungs := []engine.Rung{{Model: ceiling, Vendor: "codex", Efforts: []string{"low", "medium", "high", "xhigh"}}}
		if reg.phase == 1 {
			rungs = append(rungs, engine.Rung{Model: "mid", Vendor: "codex", Depth: 1, Efforts: []string{"low", "medium", "high"}})
		}
		rungs = append(rungs, engine.Rung{Model: "small", Vendor: "codex", Depth: len(rungs), Efforts: []string{"low"}})
		return engine.Roster{Rungs: rungs}
	}
	factory := func(_ context.Context, model, _, effort string, caps engine.SubagentCapabilities) (engine.ChatBackend, error) {
		reg.mu.Lock()
		refused := reg.refuse[model]
		reg.mu.Unlock()
		if caps.Provider != "codex" {
			t.Errorf("provider binding lost for %s: %+v", model, caps)
		}
		if refused {
			return nil, fmt.Errorf("%s is gone from the codex catalog", model)
		}
		reg.mu.Lock()
		reg.opened[model] += effort + ";"
		reg.mu.Unlock()
		return &p45Child{model: model, reg: reg}, nil
	}
	mainBackend := &p45Main{reg: reg}
	dir := t.TempDir()
	sess := session.New(dir, "sel")
	var out bytes.Buffer
	opts := engine.Options{Backend: mainBackend, Sess: sess, Root: root, Model: "sel", Mode: engine.ModeAgent, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Out: &out, MaxConcurrentTasks: 2, MaxRunCostUSD: 5,
		AgentRoster: menu, SubagentBackend: factory}
	a := engine.New(opts)
	const input = "four routed tasks"
	err := a.RunTurn(context.Background(), input)
	var paused *engine.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("phase 1: expected pause, got %v\n%s", err, out.String())
	}
	_ = a.Close()

	// ---- durable state after the pause, from disk ----
	loaded, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := loaded.Paused(); p == nil || p.PendingTurn != input {
		t.Fatalf("pending input not durable: %+v", p)
	}
	run := loaded.RunState()
	if run == nil || run.Phase != "tasks" || run.Input != input || run.Root != root || len(run.Tasks) != 4 {
		t.Fatalf("journal: %+v", run)
	}
	wantRoute := [][2]string{{"small", "low"}, {"mid", "medium"}, {"sel", "xhigh"}, {"sel", "high"}}
	for i, task := range run.Tasks {
		if task.Model != wantRoute[i][0] || task.Effort != wantRoute[i][1] || task.Vendor != "codex" || task.Status != "" {
			t.Errorf("saved task %d route/status = %s@%s vendor=%s status=%q", i+1, task.Model, task.Effort, task.Vendor, task.Status)
		}
	}
	if fmt.Sprint(run.Tasks[3].Needs) != "[0 1 2]" {
		t.Errorf("dependencies not durable: %v", run.Tasks[3].Needs)
	}
	t1, t2 := run.Tasks[0], run.Tasks[1]
	if last := t1.Messages[len(t1.Messages)-1]; last.Role != "assistant" || len(last.ToolCalls) != 1 {
		t.Errorf("T1 settled provider call not retained: %+v", t1.Messages)
	}
	if _, err := os.Stat(written); !os.IsNotExist(err) {
		t.Errorf("T1's tool ran after the sibling's limit (pause should precede the next tool call): %v", err)
	}
	hasResult := false
	for _, m := range t2.Messages {
		hasResult = hasResult || (m.Role == "tool" && m.ToolCallID == "t2-read" && strings.Contains(m.Content, p45Marker))
	}
	if !hasResult || t2.Rounds != 1 {
		t.Errorf("T2 completed tool result/rounds not durable: rounds=%d", t2.Rounds)
	}
	if reg.count("sel", "T3 hard") != 0 || reg.count("sel", "T4 unstated") != 0 {
		t.Errorf("queued work launched after a child limit")
	}
	if run.Spend.USD <= 0 || run.Spend.Limit != 5 {
		t.Errorf("run accounting not durable: %+v", run.Spend)
	}
	spentBefore := run.Spend.USD
	t.Logf("phase-1 spend=%.2f calls=%v", spentBefore, reg.calls)

	// ---- restart: refreshed menu lost "mid"; its factory now refuses it ----
	reg.mu.Lock()
	reg.phase = 2
	reg.refuse["mid"] = true
	reg.mu.Unlock()
	out.Reset()
	opts.Sess = loaded
	b := engine.New(opts)
	defer b.Close()
	pending, ok := b.Resume()
	if !ok || pending != input {
		t.Fatalf("resume claim = %q %v", pending, ok)
	}
	if err := b.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("resumed run: %v\n%s", err, out.String())
	}
	final := loaded.RunState()
	if final.Phase != "done" {
		t.Fatalf("phase = %s", final.Phase)
	}
	for i, task := range final.Tasks {
		if task.Status != "done" {
			t.Errorf("task %d status %q", i+1, task.Status)
		}
	}
	if got, _ := os.ReadFile(written); string(got) != "written once" {
		t.Errorf("T1 pending write not executed exactly once: %q", got)
	}
	if reg.count("small", "T1 mechanical") != 2 || reg.count("mid", "T2 routine") != 2 {
		t.Errorf("completed provider work repeated: %v", reg.calls)
	}
	if final.Tasks[1].Model != "sel" || final.Tasks[1].Effort != "medium" {
		t.Errorf("T2 fallback route = %s@%s", final.Tasks[1].Model, final.Tasks[1].Effort)
	}
	if !strings.Contains(out.String(), "falling back to sel") {
		t.Errorf("fallback not announced:\n%s", out.String())
	}
	if len(final.Tasks[1].Archives) != 1 {
		t.Errorf("T2 compaction archive missing: %+v", final.Tasks[1].Archives)
	}
	for _, m := range final.Tasks[1].Messages {
		if strings.Contains(m.Content, p45Marker) {
			t.Errorf("T2 working context was not compacted")
		}
	}
	archives, err := loaded.CompactionHistory()
	found := false
	for _, archive := range archives {
		for _, m := range archive.Messages {
			found = found || strings.Contains(m.Content, p45Marker)
		}
	}
	if err != nil || !found {
		t.Errorf("stored history lost the pre-compaction evidence: %v (%d archives)", err, len(archives))
	}
	users := 0
	for _, m := range loaded.GetMessages() {
		if m.Role == "user" && m.Content == input {
			users++
		}
	}
	if users != 1 {
		t.Errorf("stored transcript has the request %d times", users)
	}
	if final.Spend.USD <= spentBefore {
		t.Errorf("run accounting restarted instead of continuing: before=%.2f after=%.2f", spentBefore, final.Spend.USD)
	}
	reg.mu.Lock()
	t.Logf("opened=%v", reg.opened)
	t.Logf("calls=%v", reg.calls)
	reg.mu.Unlock()
	t.Logf("resume output:\n%s", out.String())
	// T2's resumed request on the ceiling carried its saved tool result.
	reg.mu.Lock()
	first := reg.requests["sel|T2 routine"][0]
	reg.mu.Unlock()
	carried := false
	for _, m := range first {
		carried = carried || (m.Role == "tool" && m.ToolCallID == "t2-read")
	}
	if !carried {
		t.Errorf("resumed T2 lost its completed tool result")
	}
}
