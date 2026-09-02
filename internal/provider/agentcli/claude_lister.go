package agentcli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ClaudePreviewLister previews Claude Code's models from the gateway catalog.
//
// The CLI has no catalog command, and a valid name can only be confirmed by
// spending a turn (`--max-turns 0` still spends one; measured 2026-09-02).
// The owner's rule for a vendor like that: get the exact names from the
// gateway, show them with the vendor's efforts before the first prompt, and
// let the first prompt be the verification. The gateway lists Anthropic's
// exact ids — `anthropic/claude-fable-5`, `claude-opus-5`, `claude-sonnet-5`,
// `claude-haiku-4.5` — while the CLI takes a family alias (`fable`, `opus`,
// `sonnet`, `haiku`) and resolves it to the latest of that family. So a row is
// a family: the alias the CLI takes, the exact ids the gateway knows behind it
// newest first, the largest context among them, and the CLI's effort set.
//
// Only families the CLI's own help names are rows. A family the gateway
// carries and the CLI does not is not invented here; it is nothing until a
// turn proves the CLI takes it.
type ClaudePreviewLister struct {
	Gateway []provider.ModelInfo
	// Version is `claude --version`, recorded so a row says which CLI it was
	// previewed for. Empty when not asked.
	Version string
	Now     func() time.Time
}

// claudeFamilies is the order the vendor's own picker ranks them, strongest
// first — the same order as the engine's ladder. Rank 1 is the top.
var claudeFamilies = []string{"fable", "opus", "sonnet", "haiku"}

// Both spellings Anthropic has used: `claude-<family>-<version>` today and
// `claude-<version>-<family>` for the 3.x generation.
var (
	claudeModern = regexp.MustCompile(`^anthropic/claude-(fable|opus|sonnet|haiku)-([0-9][0-9.]*)$`)
	claudeLegacy = regexp.MustCompile(`^anthropic/claude-([0-9][0-9.]*)-(fable|opus|sonnet|haiku)$`)
)

func (l ClaudePreviewLister) Discover(context.Context) (provider.VendorCatalog, error) {
	if len(l.Gateway) == 0 {
		return provider.VendorCatalog{}, fmt.Errorf("claude: no gateway catalog to preview from")
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	type member struct {
		id      string
		version float64
		context int
	}
	families := map[string][]member{}
	for _, model := range l.Gateway {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		// Variants — `:batch`, `:thinking`, `-fast` — are the same model under
		// a different bill or speed, not something the CLI alias resolves to.
		// The family patterns end at the version, so they never match; that
		// is the guard, and the test asserts it.
		family, version, ok := claudeFamily(id)
		if !ok {
			continue
		}
		families[family] = append(families[family], member{id: model.ID, version: version, context: model.ContextLength})
	}
	catalog := provider.VendorCatalog{
		Vendor:        "claude",
		Source:        "gateway preview of anthropic/claude-*, grouped by the CLI's family aliases",
		VendorVersion: l.Version,
		FetchedAt:     now(),
	}
	for rank, family := range claudeFamilies {
		members := families[family]
		if len(members) == 0 {
			continue
		}
		sort.SliceStable(members, func(i, j int) bool { return members[i].version > members[j].version })
		row := provider.DiscoveredModel{
			ID:      "claude-" + family,
			Display: family,
			Efforts: ClaudeEfforts(),
			Rank:    rank + 1,
			Status:  provider.StatusUnverified,
		}
		for _, m := range members {
			row.ExactIDs = append(row.ExactIDs, m.id)
			if m.context > row.Context {
				row.Context = m.context
			}
		}
		catalog.Models = append(catalog.Models, row)
	}
	if len(catalog.Models) == 0 {
		return provider.VendorCatalog{}, fmt.Errorf("claude: the gateway catalog carries no anthropic/claude-* family the CLI names")
	}
	return catalog, nil
}

// claudeFamily reads the family and the version out of a gateway id.
func claudeFamily(id string) (family string, version float64, ok bool) {
	if m := claudeModern.FindStringSubmatch(id); m != nil {
		return m[1], parseVersion(m[2]), true
	}
	if m := claudeLegacy.FindStringSubmatch(id); m != nil {
		return m[2], parseVersion(m[1]), true
	}
	return "", 0, false
}

// parseVersion orders `5`, `4.8`, `4.5`, `3.5` as numbers. A second dot
// (`4.5.1`) is dropped rather than refused: ordering is all this is for.
func parseVersion(raw string) float64 {
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) > 2 {
		raw = parts[0] + "." + parts[1]
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

// IsModelRefusal reports whether a Claude turn failed because the vendor did
// not recognise the model it was asked for — the one failure that is
// information about the catalog rather than about the turn. It matches the
// vendor's own phrasing (`[claude-code:unrecognized_model]` on stderr; "There's
// an issue with the selected model … It may not exist" in the result), and
// nothing looser: a false positive would retire a model over a network error.
func IsModelRefusal(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unrecognized_model") {
		return true
	}
	return strings.Contains(text, "issue with the selected model") && strings.Contains(text, "may not exist")
}

// errModelRefused is exported for callers that want to construct the case in
// a test without reproducing vendor prose.
var errModelRefused = errors.New("[claude-code:unrecognized_model]")
