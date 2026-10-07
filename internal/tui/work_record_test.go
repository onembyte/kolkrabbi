package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestWorkLogGroupsExplorationByAgentWithoutMergingTheirResults(t *testing.T) {
	first, group := workRecordText(WorkRecord{Agent: 1, Name: "read_file", Arguments: `{"path":"a.go","purpose":"check the entry point"}`}, "")
	next, group := workRecordText(WorkRecord{Agent: 1, Name: "read_file", Arguments: `{"path":"b.go"}`}, group)
	other, _ := workRecordText(WorkRecord{Agent: 2, Name: "read_file", Arguments: `{"path":"c.go"}`}, group)
	if !strings.Contains(first, "• Explored · agent 1") || !strings.Contains(first, "↳ check the entry point") || strings.Contains(next, "•") || !strings.Contains(other, "• Explored · agent 2") {
		t.Fatalf("incorrect action grouping:\n%s%s%s", first, next, other)
	}
}

func TestWorkLogKeepsCommandFailureOutputAndBoundsLongResults(t *testing.T) {
	text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":"go test ./..."}`, Failed: true, Output: strings.Repeat("failure detail\n", 30)}, "")
	for _, want := range []string{"• Failed go test ./...", "× command failed", "failure detail", "22 more lines"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "failure detail") != workOutputLines {
		t.Fatal("output was not bounded")
	}
}

func TestWorkLogReportsObservedEditCountsAndSignedDiff(t *testing.T) {
	text, _ := workRecordText(WorkRecord{Name: "edit_file", Path: "main.go", Changed: true, Added: 1, Removed: 1, Diff: "@@ -10,1 +10,1 @@\n-before\n+after\n"}, "")
	for _, want := range []string{"Edited main.go (+1 -1)", "-before", "+after", "@@ -10"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestWorkLogEmptyAndUnsafeOutput(t *testing.T) {
	text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":"true"}`}, "")
	if !strings.Contains(text, "└ (no output)") {
		t.Fatal(text)
	}
	text, _ = workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":"echo ok"}`, Output: "\x1b]52;c;stolen\aok\n```\n# fake heading\n"}, "")
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\n```") || strings.Contains(text, "\n#") {
		t.Fatal("tool output escaped its result block")
	}
}

func TestWorkDiffHasLineNumbersBackgroundsAndColouredCounts(t *testing.T) {
	SetPalette("256")
	defer SetPalette("256")
	text, _ := workRecordText(WorkRecord{Name: "edit_file", Path: "a.go", Changed: true, Added: 1, Removed: 1,
		Diff: "@@ -12,2 +12,2 @@\n-old\n+new\n context\n"}, "")
	m := New(Status{})
	m.AppendTranscript(text)
	view := m.renderView(60, 24, -1)
	for _, want := range []string{"12 - old", "12 + new", "13   context", activePalette[styleDiffAdd], activePalette[styleDiffDel], activePalette[styleAdd] + "+1", activePalette[styleDel] + "-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in diff display:\n%q", want, view)
		}
	}
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "12 - old") || strings.Contains(row, "12 + new") {
			if visibleWidth(row) != 60 {
				t.Fatalf("diff shading stops before the edge: %q", row)
			}
		}
	}
	SetPalette("none")
	if view := m.renderView(24, 30, -1); strings.Contains(view, "\x1b") || !strings.Contains(view, "12 - old") {
		t.Fatalf("colourless diff lost its meaning: %q", view)
	}
}

func TestTruecolorUsesSubtleDiffBackgroundsAndPreservesPlainOutput(t *testing.T) {
	SetPalette("truecolor")
	defer SetPalette("256")
	var out strings.Builder
	writeStyled(&out, "+ new", styleDiffAdd, true)
	writeStyled(&out, "- old", styleDiffDel, true)
	if !strings.Contains(out.String(), "48;2;") || strings.Contains(out.String(), "48;5;") {
		t.Fatalf("truecolor fell back to saturated indexed backgrounds: %q", out.String())
	}
	SetPalette("none")
	out.Reset()
	writeStyled(&out, "+ new", styleDiffAdd, true)
	if out.String() != "+ new" {
		t.Fatalf("colourless diff changed: %q", out.String())
	}
}

