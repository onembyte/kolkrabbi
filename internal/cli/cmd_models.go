package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runModels(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := provider.NewClient(config.ResolveAPIKey(cfg))
	client.BaseURL = config.ResolveBaseURL("", cfg)

	models, err := client.ListModels(ctx)
	if err != nil {
		return err
	}

	filter := strings.ToLower(strings.Join(args, " "))
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	for _, m := range models {
		if filter != "" &&
			!strings.Contains(strings.ToLower(m.ID), filter) &&
			!strings.Contains(strings.ToLower(m.Name), filter) {
			continue
		}
		fmt.Fprintf(a.stdout, "%-48s ctx %-9d %s\n",
			m.ID, m.ContextLength, formatPricing(m.Pricing.Prompt, m.Pricing.Completion))
	}
	return nil
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
