package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

// SubagentCapabilities is the explicit execution envelope handed to one
// delegated provider. The engine does not discover directories or credentials
// itself; the host resolves and verifies them before a child is opened.
type SubagentCapabilities struct {
	Workspace      string
	AdditionalDirs []string
	NetworkAccess  bool
	Provider       string
	// Permission is the agent's tier at the moment the child is opened, so
	// the host can map full-auto onto the vendor's own bypass. It is read
	// from the agent rather than declared by the host: the tier changes
	// in-session, and a copy would be the second source F6 removed.
	Permission Permission
}

// SubagentBackend opens a provider for one task, inside a declared envelope.
//
// A subagent that shares the session's backend shares everything the backend
// owns: one vendor process, one conversation, one mutex. Several tasks then
// serialise on the process and write their briefings into a single transcript,
// which is one conversation where there should be several — and a child that
// dies takes the parent's conversation with it.
//
// It arrives as a port because the engine may not construct an adapter: L4
// cannot import L5. Nil means today's behaviour, which is exactly that sharing,
// so a surface that has not wired this keeps working.
//
// mode is always code — never the session's own. A subagent asked to run agent
// mode would be a vendor orchestrating, which is the thing kolk's bus cannot
// represent.
//
// There used to be a second, simpler port that took no capabilities, and
// openSubagentBackend silently preferred it when a host set only that one. A
// host reaching for the simpler name got a child with no workspace
// confinement and no network declaration, and nothing said so — not the
// compiler, not a test. A port that cannot carry the envelope cannot be
// confined, so there is one port and it carries it.
type SubagentBackend func(ctx context.Context, model, mode, effort string, capabilities SubagentCapabilities) (ChatBackend, error)

// Subagent network policy. A delegated child either reaches the network or
// it does not, and the briefing, the status line, and the vendor flag must
// all say the same thing — so the decision is made once, here, before any of
// them is written, from three inputs: the policy the user set, what kind of
// work the task is, and whether the vendor's child process has a switch at
// all.
const (
	// SubagentNetworkAuto gives the network to research tasks — the kind a
	// user asks for when they want the web consulted — and withholds it from
	// everything else, except on a vendor whose child has no switch.
	SubagentNetworkAuto = "auto"
	// SubagentNetworkOn gives every child the network.
	SubagentNetworkOn = "on"
	// SubagentNetworkOff withholds it from every child. Strict: a vendor
	// that cannot run without network is refused rather than quietly given
	// it, and the task falls back or fails visibly.
	SubagentNetworkOff = "off"
)

// NormalizeSubagentNetwork resolves a policy name, accepting the empty
// string as auto.
func NormalizeSubagentNetwork(policy string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", SubagentNetworkAuto:
		return SubagentNetworkAuto, true
	case SubagentNetworkOn, "enabled", "true":
		return SubagentNetworkOn, true
	case SubagentNetworkOff, "disabled", "false":
		return SubagentNetworkOff, true
	default:
		return "", false
	}
}

// networkSwitchless names the vendor ladders whose child process has no
// network switch. Claude Code's sandbox has no network toggle kolk can set
// from the outside, and its Bash tool reaches the network regardless of which
// web tools are listed; declaring such a child "network disabled" would be a
// status line the child could contradict. Codex has
// sandbox_workspace_write.network_access and is switched explicitly.
var networkSwitchless = map[string]bool{"claude": true}

// vendorLacksNetworkSwitch reports whether a model's vendor child cannot be
// run without network. Unranked models are assumed switchable: the gateway
// path runs in-process where kolk's own tools decide.
func vendorLacksNetworkSwitch(model string) bool {
	ladder, _, known := modelRank(model)
	return known && networkSwitchless[ladder]
}

// kindWantsNetwork is the closed list of task kinds a user expects to reach
// the network. Research means "go and find out"; everything else works on
// what is already in the repository.
func kindWantsNetwork(kind Kind) bool {
	return kind == KindResearch
}

// subagentNetwork decides one child's network access.
func (a *Agent) subagentNetwork(kind Kind, model string) bool {
	policy, _ := NormalizeSubagentNetwork(a.SubagentNetwork)
	switch policy {
	case SubagentNetworkOn:
		return true
	case SubagentNetworkOff:
		return false
	default:
		return kindWantsNetwork(kind) || vendorLacksNetworkSwitch(model)
	}
}

// subagentCapabilities is the envelope one task's child is opened with. It is
// computed from the task and the model rather than copied from the host's
// static declaration, so the same call answers the briefing, the status line,
// and the factory — one source, no drift between what is said and what runs.
func (a *Agent) subagentCapabilities(kind Kind, model string) SubagentCapabilities {
	capabilities := a.SubagentCapabilities
	if capabilities.Workspace == "" {
		capabilities.Workspace = a.Root
	}
	capabilities.AdditionalDirs = append([]string(nil), capabilities.AdditionalDirs...)
	capabilities.NetworkAccess = a.subagentNetwork(kind, model)
	capabilities.Permission = a.Permission
	return capabilities
}

func subagentCapabilitySummary(capabilities SubagentCapabilities) string {
	workspace := strings.TrimSpace(capabilities.Workspace)
	if workspace == "" {
		workspace = "unspecified"
	}
	network := "disabled"
	if capabilities.NetworkAccess {
		network = "enabled"
	}
	return fmt.Sprintf("workspace=%s network=%s", workspace, network)
}

func (a *Agent) subagentOpeningStep(model string, capabilities SubagentCapabilities) string {
	step := "opening " + model
	if a.SubagentBackend != nil {
		step += "; " + subagentCapabilitySummary(capabilities)
	}
	return step
}

// openSubagentBackend gives one task its own provider, and a function to
// release it.
//
// The release is always safe to call: a nil port, a backend that is not a
// Closer, and a failed open all return one that does nothing. That matters
// because the caller defers it before it can know which case it got.
func (a *Agent) openSubagentBackend(ctx context.Context, model, effort string, kind Kind) (ChatBackend, func(), error) {
	if a.SubagentBackend == nil {
		return nil, func() {}, nil
	}
	capabilities := a.subagentCapabilities(kind, model)
	if strings.TrimSpace(capabilities.Workspace) == "" || !filepath.IsAbs(capabilities.Workspace) {
		return nil, func() {}, fmt.Errorf("subagent workspace is not a verified absolute directory")
	}
	backend, err := a.SubagentBackend(ctx, model, ModeCode, effort, capabilities)
	if err != nil {
		return nil, func() {}, err
	}
	owned, release := releaseSubagentBackend(backend)
	return owned, release, nil
}

func releaseSubagentBackend(backend ChatBackend) (ChatBackend, func()) {
	closer, ok := backend.(io.Closer)
	if !ok {
		return backend, func() {}
	}
	// Once: the task closes it before reporting and again in a deferred call,
	// and a vendor child must not be told to leave twice.
	var once sync.Once
	return backend, func() { once.Do(func() { _ = closer.Close() }) }
}

// pinnedBackend is a provider opened for one model.
//
// The model is carried with it because streamChat rewrites `model` in its own
// loop — free-model rotation and the metered fallback both do — and a provider
// opened for one model must not be handed another. A claude process asked for a
// gateway id fails the turn to discover it.
type pinnedBackend struct {
	backend ChatBackend
	model   string
}

// forModel returns the pinned provider only while it is still the right one.
func (p pinnedBackend) forModel(model string) ChatBackend {
	if p.backend == nil || p.model != model {
		return nil
	}
	return p.backend
}
