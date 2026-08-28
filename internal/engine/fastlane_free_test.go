package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

const aFreeModel = "meta-llama/llama-3.3-70b-instruct:free"

// The case that is wrong today: a paid main model, a free tool-capable model
// discovered and available, and mechanical work — a session title, a commit
// draft, a boilerplate subtask — billed to the paid model for no benefit.
//
// Somebody who chose a strong model for their real work should not pay it to
// name a session.
func TestAFreeModelWinsTheFastLaneEvenWhenTheSessionModelIsPaid(t *testing.T) {
	a := &Agent{Options: Options{
		Model:      "anthropic/claude-sonnet-4",
		FreeModels: []string{aFreeModel},
	}}
	if got := a.FastLaneModel(); got != aFreeModel {
		t.Errorf("fast lane chose %q while a free model was available, want %q", got, aFreeModel)
	}
}

// Unchanged: with no free model discovered there is nothing to prefer, and the
// low-cost default is still the right answer.
func TestThePaidDefaultRemainsWhenNoFreeModelExists(t *testing.T) {
	a := &Agent{Options: Options{Model: "anthropic/claude-sonnet-4"}}
	if got := a.FastLaneModel(); got != defaultPaidFastLaneModel {
		t.Errorf("fast lane chose %q with no free model available, want the low-cost default", got)
	}
}

// A free session keeps using its own model rather than a different free one:
// switching for no reason costs the prompt cache.
func TestAFreeSessionKeepsItsOwnModel(t *testing.T) {
	a := &Agent{Options: Options{Model: aFreeModel, FreeModels: []string{"other/free:free"}}}
	if got := a.FastLaneModel(); got != aFreeModel {
		t.Errorf("fast lane switched away from the session's own free model to %q", got)
	}
}

// slot.fast is the override, and it beats everything: somebody who disagrees
// with all of this says so once and is obeyed.
func TestTheFastSlotOverridesTheFreePreference(t *testing.T) {
	a := &Agent{Options: Options{
		Model:      "anthropic/claude-sonnet-4",
		FreeModels: []string{aFreeModel},
		Slots:      map[string]string{SlotFast: "chosen/model"},
	}}
	if got := a.modelForKind(KindBoilerplate); got != "chosen/model" {
		t.Errorf("boilerplate went to %q, want the configured slot.fast", got)
	}
}

// Free tiers rate-limit, and FastLaneChat calls the backend directly rather
// than through the turn's rotation — so preferring free without a net would
// trade money for a session title that sometimes fails.
func TestAFastLaneCallFallsBackWhenTheFreeModelRefuses(t *testing.T) {
	var tried []string
	a := &Agent{Options: Options{
		Model:      "anthropic/claude-sonnet-4",
		FreeModels: []string{aFreeModel},
		Backend: scriptedBackend(func(model string) (provider.Message, error) {
			tried = append(tried, model)
			if model == aFreeModel {
				return provider.Message{}, errors.New("rate limited")
			}
			return provider.Message{Content: "a title"}, nil
		}),
	}}

	got, err := a.FastLaneChat(context.Background(), "", "name this")
	if err != nil {
		t.Fatalf("FastLaneChat: %v", err)
	}
	if got != "a title" {
		t.Errorf("result = %q", got)
	}
	if len(tried) != 2 || tried[0] != aFreeModel || tried[1] != defaultPaidFastLaneModel {
		t.Errorf("models tried = %v, want the free one then the paid default", tried)
	}
}

// One retry, not a loop: if the paid default fails too, the caller hears about
// it rather than the session stalling.
func TestAFastLaneCallGivesUpAfterOneFallback(t *testing.T) {
	calls := 0
	a := &Agent{Options: Options{
		Model:      "anthropic/claude-sonnet-4",
		FreeModels: []string{aFreeModel},
		Backend: scriptedBackend(func(string) (provider.Message, error) {
			calls++
			return provider.Message{}, errors.New("still refusing")
		}),
	}}
	if _, err := a.FastLaneChat(context.Background(), "", "name this"); err == nil {
		t.Fatal("a fast lane that could not answer reported success")
	}
	if calls != 2 {
		t.Errorf("made %d attempts, want the free model and one fallback", calls)
	}
}

// scriptedBackend answers a fast-lane call however the test says, so the
// fallback can be exercised without a network.
type scriptedBackend func(model string) (provider.Message, error)

func (f scriptedBackend) StreamChat(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	msg, err := f(model)
	return msg, provider.Meta{}, err
}
