package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// modelListerFor is the registry: every connector kolk can sign into answers
// with a lister, and TestEveryConnectorCanListItsModels is what keeps it that
// way. A vendor that publishes a catalog is asked; a vendor that does not is
// previewed from the gateway catalog by its provider prefix, unverified until
// a turn confirms it; a vendor with neither is NotListable, with the reason.
//
// The gateway catalog is the session's cached one, so this never goes to the
// network on its own account.
func modelListerFor(connector string, gateway []provider.ModelInfo) provider.ModelLister {
	switch strings.ToLower(strings.TrimSpace(connector)) {
	case "codex":
		return agentcli.CodexLister{}
	case "claude":
		// Previewed, not listed: the CLI has no catalog command, and the
		// gateway carries the exact ids it publishes. One row per family the
		// CLI's aliases name, unverified until the first prompt's init.model.
		return agentcli.ClaudePreviewLister{Gateway: gateway}
	case "gemini":
		return provider.GatewayPreviewLister{Vendor: "gemini", Prefix: "google", Gateway: gateway}
	case "gemini-api":
		// Google documents GET /models on its OpenAI-compatible endpoint; the
		// live, keyed lister is V34.4c.1b.ii's next step. Until then the
		// gateway's preview, marked as such like every unshipped vendor's.
		return provider.GatewayPreviewLister{Vendor: "gemini-api", Prefix: "google", Gateway: gateway}
	case "xai-api":
		return provider.GatewayPreviewLister{Vendor: "xai-api", Prefix: "x-ai", Gateway: gateway}
	case "perplexity-api":
		return provider.GatewayPreviewLister{Vendor: "perplexity-api", Prefix: "perplexity", Gateway: gateway}
	case "mistral-api":
		return provider.GatewayPreviewLister{Vendor: "mistral-api", Prefix: "mistralai", Gateway: gateway}
	case "deepseek-api":
		return provider.GatewayPreviewLister{Vendor: "deepseek-api", Prefix: "deepseek", Gateway: gateway}
	case "qwen-api":
		return provider.GatewayPreviewLister{Vendor: "qwen-api", Prefix: "qwen", Gateway: gateway}
	case "cohere-api":
		return provider.GatewayPreviewLister{Vendor: "cohere-api", Prefix: "cohere", Gateway: gateway}
	case "ollama":
		return ollamaCloudLister{}
	case "copilot":
		// Observed 2026-09-06: the CLI names its models only inside a session
		// (session.auto_mode_resolved) and the Free plan accepts only `auto`;
		// each answered turn records the model auto chose as auto's exact id.
		return provider.NotListable{Vendor: "copilot", Reason: "the copilot CLI has no catalog command; it names the model it chose inside each turn, which kolk records under `auto`"}
	default:
		return nil
	}
}

// ollamaCloudLister asks ollama.com's public catalog, which is the list the
// `ollama` connector's paid tier draws from. The host's own models are a
// different question (`kolk localia`).
type ollamaCloudLister struct {
	list func(context.Context) ([]local.CloudCatalogModel, error)
	now  func() time.Time
}

func (o ollamaCloudLister) Discover(ctx context.Context) (provider.VendorCatalog, error) {
	list := o.list
	if list == nil {
		list = local.ListCloudCatalog
	}
	now := o.now
	if now == nil {
		now = time.Now
	}
	models, err := list(ctx)
	if err != nil {
		return provider.VendorCatalog{}, fmt.Errorf("ollama: cloud catalog: %w", err)
	}
	if len(models) == 0 {
		return provider.VendorCatalog{}, fmt.Errorf("ollama: cloud catalog names no models")
	}
	catalog := provider.VendorCatalog{Vendor: "ollama", Source: "ollama.com /api/tags", FetchedAt: now()}
	for _, model := range models {
		display := model.Name
		if model.Parameters != "" {
			display += " (" + model.Parameters + ")"
		}
		catalog.Models = append(catalog.Models, provider.DiscoveredModel{
			ID: model.Name, Display: display, Status: provider.StatusListed,
		})
	}
	return catalog, nil
}

