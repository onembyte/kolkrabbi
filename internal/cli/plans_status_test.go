package cli

import (
	"context"
	"strings"
	"testing"
)

// `available` was the status every plan carried on a fresh machine, and it read
// as "you can use this" when it meant "listed in the matrix, nothing configured
// for it". The website said "No adapter yet" about the same fifteen rows; the
// binary said available. Two answers to one question is how a user learns to
// trust neither.
func TestPlansDoNotCallAnUnconfiguredProviderAvailable(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runPlans(context.Background(), nil); err != nil {
		t.Fatalf("runPlans: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "available") {
		t.Errorf("a plan with no connector is called available:\n%s", out)
	}
	if !strings.Contains(out, "listed") {
		t.Errorf("an unconfigured plan does not say it is only listed:\n%s", out)
	}
	// The rows themselves must still be there: this is a wording fix, not a
	// decision to stop showing the matrix.
	for _, want := range []string{"anthropic", "Claude Pro", "openai", "ChatGPT Plus"} {
		if !strings.Contains(out, want) {
			t.Errorf("the matrix stopped listing %q:\n%s", want, out)
		}
	}
}

func TestPlanModelsUseTheSameWord(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runPlanModels(context.Background(), nil); err != nil {
		t.Fatalf("runPlanModels: %v", err)
	}
	if out := stdout.String(); strings.Contains(out, "available") {
		t.Errorf("/pmodels still calls an unconfigured plan available:\n%s", out)
	}
}