func TestElidedDiffResumesNumbersOnlyAtAKnownLocationAndKeepsTail(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("@@ -1,60 +1,60 @@\n")
	for i := 1; i <= 19; i++ {
		fmt.Fprintf(&patch, "-old line %d\n", i)
	}
	patch.WriteString("… 81 lines not shown …\n@@ -61,0 +41,20 @@\n")
	for i := 41; i <= 60; i++ {
		fmt.Fprintf(&patch, "+new line %d\n", i)
	}
	text, _ := workRecordText(WorkRecord{Name: "write_file", Changed: true, Diff: patch.String(), Added: 60, Removed: 60}, "")
	rows := renderMarkdownStyled(text, 80)
	view := strings.Join(rowTexts(rows), "\n")
	if !strings.Contains(view, "41 + new line 41") || !strings.Contains(view, "60 + new line 60") {
		t.Fatalf("tail was clipped or misnumbered:\n%s", view)
	}
	numbers := diffLineNumbers{}
	numbers.prefix("@@ -1 +1 @@", 80)
	numbers.prefix("… 30 lines not shown …", 80)
	if got := numbers.prefix("+unknown position", 80); strings.Contains(got, "1 +") {
		t.Fatal("invented a line number after omitted changes")
	}
	if diffStyle("--- SQL comment") != styleDiffDel {
		t.Fatal("a removed -- comment lost its deletion shading")
	}
}

// A wrapped work row keeps its shape: continuation rows hang under the text,
// never at column 0, and a heading's owner label moves whole with its
// separator — never "agent" on one row and "12" on the next.
func TestWrappedWorkRowsHangUnderTheirText(t *testing.T) {
	for _, c := range []struct{ line, hang string }{
		{"• Edited internal/tui/controller_window_layout.go (+3 -1) · agent 12", "  "},
		{"  └ Read /Users/francomichetti/kolkrabbi/internal/tui/model.go and its friends", "    "},
		{"    ↳ checking how the footer reserves room for the effort beside long models", "      "},
		{"    a later output line that is long enough to wrap around the width easily", "    "},
	} {
		for width := 30; width < 100; width++ {
			rows, _ := renderMarkdownStyledBlocks(c.line, width)
			for i, row := range rows {
				if cellWidth(row.text) > width {
					t.Fatalf("width %d: row %q is wider than the row", width, row.text)
				}
				if i > 0 && !strings.HasPrefix(row.text, c.hang) {
					t.Fatalf("width %d: %q wrapped to %q, which does not hang under its text", width, c.line, row.text)
				}
				if strings.Contains(row.text, "agent") && !strings.Contains(row.text, "· agent 12") {
					t.Fatalf("width %d: the owner label split: %q", width, row.text)
				}
			}
		}
	}
}

// Only an owner label is bound. A " · " inside what ran stays breakable, so a
// long command tail wraps between its words rather than mid-word.
func TestOnlyTheOwnerLabelIsBoundTogether(t *testing.T) {
	line := "• Ran echo done · the tests all passed on every platform we support"
	words := " " + strings.TrimPrefix(line, "• ") + " "
	rows, _ := renderMarkdownStyledBlocks(line, 40)
	for _, row := range rows {
		text := strings.TrimSpace(strings.TrimPrefix(row.text, "• "))
		if !strings.Contains(words, " "+text+" ") {
			t.Fatalf("row %q is not whole words of %q", row.text, line)
		}
	}
}

