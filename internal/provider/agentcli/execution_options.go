package agentcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func executionOptionsEmpty(options ExecutionOptions) bool {
	return options.Workspace == "" && len(options.AdditionalDirs) == 0 &&
		!options.NetworkAccess && options.Provider == ""
}

func normalizeExecutionOptions(options ExecutionOptions) (ExecutionOptions, error) {
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
	return options, nil
}

func validateClaudeExecutionOptions(options ExecutionOptions) error {
	if (options.Workspace != "" || len(options.AdditionalDirs) > 0) && !options.NetworkAccess {
		return fmt.Errorf("claude cannot prove network-disabled delegated execution; enable network for this child or use a provider with an explicit network switch")
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
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("%s must be absolute: %q", label, directory)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", label, directory, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("checking %s %q: %w", label, directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %q", label, directory)
	}
	return resolved, nil
}
