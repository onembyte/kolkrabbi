package cli

import (
	"context"
	"fmt"
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
		return provider.NotListable{Vendor: "copilot", Reason: "the copilot CLI has no catalog command and the gateway carries no github/ prefix"}
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
