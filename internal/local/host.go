package local

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// DefaultHostAddr is the only address discovery ever probes. Not OLLAMA_HOST:
// that variable may name another machine, or ollama.com itself, and a probe
// that followed it would adopt a server that is not on this box.
const DefaultHostAddr = "127.0.0.1:11434"

// hostProbeBudget bounds discovery. Loopback answers in microseconds or refuses
// in microseconds; anything that takes longer is not a healthy local server,
// and startup must not wait on it.
const hostProbeBudget = 300 * time.Millisecond

// HostState is what discovery found about the user's Ollama.
type HostState int

const (
	// HostAbsent: no discovered binary or answering server. Explicit setup or
	// local use can provision a native runtime on supported platforms.
	HostAbsent HostState = iota
	// HostInstalled: the binary is on PATH and nothing is listening.
	HostInstalled
	// HostRunning: a server answered as Ollama on the default address. It is
	// somebody else's process — adopted, never stopped.
	HostRunning
)

func (s HostState) String() string {
	switch s {
	case HostRunning:
		return "running"
	case HostInstalled:
		return "installed"
	default:
		return "absent"
	}
}

// Host is the discovered state of the user's own Ollama.
type Host struct {
	Managed bool // Kolk-owned runtime, distinct from a user's default server
	State   HostState
	Addr    string // where a running server answered
	Version string // what it reported
	Binary  string // where the executable is, when it is on PATH
	// MissingCompanion names, for a person, the official accelerator bundle
	// Kolk's setup would still add on this machine: to a managed tree that
	// lacks it, or to an installation not made yet. Empty when none.
	MissingCompanion string
	// AcceleratorNote describes accelerator hardware no official bundle
	// serves, so the runtime will not use it. Empty when there is none.
	AcceleratorNote string
	// CompanionFailure is why an earlier attempt to add MissingCompanion
	// failed, when that failure is remembered; upgrades skip that release
	// until an explicit setup forgets it or a new release arrives.
	CompanionFailure string
	// KeptRunning marks a Kolk runtime an earlier `local.ephemeral off` left
	// running for this project, which an ephemeral session reuses and never
	// stops: nothing signals a process another session started.
	KeptRunning bool
	// UnservedVendor is accelerator hardware present that no official bundle
	// serves on this platform; the runtime cannot use it. CompanionVendor is
	// the vendor MissingCompanion would serve.
	UnservedVendor, CompanionVendor string
	// StalledRuntime describes Kolk's recorded runtime for this project when
	// its process still runs but does not answer. A persistent session fails
	// until it answers; an ephemeral one starts its own beside it.
	StalledRuntime string
	// Installation facts survive CPU's reporting policy so a running tree
	// retains them even after another setup publishes a replacement tree.
	requiredCompanion, requiredFailure, requiredVendor string
}

func (h Host) companionFacts() (string, string, string) {
	if h.requiredCompanion != "" {
		return h.requiredCompanion, h.requiredFailure, h.requiredVendor
	}
	return h.MissingCompanion, h.CompanionFailure, h.CompanionVendor
}

// UnusableVendor is the accelerator vendor this runtime cannot use: hardware
// no bundle serves, or a missing bundle that the next start will not add
// (a runtime already running without it, or a remembered failure).
func (h Host) UnusableVendor() string {
	if h.UnservedVendor != "" {
		return h.UnservedVendor
	}
	if h.MissingCompanion != "" && (h.State == HostRunning || h.CompanionFailure != "") {
		return h.CompanionVendor
	}
	return ""
}

// HostDiscovery is what DiscoverHost needs from the machine, injected so the
// answer can be tested against a fake server and a fake PATH.
type HostDiscovery struct {
	Addr     string
	LookPath func(string) (string, error)
}

// DiscoverHost probes the given loopback address and PATH, and reports what is
// there. It never starts anything and never follows OLLAMA_HOST.
//
// A server is adopted only if it identifies itself: the root answers "Ollama
// is running", which is the vendor CLI's own heartbeat, and /api/version
// returns a version. Something else listening on the port — a dev server, a
// proxy, an old experiment — answers 200 too, and adopting it would send every
// local turn to a stranger.
func DiscoverHost(ctx context.Context, d HostDiscovery) Host {
	host := Host{}
	if d.LookPath != nil {
		if binary, err := d.LookPath(SidecarName); err == nil && binary != "" {
			host.Binary = binary
			host.State = HostInstalled
		}
	}
	if version, ok := probeHost(ctx, d.Addr); ok {
		host.State = HostRunning
		host.Addr = d.Addr
		host.Version = version
	}
	return host
}

// probeHost asks addr whether it is Ollama, within the budget.
func probeHost(ctx context.Context, addr string) (string, bool) {
	if strings.TrimSpace(addr) == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, hostProbeBudget)
	defer cancel()
	client := &http.Client{Timeout: hostProbeBudget}

	body, ok := hostGet(ctx, client, "http://"+addr+"/")
	if !ok || strings.TrimSpace(string(body)) != "Ollama is running" {
		return "", false
	}
	body, ok = hostGet(ctx, client, "http://"+addr+"/api/version")
	if !ok {
		return "", false
	}
	var reply struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &reply) != nil || reply.Version == "" {
		return "", false
	}
	return reply.Version, true
}

func hostGet(ctx context.Context, client *http.Client, url string) ([]byte, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return nil, false
	}
	return body, true
}

// installHints covers platforms without managed native setup.
//
// A lookup keyed by GOOS rather than a switch on it: the platform rule bans
// branching on the OS outside the platform layer, and a table is a value.
var installHints = map[string]string{
	"windows": "the installer from https://ollama.com/download (no administrator rights needed)",
}

// ManagedSetupSupported reports whether Kolk can install the official runtime
// on the platform it is running on.
func ManagedSetupSupported() bool {
	_, ok := runtimePlatforms[runtime.GOOS+"/"+runtime.GOARCH]
	return ok
}

// InstallHint is the install line for the platform kolk is running on.
func (h Host) InstallHint() string {
	if ManagedSetupSupported() {
		return "/localia setup (official Ollama from ollama.com; no Docker or sudo)"
	}
	if hint, ok := installHints[runtime.GOOS]; ok {
		return hint
	}
	return "https://ollama.com/download"
}
