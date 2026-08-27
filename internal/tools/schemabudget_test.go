package tools

import (
	"encoding/json"
	"testing"
)

// Tool schemas are sent on every request of every turn, so they are the one
// cost in this codebase that is paid per token, per turn, forever. Item 16 put
// it plainly: **tool schemas have to stop being free**, because a single MCP
// server can add a dozen at once and the research records exactly that failure
// in Hermes and Goose — schemas devour the window before the work starts.
//
// This is the budget that makes it a build failure rather than an intention.
// Like every other budget here it fails; it does not warn.
const (
	// Measured on 2026-08-27: five built-in tools, 2,816 bytes.
	//
	// The item's doc said "about 5 KB". It was wrong by nearly a factor of two,
	// which is the reason this leaf exists — a mechanism was going to be
	// designed around a number nobody had taken.
	schemaBudgetBytes = 4096

	// No single tool may dominate. The largest today is read_file at 769 bytes;
	// a schema twice that is a description doing work a system prompt should do.
	perToolBudgetBytes = 1536
)

func TestToolSchemasStayInsideTheirBudget(t *testing.T) {
	defs := Definitions()
	encoded, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > schemaBudgetBytes {
		t.Errorf("tool schemas are %d bytes, over the %d budget — every request of every turn pays this. "+
			"Shorten a description, or decide on purpose that the budget moves.",
			len(encoded), schemaBudgetBytes)
	}

	for _, def := range defs {
		one, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		if len(one) > perToolBudgetBytes {
			t.Errorf("%s's schema is %d bytes, over the %d per-tool budget — a schema that large is a "+
				"description doing work the system prompt should do", def.Function.Name, len(one), perToolBudgetBytes)
		}
	}
}

// The budget is worthless if nobody knows what it is spending. A failing build
// should say what grew, so this asserts the number is reported rather than
// merely enforced.
func TestTheSchemaBudgetReportsWhatItMeasures(t *testing.T) {
	total, perTool := SchemaCost()
	if total == 0 || len(perTool) == 0 {
		t.Fatal("SchemaCost reported nothing")
	}
	var sum int
	for _, size := range perTool {
		sum += size
	}
	// The total is the marshalled array, so it carries brackets and commas the
	// individual objects do not. It must still be within a few bytes per tool
	// of their sum, or one of the two numbers is measuring something else.
	if difference := total - sum; difference < 0 || difference > 4*len(perTool) {
		t.Errorf("total %d and per-tool sum %d disagree by %d bytes", total, sum, difference)
	}
	if _, ok := perTool["bash"]; !ok {
		t.Errorf("per-tool costs do not name the tools: %v", perTool)
	}
}
