package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// V34.4c.1b.ii: a Google key is told, once, before its first turn, what the
// terms say — the unpaid tier trains on what is sent and a paid key does not —
// in the terms' own words with their date. A vendor whose terms say nothing
// of the kind gets no notice, and so does the gateway.
func TestAGoogleKeyIsToldAboutTheUnpaidTierBeforeItsFirstTurn(t *testing.T) {
	google, err := provider.NewVendorClient("google", "AIza"+strings.Repeat("a", 35))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	printVendorNotice(&out, google)
	for _, want := range []string{"unpaid", "Do not submit sensitive, confidential, or personal information", "2026-03-23", "paid"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("google notice = %q, want it to say %q", out.String(), want)
		}
	}
	xai, err := provider.NewVendorClient("xai", "xai-"+strings.Repeat("0", 24))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	printVendorNotice(&out, xai)
	if out.Len() != 0 {
		t.Fatalf("xai got a notice its terms do not warrant: %q", out.String())
	}
	out.Reset()
	printVendorNotice(&out, provider.NewCompatibleClient("http://compatible.invalid/v1"))
	if out.Len() != 0 {
		t.Fatalf("a compatible endpoint got a vendor notice: %q", out.String())
	}
}