// Codex reads and searches through its shell, so exploration grouping has to
// recognise the commands that only read. Anything that could change the tree
// or run something else stays a command.
func TestShellReadsAndSearchesGroupAsExploration(t *testing.T) {
	shell := func(command string) WorkRecord {
		b, _ := json.Marshal(map[string]string{"command": command})
		return WorkRecord{Agent: 1, Name: "shell", Arguments: string(b), Output: "some output"}
	}
	for command, want := range map[string]string{
		"rg -n TODO internal/":            "└ Searched rg -n TODO internal/",
		"grep -rn 'func main' .":          "└ Searched grep -rn 'func main' .",
		"git grep -n Close":               "└ Searched git grep -n Close",
		"cat go.mod":                      "└ Read go.mod",
		"head -n 50 README.md":            "└ Read README.md",
		"cat -n main.go":                  "└ Read main.go",
		"sed -n '1,80p' main.go":          "└ Read main.go",
		"ls internal":                     "└ Listed internal",
		"ls":                              "└ Listed .",
		"find . -name '*.go'":             "└ Listed find . -name '*.go'",
		"rg -n Close internal | head -20": "└ Searched rg -n Close internal | head -20",
	} {
		text, group := workRecordText(shell(command), "")
		if !strings.Contains(text, "• Explored · agent 1") || !strings.Contains(text, want) || strings.Contains(text, "• Ran") || group != "agent 1:explore" {
			t.Errorf("%q rendered as:\n%s\nwant an exploration row %q", command, text, want)
		}
	}
	for _, command := range []string{
		"sed -i 's/a/b/' x.go", "sed -i -n '1p' x.go", "sed -n 'w out' main.go", "find . -name '*.tmp' -delete", "find . -exec rm {} ;",
		"cat a > b", "rg foo && rm x", "grep x $(cat list)", "echo hi", "tee out", "sort -o out in",
		"fd -x rm", "bash -lc 'rg foo' | tee out", "rg foo | xargs rm", "cat <(ls)",
	} {
		text, _ := workRecordText(shell(command), "")
		if !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") {
			t.Errorf("%q, which can change or run things, was grouped as exploration:\n%s", command, text)
		}
	}
	// Claude's LS tool lists, like list_dir.
	if text, _ := workRecordText(WorkRecord{Agent: 2, Name: "LS", Arguments: `{"path":"/src/app"}`}, ""); !strings.Contains(text, "└ Listed /src/app") {
		t.Errorf("LS rendered as:\n%s", text)
	}
	// Consecutive reads by one agent share one heading.
	first, group := workRecordText(shell("cat go.mod"), "")
	second, _ := workRecordText(shell("rg -n TODO ."), group)
	if strings.Count(first+second, "• Explored") != 1 {
		t.Errorf("two reads opened two groups:\n%s%s", first, second)
	}
}

// The work log's colour vocabulary reaches its supporting rows: results,
// purposes and output are muted, a failure is red, engine activity is purple,
// and the per-turn usage line is muted. Indented prose outside a work block
// keeps the plain style; the text itself never changes, so colourless output
// says the same.
func TestWorkLogSupportingRowsCarryTheirColour(t *testing.T) {
	text := "\n◆ planning (sel)…\n" +
		"\n• Failed go test ./... · agent 1\n" +
		"    ↳ run the tests\n" +
		"  └ × command failed\n" +
		"  └ --- FAIL: TestX\n" +
		"    second output line\n" +
		"\n  [code · sel · 120 tok · 800ms]\n" +
		"\n    indented prose that is not a work row\n"
	rows, _ := renderMarkdownStyledBlocks(text, 80)
	want := map[string]rowStyle{
		"◆ planning (sel)…":                         stylePurple,
		"• Failed go test ./... · agent 1":          styleWork,
		"    ↳ run the tests":                       stylePurpleMuted,
		"  └ × command failed":                      styleDel,
		"  └ --- FAIL: TestX":                       stylePurpleMuted,
		"    second output line":                    stylePurpleMuted,
		"  [code · sel · 120 tok · 800ms]":          stylePurpleMuted,
		"    indented prose that is not a work row": styleNone,
	}
	seen := 0
	for _, row := range rows {
		style, ok := want[row.text]
		if !ok {
			continue
		}
		seen++
		if row.style != style {
			t.Errorf("row %q has style %d, want %d", row.text, row.style, style)
		}
	}
	if seen != len(want) {
		t.Fatalf("saw %d of %d rows:\n%+v", seen, len(want), rows)
	}
}

