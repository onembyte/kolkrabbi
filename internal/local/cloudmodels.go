package local

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const cloudEnrichmentBudget = hostListBudget

// ListCloudModels resolves public Cloud catalogue entries through the user's
// local Ollama. The public entry supplies display metadata; only a successful
// local /api/show response with a remote host proves that the local server
// understands the Cloud model. Every returned row is marked NotPulled because
// pulled rows come from ListHostModels and are merged by the CLI.
func ListCloudModels(ctx context.Context, addr, version, cacheFile string, catalog []CloudCatalogModel) ([]HostModel, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("ollama cloud enrichment needs a server address")
	}
	if len(catalog) > cloudCatalogMaxRows {
		return nil, fmt.Errorf("ollama cloud catalogue has %d candidates; limit is %d", len(catalog), cloudCatalogMaxRows)
	}

	ctx, cancel := context.WithTimeout(ctx, cloudEnrichmentBudget)
	defer cancel()
	client := &http.Client{Timeout: cloudEnrichmentBudget}
	cache := loadHostCache(cacheFile)
	if cache.Version != version {
		cache = hostCatalogCache{Version: version, Models: map[string]HostModel{}}
	}

	models := make([]HostModel, 0, len(catalog))
	changed := false
	seenAliases := make(map[string]struct{}, len(catalog))
	for index, entry := range catalog {
		name, err := boundedCloudCatalogName(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("cloud candidate %d: %w", index+1, err)
		}
		alias := cloudModelAlias(name)
		if _, seen := seenAliases[alias]; seen {
			continue
		}
		seenAliases[alias] = struct{}{}
		if entry.Digest != "" {
			if cached, ok := cache.Models[entry.Digest]; ok && cached.Name == alias && cached.Cloud && cached.RemoteHost != "" {
				cached.NotPulled = true
				models = append(models, cached)
				continue
			}
		}

		shown, ok := showHostModel(ctx, client, "http://"+addr, alias)
		if !ok || !shown.remote {
			continue
		}
		model := HostModel{
			Name: alias, Digest: entry.Digest, Size: entry.Size,
			Family: entry.Family, Parameters: entry.Parameters,
			Quantization: entry.Quantization, ContextLength: shown.contextLength,
			CapabilitiesKnown: shown.capabilitiesPresent, Tools: shown.tools,
			Vision: shown.vision, Thinking: shown.thinking, Cloud: true,
			RemoteHost: shown.remoteHost, NotPulled: true,
		}
		if entry.Digest != "" {
			cache.Models[entry.Digest] = model
			changed = true
		}
		models = append(models, model)
	}
	if err := ctx.Err(); err != nil {
		return models, err
	}
	if changed && cacheFile != "" {
		saveHostCache(cacheFile, cache)
	}
	return models, nil
}

// cloudModelAlias mirrors Ollama's source-selector normalization. A direct
// name with no explicit tag uses :cloud; an explicit tag uses -cloud. Already
// normalized source selectors are kept stable so a future public catalogue can
// safely return one without producing a doubled suffix.
func cloudModelAlias(name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ":cloud") {
		return name
	}
	lastSlash := strings.LastIndex(name, "/")
	lastColon := strings.LastIndex(name, ":")
	if lastColon > lastSlash {
		suffix := name[lastColon+1:]
		if strings.HasSuffix(strings.ToLower(suffix), "-cloud") {
			return name
		}
		return name + "-cloud"
	}
	return name + ":cloud"
}
