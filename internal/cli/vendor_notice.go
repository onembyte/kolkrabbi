package cli

import (
	"fmt"
	"io"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// printVendorNotice tells a keyed vendor's user what the vendor's terms say
// about the key before its first turn — once, at the session's start, in the
// terms' words (V34.4c.1b.ii). Vendors whose terms warrant no warning, the
// gateway, and compatible endpoints print nothing.
func printVendorNotice(w io.Writer, client *provider.Client) {
	if client == nil {
		return
	}
	if notice := provider.VendorNotice(client.Origin); notice != "" {
		fmt.Fprintf(w, "◆ %s\n", notice)
	}
}
