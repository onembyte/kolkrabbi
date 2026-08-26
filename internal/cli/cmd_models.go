package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
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
	// T0.3 replaces this temporary environment-only source with the complete
	// credential chain. Config itself must never regain a credential field.
	client := provider.NewClient(os.Getenv("OPENROUTER_API_KEY"))
	client.BaseURL = config.ResolveBaseURL("", cfg)

	forceRefresh := false
	var filterArgs []string
	for _, arg := range args {
		if arg == "--refresh" {
			forceRefresh = true
		} else {
			filterArgs = append(filterArgs, arg)
		}
	}

	return a.printModelCatalog(ctx, client, d.CatalogFile(), forceRefresh, strings.Join(filterArgs, " "))
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
