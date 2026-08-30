package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// Process is what a started server looks like from here: something that can
// be stopped. shell.ManagedProcess is the real one; tests hand in a fake.
type Process interface {
	Close() error
}

// StartFunc starts one server process. Injected so the starter can be tested
// without a binary.
type StartFunc func(ctx context.Context, executable string, args, env []string) (Process, error)

// hostReadyBudget is how long a server kolk started may take to answer before
// it is stopped and the turn fails. Measured on the owner's machine: 300–440 ms
// to ready. Fifteen seconds covers a slow disk; a server that needs more is
// not going to serve a turn anyone waits for.
const hostReadyBudget = 15 * time.Second

// HostStarter starts the user's own Ollama binary for this session when nothing
// is listening, on a port kolk chooses, and stops only what it started.
//
// Every dependency on the machine is a field so a test can stand in: the
// process, the readiness probe, the port. Production fills them from shell and
// from the loopback probe E3a already has.
type HostStarter struct {
	Binary  string
	Environ []string
	Out     io.Writer
	// Start launches the process; nil means shell.StartManagedProcess.
	Start StartFunc
	// Ready reports whether the server at addr answers as Ollama; nil means
	// the discovery probe.
	Ready func(context.Context, string) bool
	// Port picks a free loopback port; nil means ask the kernel.
	Port        func() (int, error)
	ReadyBudget time.Duration

	mu      sync.Mutex
	process Process
	addr    string
	closed  bool
}

// Ensure returns the address of the server this starter owns, starting it on
// the first call. Idempotent: every later call returns the same address and
// starts nothing.
func (h *HostStarter) Ensure(ctx context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", errors.New("the session's ollama server was already stopped")
	}
	if h.addr != "" {
		return h.addr, nil
	}
	port, err := h.choosePort()
	if err != nil {
		return "", fmt.Errorf("choosing a port for ollama: %w", err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	start := h.Start
	if start == nil {
		start = managedStart
	}
	// Detached from the caller's context on purpose. The starter is reached
	// from a warm bounded at two minutes and from a turn's own context, and
	// StartManagedProcess builds an exec.CommandContext — so a server started
	// under either would be killed the moment that first request ended, and
	// every later turn would find its address pointing at nothing. The
	// server's lifetime is the session's: Close is the only thing that stops
	// it. Readiness below still waits under the caller's context, so a
	// cancelled first request stops waiting and the unready server is closed.
	process, err := start(context.WithoutCancel(ctx), h.Binary, []string{"serve"}, CuratedEnv(h.Environ, addr))
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", h.Binary, err)
	}

	if !h.waitReady(ctx, addr) {
		_ = process.Close()
		return "", fmt.Errorf("%s started but never answered on %s; its own output says why", h.Binary, addr)
	}
	h.process, h.addr = process, addr
	if h.Out != nil {
		fmt.Fprintf(h.Out, "◆ started ollama serve (pid %d) on %s for this session; it stops when kolk exits\n", pidOf(process), addr)
	}
	return addr, nil
}

// choosePort asks the kernel for a free loopback port and refuses the default:
// a kolk server on 11434 would be adopted by the next session as a host server
// it must never stop, and would outlive every kolk on a SIGKILL.
func (h *HostStarter) choosePort() (int, error) {
	pick := h.Port
	if pick == nil {
		pick = freeLoopbackPort
	}
	for attempt := 0; attempt < 3; attempt++ {
		port, err := pick()
		if err != nil {
			return 0, err
		}
		if strconv.Itoa(port) != strings.TrimPrefix(DefaultHostAddr, "127.0.0.1:") {
			return port, nil
		}
	}
	return 0, errors.New("the only free port offered was the default, which belongs to the host")
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

func (h *HostStarter) waitReady(ctx context.Context, addr string) bool {
	ready := h.Ready
	if ready == nil {
		ready = func(ctx context.Context, addr string) bool {
			_, ok := probeHost(ctx, addr)
			return ok
		}
	}
	budget := h.ReadyBudget
	if budget == 0 {
		budget = hostReadyBudget
	}
	deadline := time.Now().Add(budget)
	for {
		if ready(ctx, addr) {
			return true
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Close stops the server this starter started, and nothing else. A starter
// that never started closes nothing.
func (h *HostStarter) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.process == nil {
		return nil
	}
	return h.process.Close()
}

func managedStart(ctx context.Context, executable string, args, env []string) (Process, error) {
	return shell.StartManagedProcess(ctx, executable, args, env)
}

func pidOf(process Process) int {
	if p, ok := process.(interface{ Pid() int }); ok {
		return p.Pid()
	}
	return 0
}

// environKept is what a started server may inherit: where its store and its
// GPU libraries are, and the locale. Everything else is withheld, above all
// every credential: the only key kolk holds is the OpenRouter key, and a child
// process on this machine has no business seeing it. An allowlist rather than a
// denylist, because a secret with a name nobody anticipated is the one a
// denylist lets through.
var environKept = []string{
	"HOME", "USER", "LOGNAME", "USERPROFILE", "PATH",
	"TMPDIR", "TEMP", "TMP", "LANG", "LC_",
	"SystemRoot", "SystemDrive", "LOCALAPPDATA",
	"LD_LIBRARY_PATH", "DYLD_", "CUDA_", "HIP_", "ROCR_", "HSA_", "GGML_", "XDG_",
	"OLLAMA_",
}

// CuratedEnv is the environment a started server gets: the kept variables
// from environ, with OLLAMA_HOST set to addr whatever the user had it as — the
// server has to bind kolk's port, not the machine the user's variable names.
func CuratedEnv(environ []string, addr string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if name == "OLLAMA_HOST" || !kept(name) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "OLLAMA_HOST="+addr)
}

func kept(name string) bool {
	for _, allowed := range environKept {
		if name == allowed || (strings.HasSuffix(allowed, "_") && strings.HasPrefix(name, allowed)) {
			return true
		}
	}
	return false
}

// LazyHostBackend is the route registered when the binary is installed and
// idle. Nothing starts until the first turn asks for a host model; then the
// server is started, and that turn and every later one goes to it. Closing the
// backend stops the server, which was kolk's to stop. Everything else — the
// client, the windows, warming — is the shared host backend.
type LazyHostBackend struct {
	*hostBackend
	starter *HostStarter
}

func NewLazyHostBackend(starter *HostStarter) *LazyHostBackend {
	return &LazyHostBackend{hostBackend: newHostBackend(starter.Ensure), starter: starter}
}

// Addr is where the server is, once started; empty before then.
func (b *LazyHostBackend) Addr() string {
	b.starter.mu.Lock()
	defer b.starter.mu.Unlock()
	return b.starter.addr
}

func (b *LazyHostBackend) Close() error { return b.starter.Close() }