// recordVendorModelOutcome is what a turn teaches the vendor catalog: a turn
// that answered proves the model the session asked for, on the exact id the
// vendor reported; a refusal by name retires the row. Best effort, like the
// connector confirmation beside it — a session that works must never fail
// because a note about it could not be written.
func (a *app) recordVendorModelOutcome(vendor, asked string, meta provider.Meta, turnErr error) {
	dirs, err := a.resolve()
	if err != nil {
		return
	}
	store, err := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		return
	}
	switch {
	case turnErr == nil:
		// A vendor can only verify a model it lists. The persistent child
		// answers whatever it is asked with the model it was spawned on, so an
		// id from elsewhere — a gateway model routed to the session backend —
		// would be recorded as a verified vendor model with another model's
		// exact id. Seen live on 2026-09-02: cohere/north-mini-code:free
		// "verified" under claude with exact id claude-fable-5-1. Not
		// recorded, and said once, because a request that reached the wrong
		// vendor is a routing fault worth a line, not a catalog row.
		if !a.vendorKnowsModel(store, vendor, asked) {
			a.noteUnknownAskedOnce(vendor, asked, meta.Model)
			return
		}
		store.Verify(vendor, asked, meta.Model, time.Now())
	case agentcli.IsModelRefusal(turnErr):
		if !store.Gone(vendor, asked) {
			return
		}
	default:
		return
	}
	_ = provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store)
}

// vendorDiscoveryTimeout bounds one vendor's answer. `codex debug models`
// answers in tens of milliseconds and a gateway preview in none; the bound is
// for a vendor CLI that hangs on a network it cannot reach.
const vendorDiscoveryTimeout = 15 * time.Second

// vendorCatalogTTL is how long a vendor's answer is taken at its word.
//
// Every start used to spawn every signed-in vendor's CLI — `codex --version`,
// `codex debug models`, and one process per vendor after that — behind the
// prompt but on the same machine the first turn is running on
// (OPTIMIZATION_PLAN.md O8). A vendor's catalog changes when the vendor ships,
// which is not hourly, so a start inside this window uses what the last one
// learned and spawns nothing at all. A login, `kolk models --refresh`, and a
// vendor that has never answered are all outside it, and the first turn to
// meet a model the vendor no longer has still marks it gone on contact (F4.3).
//
// Rollback for a suspected stale catalog is one word here: 0.
const vendorCatalogTTL = 6 * time.Hour

// vendorDiscoveryYieldPoll is how often a background discovery asks whether
// the turn has finished with the machine.
const vendorDiscoveryYieldPoll = 250 * time.Millisecond

// discoveryRequest is what one round of discovery may do.
type discoveryRequest struct {
	// only names one connector; empty means every enabled one.
	only string
	// force asks every named vendor however recently it answered. A login and
	// `kolk models --refresh` are a person saying "now"; a start is not.
	force bool
	// busy reports that a turn is streaming. When it does, discovery waits
	// rather than competing with the turn for the CPU and the disk. Nil is
	// "nothing is streaming", which is every foreground caller.
	busy func() bool
}

// vendorDiscovery is what one connector's discovery came to.
type vendorDiscovery struct {
	Connector string
	Catalog   provider.VendorCatalog
	// VersionChanged says the vendor's version differs from the last
	// discovery's, so everything a turn had verified was forgotten first.
	VersionChanged bool
	Err            error
}

// lister resolves a connector's ModelLister, through the test seam when one
// is set. Production is the registry.
func (a *app) lister(connector string, gateway []provider.ModelInfo) provider.ModelLister {
	if a.modelLister != nil {
		return a.modelLister(connector, gateway)
	}
	return modelListerFor(connector, gateway)
}