// Adopted from the V43.5 verification (N1): commands that write or execute,
// through clustered short options, extra scripts or program options, must
// stay commands with their output shown. Checking only whole-argument
// prefixes let every one of these read as exploration.
func TestWritingOrExecutingCommandsNeverReadAsExploration(t *testing.T) {
	for _, command := range []string{
		`sed -ni '1,5p' main.go`, `sed -n -e 1p -e 'w /tmp/out' main.go`, `sed -n -e 1p -e '1e touch /tmp/pwn' main.go`,
		`sed -n 1p -s --in-place main.go`, `sed -n 1p main.go -i`, `sed -En -i '1p' main.go`, `sed --quiet -i 1p main.go`,
		`sed -n '1p' -f /tmp/script.sed main.go`, `sed -n --expression=1p --expression='w out' main.go`,
		`sort -uo out.txt in.txt`, `sort -ro out.txt in.txt`, `sort --compress-program=sh big.txt`,
		`tree -o listing.txt`, `tree -fo listing.txt`, `fd -Hx rm {}`, `fd -uX rm`, `fd --exec-batch rm`,
		`find . -execdir rm {} +`, `find . -fprintf out.txt %p`, `find . -okdir rm {} +`,
		`rg --pre sh pattern`, `rg --pre=./evil.sh pattern`, `rg -z --pre cat x`, `rg -z pattern`,
		`git grep -Ovim TODO`, `git grep --open-files-in-pager=sh TODO`, `git -c core.pager=sh grep TODO`,
		`cat a |& tee b`, `cat a >> b`, `cat a || rm b`, `FOO=1 cat a`, `command rm -rf x`, `exec rm x`,
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`, Output: "some output"}, "")
		if !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") || !strings.Contains(text, "some output") {
			t.Errorf("%q, which can write or execute, was not shown as a command with its output:\n%s", command, text)
		}
	}
	// An option that takes a value never names its value as what was read.
	for command, want := range map[string]string{
		"tree -L 2": "└ Listed .", "git ls-files src": "└ Listed src", "sort -t , -k 2 data.csv": "└ Read data.csv",
		"nl -w 3 main.go": "└ Read main.go", "ls -I vendor src": "└ Listed src",
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`}, "")
		if !strings.Contains(text, want) {
			t.Errorf("%q rendered as:\n%s\nwant %q", command, text, want)
		}
	}
}

// Codex runs every command through its shell as `/usr/bin/bash -lc '…'` (see
// spec/testdata/foreign/codex-tool-use.jsonl), so the wrapper is unwrapped —
// exactly a shell, -c or -lc, and one single-quoted script with no quote in
// it — and the script inside is held to the same rules.
func TestCodexShellWrappedReadsGroupAsExploration(t *testing.T) {
	record := func(command string) (string, string) {
		return workRecordText(WorkRecord{Agent: 1, Name: "command_execution", Arguments: `{"command":` + strconv.Quote(command) + `}`, Output: "some output"}, "")
	}
	for command, want := range map[string]string{
		"/usr/bin/bash -lc 'rg -n TODO internal'": "└ Searched rg -n TODO internal",
		"/bin/zsh -lc 'cat go.mod'":               "└ Read go.mod",
		"bash -c 'ls internal'":                   "└ Listed internal",
	} {
		if text, _ := record(command); !strings.Contains(text, "• Explored · agent 1") || !strings.Contains(text, want) {
			t.Errorf("%q rendered as:\n%s\nwant %q", command, text, want)
		}
	}
	// The exact shape the Codex adapter emits: tool "shell", the raw command
	// as its input rather than JSON.
	if text, _ := workRecordText(WorkRecord{Agent: 1, Name: "shell", Arguments: "/usr/bin/bash -lc 'rg -n Close internal'"}, ""); !strings.Contains(text, "└ Searched rg -n Close internal") {
		t.Errorf("a Codex-shaped record rendered as:\n%s", text)
	}
	for _, command := range []string{
		"/usr/bin/bash -lc 'rg -n TODO internal' extra", "bash -lc 'cat a' ; rm b", `bash -lc 'cat a'\''b'`,
		"bash -lc 'sed -i s/a/b/ x'", "bash -lc 'cat a && rm b'", "bash -x -c 'cat a'", "python -c 'print(1)'", "python3 -c 'ls'", "bash -s 'cat a'",
		"/usr/bin/bash -lc 'od -An -tx1 -v hello.txt && wc -c < hello.txt'",
	} {
		if text, _ := record(command); !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") {
			t.Errorf("%q was grouped as exploration:\n%s", command, text)
		}
	}
}

