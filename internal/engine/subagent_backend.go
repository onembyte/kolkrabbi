package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// SubagentCapabilities is the explicit execution envelope handed to one
// delegated provider. The engine does not discover directories or credentials
// itself; the host resolves and verifies them before a child is opened.
type SubagentCapabilities struct {
	Workspace      string
	AdditionalDirs []string
	NetworkAccess  bool
	Provider       string
}

// SubagentBackend opens a provider for one task.
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
type SubagentBackend func(ctx context.Context, model, mode, effort string) (ChatBackend, error)

// SubagentBackendWithCapabilities is the capability-aware form of
// SubagentBackend. It is separate so existing embedders and test doubles keep
// compiling while hosts migrate to an explicit child-process envelope.
type SubagentBackendWithCapabilities func(ctx context.Context, model, mode, effort string, capabilities SubagentCapabilities) (ChatBackend, error)

func (a *Agent) subagentCapabilities() SubagentCapabilities {
	capabilities := a.SubagentCapabilities
	if capabilities.Workspace == "" {
		capabilities.Workspace = a.Root
	}
	capabilities.AdditionalDirs = append([]string(nil), capabilities.AdditionalDirs...)
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
	if a.SubagentBackendWithCapabilities != nil {
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
func (a *Agent) openSubagentBackend(ctx context.Context, model, effort string) (ChatBackend, func(), error) {
	if a.SubagentBackendWithCapabilities != nil {
		capabilities := a.subagentCapabilities()
		if strings.TrimSpace(capabilities.Workspace) == "" || !filepath.IsAbs(capabilities.Workspace) {
			return nil, func() {}, fmt.Errorf("subagent workspace is not a verified absolute directory")
		}
		backend, err := a.SubagentBackendWithCapabilities(ctx, model, ModeCode, effort, capabilities)
		if err != nil {
			return nil, func() {}, err
		}
		owned, release := releaseSubagentBackend(backend)
		return owned, release, nil
	}
	if a.SubagentBackend == nil {
		return nil, func() {}, nil
	}
	backend, err := a.SubagentBackend(ctx, model, ModeCode, effort)
	if err != nil {
		return nil, func() {}, err
	}
	// ChatBackend declares StreamChat and nothing else, so teardown is found
	// rather than required — the same assertion Agent.Close already makes.
	closer, ok := backend.(io.Closer)
	if !ok {
		return backend, func() {}, nil
	}
	return backend, func() { _ = closer.Close() }, nil
}

func releaseSubagentBackend(backend ChatBackend) (ChatBackend, func()) {
	closer, ok := backend.(io.Closer)
	if !ok {
		return backend, func() {}
	}
	return backend, func() { _ = closer.Close() }
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
