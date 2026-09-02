package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runModels(ctx context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}
	endpoint := config.ResolveBaseURL("", cfg)
	client, err := providerClientForEndpoint(ctx, endpoint, d.CredentialsFile())
	if err != nil {
		return err
	}

	forceRefresh := false
	var filterArgs []string
	for _, arg := range args {
		if arg == "--refresh" {
			forceRefresh = true
		} else {
			filterArgs = append(filterArgs, arg)
		}
	}

	filter := strings.Join(filterArgs, " ")
	if err := a.printModelCatalog(ctx, client, d.CatalogFile(), forceRefresh, filter); err != nil {
		return err
	}
	// --refresh asks every signed-in vendor before the sections print, not
	// after: a refresh that renders the previous catalog and then says it
	// fetched a new one shows the user the wrong list and tells them it is
	// current. Found by running it (F4.7).
	var discovered []vendorDiscovery
	if forceRefresh {
		discovered = a.discoverVendorModels(ctx, provider.CachedCatalog(d.CatalogFile()), "")
	}
	a.printVendorModels(filter)
	for _, result := range discovered {
		if result.Err != nil {
			fmt.Fprintln(a.stdout, describeVendorDiscovery(result))
		}
	}
	a.printHostModels(ctx, d.HostCatalogFile(), filter)
	return nil
}

// printHostModels lists what the user's own Ollama serves, below the gateway
// rows and never mixed into them: a host id in the gateway list is a 404
// waiting to happen, and a reader should see at a glance which rows cost
// nothing because they never leave the machine.
// printVendorModels is what each signed-in vendor said it offers, in its own
// section: these are not gateway rows and must not be mixed into them — one
// is billed per token and the other is a subscription, and a list that blurs
// that is a list that costs someone money.
func (a *app) printVendorModels(filter string) {
	store := a.vendorCatalogs()
	if len(store.Vendors) == 0 {
		return
	}
	names := make([]string, 0, len(store.Vendors))
	for name := range store.Vendors {
		names = append(names, name)
	}
	sort.Strings(names)
	filter = strings.ToLower(strings.TrimSpace(filter))
	for _, name := range names {
		catalog := store.Vendors[name]
		version := ""
		if catalog.VendorVersion != "" {
			version = " " + catalog.VendorVersion
		}
		rows := make([]provider.DiscoveredModel, 0, len(catalog.Models))
		for _, model := range catalog.Visible() {
			if filter != "" && !strings.Contains(strings.ToLower(model.ID), filter) &&
				!strings.Contains(strings.ToLower(name), filter) {
				continue
			}
			rows = append(rows, model)
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(a.stdout, "\nsubscription · %s%s — %s, %s\n", name, version, catalog.Source, ageLabel(catalog.FetchedAt, time.Now()))
		for _, model := range rows {
			note := statusNote(model.Status)
			if note != "" {
				note = "  (" + note + ")"
			}
			exact := ""
			if len(model.ExactIDs) > 0 {
				exact = "  → " + model.ExactIDs[0]
			}
			fmt.Fprintf(a.stdout, "%-28s ctx %-7s efforts %-28s%s%s\n",
				model.ID, contextWindowLabel(model.Context), effortsLabel(model.Efforts), exact, note)
		}
	}
}

func (a *app) printHostModels(ctx context.Context, cacheFile, filter string) {
	host := a.discoverHost(ctx)
	switch host.State {
	case local.HostInstalled:
		fmt.Fprintf(a.stdout, "\nlocal · ollama at %s is installed but not running; its models are listed here once it runs, and `/model` can still pick a pulled one and start it\n", host.Binary)
		return
	case local.HostAbsent:
		fmt.Fprintf(a.stdout, "\nlocal · ollama is not installed; install it with: %s\n", host.InstallHint())
		return
	}
	models, err := a.listHostModels(ctx, host.Addr, cacheFile)
	if err != nil {
		fmt.Fprintf(a.stdout, "\nlocal · ollama %s at %s answered, but its model list did not: %v\n", host.Version, host.Addr, err)
		return
	}
	models = mergeHostModels(models, a.cloudHostModels(ctx, host, cacheFile))
	renderHostModels(a.stdout, host, models, filter)
}

func renderHostModels(out io.Writer, host local.Host, models []local.HostModel, filter string) {
	fmt.Fprintf(out, "\nlocal · ollama %s at %s\n", host.Version, host.Addr)
	if len(models) == 0 {
		fmt.Fprintln(out, "  nothing pulled yet; `ollama pull <model>` adds one")
		return
	}
	filter = strings.ToLower(filter)
	for _, m := range models {
		info := m.ModelInfo()
		if filter != "" && !strings.Contains(strings.ToLower(info.ID), filter) {
			continue
		}
		description := info.Description
		if m.NotPulled {
			if description != "" {
				description += " · "
			}
			description += "not pulled: ollama pull " + m.Name
		}
		fmt.Fprintf(out, "%-48s ctx %-9d %s\n", info.ID, info.ContextLength, description)
	}
}

func (a *app) printModelCatalog(ctx context.Context, client *provider.Client, catalogFile string, forceRefresh bool, filter string) error {
	if client == nil {
		return fmt.Errorf("model provider is not configured")
	}
	models, err := client.ListModelsCached(ctx, catalogFile, provider.DefaultCatalogTTL, forceRefresh)
	if err != nil {
		return err
	}
	renderModelCatalog(a.stdout, models, filter)
	return nil
}

func renderModelCatalog(out io.Writer, models []provider.ModelInfo, filter string) {
	filter = strings.ToLower(filter)
	sort.Slice(models, func(i, j int) bool {
		iFree := provider.ModelIsFree(models[i])
		jFree := provider.ModelIsFree(models[j])
		if iFree != jFree {
			return iFree
		}
		return models[i].ID < models[j].ID
	})
	for _, m := range models {
		if filter != "" &&
			!strings.Contains(strings.ToLower(m.ID), filter) &&
			!strings.Contains(strings.ToLower(m.Name), filter) {
			continue
		}
		fmt.Fprintf(out, "%-48s ctx %-9d %s\n",
			m.ID, m.ContextLength, formatPricing(m.Pricing.Prompt, m.Pricing.Completion))
	}
}

// formatPricing converts OpenRouter's per-token USD strings to $/1M tokens.
func formatPricing(promptPerTok, completionPerTok string) string {
	in, err1 := strconv.ParseFloat(promptPerTok, 64)
	out, err2 := strconv.ParseFloat(completionPerTok, 64)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("in %s / out %s per token", promptPerTok, completionPerTok)
	}
	if in == 0 && out == 0 {
		return "free"
	}
	return fmt.Sprintf("$%.2f in / $%.2f out per 1M tokens", in*1e6, out*1e6)
}
