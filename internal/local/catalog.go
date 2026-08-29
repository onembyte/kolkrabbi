package local

import (
	"fmt"
	"strings"
)

// CatalogEntry is one local model variant Kolkrabbi knows how to plan for.
//
// StorageBytes is the published size of that quantization's weights, which is
// a fact. Everything about runtime memory is an estimate derived from it, and
// is labelled that way wherever it is shown: the exact figure depends on
// context length, batch size and the runtime's own overhead, none of which
// Kolkrabbi controls.
type CatalogEntry struct {
	Name         string
	Quantization string
	Parameters   string
	StorageBytes uint64
}

const (
	gibibyte = 1 << 30
	mebibyte = 1 << 20
)

// SidecarName is the user's own Ollama binary, looked for on PATH, and the
// route prefix the engine owns for its models (option E). It once named a
// runtime kolk would install itself; that contract is gone, and the name stays
// because the route key and the connector name were built on it.
const SidecarName = "ollama"

// catalog is deliberately short. A long list of models nobody verified is not
// more useful than a few whose sizes were checked, and every entry here has to
// be sized before it can honestly be offered.
var catalog = []CatalogEntry{
	{Name: "qwen2.5-coder:7b", Quantization: "Q4_K_M", Parameters: "7B", StorageBytes: 4683 * mebibyte},
	{Name: "qwen2.5-coder:14b", Quantization: "Q4_K_M", Parameters: "14B", StorageBytes: 8988 * mebibyte},
	{Name: "llama3.1:8b", Quantization: "Q4_K_M", Parameters: "8B", StorageBytes: 4920 * mebibyte},
	{Name: "gemma2:9b", Quantization: "Q4_K_M", Parameters: "9B", StorageBytes: 5443 * mebibyte},
	{Name: "phi4:14b", Quantization: "Q4_K_M", Parameters: "14B", StorageBytes: 9053 * mebibyte},
}

// Catalog returns the known local models, filtered case-insensitively by name,
// quantization or parameter count.
func Catalog(filter string) []CatalogEntry {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := make([]CatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if filter != "" &&
			!strings.Contains(strings.ToLower(entry.Name), filter) &&
			!strings.Contains(strings.ToLower(entry.Quantization), filter) &&
			!strings.Contains(strings.ToLower(entry.Parameters), filter) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// LookupModel resolves an exact catalog name.
func LookupModel(name string) (CatalogEntry, error) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for _, entry := range catalog {
		if strings.ToLower(entry.Name) == wanted {
			return entry, nil
		}
	}
	return CatalogEntry{}, fmt.Errorf("no local model named %q; `kolk localia models` lists them", name)
}

// Requirement estimates what one entry needs at runtime.
//
// Weights have to be resident, and the runtime needs working memory on top of
// them for the KV cache and its own buffers. The estimate is the weights plus
// a fifth, with a floor, because a proportional overhead alone under-counts
// small models. It is deliberately generous: over-estimating costs a user a
// model that would probably have fit, while under-estimating costs them a
// multi-gigabyte download that cannot load.
func (e CatalogEntry) Requirement() ModelRequirement {
	overhead := e.StorageBytes / 5
	if overhead < 512*mebibyte {
		overhead = 512 * mebibyte
	}
	runtime := e.StorageBytes + overhead
	return ModelRequirement{
		Name:         e.Name,
		StorageBytes: e.StorageBytes,
		VRAMBytes:    runtime,
		// Running on the CPU keeps the weights in system RAM and leaves less
		// room for the runtime to work in, so it asks for more, not the same.
		RAMBytes: runtime + overhead,
	}
}
