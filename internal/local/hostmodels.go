package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// HostPrefix is the route the engine owns for host models: ollama/<name>.
const HostPrefix = "ollama/"

// hostListBudget bounds the whole listing. A server that is up but wedged
// must not hold the picker hostage; a partial answer is reported as partial.
const hostListBudget = 5 * time.Second

// capabilitiesSince is the first Ollama whose /api/show says what a model can
// do. Before it nothing can be claimed, and a model with no claim gets no tool
// schemas — a ranker that guessed here would send tools to a model that 400s.
var capabilitiesSince = [3]int{0, 6, 4}

// HostModel is one model the user's own Ollama serves, with what the server
// itself says about it. Nothing here is inferred from the name.
type HostModel struct {
	Name         string `json:"name"`
	Digest       string `json:"digest"`
	Size         uint64 `json:"size"`
	Family       string `json:"family,omitempty"`
	Parameters   string `json:"parameters,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	// ContextLength is the model's trained window, from /api/show. The
	// server's effective window can be smaller; E8 reads that from /api/ps.
	ContextLength int `json:"context_length,omitempty"`
	// CapabilitiesKnown is false when the server could not say — too old, or
	// /api/show failed for this model. Tools is then false and means "not
	// claimed", never "absent".
	CapabilitiesKnown bool `json:"capabilities_known"`
	Tools             bool `json:"tools"`
	Vision            bool `json:"vision"`
	Thinking          bool `json:"thinking"`
	// Cloud models are served by ollama.com through the local server; the
	// local server signs the request with its own key.
	Cloud      bool   `json:"cloud"`
	RemoteHost string `json:"remote_host,omitempty"`
	// NotPulled marks a catalogued model the picker offers with its pull
	// command; the server has never seen it. Never set by the decoder.
	NotPulled bool `json:"-"`
}

// ModelInfo projects a host model into the shape the rest of kolk ranks and
// prints. The id carries the route prefix so the engine sends it to the
// server with the prefix stripped, and never to the gateway.
func (m HostModel) ModelInfo() provider.ModelInfo {
	info := provider.ModelInfo{
		ID:            HostPrefix + m.Name,
		Name:          m.Name,
		ContextLength: m.ContextLength,
	}
	var parts []string
	if m.Cloud {
		parts = append(parts, "cloud · via ollama.com")
		// Billed against the Ollama plan, not per token: pricing left blank
		// so nothing reads it as free. E7 gives it a cost class of its own.
	} else {
		parts = append(parts, "local")
		info.Pricing.Prompt, info.Pricing.Completion = "0", "0"
	}
	if m.Parameters != "" {
		parts = append(parts, m.Parameters)
	}
	if m.Quantization != "" {
		parts = append(parts, m.Quantization)
	}
	if !m.CapabilitiesKnown {
		parts = append(parts, "capabilities unknown")
	}
	info.Description = strings.Join(parts, " · ")
	if m.Tools {
		info.SupportedParameters = append(info.SupportedParameters, "tools")
	}
	return info
}

// hostCatalogCache is what one listing left behind: the server version it was
// read from and every model's show answer, keyed by digest. The tags list is
// always fetched fresh — it is one request — so a pulled or removed model is
// seen immediately; only the per-model detail is remembered.
type hostCatalogCache struct {
	Version string               `json:"version"`
	Models  map[string]HostModel `json:"models"` // digest → detail
}

// ListHostModels reads what the server at addr serves. One /api/tags, then
// one /api/show per model not already cached by digest at cacheFile.
//
// A model whose show fails is still listed, with its capabilities unknown: the
// user pulled it and can see it in `ollama list`, and a listing that dropped it
// would be a listing they cannot trust.
func ListHostModels(ctx context.Context, addr, cacheFile string) ([]HostModel, error) {
	ctx, cancel := context.WithTimeout(ctx, hostListBudget)
	defer cancel()
	client := &http.Client{Timeout: hostListBudget}
	base := "http://" + addr

	version, _ := probeHost(ctx, addr)
	cache := loadHostCache(cacheFile)
	if cache.Version != version {
		// A new server may answer differently for the same digest.
		cache = hostCatalogCache{Version: version, Models: map[string]HostModel{}}
	}

	tags, ok := hostGet(ctx, client, base+"/api/tags")
	if !ok {
		return nil, fmt.Errorf("ollama at %s did not answer /api/tags", addr)
	}
	var listed struct {
		Models []struct {
			Name       string `json:"name"`
			Size       uint64 `json:"size"`
			Digest     string `json:"digest"`
			RemoteHost string `json:"remote_host"`
			Details    struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
				ContextLength int    `json:"context_length"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(tags, &listed); err != nil {
		return nil, fmt.Errorf("ollama at %s answered /api/tags with something that is not a model list: %w", addr, err)
	}

	canSay := versionAtLeast(version, capabilitiesSince)
	models := make([]HostModel, 0, len(listed.Models))
	changed := false
	for _, entry := range listed.Models {
		if cached, hit := cache.Models[entry.Digest]; hit && entry.Digest != "" {
			models = append(models, cached)
			continue
		}
		model := HostModel{
			Name:          entry.Name,
			Digest:        entry.Digest,
			Size:          entry.Size,
			Family:        entry.Details.Family,
			Parameters:    entry.Details.ParameterSize,
			Quantization:  entry.Details.Quantization,
			ContextLength: entry.Details.ContextLength,
			Cloud:         entry.RemoteHost != "",
			RemoteHost:    entry.RemoteHost,
		}
		if shown, ok := showHostModel(ctx, client, base, entry.Name); ok {
			model.ContextLength = firstNonZero(shown.contextLength, model.ContextLength)
			if canSay {
				model.CapabilitiesKnown = shown.capabilitiesPresent
				model.Tools, model.Vision, model.Thinking = shown.tools, shown.vision, shown.thinking
			}
			if entry.Digest != "" {
				cache.Models[entry.Digest] = model
				changed = true
			}
		}
		models = append(models, model)
	}
	if changed && cacheFile != "" {
		saveHostCache(cacheFile, cache)
	}
	return models, nil
}

