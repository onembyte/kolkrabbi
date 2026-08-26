package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// DefaultCatalogTTL is 1 hour per docs/plan/08-model-routing.md §1.1.
const DefaultCatalogTTL = 1 * time.Hour

// CatalogCache represents the on-disk format of cached models.
type CatalogCache struct {
	Version  int         `json:"version"`
	CachedAt time.Time   `json:"cached_at"`
	Models   []ModelInfo `json:"models"`
}

// FallbackCatalogSeed returns a minimal list of verified models used when
// the machine is completely offline on first launch.
func FallbackCatalogSeed() []ModelInfo {
	return []ModelInfo{
		{ID: "openrouter/auto", Name: "Auto Router", ContextLength: 128000},
		{ID: "openrouter/free", Name: "Free Router", ContextLength: 128000},
		{ID: "anthropic/claude-3-7-sonnet", Name: "Claude 3.7 Sonnet", ContextLength: 200000},
		{ID: "google/gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextLength: 1048576},
		{ID: "meta-llama/llama-3.3-70b-instruct:free", Name: "Llama 3.3 70B (free)", ContextLength: 131072},
	}
}

// ListModelsCached returns the model catalog, checking the on-disk cache at path
// before performing a network request. If the cache is valid and not expired,
// it is returned immediately without contacting the provider.
func (c *Client) ListModelsCached(ctx context.Context, path string, ttl time.Duration, forceRefresh bool) ([]ModelInfo, error) {
	if ttl <= 0 {
		ttl = DefaultCatalogTTL
	}

	var staleCache *CatalogCache

	// 1. If not forcing refresh, check existing cache
	if !forceRefresh && path != "" {
		if cached, err := loadCatalog(path); err == nil {
			if time.Since(cached.CachedAt) <= ttl && len(cached.Models) > 0 {
				return cached.Models, nil
			}
			staleCache = cached
		}
	}

	// 2. Fetch fresh catalog from network
	models, err := c.ListModels(ctx)
	if err == nil && len(models) > 0 {
		if path != "" {
			_ = saveCatalog(path, models)
		}
		return models, nil
	}

	// 3. Network failed: fall back to stale cache if available
	if staleCache != nil && len(staleCache.Models) > 0 {
		return staleCache.Models, nil
	}

	// 4. Try reading cache file anyway in case forceRefresh was set but network failed
	if path != "" {
		if cached, cErr := loadCatalog(path); cErr == nil && len(cached.Models) > 0 {
			return cached.Models, nil
		}
	}

	return nil, err
}

func loadCatalog(path string) (*CatalogCache, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cache CatalogCache
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveCatalog(path string, models []ModelInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	cache := CatalogCache{
		Version:  1,
		CachedAt: time.Now(),
		Models:   models,
	}
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o600)
}
