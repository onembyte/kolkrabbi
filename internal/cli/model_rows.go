package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// How a discovered model is shown.
//
// The status is the point of the whole phase: a row says how kolk knows it
// exists, so nobody has to guess whether a name came from the vendor or from
// kolk's own source. `listed` and `verified` are quiet — that is the normal
// case and a column of decoration is noise; `unverified` and `gone` are said,
// because those are the two a person acts on.

// statusNote is the short phrase a row carries, or "" when the row is
// unremarkable.
func statusNote(status provider.ModelStatus) string {
	switch status {
	case provider.StatusUnverified:
		return "unverified"
	case provider.StatusGone:
		return "gone"
	default:
		return ""
	}
}

// contextWindowLabel renders a context window the way a person reads one.
// (contextLabel is taken: the TUI uses it for the session's context usage.)
func contextWindowLabel(tokens int) string {
	switch {
	case tokens <= 0:
		return ""
	case tokens >= 1_000_000:
		return strconv.FormatFloat(float64(tokens)/1_000_000, 'f', -1, 64) + "M"
	case tokens >= 1_000:
		return strconv.Itoa(tokens/1_000) + "K"
	default:
		return strconv.Itoa(tokens)
	}
}

// vendorCatalogFooter is one line per vendor saying where its rows came from
// and when. A catalog with no provenance is a catalog nobody can date, which
// is how a stale list passes for a current one.
func vendorCatalogFooter(out io.Writer, store provider.VendorCatalogs, now time.Time) {
	if len(store.Vendors) == 0 {
		return
	}
	names := make([]string, 0, len(store.Vendors))
	for name := range store.Vendors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		catalog := store.Vendors[name]
		version := ""
		if catalog.VendorVersion != "" {
			version = " " + catalog.VendorVersion
		}
		source := catalog.Source
		if source == "" {
			source = "unknown source"
		}
		fmt.Fprintf(out, "%s%s: %s, %s\n", name, version, source, ageLabel(catalog.FetchedAt, now))
	}
}

// ageLabel says how old a fetch is in the units a person would use.
func ageLabel(at, now time.Time) string {
	if at.IsZero() {
		return "never fetched"
	}
	elapsed := now.Sub(at)
	switch {
	case elapsed < 0:
		return "fetched just now"
	case elapsed < time.Minute:
		return "fetched just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("fetched %dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("fetched %dh ago", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("fetched %dd ago", int(elapsed.Hours()/24))
	}
}

// planModelStatusSuffix is what a compact one-line row appends: nothing for a
// listed or verified model, the reason otherwise.
func planModelStatusSuffix(model provider.PlanModel) string {
	note := statusNote(model.Status)
	if note == "" {
		return ""
	}
	if model.Status == provider.StatusGone {
		return " · gone: the vendor no longer lists it"
	}
	return " · " + note + " until a turn confirms it"
}

// effortsLabel joins an effort set, saying so when there is none.
func effortsLabel(efforts []string) string {
	if len(efforts) == 0 {
		return "-"
	}
	return strings.Join(efforts, ",")
}