// Muting belongs to kolk's own work records only (V43.5 N6). A record's
// heading always ends with its owner label, and its rows have known shapes; an
// assistant bullet, or prose that follows a record, keeps the plain style.
func TestOnlyWorkRecordsAreMuted(t *testing.T) {
	for name, text := range map[string]string{
		"assistant bullet":     "\n• an assistant bullet point\n    deeper prose shaped like a work row\n  indented assistant prose\n",
		"prose after a record": "\n• Ran go test · agent 1\n  └ ok\n  child prose that follows the record\n",
	} {
		rows, _ := renderMarkdownStyledBlocks(text, 80)
		for _, row := range rows {
			if strings.Contains(row.text, "prose") && row.style != styleNone {
				t.Errorf("%s: row %q has style %d, want the plain style", name, row.text, row.style)
			}
		}
	}
}

// A heading's change counts move whole when it wraps (V43.5 N7): "(+12" on
// one row and "-3)" on the next is neither recognised as counts nor coloured.
func TestWrappedHeadingsKeepTheirCountsWhole(t *testing.T) {
	line := "• Edited internal/tui/controller_window_layout.go (+12 -3) · agent 3"
	for width := 30; width < 100; width++ {
		rows, _ := renderMarkdownStyledBlocks(line, width)
		for _, row := range rows {
			if (strings.Contains(row.text, "(+12") || strings.Contains(row.text, "-3)")) && !strings.Contains(row.text, "(+12 -3)") {
				t.Fatalf("width %d: the counts split across rows at %q", width, row.text)
			}
		}
	}
}

// Adopted from the V43.5 re-check (Q1): the shell removes quotes and escapes
// and expands $…, braces and globs before a tool sees its words, so an option
// can hide from a plain word split. Every such form stays a command.
func TestQuotedOrExpandedOptionsNeverReadAsExploration(t *testing.T) {
	for _, command := range []string{
		`find . '-delete'`, `find . "-delete"`, `find . \-delete`, `find . ''-delete`, `find . $'-delete'`,
		`find . {-delete,}`, `find . ${IFS}-delete`, `find . '-exec' rm '{}' '+'`, `sort '-o' out.txt in.txt`,
		`sort -u "-o" out.txt in.txt`, `sed -n 1,5p main.go '-i'`, `sed -n 1,5p main.go \-i`, `rg '--pre' sh pattern`,
		`rg "--pre=sh" pattern`, `tree '-o' out.txt`, `fd x '-x' rm`, `git grep '-Ovim' TODO`, `find . -name *`,
		`find . "${IFS}-delete"`, `bash -lc 'find . "-delete"'`, `bash -lc 'sort \-o out in'`, `/usr/bin/bash -lc 'find . ${IFS}-delete'`,
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`, Output: "some output"}, "")
		if !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") || !strings.Contains(text, "some output") {
			t.Errorf("%q hides an option from the check and was not shown as a command:\n%s", command, text)
		}
	}
	// Quotes are removed the way the shell removes them, so a quoted name
	// reads as that name, and a quoted | is a pattern, not a pipe.
	for command, want := range map[string]string{
		`cat 'my notes.txt'`:           "└ Read my notes.txt",
		`rg -n 'a|b' internal`:         "└ Searched rg -n 'a|b' internal",
		`ls ~/src`:                     "└ Listed ~/src",
		`rg -n "TODO" internal | head`: "└ Searched rg -n \"TODO\" internal | head",
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`}, "")
		if !strings.Contains(text, want) {
			t.Errorf("%q rendered as:\n%s\nwant %q", command, text, want)
		}
	}
}

