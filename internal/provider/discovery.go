package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Model discovery.
//
// No vendor model name is true because it is in kolk's source. The owner's
// rule, 2026-09-02: "do not burn model names before knowing what's available
// … tomorrow claude or codex will update his model names and kolk will stop
// working correctly" — and it applies to every vendor. So every connector
// supplies a ModelLister, kolk runs it on every start and every login, and
// what the vendor said is what the model commands show, each row with a
// status that says how kolk knows.
//
// Two kinds of vendor, one vocabulary. A vendor with a catalog command
// (`codex debug models`) is listed: the rows are its. A vendor with no such
// command (Claude Code) is previewed from the gateway catalog, which carries
// the exact ids the vendor publishes, and each row stays unverified until the
// first real turn confirms it — the turn is the verification, never a probe
// spent to discover.

// ModelStatus is how kolk knows a model exists.
type ModelStatus string

const (
	// StatusListed: the vendor's own catalog names it.
	StatusListed ModelStatus = "listed"
	// StatusVerified: the vendor answered a turn on it.
	StatusVerified ModelStatus = "verified"
	// StatusUnverified: a seed or a gateway preview nothing has confirmed yet.
	StatusUnverified ModelStatus = "unverified"
	// StatusGone: the vendor no longer lists it, or refused it by name.
	StatusGone ModelStatus = "gone"
)

// DiscoveredModel is one row a vendor catalog knows.
type DiscoveredModel struct {
	// ID is what a backend is asked for: a vendor slug (`gpt-5.6-sol`), a
	// kolk plan id (`claude-fable`), or a gateway id (`anthropic/claude-fable-5`).
	ID      string `json:"id"`
	Display string `json:"display,omitempty"`
	// ExactIDs are the vendor's full ids behind an alias, when an alias is
	// what the CLI takes and the gateway is what knows the exact name.
	ExactIDs      []string `json:"exact_ids,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	Context       int      `json:"context,omitempty"`
	// Rank orders a vendor's models strongest first; 0 means unranked, which
	// the ceiling treats as "never clamped" and the roster as "never
	// descended to".
	Rank   int         `json:"rank,omitempty"`
	Hidden bool        `json:"hidden,omitempty"`
	Status ModelStatus `json:"status"`
}

// VendorCatalog is what one vendor offered when it was last asked.
type VendorCatalog struct {
	Vendor        string            `json:"vendor"`
	Source        string            `json:"source"`
	VendorVersion string            `json:"vendor_version,omitempty"`
	FetchedAt     time.Time         `json:"fetched_at"`
	Models        []DiscoveredModel `json:"models"`
}

// Find returns the row for an id, case-insensitively.
func (c VendorCatalog) Find(id string) (DiscoveredModel, bool) {
	wanted := strings.ToLower(strings.TrimSpace(id))
	for _, model := range c.Models {
		if strings.ToLower(model.ID) == wanted {
			return model, true
		}
	}
	return DiscoveredModel{}, false
}

// Visible is the catalog a person should see: not hidden, not gone, ranked
// strongest first with unranked rows after, stable within a rank.
func (c VendorCatalog) Visible() []DiscoveredModel {
	out := make([]DiscoveredModel, 0, len(c.Models))
	for _, model := range c.Models {
		if model.Hidden || model.Status == StatusGone {
			continue
		}
		out = append(out, model)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].Rank, out[j].Rank
		if ri == 0 {
			ri = int(^uint(0) >> 1)
		}
		if rj == 0 {
			rj = int(^uint(0) >> 1)
		}
		return ri < rj
	})
	return out
}

// ModelLister is the port every connector must implement. A connector that
// cannot say what it offers cannot be registered; "cannot list" is itself an
// answer (NotListable), never an omission.
type ModelLister interface {
	Discover(ctx context.Context) (VendorCatalog, error)
}

// NotListable is the lister for a vendor kolk knows it cannot ask. It always
// fails, with the reason, so a surface can say why a vendor has no rows
// instead of showing an empty list that looks like "nothing offered".
type NotListable struct {
	Vendor string
	Reason string
}

func (n NotListable) Discover(context.Context) (VendorCatalog, error) {
	return VendorCatalog{}, fmt.Errorf("%s: models cannot be listed: %s", n.Vendor, n.Reason)
}

// GatewayPreviewLister previews a vendor that publishes no catalog of its own
// from the gateway catalog, which carries the vendor's exact ids. Rows are
// unverified: the gateway knows the names, and only a turn on the vendor's own
// CLI proves the vendor's login can use them. This is the owner's rule for
// "every vendor that does not expose the models like codex does".
type GatewayPreviewLister struct {
	// Vendor names the catalog (what kolk calls the connector).
	Vendor string
	// Prefix selects the gateway ids: `anthropic/`, `openai/`, `x-ai/` …
	Prefix string
	// Efforts is what the vendor's own CLI accepts, when it has an effort
	// switch; the gateway does not know that and must not guess it.
	Efforts []string
	// Gateway is the catalog to preview from — the session's cached one.
	Gateway []ModelInfo
	// Version is the vendor CLI version to record, when known.
	Version string
	Now     func() time.Time
}

func (g GatewayPreviewLister) Discover(context.Context) (VendorCatalog, error) {
	if strings.TrimSpace(g.Prefix) == "" {
		return VendorCatalog{}, fmt.Errorf("%s: gateway preview needs a provider prefix", g.Vendor)
	}
	if len(g.Gateway) == 0 {
		return VendorCatalog{}, fmt.Errorf("%s: no gateway catalog to preview from", g.Vendor)
	}
	prefix := strings.ToLower(strings.TrimSuffix(g.Prefix, "/")) + "/"
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	catalog := VendorCatalog{
		Vendor:        g.Vendor,
		Source:        "gateway preview of " + prefix + "*",
		VendorVersion: g.Version,
		FetchedAt:     now(),
	}
	for _, model := range g.Gateway {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		// Variants are the same model under a different bill or speed, not
		// a model a vendor CLI can be asked for.
		if strings.Contains(id, ":") {
			continue
		}
		catalog.Models = append(catalog.Models, DiscoveredModel{
			ID:      model.ID,
			Display: model.Name,
			Efforts: append([]string(nil), g.Efforts...),
			Context: model.ContextLength,
			Status:  StatusUnverified,
		})
	}
	if len(catalog.Models) == 0 {
		return VendorCatalog{}, fmt.Errorf("%s: the gateway catalog lists nothing under %s", g.Vendor, prefix)
	}
	return catalog, nil
}
