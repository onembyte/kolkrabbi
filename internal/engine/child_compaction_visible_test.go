package engine_test

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

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Adopted from the V43.5 §7 item 3 exercise (Z1): beside other agents a
// child's transcript is buffered, and a live surface never prints that buffer
// (it shows each agent's status instead). A compaction, and above all a failed
// one, must still reach the person: failed persistence is visible.
func TestAChildsCompactionIsSeenOnALiveSurface(t *testing.T) {
	for _, c := range []struct {
		name    string
		archive func([]provider.Message) (string, error)
		want    []string
	}{
		{"compacted", nil, []string{"compacted agent 1 context", "agent 1: request was too long; retrying once with the smaller context"}},
		{"archive failed", func([]provider.Message) (string, error) { return "", errors.New("disk full") },
			[]string{"could not compact agent 1: full history could not be archived: disk full"}},
	} {
		// Concurrency 1 writes straight to the surface already: a notice must
		// still show once, not twice (V43.5 F2).
		for _, concurrency := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/concurrency %d", c.name, concurrency), func(t *testing.T) {
				root := resolvedTempDir(t)
				path := filepath.Join(root, "evidence.txt")
				if err := os.WriteFile(path, []byte(strings.Repeat("a line of evidence\n", 400)), 0o600); err != nil {
					t.Fatal(err)
				}
				args, _ := json.Marshal(map[string]string{"path": path})
				srv := enginetest.New(
					enginetest.Step{Text: `[{"title":"inspect evidence","kind":"research"},{"title":"verify result","kind":"research","needs":[1]}]`},
					enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "read", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)}}}},
					enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"context_length_exceeded"}}`},
					enginetest.Step{Text: "inspection complete"},
					enginetest.Step{Text: "verification complete"},
					enginetest.Step{Text: "both complete"},
				)
				defer srv.Close()
				out := &lockedBuffer{}
				a := engine.New(engine.Options{Client: provider.NewCompatibleClient(srv.URL), Sess: session.New(t.TempDir(), "mock/model"),
					Root: root, Model: "mock/model", Mode: engine.ModeAgent, Permission: engine.PermissionFullAuto, Out: out,
					MaxConcurrentTasks: concurrency, ArchiveCompaction: c.archive,
					// A live surface: each agent's status is shown, its buffer is not.
					Subagents: func(engine.SubagentStatus) {}})
				defer a.Close()
				_ = a.RunTurn(context.Background(), "inspect then verify the evidence")
				for _, want := range c.want {
					if got := strings.Count(out.String(), want); got != 1 {
						t.Errorf("the surface showed %q %d times, want once:\n%s", want, got, out.String())
					}
				}
			})
		}
	}
}

// A child whose window is nearly full compacts before its next request, not
// only after an overflow; that notice reaches a live surface too.
type smallWindowChild struct {
	mu       sync.Mutex
	calls    int
	readArgs string
}

func (b *smallWindowChild) ContextWindow(string) int { return 3000 }

func (b *smallWindowChild) StreamChat(_ context.Context, wire string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	if n == 1 {
		return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "read", Type: "function",
			Function: provider.FunctionCall{Name: "read_file", Arguments: b.readArgs}}}}, provider.Meta{Model: wire}, nil
	}
	return provider.Message{Role: "assistant", Content: "read it"}, provider.Meta{Model: wire}, nil
}

type plannerThenSynthesis struct{}

func (plannerThenSynthesis) StreamChat(_ context.Context, wire string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	if len(msgs) > 0 && strings.Contains(msgs[0].Content, "JSON") {
		return provider.Message{Role: "assistant", Content: `[{"title":"inspect evidence","kind":"research"},{"title":"inspect again","kind":"research","needs":[1]}]`}, provider.Meta{Model: wire}, nil
	}
	return provider.Message{Role: "assistant", Content: "both read"}, provider.Meta{Model: wire}, nil
}

func TestAChildsProactiveCompactionIsSeenWhereItsReportIs(t *testing.T) {
	root := resolvedTempDir(t)
	path := filepath.Join(root, "evidence.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a line of evidence\n", 800)), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path})
	for _, live := range []bool{true, false} {
		out := &lockedBuffer{}
		opts := engine.Options{Backend: plannerThenSynthesis{}, Sess: session.New(t.TempDir(), "mock/model"),
			Root: root, Model: "mock/model", Mode: engine.ModeAgent, Permission: engine.PermissionFullAuto, Out: out,
			MaxConcurrentTasks: 2,
			SubagentBackend: func(context.Context, string, string, string, engine.SubagentCapabilities) (engine.ChatBackend, error) {
				return &smallWindowChild{readArgs: string(args)}, nil
			}}
		if live {
			opts.Subagents = func(engine.SubagentStatus) {}
		}
		a := engine.New(opts)
		_ = a.RunTurn(context.Background(), "inspect the evidence twice")
		_ = a.Close()
		text := out.String()
		notice := strings.Index(text, "compacted agent 1 context")
		if notice < 0 {
			t.Fatalf("live=%v: the proactive compaction was never shown:\n%s", live, text)
		}
		// Without a live surface the child's report is printed whole, after
		// delegation, and the notice belongs inside it, not ahead of it.
		if report := strings.Index(text, "subagent 1/2 inspect evidence"); !live && (report < 0 || notice < report) {
			t.Errorf("live=false: the notice is not inside the child's report:\n%s", text)
		}
	}
}

// A child whose own conversation has nothing to give up still has its
// briefing of earlier results to shorten; that compaction is shown too.
func TestAChildsBriefingCompactionIsSeenOnALiveSurface(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"gather","kind":"research"},{"title":"summarize","kind":"explain","needs":[1]}]`},
		enginetest.Step{Text: strings.Repeat("a long gathered result line\n", 1500)},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"context_length_exceeded"}}`},
		enginetest.Step{Text: "summarized"},
		enginetest.Step{Text: "done"},
	)
	defer srv.Close()
	out := &lockedBuffer{}
	a := engine.New(engine.Options{Client: provider.NewCompatibleClient(srv.URL), Sess: session.New(t.TempDir(), "mock/model"),
		Root: resolvedTempDir(t), Model: "mock/model", Mode: engine.ModeAgent, Permission: engine.PermissionFullAuto, Out: out,
		MaxConcurrentTasks: 2, Subagents: func(engine.SubagentStatus) {}})
	defer a.Close()
	_ = a.RunTurn(context.Background(), "gather then summarize")
	for _, want := range []string{"compacted agent 2 context", "agent 2: request was too long; retrying once with the smaller context"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the surface never showed %q:\n%s", want, out.String())
		}
	}
}