// discoverVendorModels asks every enabled connector — or `only` that one —
// what it offers, and writes what it said. This is the mapping the owner
// asked for on every start and every login; the callers decide whether it
// runs behind the prompt or in front of the user.
//
// A vendor whose version changed since the last discovery is forgotten before
// its fresh catalog lands, so a model verified under the old CLI is
// unverified again under the new one. A vendor that will not answer keeps
// its last catalog and is reported, never blanked: yesterday's list with a
// warning beats no list at all.
func (a *app) discoverVendorModels(ctx context.Context, gateway []provider.ModelInfo, req discoveryRequest) []vendorDiscovery {
	dirs, err := a.resolve()
	if err != nil {
		return []vendorDiscovery{{Connector: req.only, Err: err}}
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return []vendorDiscovery{{Connector: req.only, Err: err}}
	}
	store, err := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		// A corrupt store is reported, and then replaced: discovery is the
		// only thing that can repair it, and refusing to run it would leave
		// the file corrupt forever.
		store = provider.VendorCatalogs{}
	}

	connectors := make([]string, 0, len(manifest.Connectors))
	seen := map[string]bool{}
	for _, connector := range manifest.Connectors {
		name := strings.ToLower(strings.TrimSpace(connector.Name))
		if !connector.Enabled || seen[name] || (req.only != "" && name != strings.ToLower(strings.TrimSpace(req.only))) {
			continue
		}
		seen[name] = true
		connectors = append(connectors, name)
	}
	sort.Strings(connectors)

	now := time.Now
	if a.now != nil {
		now = a.now
	}

	results := make([]vendorDiscovery, 0, len(connectors))
	changed := false
	for _, name := range connectors {
		// A vendor that answered recently is not asked again, and asking is
		// the expensive part: it is a process. Nothing is reported for it
		// either — a catalog that did not change is not news.
		if !req.force && catalogIsFresh(store.Vendors[name], now()) {
			continue
		}
		// The turn owns the machine while it is streaming. This is the only
		// wait in discovery, and a cancelled context ends the round rather
		// than the vendor.
		if !awaitQuietTurn(ctx, req.busy) {
			break
		}
		result := vendorDiscovery{Connector: name}
		lister := a.lister(name, gateway)
		if lister == nil {
			result.Err = fmt.Errorf("%s: no model lister is registered for this connector", name)
			results = append(results, result)
			continue
		}
		vctx, cancel := context.WithTimeout(ctx, vendorDiscoveryTimeout)
		catalog, err := lister.Discover(vctx)
		cancel()
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		if previous, ok := store.Vendors[name]; ok && previous.VendorVersion != "" && catalog.VendorVersion != "" && previous.VendorVersion != catalog.VendorVersion {
			store.Forget(name)
			result.VersionChanged = true
		}
		// A lister that did not say when it fetched is stamped now, so the TTL
		// above has something to measure from. Never overwritten: a lister
		// reporting a cached answer's age knows better than this clock does.
		if catalog.FetchedAt.IsZero() {
			catalog.FetchedAt = now()
		}
		store.Replace(catalog)
		result.Catalog = catalog
		results = append(results, result)
		changed = true
	}
	// SaveVendorCatalogs is a no-op when the document is unchanged, so a round
	// that only re-learned what the file already said leaves its mtime alone.
	if changed {
		if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
			results = append(results, vendorDiscovery{Connector: "vendor catalog", Err: err})
		}
	}
	return results
}

// catalogIsFresh reports that this vendor answered recently enough to be taken
// at its word. A vendor that never answered, or one whose answer carried no
// timestamp, is not fresh: it has nothing to be fresh from.
func catalogIsFresh(catalog provider.VendorCatalog, now time.Time) bool {
	if vendorCatalogTTL <= 0 || catalog.FetchedAt.IsZero() || len(catalog.Models) == 0 {
		return false
	}
	age := now.Sub(catalog.FetchedAt)
	// A catalog stamped in the future is a clock that moved, not a fresh
	// answer; asking again is the cheap way to be sure.
	return age >= 0 && age < vendorCatalogTTL
}

// awaitQuietTurn waits until no turn is streaming, and reports whether the
// caller may go on. A cancelled context ends the wait and the round.
func awaitQuietTurn(ctx context.Context, busy func() bool) bool {
	if ctx.Err() != nil {
		return false
	}
	for busy != nil && busy() {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(vendorDiscoveryYieldPoll):
		}
	}
	return ctx.Err() == nil
}

// refreshVendorCatalogsInBackground is the startup mapping: every enabled
// connector that has not answered inside the TTL, behind the prompt, never on
// the startup path's clock and never while a turn is streaming. The gateway
// catalog it previews from is the one startup already loaded.
//
// A warm start therefore spawns no vendor process at all: the loop above skips
// every fresh vendor before it reaches a lister.
func (a *app) refreshVendorCatalogsInBackground(ctx context.Context, gateway []provider.ModelInfo, busy func() bool) {
	a.startBackground(ctx, func(ctx context.Context) {
		for _, result := range a.discoverVendorModels(ctx, gateway, discoveryRequest{busy: busy}) {
			if result.Err != nil {
				a.debugLog.Printf("vendor discovery: %v", result.Err)
			}
		}
	})
}

// reportVendorDiscovery is the login mapping: this connector, in front of the
// user, with what it found. The gateway preview reads the cached catalog
// without a client — a login is not the moment to reach the network for a
// second vendor.
func (a *app) reportVendorDiscovery(ctx context.Context, connector string) {
	var gateway []provider.ModelInfo
	if dirs, err := a.resolve(); err == nil {
		gateway = provider.CachedCatalog(dirs.CatalogFile())
	}
	// A login is a person saying "now", so it asks whatever the TTL says.
	for _, result := range a.discoverVendorModels(ctx, gateway, discoveryRequest{only: connector, force: true}) {
		fmt.Fprintln(a.stdout, describeVendorDiscovery(result))
	}
}

