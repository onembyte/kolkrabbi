package engine

import "testing"

// The one ranking signal no vendor benchmark has: what happened when this
// person used this model for this kind of work. A model they rated badly should
// stop being chosen for them.
func TestABadlyRatedModelIsDemoted(t *testing.T) {
	a := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue(),
		ModelRatings: map[string]ModelRating{
			"vendor/strong-coder": {Average: 1.5, Count: 4},
		},
	}}
	if got := a.modelForKind(KindDesign); got == "vendor/strong-coder" {
		t.Errorf("design still chose %q after four bad ratings", got)
	}
}

// And a well-rated one is preferred over an equally suited rival.
func TestAWellRatedModelIsPreferred(t *testing.T) {
	a := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue(),
		ModelRatings: map[string]ModelRating{
			"vendor/cheap-reader": {Average: 5, Count: 6},
		},
	}}
	// cheap-reader is not the strongest coder, but a run of five-star results
	// is evidence about this person's work that a description is not.
	if got := a.modelForKind(KindDesign); got != "vendor/cheap-reader" {
		t.Errorf("design chose %q, want the model this machine rates highest", got)
	}
}

// One bad turn is a bad turn, not a verdict. A single rating must not rearrange
// a run, or a person who mis-clicks once loses a model.
func TestASingleRatingDoesNotMoveTheChoice(t *testing.T) {
	withRating := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue(),
		ModelRatings: map[string]ModelRating{"vendor/strong-coder": {Average: 1, Count: 1}},
	}}
	without := &Agent{Options: Options{Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue()}}
	if withRating.modelForKind(KindDesign) != without.modelForKind(KindDesign) {
		t.Error("a single one-star rating changed the plan")
	}
}

// A middling average is not a signal. Only clear opinions move anything, or the
// ranking becomes noise with a numeric face.
func TestAMiddlingRatingChangesNothing(t *testing.T) {
	withRating := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue(),
		ModelRatings: map[string]ModelRating{"vendor/strong-coder": {Average: 3, Count: 10}},
	}}
	without := &Agent{Options: Options{Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue()}}
	if withRating.modelForKind(KindDesign) != without.modelForKind(KindDesign) {
		t.Error("a three-star average rearranged the ranking")
	}
}

// Demotion is not exclusion: a badly rated model is still better than no model.
func TestABadlyRatedModelIsStillUsedWhenItIsTheOnlyOne(t *testing.T) {
	only := []struct{}{}
	_ = only
	a := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium,
		Catalog: catalogue()[:1], // strong-coder alone
		ModelRatings: map[string]ModelRating{
			"vendor/strong-coder": {Average: 1, Count: 9},
		},
	}}
	if got := a.modelForKind(KindDesign); got != "vendor/strong-coder" {
		t.Errorf("design chose %q when the only tool-capable model was badly rated; "+
			"demotion must not mean exclusion", got)
	}
}

// No ratings at all is the normal state and must change nothing.
func TestNoRatingsLeavesTheRankingAlone(t *testing.T) {
	withEmpty := &Agent{Options: Options{
		Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue(),
		ModelRatings: map[string]ModelRating{},
	}}
	without := &Agent{Options: Options{Model: "vendor/main", Effort: EffortMedium, Catalog: catalogue()}}
	if withEmpty.modelForKind(KindDesign) != without.modelForKind(KindDesign) {
		t.Error("an empty ratings map changed the choice")
	}
}
