// Package config handles kolk's persistent settings: the default model, the
// endpoint, and the effort tiers.
//
// Every setting here is an override for a default that already works. Nothing
// in this package may become required reading for a new user — a config file
// someone has to open before kolk does anything useful is the product failing,
// not the file being incomplete.
//
// The package takes the file path rather than computing it. Locating
// directories belongs to internal/paths and nowhere else, which is what keeps
// the answer to "where does my config live?" a single one.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

type Config struct {
	Model   string            `json:"model,omitempty"`
	Effort  string            `json:"effort,omitempty"`
	Mode    string            `json:"mode,omitempty"`
	BaseURL string            `json:"base_url,omitempty"`
	Tiers   map[string]string `json:"tiers,omitempty"` // effort level -> model id
	// Slots maps an orchestration role (orchestrator, worker, explore, fast)
	// to a model. Unset roles fall back to the session model, so this file
	// never has to be complete to be useful.
	Slots map[string]string `json:"slots,omitempty"`
	// MaxRunCostUSD stops an orchestrated run once it has cost this much.
	// Zero, the default, means no ceiling.
	MaxRunCostUSD float64 `json:"max_run_cost_usd,omitempty"`
	// MaxConcurrentTasks is how many orchestrated tasks may run at once.
	// Zero means the default of three; one makes a run sequential.
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`
	// SubagentNetwork is the network policy for orchestrated children:
	// "auto" (research tasks only; a vendor with no switch always has it),
	// "on", or "off" (strict — a vendor that cannot run without network is
	// refused). Empty means auto.
	SubagentNetwork string `json:"subagent_network,omitempty"`
	// Sandbox is "on" or "off" (default): whether bash commands run inside
	// the OS sandbox of plan 13 §7.2. Opt-in by the owner's decision; there
	// is no "auto", because auto is a downgrade nobody sees.
	Sandbox string        `json:"sandbox,omitempty"`
	Local   LocalSettings `json:"local,omitempty"`
	// Routing decides what happens when the model behind the session stops
	// being able to answer — today, when a subscription runs out mid-run.
	Routing RoutingSettings `json:"routing,omitempty"`
	// Continuity is the plan-35 successor: pause and resume today, switching
	// and chains as the plan lands.
	Continuity ContinuitySettings `json:"continuity,omitempty"`
	// AutoRestartAfterUpdate re-executes kolk into the new version once an
	// in-session `kolk update` succeeds, resuming the same session. A pointer
	// so "never set" is distinguishable from "set to off": the default is off,
	// and replacing a running process is not something to start doing to
	// someone because they upgraded.
	AutoRestartAfterUpdate *bool `json:"auto_restart_after_update,omitempty"`
}

// Load reads a config file. A missing file is not an error: it returns a
// zero-value Config, because "no config" is the supported, expected state.
func Load(file string) (*Config, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		// Name the file. A parse error with no path is a scavenger hunt.
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return &cfg, nil
}

// Save writes a config file, creating its directory if needed.
//
// 0600 on the file and 0700 on the directory keep user preferences private.
// The prototype stored an API key here, but the Config schema no longer has a
// field capable of writing one; keystore.MigrateLegacyConfig owns evacuation.
func Save(file string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// Atomic: a config file truncated by a crash mid-write silently forgets the
	// user's model and endpoint choices.
	return atomicfile.Write(file, append(b, '\n'), 0o600)
}