// describeVendorDiscovery is one line a person can act on.
func describeVendorDiscovery(result vendorDiscovery) string {
	if result.Err != nil {
		return fmt.Sprintf("%s: models could not be listed — %v", result.Connector, result.Err)
	}
	visible := result.Catalog.Visible()
	names := make([]string, 0, len(visible))
	for _, model := range visible {
		names = append(names, model.ID)
	}
	previewed := 0
	for _, model := range result.Catalog.Models {
		if model.Status == provider.StatusUnverified {
			previewed++
		}
	}
	version := ""
	if result.Catalog.VendorVersion != "" {
		version = " " + result.Catalog.VendorVersion
	}
	changed := ""
	if result.VersionChanged {
		changed = "; the vendor's version changed, so earlier verifications were forgotten"
	}
	if previewed == len(result.Catalog.Models) && previewed > 0 {
		return fmt.Sprintf("%s%s: %d models previewed from the gateway, unverified until the first prompt: %s%s — `/models` shows them",
			result.Connector, version, len(visible), strings.Join(names, ", "), changed)
	}
	return fmt.Sprintf("%s%s: %d models listed by %s: %s%s — `/models` shows them",
		result.Connector, version, len(visible), result.Catalog.Source, strings.Join(names, ", "), changed)
}

// vendorCatalogs is the vendor catalog file as it stands. A missing or
// unreadable file is an empty store: every caller falls back to the seed and
// says so through Status, never by refusing to answer.
func (a *app) vendorCatalogs() provider.VendorCatalogs {
	dirs, err := a.resolve()
	if err != nil {
		return provider.VendorCatalogs{}
	}
	store, err := provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		return provider.VendorCatalogs{}
	}
	return store
}

// vendorKnowsModel is availability as the vendor states it. When the vendor
// has been asked, its catalog is the answer — a row that is listed, verified,
// or previewed is spawnable and a row that is gone is not. When it has not
// been asked, the adapter's seed answers, which is what happened before
// discovery existed.
func (a *app) vendorKnowsModel(store provider.VendorCatalogs, vendor, model string) bool {
	if catalog, ok := store.Vendors[strings.ToLower(vendor)]; ok {
		row, found := catalog.Find(model)
		return found && row.Status != provider.StatusGone
	}
	switch strings.ToLower(vendor) {
	case "claude":
		return agentcli.ClaudeKnowsModel(model)
	case "codex":
		return agentcli.CodexKnowsModel(model)
	case "copilot":
		return agentcli.CopilotKnowsModel(model)
	default:
		return false
	}
}

// discoveredEfforts is the effort set the vendor listed for a model, or nil
// when the vendor has not been asked, so the adapter's seed set applies.
func (a *app) discoveredEfforts(store provider.VendorCatalogs, vendor, model string) []string {
	catalog, ok := store.Vendors[strings.ToLower(vendor)]
	if !ok {
		return nil
	}
	row, found := catalog.Find(model)
	if !found {
		return nil
	}
	return append([]string(nil), row.Efforts...)
}

// planModels and resolvePlanModel are the plan catalog as the vendors
// describe it; every surface reads through these, never the bare seed.
func (a *app) planModels(filter string) []provider.PlanModel {
	return provider.PlanModelsFrom(a.vendorCatalogs(), filter)
}

func (a *app) resolvePlanModel(ref string, manifest provider.ConnectorManifest) (provider.PlanModel, error) {
	return provider.ResolvePlanModelFrom(a.vendorCatalogs(), ref, manifest)
}

// noteUnknownAskedOnce says, once per (vendor, model), that a vendor answered
// a request for a model it does not list. It names what actually answered.
func (a *app) noteUnknownAskedOnce(vendor, asked, answered string) {
	key := strings.ToLower(vendor) + "\x00" + strings.ToLower(strings.TrimSpace(asked))
	if a.unknownAskedNoted == nil {
		a.unknownAskedNoted = map[string]bool{}
	}
	if a.unknownAskedNoted[key] {
		return
	}
	a.unknownAskedNoted[key] = true
	if answered == "" {
		answered = "its own model"
	}
	fmt.Fprintf(a.stderr, "note: %s was asked for %s, which it does not list, and answered on %s; not recorded as a %s model\n",
		vendor, asked, answered, vendor)
}