// Adopted from the V43.5 re-check (Q2, Q3). For find, -- ends only the
// leading options, and the expression after it still runs. A wrapper's shell
// must be the system's, not any program whose name ends in sh.
func TestFindDashDashAndForeignShellsStayCommands(t *testing.T) {
	for _, command := range []string{
		`find -- . -delete`, `find -- . -fprint out.txt`,
		`./bash -lc 'cat a'`, `/tmp/x/sh -c 'ls'`, `bin/zsh -c 'ls'`, `/home/me/bin/bash -lc 'cat a'`,
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`, Output: "some output"}, "")
		if !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") {
			t.Errorf("%q was grouped as exploration:\n%s", command, text)
		}
	}
	for command, want := range map[string]string{
		`/bin/zsh -lc 'cat go.mod'`:              "└ Read go.mod",
		`/opt/homebrew/bin/bash -lc 'ls src'`:    "└ Listed src",
		`/usr/local/bin/bash -c 'rg -n -- -x .'`: "└ Searched rg -n -- -x .",
		`grep -rn -- -delete .`:                  "└ Searched grep -rn -- -delete .",
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`}, "")
		if !strings.Contains(text, want) {
			t.Errorf("%q rendered as:\n%s\nwant %q", command, text, want)
		}
	}
}

// Adopted from the V43.5 re-check (Q4): a blank line of tool output is
// written as the output indent alone, so it is still a row of the record. A
// truly blank line still ends it.
func TestABlankOutputRowDoesNotEndTheRecord(t *testing.T) {
	text, _ := workRecordText(WorkRecord{Agent: 1, Name: "bash", Arguments: `{"command":"go test ./..."}`,
		Output: "--- FAIL: TestRender\n\n    render_test.go:12: got 3\nFAIL\n[exit error: exit status 1]"}, "")
	if !strings.Contains(text, "\n    \n") {
		t.Fatalf("the blank output line is no longer the output indent alone:\n%q", text)
	}
	rows, _ := renderMarkdownStyledBlocks("\n"+text, 80)
	for _, want := range []string{"render_test.go:12", "FAIL", "[exit error"} {
		found := false
		for _, row := range rows {
			if strings.Contains(row.text, want) && !strings.Contains(row.text, "TestRender") {
				found = true
				if row.style != stylePurpleMuted {
					t.Errorf("%q after the blank output row has style %d, want muted", row.text, row.style)
				}
			}
		}
		if !found {
			t.Errorf("no row shows %q:\n%s", want, text)
		}
	}
	rows, _ = renderMarkdownStyledBlocks("\n• Ran go test · agent 1\n  └ ok\n\n    indented prose after a blank line\n", 80)
	for _, row := range rows {
		if strings.Contains(row.text, "prose") && row.style != styleNone {
			t.Errorf("a blank line no longer ends the record: %q has style %d", row.text, row.style)
		}
	}
}

// Adopted from the V43.5 final re-check (R1): tree does not use getopt. It
// reads a cluster one letter at a time, and -L, -I, -P and -o each take the
// next word, so -Lo 2 out.txt is a level and an output file, not -L "o".
func TestTreeReadsAClusterLetterByLetter(t *testing.T) {
	for _, command := range []string{
		`tree -Lo 2 out.txt`, `tree -L2o out2.txt`, `tree -Io x out.txt`, `tree -Po x out.txt`,
		`tree -L=2 .`, `tree -I2 x`, `bash -lc 'tree -Lo 2 out.txt'`,
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`, Output: "some output"}, "")
		if !strings.Contains(text, "• Ran") || strings.Contains(text, "Explored") || !strings.Contains(text, "some output") {
			t.Errorf("%q was grouped as exploration:\n%s", command, text)
		}
	}
	for command, want := range map[string]string{
		`tree -L 2`:                "└ Listed .",
		`tree -L2 internal`:        "└ Listed internal",
		`tree -a -L 3 -I vendor x`: "└ Listed x",
		`head -n5 main.go`:         "└ Read main.go",
	} {
		text, _ := workRecordText(WorkRecord{Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`}, "")
		if !strings.Contains(text, want) {
			t.Errorf("%q rendered as:\n%s\nwant %q", command, text, want)
		}
	}
}

