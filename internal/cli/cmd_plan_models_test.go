package cli

import (
	"context"
	"strings"
	"testing"
)

func TestPlanModelsListsAndFilters(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"pmodels", "gemini"}); code != ExitOK {
		t.Fatalf("pmodels exit = %d, stderr = %q", code, errOut.String())
	}
	if got := out.String(); !strings.Contains(got, "gemini-2.5-pro") ||
		!strings.Contains(got, "low,medium,high") ||
		!strings.Contains(got, "unsupported subscription") {
		t.Fatalf("plan-models output = %q", got)
	}
}

func TestSlashPlanModelsListsAndFilters(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/pmodels claude") {
		t.Fatal("/pmodels must not exit the REPL")
	}
	if got := out.String(); !strings.Contains(got, "claude-sonnet") {
		t.Fatalf("slash plan-models output = %q", got)
	}
}
