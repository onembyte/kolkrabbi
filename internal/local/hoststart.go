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
	"syscall"
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
	// Persistent is fixed for this session. Only persistent runtimes publish
	// an endpoint; a session-owned server must never be shared with another.
	Persistent bool
	StateDir   string
	Project    string
	Discover   func(context.Context) Host
	// CPUChosen reads the current setting independently of installation discovery.
	CPUChosen func() bool
	// Provision installs a missing native runtime only when Ensure is requested.
	// Discovery and status never call it. Output belongs to the active surface.
	Provision func(context.Context, io.Writer) (Host, error)
	// Tidy is housekeeping run on a start, before discovery: it clears what
	// an interrupted setup left behind, on the starts that never reach
	// Provision too. It writes where the starter writes and must never wait
	// on a setup in progress.
	Tidy func(io.Writer)
	// DetachedStart and Identity are the persistent process seams.
	DetachedStart StartFunc
	Identity      func(context.Context, int) (string, error)
	// EndpointOwner must prove that the recorded process owns the loopback
	// listener before an explicit stop may signal it.
	EndpointOwner func(context.Context, int, string) (bool, error)
	// Signal is the final explicit-stop action. Nil uses the platform process
	// group signal; tests inject it so no real process is harmed.
	Signal func(context.Context, int) error
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
	managed bool
	version string
	closed  bool
	// missing is the accelerator bundle the running managed runtime lacks,
	// kept so status stays true after a fallback or a start without setup.
	missing string
	// failure is why adding missing failed, when that is remembered; the
	// next start skips that release too, so status must not promise it.
	failure string
	// Explicit setup forgets failures even if discovery still has a cached one.
	failureCleared bool
	// kept is a runtime an earlier persistent setting left running for this
	// project: reused by an ephemeral session, never owned or stopped by it.
	kept bool
	// unserved and missingVendor carry discovery's accelerator facts into a
	// running runtime, so fit plans leave out a GPU it cannot use.
	unserved, missingVendor string
}

// Ensure returns the address of the server this starter owns, starting it on
// the first call. Idempotent: every later call returns the same address and
// starts nothing.
func (h *HostStarter) Ensure(ctx context.Context) (string, error) {
	return h.ensure(ctx, true)
}

// EnsureInstalled is Ensure without setup: it starts an installed runtime as
// it is, and never installs one or completes one with an accelerator bundle.
// A sign-in uses it, since it talks to a server and must not turn into a
// runtime download nobody was asked about.
func (h *HostStarter) EnsureInstalled(ctx context.Context) (string, error) {
	return h.ensure(ctx, false)
}

