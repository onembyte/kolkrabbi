package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// CodexLister asks the vendor what it offers.
//
// `codex debug models` renders the raw model catalog as JSON — the same list
// the vendor's own picker shows, refreshed from the service when it can be and
// bundled with the binary otherwise. Verified against codex-cli 0.149.1 on
// 2026-09-02, when it listed eight models and `gpt-5.6-pro` — which kolk had
// carried as a rung since 08-30 — was not among them. That is the whole case
// for asking instead of knowing.
type CodexLister struct {
	// Run executes the vendor CLI and returns its stdout. Nil runs `codex`
	// through the scrubbed provider child path.
	Run func(ctx context.Context, args ...string) ([]byte, error)
	Now func() time.Time
}

// codexCatalogArgs is the documented catalog command. `--bundled` would skip
// the refresh and answer from the binary alone; not used, because the point
// is what the vendor offers today.
var codexCatalogArgs = []string{"debug", "models"}

func (l CodexLister) Discover(ctx context.Context) (provider.VendorCatalog, error) {
	run := l.Run
	if run == nil {
		run = runCodexCommand
	}
	version, err := run(ctx, "--version")
	if err != nil {
		return provider.VendorCatalog{}, fmt.Errorf("codex: cannot ask the vendor for its version: %w", err)
	}
	raw, err := run(ctx, codexCatalogArgs...)
	if err != nil {
		return provider.VendorCatalog{}, fmt.Errorf("codex: `codex debug models` failed: %w", err)
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	return ParseCodexModelCatalog(raw, codexVersion(version), now())
}

// runCodexCommand runs the vendor CLI with kolk's provider child rules: scrubbed
// environment, no inherited credential, bounded by the caller's context.
func runCodexCommand(ctx context.Context, args ...string) ([]byte, error) {
	var out bytes.Buffer
	err := shell.RunLines(ctx, "codex", args, nil, func(line []byte) error {
		out.Write(line)
		out.WriteByte('\n')
		return nil
	})
	return out.Bytes(), err
}

// codexVersion reduces `codex-cli 0.149.1` to the version.
func codexVersion(raw []byte) string {
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// codexCatalog is the vendor's JSON shape, the fields kolk reads. Unknown
// fields are ignored so a vendor that adds one does not break discovery —
// the exact opposite of a burned-in table.
type codexCatalog struct {
	Models []struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		Visibility               string `json:"visibility"`
		Priority                 int    `json:"priority"`
		ContextWindow            int    `json:"context_window"`
		DefaultReasoningLevel    string `json:"default_reasoning_level"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

// ParseCodexModelCatalog maps the vendor's catalog onto kolk's. Visibility
// `hide` becomes Hidden rather than dropped: a hidden model can still be
// asked for by name, and a person who knows one should see it named as such
// rather than told it does not exist. Priority is the vendor's own strongest-
// first order and becomes Rank verbatim.
func ParseCodexModelCatalog(raw []byte, version string, at time.Time) (provider.VendorCatalog, error) {
	var parsed codexCatalog
	if err := json.Unmarshal(bytes.TrimSpace(raw), &parsed); err != nil {
		return provider.VendorCatalog{}, fmt.Errorf("codex: catalog is not the JSON `codex debug models` renders: %w", err)
	}
	if len(parsed.Models) == 0 {
		return provider.VendorCatalog{}, fmt.Errorf("codex: catalog names no models")
	}
	catalog := provider.VendorCatalog{
		Vendor:        "codex",
		Source:        "codex " + strings.Join(codexCatalogArgs, " "),
		VendorVersion: version,
		FetchedAt:     at,
	}
	for _, model := range parsed.Models {
		slug := strings.TrimSpace(model.Slug)
		if slug == "" {
			continue
		}
		efforts := make([]string, 0, len(model.SupportedReasoningLevels))
		for _, level := range model.SupportedReasoningLevels {
			if effort := strings.TrimSpace(level.Effort); effort != "" {
				efforts = append(efforts, effort)
			}
		}
		catalog.Models = append(catalog.Models, provider.DiscoveredModel{
			ID:            slug,
			Display:       model.DisplayName,
			Efforts:       efforts,
			DefaultEffort: model.DefaultReasoningLevel,
			Context:       model.ContextWindow,
			Rank:          model.Priority,
			Hidden:        strings.EqualFold(model.Visibility, "hide"),
			Status:        provider.StatusListed,
		})
	}
	if len(catalog.Models) == 0 {
		return provider.VendorCatalog{}, fmt.Errorf("codex: catalog rows carry no slugs")
	}
	return catalog, nil
}
