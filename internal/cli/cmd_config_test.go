package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

// The two brakes on a fan-out run were printed by `kolk config` and refused by
// `kolk config set`, so the only way to set either was to edit the JSON by
// hand. Everything Stigi adds leans on them.
func TestARunCostCeilingCanBeSetAndReadBack(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"set", "max_run_cost_usd", "2.50"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	out.Reset()
	if err := a.runConfig(context.Background(), []string{"get", "max_run_cost_usd"}); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "2.50" {
		t.Errorf("get returned %q, want 2.50", got)
	}
}

// Zero means "no ceiling" and is how the config expresses it. A negative one is
// a run that is over budget before it starts.
func TestANegativeRunCostCeilingIsRefused(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"set", "max_run_cost_usd", "-1"}); err == nil {
		t.Error("a negative ceiling was accepted")
	}
	if err := a.runConfig(context.Background(), []string{"set", "max_run_cost_usd", "0"}); err != nil {
		t.Errorf("zero was refused, but it is how no-ceiling is written: %v", err)
	}
	if err := a.runConfig(context.Background(), []string{"set", "max_run_cost_usd", "lots"}); err == nil {
		t.Error("a non-number was accepted")
	}
}

// One is sequential, which is a choice someone might make. Zero is a run that
// never starts a task, which is not.
func TestAConcurrencyLimitBelowOneIsRefused(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"set", "max_concurrent_tasks", "0"}); err == nil {
		t.Error("a limit of zero was accepted; it would run nothing")
	}
	if err := a.runConfig(context.Background(), []string{"set", "max_concurrent_tasks", "1"}); err != nil {
		t.Errorf("one is sequential, not invalid: %v", err)
	}
}

// A typo in a slot name has to be refused where it is typed. The alternative is
// a warning at the next session start, which on a setting nobody looks at twice
// means paying for the wrong model until they happen to notice.
func TestSettingAnUnknownSlotNamesTheFourThatExist(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	err := a.runConfig(context.Background(), []string{"set", "slot.explorer", "claude-haiku"})
	if err == nil {
		t.Fatal("slot.explorer was accepted; the slot is called explore")
	}
	for _, slot := range []string{"orchestrator", "worker", "explore", "fast"} {
		if !strings.Contains(err.Error(), slot) {
			t.Errorf("the refusal does not name the %s slot: %v", slot, err)
		}
	}
}

func TestASlotCanBeSetAndReadBack(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"set", "slot.fast", "claude-haiku"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	out.Reset()
	if err := a.runConfig(context.Background(), []string{"get", "slot.fast"}); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "claude-haiku" {
		t.Errorf("get returned %q, want claude-haiku", got)
	}
}

// An emptied map must leave the file entirely: a `"slots": {}` left behind
// reads as a decision someone made.
func TestUnsettingTheLastSlotLeavesNoSlotsKey(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	if err := a.runConfig(context.Background(), []string{"set", "slot.fast", "claude-haiku"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := a.runConfig(context.Background(), []string{"unset", "slot.fast"}); err != nil {
		t.Fatalf("unset: %v", err)
	}
	raw, err := os.ReadFile(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "slots") {
		t.Errorf("the slots key survived an emptied map:\n%s", raw)
	}
}

// Unsetting says what the value falls back to, so nobody has to guess.
func TestUnsettingTheBrakesSaysWhatTheyFallBackTo(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	for _, key := range []string{"max_run_cost_usd", "max_concurrent_tasks"} {
		if err := a.runConfig(context.Background(), []string{"unset", key}); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	got := out.String()
	if !strings.Contains(got, "no cost ceiling") {
		t.Errorf("unsetting the cost ceiling did not say what happens:\n%s", got)
	}
	if !strings.Contains(got, "3 at a time") {
		t.Errorf("unsetting the width did not name the default:\n%s", got)
	}
}
