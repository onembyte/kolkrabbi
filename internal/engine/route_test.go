package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func routingAgent(slots map[string]string) *Agent {
	return &Agent{Options: Options{Model: "session/model", Effort: EffortMedium, Slots: slots}}
}

func TestWithNothingConfiguredEverythingRunsOnTheSessionModel(t *testing.T) {
	// This is today's behaviour, and it is the one a user who configured
	// nothing must keep: routing that changes what a run costs without being
	// asked for is a surprise, not a feature.
	agent := routingAgent(nil)
	for _, kind := range []Kind{KindUnknown, KindEdit, KindTest, KindResearch, KindDesign} {
		if got := agent.modelForKind(kind); got != "session/model" {
			t.Fatalf("%s routed to %q, want the session model", kind, got)
		}
	}
}

func TestKindsResolveThroughSlotsNotDirectlyToModels(t *testing.T) {
	agent := routingAgent(map[string]string{
		SlotExplore:      "cheap/reader",
		SlotOrchestrator: "strong/thinker",
	})

	for kind, want := range map[Kind]string{
		KindResearch: "cheap/reader",
		KindExplain:  "cheap/reader",
		KindDesign:   "strong/thinker",
		KindEdit:     "session/model", // worker slot unset
		KindTest:     "session/model",
	} {
		if got := agent.modelForKind(kind); got != want {
			t.Fatalf("%s routed to %q, want %q", kind, got, want)
		}
	}
}

func TestSettingOneSlotChangesOneThing(t *testing.T) {
	agent := routingAgent(map[string]string{SlotWorker: "good/editor"})

	if got := agent.modelForKind(KindEdit); got != "good/editor" {
		t.Fatalf("edit routed to %q", got)
	}
	if got := agent.modelForKind(KindResearch); got != "session/model" {
		t.Fatalf("research routed to %q, want the untouched default", got)
	}
}

func TestBoilerplateUsesTheFastLaneWithoutBeingConfigured(t *testing.T) {
	// The fast lane already exists and already knows how to pick a cheap model.
	// Making the user configure it again to get mechanical work off the
	// expensive model would be a setting that should not need to exist.
	agent := routingAgent(nil)
	if got, want := agent.modelForKind(KindBoilerplate), agent.FastLaneModel(); got != want {
		t.Fatalf("boilerplate routed to %q, want the fast lane %q", got, want)
	}
	// And it is still overridable.
	agent.Slots = map[string]string{SlotFast: "my/choice"}
	if got := agent.modelForKind(KindBoilerplate); got != "my/choice" {
		t.Fatalf("boilerplate routed to %q despite an explicit slot", got)
	}
}

func TestATypoInASlotNameIsReported(t *testing.T) {
	// Silently ignoring `explorer` when the slot is `explore` means someone
	// pays for the wrong model and has no way to notice.
	err := ValidateSlots(map[string]string{"explorer": "a/b"})
	if err == nil {
		t.Fatal("an unknown slot was accepted")
	}
	if !strings.Contains(err.Error(), "explorer") || !strings.Contains(err.Error(), SlotExplore) {
		t.Fatalf("error %q must name the typo and the real slots", err)
	}
	if err := ValidateSlots(map[string]string{SlotWorker: "a/b", SlotFast: ""}); err != nil {
		t.Fatalf("valid slots rejected: %v", err)
	}
}

func TestTheRoutedModelIsShownWithThePlan(t *testing.T) {
	task := Task{Title: "rename them", Kind: KindEdit, Model: "good/editor"}
	got := task.annotation()
	if !strings.Contains(got, "edit") || !strings.Contains(got, "good/editor") {
		t.Fatalf("annotation = %q, want the kind and the model a reader can account for", got)
	}
}

func TestARunActuallyUsesTheRoutedModels(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"read it","kind":"research"},{"title":"change it","kind":"edit"}]`},
		enginetest.Step{Text: "read"},
		enginetest.Step{Text: "changed"},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Slots = map[string]string{SlotExplore: "cheap/reader"}
	// One at a time: this test is about which model each task asks for, and
	// under concurrency the order requests arrive in is not a fact about that.
	agent.MaxConcurrentTasks = 1

	if err := agent.runOrchestrated(context.Background(), "read then change"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	// Routing that is only printed and not applied is worse than none.
	if got := srv.Models[1]; got != "cheap/reader" {
		t.Fatalf("the research task ran on %q, want the explore slot", got)
	}
	if got := srv.Models[2]; got != "mock/model" {
		t.Fatalf("the edit task ran on %q, want the session model", got)
	}
	// And the user can see what they are paying for before it happens.
	if !strings.Contains(out.String(), "cheap/reader") {
		t.Fatalf("the plan did not show the routing:\n%s", out.String())
	}
}
