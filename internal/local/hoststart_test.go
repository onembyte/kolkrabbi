package local

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type startedProcess struct {
	closed atomic.Bool
	pid    int
}

func (p *startedProcess) Close() error { p.closed.Store(true); return nil }
func (p *startedProcess) Pid() int     { return p.pid }

// starterFixture is a HostStarter whose process, port and readiness are all
// fakes, so what it does can be asserted without a binary.
type starterFixture struct {
	starter  *HostStarter
	out      *strings.Builder
	process  *startedProcess
	starts   atomic.Int32
	env      []string
	args     []string
	readyIn  int // readiness polls before the fake server "answers"
	polls    atomic.Int32
	portList []int
}

func newStarterFixture(readyIn int, ports ...int) *starterFixture {
	f := &starterFixture{out: &strings.Builder{}, process: &startedProcess{pid: 4242}, readyIn: readyIn, portList: ports}
	f.starter = &HostStarter{
		Binary:  "/opt/ollama",
		Environ: []string{"HOME=/home/x", "PATH=/usr/bin", "OPENROUTER_API_KEY=sk-or-secret", "OLLAMA_HOST=10.0.0.9:11434"},
		Out:     f.out,
		Start: func(_ context.Context, _ string, args, env []string) (Process, error) {
			f.starts.Add(1)
			f.args, f.env = args, env
			return f.process, nil
		},
		Ready: func(context.Context, string) bool {
			return int(f.polls.Add(1)) > f.readyIn
		},
		Port: func() (int, error) {
			if len(f.portList) == 0 {
				return 0, errors.New("no ports")
			}
			p := f.portList[0]
			f.portList = f.portList[1:]
			return p, nil
		},
		ReadyBudget: 200 * time.Millisecond,
	}
	return f
}

func has(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}

// The guard that matters. The server is started with a curated environment:
// what it needs to find its store and its GPU, and never a credential. The
// only key kolk holds is the OpenRouter key, and a child process on this
// machine has no business seeing it.
func TestCuratedEnvNeverCarriesACredential(t *testing.T) {
	env := CuratedEnv([]string{
		"HOME=/home/x", "PATH=/usr/bin", "LD_LIBRARY_PATH=/opt/cuda/lib", "CUDA_VISIBLE_DEVICES=0",
		"OLLAMA_MODELS=/data/models", "OLLAMA_HOST=10.0.0.9:11434",
		"OPENROUTER_API_KEY=sk-or-secret", "GITHUB_TOKEN=ghp_x", "AWS_SECRET_ACCESS_KEY=y", "KOLK_CONFIG_DIR=/z",
	}, "127.0.0.1:43111")
	for _, want := range []string{"HOME=/home/x", "PATH=/usr/bin", "LD_LIBRARY_PATH=/opt/cuda/lib", "CUDA_VISIBLE_DEVICES=0", "OLLAMA_MODELS=/data/models", "OLLAMA_HOST=127.0.0.1:43111"} {
		if !has(env, want) {
			t.Errorf("curated env lacks %q: %v", want, env)
		}
	}
	for _, e := range env {
		for _, secret := range []string{"OPENROUTER_API_KEY", "GITHUB_TOKEN", "AWS_SECRET", "KOLK_"} {
			if strings.Contains(e, secret) {
				t.Errorf("curated env carries %q", e)
			}
		}
		if strings.HasPrefix(e, "OLLAMA_HOST=") && e != "OLLAMA_HOST=127.0.0.1:43111" {
			t.Errorf("the user's OLLAMA_HOST leaked through: %q; the started server must bind kolk's port", e)
		}
	}
}