type shownModel struct {
	contextLength       int
	capabilitiesPresent bool
	tools, vision       bool
	thinking            bool
	remote              bool
}

// showHostModel asks /api/show for one model. Context comes from
// details.context_length when the server fills it, else from
// model_info["<architecture>.context_length"], which is where older servers
// and cloud models put it.
func showHostModel(ctx context.Context, client *http.Client, base, name string) (shownModel, bool) {
	body, _ := json.Marshal(map[string]string{"model": name})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/show", bytes.NewReader(body))
	if err != nil {
		return shownModel{}, false
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return shownModel{}, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return shownModel{}, false
	}
	var reply struct {
		Capabilities []string `json:"capabilities"`
		RemoteHost   string   `json:"remote_host"`
		Details      struct {
			ContextLength int `json:"context_length"`
		} `json:"details"`
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return shownModel{}, false
	}
	shown := shownModel{contextLength: reply.Details.ContextLength, capabilitiesPresent: reply.Capabilities != nil, remote: reply.RemoteHost != ""}
	for _, capability := range reply.Capabilities {
		switch strings.ToLower(capability) {
		case "tools":
			shown.tools = true
		case "vision":
			shown.vision = true
		case "thinking":
			shown.thinking = true
		}
	}
	if shown.contextLength == 0 {
		if arch, ok := reply.ModelInfo["general.architecture"]; ok {
			var name string
			if json.Unmarshal(arch, &name) == nil {
				if raw, ok := reply.ModelInfo[name+".context_length"]; ok {
					var n int
					if json.Unmarshal(raw, &n) == nil {
						shown.contextLength = n
					}
				}
			}
		}
	}
	return shown, true
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// versionAtLeast compares a "major.minor.patch" string against a floor. An
// unparseable version is below every floor: a server that cannot say what it
// is cannot be trusted to say what its models can do.
func versionAtLeast(version string, floor [3]int) bool {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".", 3)
	if len(parts) != 3 {
		return false
	}
	for i, part := range parts {
		// A pre-release suffix ("0.9.0-rc1") is the release for this purpose.
		if dash := strings.IndexByte(part, '-'); dash >= 0 {
			part = part[:dash]
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		if n != floor[i] {
			return n > floor[i]
		}
	}
	return true
}

func loadHostCache(file string) hostCatalogCache {
	empty := hostCatalogCache{Models: map[string]HostModel{}}
	if file == "" {
		return empty
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return empty
	}
	var cache hostCatalogCache
	if json.Unmarshal(b, &cache) != nil || cache.Models == nil {
		return empty
	}
	return cache
}

// saveHostCache is best-effort: a cache that cannot be written costs the next
// startup a few requests, which is not worth failing this one over.
func saveHostCache(file string, cache hostCatalogCache) {
	b, err := json.Marshal(cache)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(file), 0o700)
	_ = atomicfile.Write(file, b, 0o600)
}
