package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendLoadAggregate(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	recs := []Record{
		{Kind: "call", Turn: "t1", Mode: "code", Role: "main", Model: "a/fast", PromptTokens: 100, CompletionTokens: 50, Cost: 0.001, Ms: 200, ToolCalls: 1, Time: now},
		{Kind: "call", Turn: "t1", Mode: "code", Role: "main", Model: "a/fast", PromptTokens: 120, CompletionTokens: 30, Cost: 0.002, Ms: 400, Time: now},
		{Kind: "call", Turn: "t2", Mode: "agent", Role: "planner", Model: "b/big", PromptTokens: 500, CompletionTokens: 100, Cost: 0.02, Ms: 1000, Time: now},
		{Kind: "rating", Turn: "t1", Rating: 4, Time: now},
		{Kind: "rating", Turn: "t1", Rating: 5, Time: now}, // last rating wins
	}
	for _, r := range recs {
		if err := Append(dir, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// a corrupt line must be skipped, not break loading
	f, _ := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{not json}\n")
	f.Close()

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 5 {
		t.Fatalf("loaded %d records, want 5", len(loaded))
	}

	rows := Aggregate(loaded)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// sorted by cost desc: b/big first
	if rows[0].Model != "b/big" || rows[1].Model != "a/fast" {
		t.Errorf("order = %s, %s", rows[0].Model, rows[1].Model)
	}
	fast := rows[1]
	if fast.Calls != 2 || fast.Tokens != 300 || fast.AvgMs != 300 || fast.ToolCalls != 1 {
		t.Errorf("a/fast row = %+v", fast)
	}
	// both t1 calls inherit the turn's final rating (5)
	if fast.Ratings != 2 || fast.AvgRating != 5 {
		t.Errorf("a/fast ratings = %d avg %.1f, want 2 avg 5", fast.Ratings, fast.AvgRating)
	}
	if rows[0].ByMode["agent"] != 1 {
		t.Errorf("b/big modes = %v", rows[0].ByMode)
	}
}

func TestLoadMissingFile(t *testing.T) {
	recs, err := Load(t.TempDir())
	if err != nil || recs != nil {
		t.Errorf("Load empty dir = (%v,%v), want (nil,nil)", recs, err)
	}
}

func TestRender(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "no usage recorded") {
		t.Errorf("empty render = %q", out)
	}
	rows := []ModelRow{{Model: "a/fast", Calls: 3, Tokens: 900, Cost: 0.05, AvgMs: 250, Ratings: 1, AvgRating: 4, ByMode: map[string]int{"code": 3}}}
	out = Render(rows)
	for _, want := range []string{"a/fast", "$0.05", "4.0★", "code:3", "TOTAL"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}