func (h *HostStarter) ensure(ctx context.Context, setup bool) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if h.closed {
		return "", errors.New("the session's ollama server was already stopped")
	}
	h.forgetExited()
	if h.addr != "" {
		if (h.Persistent || h.kept) && h.managed && h.process == nil {
			addr, err := h.reusable(ctx)
			// A kept runtime that stalls mid-session is left behind; the
			// check below starts this session's own and says why.
			var stalled *runtimeStalled
			if err != nil && (h.Persistent || !errors.As(err, &stalled)) {
				return "", err
			}
			h.addr = addr
			if addr == "" {
				h.kept = false
			}
		}
	}
	if h.addr != "" {
		return h.addr, nil
	}
	h.failureCleared = false
	// A managed tree that lacks the accelerator bundle this machine needs is
	// completed through the same setup that installs a missing runtime. Setup
	// keeps the working tree if the bundle cannot be added.
	incomplete, missing, failure := false, "", ""
	unserved, missingVendor := "", ""
	if h.Tidy != nil {
		out := h.Out
		if out == nil {
			out = io.Discard
		}
		h.Tidy(out)
	}
	if h.Discover != nil {
		host := h.Discover(ctx)
		if host.State == HostRunning {
			// Adopted as it is: a user's own server lacks nothing of Kolk's.
			h.addr = host.Addr
			h.managed, h.version, h.missing, h.failure, h.kept = false, host.Version, "", "", false
			h.unserved, h.missingVendor = "", ""
			return h.addr, nil
		}
		if host.Binary != "" {
			h.Binary = host.Binary
			if host.Version != "" {
				h.version = host.Version
			}
		}
		incomplete = host.Managed && host.State == HostInstalled && host.MissingCompanion != ""
		missing, failure, missingVendor = host.companionFacts()
		unserved = host.UnservedVendor
	}
	if h.Persistent {
		held, err := h.lockRegistry(ctx)
		if err != nil {
			return "", err
		}
		defer func() { _ = held.Close() }()
		addr, err := h.reusable(ctx)
		if err != nil {
			return "", err
		}
		if addr != "" {
			h.addr, h.managed, h.missing, h.failure = addr, true, missing, failure
			h.unserved, h.missingVendor = unserved, missingVendor
			return addr, nil
		}
	} else if h.StateDir != "" && h.Project != "" {
		// A runtime an earlier `local.ephemeral off` left running for this
		// project is reused rather than started beside, and never stopped.
		// One that runs but does not answer is not waited on either: this
		// session starts its own beside it, as before any was kept.
		addr, err := h.reusable(ctx)
		var stalled *runtimeStalled
		switch {
		case errors.As(err, &stalled):
			if h.Out != nil {
				fmt.Fprintf(h.Out, "Localia: %s; this session starts its own and leaves that one alone\n", stalled.describe())
			}
		case err != nil:
			return "", err
		case addr != "":
			h.addr, h.managed, h.kept, h.missing, h.failure = addr, true, true, missing, failure
			h.unserved, h.missingVendor = unserved, missingVendor
			return addr, nil
		}
	}
	if setup && (h.Binary == "" || incomplete) && h.Provision != nil {
		before := h.Binary
		host, err := h.Provision(ctx, h.Out)
		if err != nil {
			// A cancellation passes through: it is not a net.Error, so it is
			// never mistaken for an endpoint limit. A deadline is one, so only
			// the caller's own passes through; the installer's own deadlines
			// (its release lookup behind a firewall that drops traffic) are
			// setup failures like any other.
			if errors.Is(err, context.Canceled) {
				return "", err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			return "", &RuntimeSetupError{err: err}
		}
		h.Binary, h.version = host.Binary, host.Version
		// A new tree carries what was missing; setup that kept the old tree
		// (a companion it could not add) leaves the gap in place.
		// Discovery reads the record of the tree about to run: whether setup
		// added the bundle, kept the old tree (and why), or installed a first
		// runtime without it. Only its answer for this binary is trusted; a
		// different binary means a new tree, which carries what was missing.
		var again Host
		if h.Discover != nil {
			again = h.Discover(ctx)
		}
		switch {
		case again.Binary != "" && again.Binary == h.Binary:
			missing, failure, missingVendor = again.companionFacts()
			unserved = again.UnservedVendor
		case h.Binary != before:
			missing, failure, missingVendor = "", "", ""
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if h.Binary == "" {
		return "", errors.New("ollama is not installed; open /localia for local model setup")
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
	if h.Persistent {
		start = h.DetachedStart
		if start == nil {
			start = detachedStart
		}
	}
	// Detached from the caller's context on purpose. The starter is reached
	// from a warm bounded at two minutes and from a turn's own context, and
	// StartManagedProcess builds an exec.CommandContext — so a server started
	// under either would be killed the moment that first request ended, and
	// every later turn would find its address pointing at nothing. The
	// server's lifetime belongs to the starter's policy. Readiness below still
	// waits under the caller's context, so a
	// cancelled first request stops waiting and the unready server is closed.
	process, err := start(context.WithoutCancel(ctx), h.Binary, []string{"serve"}, CuratedEnv(h.Environ, addr))
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", h.Binary, err)
	}

	if !h.waitReady(ctx, addr) {
		_ = process.Close()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%s started but never answered on %s; check that Ollama can run on this machine", h.Binary, addr)
	}
	if h.Persistent {
		if err := h.publish(ctx, addr, process); err != nil {
			_ = process.Close()
			return "", fmt.Errorf("saving persistent Ollama runtime: %w", err)
		}
	}
	h.process, h.addr, h.managed, h.missing, h.failure = process, addr, true, missing, failure
	h.unserved, h.missingVendor = unserved, missingVendor
	if h.Out != nil {
		lifetime := "for this session; it stops when kolk exits"
		if h.Persistent {
			lifetime = "for this project; it stays running after kolk exits"
		}
		fmt.Fprintf(h.Out, "◆ started ollama serve (pid %d) on %s %s\n", pidOf(process), addr, lifetime)
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
		if ctx.Err() != nil {
			return false
		}
		if ready(ctx, addr) {
			return true
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Close stops only this session's ephemeral server. Persistent and adopted
// servers are left running. Failed startup is rolled back before publication.
func (h *HostStarter) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	if h.process == nil || h.Persistent {
		return nil
	}
	return h.process.Close()
}

func managedStart(ctx context.Context, executable string, args, env []string) (Process, error) {
	return shell.StartManagedProcess(ctx, executable, args, env)
}

func detachedStart(ctx context.Context, executable string, args, env []string) (Process, error) {
	return shell.StartDetachedProcess(ctx, executable, args, env)
}

// SetOutput moves startup notices onto the active terminal surface safely.
func (h *HostStarter) SetOutput(out io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Out = out
}

// Host observes the session's endpoint before looking at the user's default
// server or a persistent record. It never starts a server or installs software.
func (h *HostStarter) Host(ctx context.Context) Host {
	host := h.host(ctx)
	if h.CPUChosen != nil && h.CPUChosen() {
		if host.UnservedVendor == "" && host.MissingCompanion != "" {
			host.UnservedVendor = host.CompanionVendor
		}
		host.MissingCompanion, host.CompanionFailure, host.AcceleratorNote = "", "", ""
		host.CompanionVendor = ""
	}
	return host
}

func (h *HostStarter) host(ctx context.Context) Host {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return Host{}
	}
	h.forgetExited()
	if h.addr != "" && (h.Persistent || h.kept) && h.managed && h.process == nil {
		addr, err := h.reusable(ctx)
		if err != nil || addr == "" {
			// Keep the cached identity on a temporarily unready process so
			// Ensure can report it rather than launch a duplicate.
			host := Host{State: HostInstalled, Binary: h.Binary, Managed: true}
			var stalled *runtimeStalled
			if errors.As(err, &stalled) {
				host.StalledRuntime = stalled.describe()
			}
			return host
		}
		h.addr = addr
	}
	if h.addr != "" {
		host := Host{State: HostRunning, Addr: h.addr, Binary: h.Binary, Version: h.version, Managed: h.managed, KeptRunning: h.kept}
		if h.managed {
			host.MissingCompanion, host.CompanionFailure = h.missing, h.failure
			host.UnservedVendor, host.CompanionVendor = h.unserved, h.missingVendor
			// The choice in force now: the tree this runtime runs is read
			// again (offline), so a /config change shows at once. A tree
			// setup has replaced since is not the one running, and keeps what
			// the runtime started with. Recover a failure hidden by CPU at
			// startup, unless explicit setup has since forgotten failures.
			if h.Discover != nil {
				if again := h.Discover(ctx); again.Binary != "" && again.Binary == h.Binary {
					host.MissingCompanion = again.MissingCompanion
					host.UnservedVendor, host.CompanionVendor = again.UnservedVendor, again.CompanionVendor
					if !h.failureCleared {
						host.CompanionFailure = again.CompanionFailure
					}
					if host.MissingCompanion == "" {
						host.CompanionFailure = ""
					}
				}
			}
		}
		return host
	}
	host := Host{Binary: h.Binary}
	if h.Binary != "" {
		host.State = HostInstalled
	}
	if h.Discover != nil {
		discovered := h.Discover(ctx)
		if discovered.State != HostAbsent {
			host = discovered
		} else {
			// Nothing to run yet still says what setup would add, and
			// what it cannot serve. Not a remembered failure: a fresh
			// install ignores it and tries the bundle again.
			host.MissingCompanion, host.AcceleratorNote = discovered.MissingCompanion, discovered.AcceleratorNote
			host.UnservedVendor, host.CompanionVendor = discovered.UnservedVendor, discovered.CompanionVendor
		}
	}
	if host.State != HostRunning && (h.Persistent || h.StateDir != "" && h.Project != "") {
		addr, err := h.reusable(ctx)
		var stalled *runtimeStalled
		switch {
		case errors.As(err, &stalled):
			host.StalledRuntime = stalled.describe()
		case err == nil && addr != "":
			host.State, host.Addr, host.Managed, host.KeptRunning = HostRunning, addr, true, !h.Persistent
		}
	}
	return host
}

// An owned process that has exited can be replaced on the next request. Never
// signal the old PID: Wait has already reaped it and it may have been reused.
func (h *HostStarter) forgetExited() {
	if process, ok := h.process.(interface{ Exited() bool }); ok && process.Exited() {
		h.process, h.addr, h.managed, h.version, h.missing, h.failure = nil, "", false, "", "", ""
		h.unserved, h.missingVendor = "", ""
	}
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
// backend applies the session's ephemeral policy. Everything else — the
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

// RuntimeSetupError is a failure to set up the local runtime: fetching,
// verifying or installing Ollama. It deliberately does not unwrap. A network
// error inside it concerns GitHub, not the model endpoint, and unwrapped it
// would be classified as that endpoint's transport limit, pausing the turn
// and hiding the cause.
//
// Its advice fits the cause: the network only for a failure to reach or read
// the release, room for a full disk, and nothing added where the cause
// already names its remedy.
type RuntimeSetupError struct{ err error }

func (e *RuntimeSetupError) Error() string {
	message := "setting up Localia failed: " + e.err.Error()
	var fetch *runtimeFetchError
	switch {
	case errors.Is(e.err, errManagedSetupUnsupported):
		return message
	case errors.As(e.err, &fetch):
		return message + "; check the network and retry, or install Ollama yourself and Kolk will use it"
	case errors.Is(e.err, errRuntimeNoRoom) || errors.Is(e.err, syscall.ENOSPC):
		return message + "; free some disk space and retry, or install Ollama yourself and Kolk will use it"
	}
	return message + "; installing Ollama yourself also works, and Kolk will use it"
}

// ClearCompanionFailure forgets the cached reason the running runtime lacks
// its accelerator bundle, once an explicit setup has forgotten the remembered
// failure: the next start retries, and status must say that instead.
func (h *HostStarter) ClearCompanionFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failure = ""
	h.failureCleared = true
}