// Adopted from the V43.5 final re-check (R2): a blank first line of output is
// the result branch alone ("  └ "), and like a blank later row (Q4) it is
// still a row of the record. npm prints one before its build output.
func TestABlankFirstOutputRowDoesNotEndTheRecord(t *testing.T) {
	text, _ := workRecordText(WorkRecord{Agent: 1, Name: "bash", Arguments: `{"command":"npm run build"}`,
		Output: "\n> app@1.0.0 build\n> tsc\n\nerror TS2322"}, "")
	if !strings.Contains(text, "  └ \n") {
		t.Fatalf("the blank first output line is no longer the result branch alone:\n%q", text)
	}
	rows, _ := renderMarkdownStyledBlocks("\n"+text, 80)
	for _, want := range []string{"app@1.0.0 build", "> tsc", "error TS2322"} {
		found := false
		for _, row := range rows {
			if strings.Contains(row.text, want) {
				found = true
				if row.style != stylePurpleMuted {
					t.Errorf("%q after the blank first output row has style %d, want muted", row.text, row.style)
				}
			}
		}
		if !found {
			t.Errorf("no row shows %q:\n%s", want, text)
		}
	}
	// Only the branch kolk writes continues a record; a row that merely
	// starts with the glyph does not.
	rows, _ = renderMarkdownStyledBlocks("\n• Ran go test · agent 1\n  └ ok\n  └prose that only looks like a result\n", 80)
	for _, row := range rows {
		if strings.Contains(row.text, "prose") && row.style != styleNone {
			t.Errorf("a lookalike row continued the record: %q has style %d", row.text, row.style)
		}
	}
}

// Adopted from Codex's V43.5 review (C1, C2; its unposted regressions in
// /tmp/kolk-v43-5-extra-worklog_test.go): a record says only what was
// observed. A provider tool that failed, never finished or warned did not
// complete, and counts that were reported are not "no text changes".
func TestUnsettledOrUnexcerptedWorkClaimsNothingItDidNotSee(t *testing.T) {
	for _, c := range []struct {
		name   string
		record WorkRecord
		want   string
	}{
		{"unfinished", WorkRecord{Name: "Bash", Provider: true, Pending: true}, "└ (no output reported)"},
		{"failed", WorkRecord{Name: "Bash", Provider: true, Failed: true}, "└ (no output reported)"},
		{"warning", WorkRecord{Name: "Bash", Provider: true, Warning: true}, "└ (no output reported)"},
		{"completed", WorkRecord{Name: "Bash", Provider: true}, "└ (completed; no output reported)"},
		{"native", WorkRecord{Name: "bash", Arguments: `{"command":"true"}`}, "└ (no output)"},
		{"counts without an excerpt", WorkRecord{Name: "file_change", Path: "main.go", Changed: true, Provider: true, Added: 4, Removed: 2}, "└ (no excerpt reported)"},
		{"removals without an excerpt", WorkRecord{Name: "file_change", Path: "old.go", Changed: true, Provider: true, Removed: 3}, "└ (no excerpt reported)"},
		{"no change", WorkRecord{Name: "edit_file", Path: "main.go", Changed: true}, "└ no text changes"},
	} {
		got, _ := workRecordText(c.record, "")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: record reads\n%s\nwant %q", c.name, got, c.want)
		}
		if c.name != "completed" && strings.Contains(got, "(completed;") {
			t.Errorf("%s: record claims a completion it did not observe:\n%s", c.name, got)
		}
		if c.name == "counts without an excerpt" && (strings.Contains(got, "no text changes") || !strings.Contains(got, "(+4 -2)")) {
			t.Errorf("%s: reported counts contradicted or dropped:\n%s", c.name, got)
		}
	}
}

// Adopted from the V43.5 C1/C2 re-check (C5): only a command's failure is a
// "command failed"; a failed read or edit gets a mark that fits any tool.
func TestAFailedToolThatIsNotACommandIsNotCalledOne(t *testing.T) {
	for name, c := range map[string]struct {
		record WorkRecord
		want   string
	}{
		"read":    {WorkRecord{Name: "Read", Arguments: `{"file_path":"/work/missing.go"}`, Provider: true, Failed: true}, "└ × failed"},
		"edit":    {WorkRecord{Name: "Edit", Arguments: `{"file_path":"/work/main.go"}`, Provider: true, Failed: true}, "└ × failed"},
		"command": {WorkRecord{Name: "Bash", Arguments: `{"command":"make build"}`, Provider: true, Failed: true}, "└ × command failed"},
	} {
		got, _ := workRecordText(c.record, "")
		if !strings.Contains(got, c.want) || (name != "command" && strings.Contains(got, "command failed")) {
			t.Errorf("%s: record reads\n%s\nwant %q", name, got, c.want)
		}
	}
}
