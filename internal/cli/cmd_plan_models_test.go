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

func TestBareModelChoicesGiveSharedGPT56ModelsPlanQualifiedShortcuts(t *testing.T) {
	a, _, out := replFixture(t, "")
	if err := a.printPlanModelChoices(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"/model gpt-plus-terra → gpt-5.6-terra",
		"/model gpt-plus-luna → gpt-5.6-luna",
		"/model gpt-pro-sol → gpt-5.6-sol",
		"/model gpt-pro-terra → gpt-5.6-terra",
		"/model gpt-pro-luna → gpt-5.6-luna",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plan model choices omitted %q: %s", want, got)
		}
	}
}
