package engine

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func model(id string, ctx int, prompt string, tools bool, desc string) provider.ModelInfo {
	m := provider.ModelInfo{ID: id, Name: id, Description: desc, ContextLength: ctx}
	m.Pricing.Prompt = prompt
	m.Pricing.Completion = prompt
	if tools {
		m.SupportedParameters = []string{"tools"}
	}
	return m
}

func catalogue() []provider.ModelInfo {
	return []provider.ModelInfo{
		model("vendor/strong-coder", 200000, "0.000015", true, "state of the art coding model, swe-bench"),
		model("vendor/cheap-reader", 1000000, "0.0000001", true, "long context summarisation"),
		model("vendor/free-worker:free", 131072, "0", true, "free coding model"),
		model("vendor/no-tools", 200000, "0.000001", false, "chat only"),
		model("vendor/tiny", 8000, "0.0000001", true, "small context"),
	}
}

// Every slot must produce a tool-capable model: a subagent that cannot call a
// tool cannot do the work any of these slots exist for.
func TestEverySlotChoosesAToolCapableModel(t *testing.T) {
	for _, slot := range []string{SlotOrchestrator, SlotWorker, SlotExplore, SlotFast} {
		got := rankForSlot(catalogue(), slot, nil)
		if len(got) == 0 {
			t.Errorf("%s got no candidates at all", slot)
			continue
		}
		if got[0] == "vendor/no-tools" {
			t.Errorf("%s chose a model that cannot call tools", slot)
		}
		if got[0] == "vendor/tiny" {
			t.Errorf("%s chose a model whose context is too small to work in", slot)
		}
	}
}

// The orchestrator plans. A weak model costs the most there, so the strongest
// coding-oriented model wins even though it is the most expensive.
func TestTheOrchestratorSlotTakesTheStrongestModel(t *testing.T) {
	if got := rankForSlot(catalogue(), SlotOrchestrator, nil); got[0] != "vendor/strong-coder" {
		t.Errorf("orchestrator chose %q, want the strongest coding model", got[0])
	}
}

// Explore reads and summarises, so its job is volume. A free model that clears
// the context floor beats a nearly-free one for work measured in pages read.
func TestTheExploreSlotPrefersFree(t *testing.T) {
	if got := rankForSlot(catalogue(), SlotExplore, nil); got[0] != "vendor/free-worker:free" {
		t.Errorf("explore chose %q, want the free model", got[0])
	}
}

// With nothing free available, the cheap high-context model is what explore is
// for — this is where a million-token window earns its place.
func TestTheExploreSlotTakesCheapAndHighContextWhenNothingIsFree(t *testing.T) {
	var paidOnly []provider.ModelInfo
	for _, m := range catalogue() {
		if !provider.ModelIsFree(m) {
			paidOnly = append(paidOnly, m)
		}
	}
	if got := rankForSlot(paidOnly, SlotExplore, nil); got[0] != "vendor/cheap-reader" {
		t.Errorf("explore chose %q, want the cheap high-context model", got[0])
	}
}

// Fast is free, always, when a free model exists — A33.3's decision, applied to
// the slot that does mechanical work.
func TestTheFastSlotTakesAFreeModel(t *testing.T) {
	got := rankForSlot(catalogue(), SlotFast, nil)
	if len(got) == 0 || got[0] != "vendor/free-worker:free" {
		t.Errorf("fast chose %v, want the free model", got)
	}
}

// A slot nobody defined is not a licence to guess.
func TestAnUnknownSlotRanksNothing(t *testing.T) {
	if got := rankForSlot(catalogue(), "invented", nil); len(got) != 0 {
		t.Errorf("an unknown slot produced %v", got)
	}
}

// An empty or unusable catalogue must produce nothing rather than a wrong
// answer: the caller falls back to the model it already had.
func TestAnEmptyCatalogueRanksNothing(t *testing.T) {
	if got := rankForSlot(nil, SlotWorker, nil); len(got) != 0 {
		t.Errorf("an empty catalogue produced %v", got)
	}
	onlyUnusable := []provider.ModelInfo{model("vendor/no-tools", 200000, "0.000001", false, "chat")}
	if got := rankForSlot(onlyUnusable, SlotWorker, nil); len(got) != 0 {
		t.Errorf("a catalogue with nothing usable produced %v", got)
	}
}

// The order must be stable: the same catalogue twice is the same plan twice, or
// a run stops being reproducible for the person watching it.
func TestRankingIsStable(t *testing.T) {
	first := rankForSlot(catalogue(), SlotWorker, nil)
	second := rankForSlot(catalogue(), SlotWorker, nil)
	if len(first) != len(second) {
		t.Fatalf("two rankings of one catalogue differ in length")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("ranking is not stable: %v then %v", first, second)
		}
	}
}

// The behaviour this leaf exists to change: with no slots configured, every
// task used the effort model, so an orchestrated run opened one model many
// times instead of the right model for each role.
func TestAnEmptySlotResolvesFromTheCatalogueInsteadOfCollapsing(t *testing.T) {
	a := &Agent{Options: Options{
		Model:      "vendor/main",
		Effort:     EffortMedium,
		Catalog:    catalogue(),
		Slots:      map[string]string{},
		FreeModels: []string{"vendor/free-worker:free"},
	}}

	design := a.modelForKind(KindDesign)     // orchestrator
	edit := a.modelForKind(KindEdit)         // worker
	research := a.modelForKind(KindResearch) // explore
	if design == edit && edit == research {
		t.Fatalf("every kind resolved to %q — the slots collapsed to one model", design)
	}
	if design != "vendor/strong-coder" {
		t.Errorf("design went to %q, want the strongest model", design)
	}
}

// A configured slot is the user's decision and beats any ranking.
func TestAConfiguredSlotBeatsTheCatalogue(t *testing.T) {
	a := &Agent{Options: Options{
		Model:   "vendor/main",
		Effort:  EffortMedium,
		Catalog: catalogue(),
		Slots:   map[string]string{SlotWorker: "chosen/model"},
	}}
	if got := a.modelForKind(KindEdit); got != "chosen/model" {
		t.Errorf("edit went to %q, want the configured slot.worker", got)
	}
}

// With no catalogue there is nothing to choose from, and the effort model is
// still the right answer rather than an empty one.
func TestNoCatalogueFallsBackToTheEffortModel(t *testing.T) {
	a := &Agent{Options: Options{Model: "vendor/main", Effort: EffortMedium}}
	if got := a.modelForKind(KindEdit); got != a.modelFor(EffortMedium) {
		t.Errorf("edit went to %q with no catalogue, want the effort model", got)
	}
}
