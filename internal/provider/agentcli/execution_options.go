package agentcli

import (
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// ExecutionOptions is the bounded capability envelope for a provider-owned
// agent process. Workspace is the primary project directory; additional
// directories are explicit extra roots; NetworkAccess is consumed by
// providers that expose a narrow network switch.
type ExecutionOptions struct {
	Workspace      string
	AdditionalDirs []string
	NetworkAccess  bool
	Provider       string
	// BypassPermissions is kolk's full-auto tier, pushed down to a Claude
	// child as --permission-mode bypassPermissions (docs/plan/04 §4.2). It
	// was designed there and never built, and the gap was invisible until a
	// Fable saga ran its first command: under acceptEdits the child denies
	// every Bash command that needs an approval, and nobody is there to
	// give one, so a "full-auto" session had a child that could edit files
	// and run nothing. Chat mode ignores it — a child with no tools has
	// nothing to bypass. Not part of the envelope for emptiness: it grants
	// nothing to a directory, so it never makes an invocation delegated.
	BypassPermissions bool
	// normalized records that this envelope has already been through
	// normalizeExecutionOptions: its directories are absolute, symlink-free,
	// verified to exist, and deduplicated.
	//
	// Unexported on purpose. A caller outside this package builds
	// ExecutionOptions as a literal, so the field is false and the envelope is
	// validated — the marker can be trusted precisely because nobody outside
	// can set it. Measured before it existed: validating a workspace and two
	// additional directories was 48 of the 54 allocations in building one
	// Codex turn's argv, and a provider CLI turn pays that every turn.
	normalized bool
	// Efforts is the effort set the vendor's catalog listed for this model,
	// when discovery has one. Non-empty, it replaces the adapter's seed set
	// for validation: a level the vendor lists today is accepted today, and
	// one it dropped is refused, without a code change either way. It is not
	// part of the execution envelope and never makes the options non-empty.
	Efforts []string
}

// effortAllowed validates one provider-spelled effort against the discovered
// set when there is one, and the adapter's seed set otherwise.
func effortAllowed(effort string, discovered []string, seed map[string]bool) bool {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return true
	}
	if len(discovered) == 0 {
		return seed[effort]
	}
	for _, level := range discovered {
		if strings.EqualFold(strings.TrimSpace(level), effort) {
			return true
		}
	}
	return false
}

// executionOptionsEmpty reports whether there is no envelope at all — the
// session's own process rather than a delegated child.
//
// Two fields are deliberately not part of the answer. `normalized` records
// that validation has run, not that anything was declared. `Efforts` is the
// vendor's own catalog talking back, not a capability kolk granted — counting
// either would make an envelope look delegated, and a delegated Codex envelope
// states the sandbox network flag (F2), so the session's own invocation would
// start overriding the user's config.
func executionOptionsEmpty(options ExecutionOptions) bool {
	return options.Workspace == "" && len(options.AdditionalDirs) == 0 &&
		!options.NetworkAccess && options.Provider == ""
}

func normalizeExecutionOptions(options ExecutionOptions) (ExecutionOptions, error) {
	// Already canonical: the constructor did this, and the directories cannot
	// have become more valid since. Re-checking per turn was work the answer
	// to which was already known.
	if options.normalized {
		return options, nil
	}
	workspace, err := normalizeExecutionDirectory("workspace", options.Workspace, false)
	if err != nil {
		return ExecutionOptions{}, err
	}
	additional := make([]string, 0, len(options.AdditionalDirs))
	seen := make(map[string]struct{}, len(options.AdditionalDirs))
	for _, directory := range options.AdditionalDirs {
		resolved, err := normalizeExecutionDirectory("additional directory", directory, true)
		if err != nil {
			return ExecutionOptions{}, err
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		additional = append(additional, resolved)
	}
	options.Workspace = workspace
	options.AdditionalDirs = additional
	options.normalized = true
	return options, nil
}

// providerNetworkSwitch says whether a provider's child process can be told
// not to use the network.
//
// Data, not a special case. This was a free function named for one vendor,
// which meant the invariant existed wherever someone remembered to call it —
// Claude's three constructors did, Codex's did not, and nothing would have
// told a fourth provider it was supposed to. Keyed on the envelope's own
// Provider field, the rule applies to whoever is being constructed, and adding
// a provider means adding a row.
//
// Codex has `sandbox_workspace_write.network_access`, which F2 states both
// ways on every delegated envelope. Claude Code has no such switch: its Bash
// tool reaches the network whatever web tools are listed, so an envelope that
// claims network-disabled delegated execution is a claim the child can
// contradict, and it is refused rather than believed.
var providerNetworkSwitch = map[string]bool{
	"claude": false,
	"codex":  true,
}

func validateExecutionOptions(options ExecutionOptions) error {
	delegated := options.Workspace != "" || len(options.AdditionalDirs) > 0
	if !delegated || options.NetworkAccess {
		return nil
	}
	if canSwitch, known := providerNetworkSwitch[options.Provider]; known && !canSwitch {
		return fmt.Errorf("%s cannot prove network-disabled delegated execution; enable network for this child or use a provider with an explicit network switch", options.Provider)
	}
	return nil
}

func normalizeExecutionDirectory(label, directory string, optional bool) (string, error) {
	if directory == "" {
		if optional {
			return "", fmt.Errorf("%s cannot be empty", label)
		}
		return "", nil
	}
	// One implementation of the four checks, in internal/shell (F6.1). The
	// label keeps this error saying "workspace" or "additional directory".
	return shell.VerifiedDir(label, directory)
}
