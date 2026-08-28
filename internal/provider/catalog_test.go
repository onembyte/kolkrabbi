package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestCatalogCacheHitAvoidsNetwork(t *testing.T) {
	var networkHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&networkHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []provider.ModelInfo{
				{ID: "network/model", ContextLength: 128000},
			},
		})
	}))
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "models.json")

	// 1. Pre-seed cache file with timestamp = now
	seed := provider.CatalogCache{
		Version:  1,
		CachedAt: time.Now(),
		Models: []provider.ModelInfo{
			{ID: "cached/model", ContextLength: 64000},
		},
	}
	body, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Fetch with TTL 1 hour -> should hit cache, 0 network hits
	models, err := client.ListModelsCached(context.Background(), cacheFile, time.Hour, false)
	if err != nil {
		t.Fatalf("ListModelsCached: %v", err)
	}
	if len(models) != 1 || models[0].ID != "cached/model" {
		t.Fatalf("expected cached/model, got %#v", models)
	}
	if hits := atomic.LoadInt32(&networkHits); hits != 0 {
		t.Errorf("network hits = %d, want 0 on cache hit", hits)
	}
}

func TestCatalogCacheMissFetchesAndSaves(t *testing.T) {
	var networkHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&networkHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []provider.ModelInfo{
				{ID: "fetched/model", ContextLength: 200000},
			},
		})
	}))
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "models.json")

	// 1. Initial call -> cache miss, fetches from server and saves to disk
	models, err := client.ListModelsCached(context.Background(), cacheFile, time.Hour, false)
	if err != nil {
		t.Fatalf("ListModelsCached: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fetched/model" {
		t.Fatalf("expected fetched/model, got %#v", models)
	}
	if hits := atomic.LoadInt32(&networkHits); hits != 1 {
		t.Errorf("network hits = %d, want 1", hits)
	}

	// 2. Verify cache file was written
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("cache file was not written: %v", err)
	}

	// 3. Second call -> cache hit, 0 additional network hits
	models2, err := client.ListModelsCached(context.Background(), cacheFile, time.Hour, false)
	if err != nil {
		t.Fatalf("ListModelsCached 2nd: %v", err)
	}
	if len(models2) != 1 || models2[0].ID != "fetched/model" {
		t.Fatalf("expected fetched/model, got %#v", models2)
	}
	if hits := atomic.LoadInt32(&networkHits); hits != 1 {
		t.Errorf("network hits = %d, want still 1", hits)
	}
}

func TestCatalogStaleFallbackOnNetworkError(t *testing.T) {
	// Server returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "models.json")

	// Pre-seed expired cache (2 hours old)
	seed := provider.CatalogCache{
		Version:  1,
		CachedAt: time.Now().Add(-2 * time.Hour),
		Models: []provider.ModelInfo{
			{ID: "stale/fallback-model", ContextLength: 32000},
		},
	}
	body, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, body, 0o600); err != nil {
		t.Fatal(err)
	}

	// TTL is 1 hour, so cache is expired, network fails -> falls back to stale cache
	models, err := client.ListModelsCached(context.Background(), cacheFile, time.Hour, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "stale/fallback-model" {
		t.Fatalf("expected stale/fallback-model, got %#v", models)
	}
}

func TestCatalogForceRefreshBypassesCache(t *testing.T) {
	var networkHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&networkHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []provider.ModelInfo{
				{ID: "fresh/network-model", ContextLength: 128000},
			},
		})
	}))
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "models.json")

	// Pre-seed cache with timestamp = now
	seed := provider.CatalogCache{
		Version:  1,
		CachedAt: time.Now(),
		Models: []provider.ModelInfo{
			{ID: "cached/model", ContextLength: 64000},
		},
	}
	body, _ := json.Marshal(seed)
	_ = os.WriteFile(cacheFile, body, 0o600)

	// forceRefresh = true
	models, err := client.ListModelsCached(context.Background(), cacheFile, time.Hour, true)
	if err != nil {
		t.Fatalf("ListModelsCached force: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fresh/network-model" {
		t.Fatalf("expected fresh/network-model, got %#v", models)
	}
	if hits := atomic.LoadInt32(&networkHits); hits != 1 {
		t.Errorf("network hits = %d, want 1", hits)
	}
}

func snapshotServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []provider.ModelInfo{{ID: "network/model", ContextLength: 128000}},
		})
	}))
}

func TestCatalogSnapshotServesAStaleCacheWithoutTheNetwork(t *testing.T) {
	var hits int32
	srv := snapshotServer(t, &hits)
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	cacheFile := filepath.Join(t.TempDir(), "models.json")
	seed := provider.CatalogCache{
		Version: 1, CachedAt: time.Now().Add(-2 * time.Hour),
		Models: []provider.ModelInfo{{ID: "cached/model", ContextLength: 64000}},
	}
	body, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, body, 0o600); err != nil {
		t.Fatal(err)
	}

	models, stale, err := client.CatalogSnapshot(context.Background(), cacheFile, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("a two-hour-old cache under a one-hour TTL must report stale")
	}
	if len(models) != 1 || models[0].ID != "cached/model" {
		t.Fatalf("models = %#v, want the stale cache served as-is", models)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("network hits = %d; a stale cache must be served without a request", n)
	}

	// The refresh is the caller's decision, off the startup path.
	if err := client.RefreshCatalog(context.Background(), cacheFile); err != nil {
		t.Fatal(err)
	}
	models, stale, err = client.CatalogSnapshot(context.Background(), cacheFile, time.Hour)
	if err != nil || stale || len(models) != 1 || models[0].ID != "network/model" {
		t.Fatalf("after refresh: models=%#v stale=%v err=%v", models, stale, err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("network hits = %d, want exactly the one refresh", n)
	}
}

func TestCatalogSnapshotFetchesOnlyWhenNothingIsCached(t *testing.T) {
	var hits int32
	srv := snapshotServer(t, &hits)
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	cacheFile := filepath.Join(t.TempDir(), "models.json")

	models, stale, err := client.CatalogSnapshot(context.Background(), cacheFile, time.Hour)
	if err != nil || stale || len(models) != 1 || models[0].ID != "network/model" {
		t.Fatalf("first run: models=%#v stale=%v err=%v", models, stale, err)
	}
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("the first fetch must write the cache so the next start is instant: %v", err)
	}
	if _, _, err := client.CatalogSnapshot(context.Background(), cacheFile, time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("network hits = %d, want 1: the second start must be a cache hit", n)
	}
}

func TestCatalogSnapshotReportsAnOutageWhenNothingIsCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	if _, _, err := client.CatalogSnapshot(context.Background(), filepath.Join(t.TempDir(), "models.json"), time.Hour); err == nil {
		t.Fatal("no cache and a 503 must surface an error so the caller falls back to the seed")
	}
}
