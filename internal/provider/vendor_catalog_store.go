package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// The vendor catalog file: what every vendor offered when it was last asked,
// and what the session has since learned by using it.
//
// Rebuildable, so it lives in Cache beside the gateway catalog. It is written
// at two moments — a discovery (start, login, refresh) replaces a vendor's
// rows; a turn promotes one row to verified or marks it gone — and read by
// every model command, which shows nothing it does not contain.

const vendorCatalogVersion = 1

// VendorCatalogs is the file's shape: one catalog per vendor.
type VendorCatalogs struct {
	Version int                      `json:"version"`
	Vendors map[string]VendorCatalog `json:"vendors"`
}

// LoadVendorCatalogs reads the file. A missing file is an empty store, not an
// error; a corrupt one is an error, because silently starting over would
// forget every verification a turn ever earned.
func LoadVendorCatalogs(path string) (VendorCatalogs, error) {
	store := VendorCatalogs{Version: vendorCatalogVersion, Vendors: map[string]VendorCatalog{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return VendorCatalogs{}, fmt.Errorf("vendor catalog: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return VendorCatalogs{}, fmt.Errorf("vendor catalog: parse %s: %w", path, err)
	}
	if store.Vendors == nil {
		store.Vendors = map[string]VendorCatalog{}
	}
	store.Version = vendorCatalogVersion
	return store, nil
}

// SaveVendorCatalogs writes the file atomically.
func SaveVendorCatalogs(path string, store VendorCatalogs) error {
	store.Version = vendorCatalogVersion
	if store.Vendors == nil {
		store.Vendors = map[string]VendorCatalog{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("vendor catalog: encode: %w", err)
	}
	// The cache directory is created here rather than assumed: the first
	// thing that writes this file may be a turn on a fresh machine, before
	// anything else has made the cache.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("vendor catalog: create cache directory: %w", err)
	}
	return atomicfile.Write(path, append(data, '\n'), 0o600)
}

// Replace installs a fresh discovery for one vendor, carrying forward what
// a turn had proved: a row the new listing still names keeps `verified` and
// its exact ids, because the vendor listing it again does not un-prove the
// turn; a row the new listing no longer names is kept as `gone`, so a person
// who configured it is told rather than left wondering where it went.
func (s *VendorCatalogs) Replace(fresh VendorCatalog) {
	if s.Vendors == nil {
		s.Vendors = map[string]VendorCatalog{}
	}
	previous, had := s.Vendors[fresh.Vendor]
	if had {
		for i := range fresh.Models {
			old, ok := previous.Find(fresh.Models[i].ID)
			if !ok {
				continue
			}
			if old.Status == StatusVerified {
				fresh.Models[i].Status = StatusVerified
				fresh.Models[i].ExactIDs = mergeExact(old.ExactIDs, fresh.Models[i].ExactIDs)
			}
		}
		for _, old := range previous.Models {
			if _, ok := fresh.Find(old.ID); ok {
				continue
			}
			old.Status = StatusGone
			fresh.Models = append(fresh.Models, old)
		}
	}
	s.Vendors[fresh.Vendor] = fresh
}

// Verify records that a turn on `vendor` asked for `id` and the vendor
// answered on `exact` (the resolved model the vendor reported, when it did).
// A row not yet in the catalog is created, so the first turn of a session
// that never previewed still leaves a record.
func (s *VendorCatalogs) Verify(vendor, id, exact string, at time.Time) {
	if s.Vendors == nil {
		s.Vendors = map[string]VendorCatalog{}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	catalog := s.Vendors[vendor]
	catalog.Vendor = vendor
	if catalog.Source == "" {
		catalog.Source = "verified by a turn"
	}
	found := false
	for i := range catalog.Models {
		if !strings.EqualFold(catalog.Models[i].ID, id) {
			continue
		}
		catalog.Models[i].Status = StatusVerified
		if exact != "" {
			catalog.Models[i].ExactIDs = mergeExact([]string{exact}, catalog.Models[i].ExactIDs)
		}
		found = true
	}
	if !found {
		row := DiscoveredModel{ID: id, Status: StatusVerified}
		if exact != "" {
			row.ExactIDs = []string{exact}
		}
		catalog.Models = append(catalog.Models, row)
	}
	if catalog.FetchedAt.IsZero() {
		catalog.FetchedAt = at
	}
	s.Vendors[vendor] = catalog
}

// Gone records that the vendor refused `id` by name. It is only ever set on a
// row that exists: a refusal of a name nobody listed is not information about
// the catalog.
func (s *VendorCatalogs) Gone(vendor, id string) bool {
	catalog, ok := s.Vendors[vendor]
	if !ok {
		return false
	}
	for i := range catalog.Models {
		if strings.EqualFold(catalog.Models[i].ID, strings.TrimSpace(id)) {
			catalog.Models[i].Status = StatusGone
			s.Vendors[vendor] = catalog
			return true
		}
	}
	return false
}

// mergeExact puts the ids in `first` ahead of `rest`, without duplicates.
func mergeExact(first, rest []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(first)+len(rest))
	for _, list := range [][]string{first, rest} {
		for _, id := range list {
			key := strings.ToLower(strings.TrimSpace(id))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, strings.TrimSpace(id))
		}
	}
	return out
}
