package local

import (
	"context"
	"errors"
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

// ErrNotCloudModel is the server's own answer that a name is not a Cloud
// model: it knows no such model, or knows it without a remote host.
var ErrNotCloudModel = errors.New("not an Ollama Cloud model")

// ErrCloudSignedOut is a server that refused the question because it is not
// signed in to ollama.com.
var ErrCloudSignedOut = errors.New("the Ollama server is signed out of ollama.com")

// CloudModelRemoteHost asks the server at addr where name runs, with the same
// proof ListCloudModels requires: an /api/show answer naming a remote host.
// Only ErrNotCloudModel is a verdict about name. A cancelled caller gets its
// context's error, and any other failure means the server could not be asked.
func CloudModelRemoteHost(ctx context.Context, addr, name string) (string, error) {
	check, cancel := context.WithTimeout(ctx, cloudEnrichmentBudget)
	defer cancel()
	client := &http.Client{Timeout: cloudEnrichmentBudget}
	shown, err := requestShowHostModel(check, client, "http://"+addr, name)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	var status *showStatusError
	switch {
	case err == nil && shown.remote:
		return shown.remoteHost, nil
	case err == nil, errors.As(err, &status) && status.status == http.StatusNotFound:
		return "", ErrNotCloudModel
	case errors.As(err, &status) && status.status == http.StatusUnauthorized:
		return "", ErrCloudSignedOut
	}
	return "", fmt.Errorf("asking ollama at %s about %s: %w", addr, name, err)
}

// IsCloudModelName reports whether name already selects an Ollama Cloud
// source: a :cloud tag, or an explicit tag ending in -cloud.
func IsCloudModelName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && cloudModelAlias(name) == name
}
