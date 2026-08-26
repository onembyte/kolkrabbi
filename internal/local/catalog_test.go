package local

import (
	"strings"
	"testing"
)

func TestCatalogFiltersByName(t *testing.T) {
	all := Catalog("")
	if len(all) == 0 {
		t.Fatal("the catalog must offer something")
	}
	filtered := Catalog("qwen")
	if len(filtered) == 0 || len(filtered) >= len(all) {
		t.Fatalf("filter matched %d of %d", len(filtered), len(all))
	}
	for _, entry := range filtered {
		if !strings.Contains(strings.ToLower(entry.Name), "qwen") {
			t.Fatalf("filter returned %q", entry.Name)
		}
	}
}

func TestCatalogEntriesCarryAStorageSizeAndQuantization(t *testing.T) {
	for _, entry := range Catalog("") {
		if entry.StorageBytes == 0 {
			t.Fatalf("%s has no storage size, so no pull could ever be sized", entry.Name)
		}
		if entry.Quantization == "" {
			t.Fatalf("%s has no quantization, so its size means nothing", entry.Name)
		}
	}
}

func TestRequirementIsAlwaysLargerThanTheFileOnDisk(t *testing.T) {
	// Weights plus working memory. A planner that used the file size alone
	// would approve models that load and then immediately fail.
	for _, entry := range Catalog("") {
		requirement := entry.Requirement()
		if requirement.StorageBytes != entry.StorageBytes {
			t.Fatalf("%s storage = %d, want the catalog's %d", entry.Name, requirement.StorageBytes, entry.StorageBytes)
		}
		if requirement.VRAMBytes <= entry.StorageBytes {
			t.Fatalf("%s needs %d VRAM for %d of weights", entry.Name, requirement.VRAMBytes, entry.StorageBytes)
		}
		if requirement.RAMBytes < requirement.VRAMBytes {
			t.Fatalf("%s wants less RAM than VRAM, which cannot be right", entry.Name)
		}
	}
}

func TestLookupIsExactAndReportsWhatItKnows(t *testing.T) {
	entry, err := LookupModel("qwen2.5-coder:7b")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "qwen2.5-coder:7b" {
		t.Fatalf("entry = %+v", entry)
	}
	_, err = LookupModel("not-a-model")
	if err == nil {
		t.Fatal("an unknown model must be refused")
	}
	if !strings.Contains(err.Error(), "localia models") {
		t.Fatalf("error = %v, want it to name the command that lists them", err)
	}
}
