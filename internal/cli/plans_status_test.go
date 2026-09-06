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
	// Re-read 2026-09-06 (V34.4c.4): "listed" was the word for a row kolk had
	// nothing to say about. Every row now has something to say — the
	// disposition's own word, or whether a key is held — so the requirement
	// is that an unconfigured row says one of those, never nothing.
	for _, want := range []string{"no key", "unsupported", "investigating", "deferred"} {
		if !strings.Contains(out, want) {
			t.Errorf("no unconfigured row says %q:\n%s", want, out)
		}
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

// A row whose provider has a disposition says it in the disposition's own
// word (V34.4c.4): chosen, investigating, deferred — or unsupported for a
// subscription row of a provider whose only path is a key. "listed" is what
// a row says when kolk has nothing to say, and these rows have something.
func TestPlansRowsSayTheirDispositionWord(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	if err := a.runPlans(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	want := map[string]string{
		"Gemini API":     "no key",
		"Google AI Pro":  "unsupported",
		"Grok":           "no key",
		"Perplexity API": "investigating",
		"Le Chat":        "deferred",
		"DeepSeek":       "deferred",
	}
	for _, line := range strings.Split(out, "\n") {
		for plan, status := range want {
			if strings.Contains(line, plan) && !strings.HasSuffix(strings.TrimSpace(line), "  "+status) && !strings.HasSuffix(strings.TrimSpace(line), " "+status) {
				t.Errorf("row %q does not end with %q", line, status)
			}
		}
	}
	if strings.Contains(out, "Perplexity Pro") {
		t.Error("the Perplexity row is still named after a consumer plan that is not the access path")
	}
}
