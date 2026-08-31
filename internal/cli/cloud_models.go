package cli

import (
	"context"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/local"
)

// cloudHostModels is optional discovery layered over the host's own rows. A
// public-catalogue or local-proxy failure is deliberately silent here: the
// caller already has the authoritative pulled rows, and Cloud discovery is a
// supplement rather than a reason to hide them.
func (a *app) cloudHostModels(ctx context.Context, host local.Host, cacheFile string) []local.HostModel {
	if host.State != local.HostRunning || a.listCloudCatalog == nil || a.listCloudModels == nil {
		return nil
	}
	catalog, err := a.listCloudCatalog(ctx)
	if err != nil {
		return nil
	}
	models, _ := a.listCloudModels(ctx, host.Addr, host.Version, cacheFile, catalog)
	for index := range models {
		// The Cloud function's contract is catalogue discovery, so every row it
		// contributes is absent from the local tags list until merge proves
		// otherwise. Keeping this invariant at the app boundary also protects
		// alternate implementations of the test seam.
		models[index].NotPulled = true
	}
	return models
}

// mergeHostModels preserves the first row for a model name. Pulled host rows
// are passed first, so an unpulled public Cloud row cannot overwrite the
// authoritative local record or produce duplicate picker/command rows.
func mergeHostModels(groups ...[]local.HostModel) []local.HostModel {
	count := 0
	for _, group := range groups {
		count += len(group)
	}
	out := make([]local.HostModel, 0, count)
	seen := make(map[string]struct{}, count)
	for _, group := range groups {
		for _, model := range group {
			name := strings.TrimSpace(model.Name)
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, model)
		}
	}
	return out
}
