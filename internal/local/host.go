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
	// HostAbsent: no binary on PATH and nothing answering. The install line is
	// the only help kolk can offer, and it names it rather than running it.
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
	State   HostState
	Addr    string // where a running server answered
	Version string // what it reported
	Binary  string // where the executable is, when it is on PATH
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

// installHints is the one line that installs Ollama, per platform. The Linux
// script needs sudo and pipes curl into sh, both of which kolk's own hardline
// forbids, so kolk names the line and never runs it: a floor the product steps
// over itself is not a floor.
//
// A lookup keyed by GOOS rather than a switch on it: the platform rule bans
// branching on the OS outside the platform layer, and a table is a value.
var installHints = map[string]string{
	"linux":   "curl -fsSL https://ollama.com/install.sh | sh  (needs sudo; kolk will not run it for you)",
	"darwin":  "brew install ollama, or the app from https://ollama.com/download",
	"windows": "the installer from https://ollama.com/download (no administrator rights needed)",
}

// InstallHint is the install line for the platform kolk is running on.
func (h Host) InstallHint() string {
	if hint, ok := installHints[runtime.GOOS]; ok {
		return hint
	}
	return "https://ollama.com/download"
}
