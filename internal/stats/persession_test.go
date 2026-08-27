package stats

import (
	"testing"
	"time"
)

func TestCostBySessionSumsEverySessionsCalls(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 0.25},
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 0.75},
		{Kind: "call", Time: time.Now(), Session: "s_b", Cost: 1.50},
	} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}

	costs, err := costBySession(dir)
	if err != nil {
		t.Fatalf("costBySession: %v", err)
	}
	if got := costs["s_a"]; got < 0.999 || got > 1.001 {
		t.Errorf("s_a = %v, want 1.00", got)
	}
	if got := costs["s_b"]; got < 1.499 || got > 1.501 {
		t.Errorf("s_b = %v, want 1.50", got)
	}
}

// A rating is not a call: it costs nothing and joining it in would inflate the
// number a person is reading to decide whether to stop a session.
func TestCostBySessionIgnoresRatings(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Record{Kind: "rating", Time: time.Now(), Session: "s_a", Turn: "t_1"}); err != nil {
		t.Fatal(err)
	}
	costs, err := costBySession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := costs["s_a"]; present {
		t.Error("a rating row produced a cost entry")
	}
}

// A free model reports zero, and zero is a real answer: the session exists and
// has cost nothing. Absent and zero must stay distinguishable.
func TestCostBySessionKeepsAZeroCostSession(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Record{Kind: "call", Time: time.Now(), Session: "s_free", Cost: 0}); err != nil {
		t.Fatal(err)
	}
	costs, err := costBySession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := costs["s_free"]; !present {
		t.Error("a session that cost nothing was dropped rather than reported as free")
	}
}

func TestCostBySessionIsEmptyWithNoLog(t *testing.T) {
	costs, err := costBySession(t.TempDir())
	if err != nil {
		t.Fatalf("costBySession with no log: %v", err)
	}
	if len(costs) != 0 {
		t.Errorf("costs = %v, want empty", costs)
	}
}

func TestCostForSessionsAnswersOnlyForWhatWasAsked(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 0.25},
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 0.25},
		{Kind: "call", Time: time.Now(), Session: "s_b", Cost: 9.99},
		{Kind: "rating", Time: time.Now(), Session: "s_a", Turn: "t_1"},
	} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	costs, err := CostForSessions(dir, map[string]bool{"s_a": true})
	if err != nil {
		t.Fatal(err)
	}
	if got := costs["s_a"]; got < 0.499 || got > 0.501 {
		t.Errorf("s_a = %v, want 0.50", got)
	}
	if _, present := costs["s_b"]; present {
		t.Error("a session nobody asked about was totalled anyway")
	}
}

func TestCostForSessionsAgreesWithTheFullDecode(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Record{
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 0.25},
		{Kind: "call", Time: time.Now(), Session: "s_a", Cost: 1.00},
		{Kind: "call", Time: time.Now(), Session: "s_free", Cost: 0},
	} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	all, err := costBySession(dir)
	if err != nil {
		t.Fatal(err)
	}
	some, err := CostForSessions(dir, map[string]bool{"s_a": true, "s_free": true})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range all {
		if got := some[id]; got != want {
			t.Errorf("%s: cheap path says %v, full path says %v", id, got, want)
		}
	}
	if len(some) != len(all) {
		t.Errorf("cheap path returned %d sessions, full path %d", len(some), len(all))
	}
}
