package cli

import (
	"context"
	"strings"
	"testing"
)

func TestPlansListsAndFiltersProviderPlans(t *testing.T) {
	a, out, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"plans", "gemini"}); code != ExitOK {
		t.Fatalf("plans exit = %d, stderr = %q", code, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, "Google AI Pro") || strings.Contains(got, "Claude Max") {
		t.Fatalf("filtered plans output = %q", got)
	}
}

func TestSlashPlansListsAndFiltersProviderPlans(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/plans pro") {
		t.Fatal("/plans must not exit the REPL")
	}
	if got := out.String(); !strings.Contains(got, "Google AI Pro") ||
		!strings.Contains(got, "ChatGPT Pro") {
		t.Fatalf("slash plans output = %q", got)
	}
}