func TestEnsureStartsOnceAndWaitsForReady(t *testing.T) {
	f := newStarterFixture(2, 43111)
	addr, err := f.starter.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:43111" {
		t.Fatalf("addr = %q", addr)
	}
	if again, _ := f.starter.Ensure(context.Background()); again != addr || f.starts.Load() != 1 {
		t.Fatalf("second Ensure started again (%d starts) or moved (%q)", f.starts.Load(), again)
	}
	if len(f.args) != 1 || f.args[0] != "serve" {
		t.Errorf("args = %v, want [serve]", f.args)
	}
	if !has(f.env, "OLLAMA_HOST=127.0.0.1:43111") || has(f.env, "OPENROUTER_API_KEY=sk-or-secret") {
		t.Errorf("started with env %v", f.env)
	}
	line := f.out.String()
	if strings.Count(line, "started ollama serve") != 1 || !strings.Contains(line, "4242") || !strings.Contains(line, "127.0.0.1:43111") {
		t.Errorf("transcript = %q, want one line naming pid and address", line)
	}
}

func TestEnsureGivesUpWhenTheServerNeverAnswers(t *testing.T) {
	f := newStarterFixture(1_000_000, 43111)
	start := time.Now()
	_, err := f.starter.Ensure(context.Background())
	if err == nil {
		t.Fatal("a server that never answered was reported ready")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("waited %v for a dead server; the budget is 200ms", time.Since(start))
	}
	if !f.process.closed.Load() {
		t.Fatal("the process that never became ready was left running")
	}
	if !strings.Contains(err.Error(), "/opt/ollama") {
		t.Errorf("error %q does not name the binary that failed", err)
	}
}

// 11434 is the default and belongs to whoever runs the host's server. A kolk
// server on it would be adopted by the next session as a host server it must
// never stop — and outlive every kolk on a SIGKILL.
func TestEnsureNeverBindsTheDefaultPort(t *testing.T) {
	f := newStarterFixture(0, 11434, 43112)
	addr, err := f.starter.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:43112" {
		t.Fatalf("addr = %q, want the second port offered", addr)
	}
}

func TestCloseStopsOnlyWhatItStarted(t *testing.T) {
	idle := newStarterFixture(0, 43111)
	if err := idle.starter.Close(); err != nil || idle.process.closed.Load() {
		t.Fatal("closing a starter that never started touched a process")
	}
	started := newStarterFixture(0, 43111)
	if _, err := started.starter.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := started.starter.Close(); err != nil || !started.process.closed.Load() {
		t.Fatal("closing a starter that started did not stop its process")
	}
}

// The lazy backend is the route registered when the binary is installed and
// idle: nothing starts until the first turn asks for a host model, and then
// the turn is answered by the server that was just started.
func TestLazyHostBackendStartsOnTheFirstTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("Ollama is running"))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.33.1"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"from the started server\"}}]}\n\ndata: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	_, portText, _ := strings.Cut(strings.TrimPrefix(server.URL, "http://"), ":")
	port, _ := strconv.Atoi(portText)

	f := newStarterFixture(0, port)
	f.starter.Ready = nil // the real probe, against the fake server
	backend := NewLazyHostBackend(f.starter)
	if f.starts.Load() != 0 {
		t.Fatal("constructing the backend started a server")
	}
	msg, _, err := backend.StreamChat(context.Background(), "qwen2.5-coder:7b", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "from the started server" || f.starts.Load() != 1 {
		t.Fatalf("content = %q, starts = %d", msg.Content, f.starts.Load())
	}
	if err := backend.Close(); err != nil || !f.process.closed.Load() {
		t.Fatal("closing the backend did not stop the server it started")
	}
}

// The guard the first release review found missing: the process is started
// under exec.CommandContext, so a server started with a request's context dies
// with that request. The fake here honours its context the way the real
// starter does, which the earlier fixture did not.
func TestEnsureDoesNotTieTheServerToTheRequestThatStartedIt(t *testing.T) {
	f := newStarterFixture(0, 43111)
	var started context.Context
	f.starter.Start = func(ctx context.Context, _ string, _, _ []string) (Process, error) {
		started = ctx
		return f.process, nil
	}
	request, cancel := context.WithCancel(context.Background())
	if _, err := f.starter.Ensure(request); err != nil {
		t.Fatal(err)
	}
	cancel()
	if started.Err() != nil {
		t.Fatal("the server's context ended with the request that started it; every later turn would find a dead server")
	}
	if f.process.closed.Load() {
		t.Fatal("the server was closed when the first request ended")
	}
}
