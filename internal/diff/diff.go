// Package diff renders a unified diff of two texts.
//
// It exists so that a person approving an edit is shown the change rather than
// a description of it. That is the whole requirement, and it is why this is a
// presentation primitive with no notion of files, paths or permissions: it
// takes two strings and returns lines a human can read.
//
// The algorithm is a common-prefix/suffix trim followed by an LCS over what is
// left, with a size guard. It is not the fastest diff anyone has written. It is
// in front of somebody waiting to answer a prompt, so the property that matters
// is that it always returns, quickly, on anything a tool call can produce.
package diff

import (
	"fmt"
	"strings"
)

// maxLCSCells bounds the quadratic step. Beyond it the two texts are reported
// as a wholesale replacement, which is both true and instant — a diff nobody
// can read is not worth making a person wait for.
const maxLCSCells = 4 << 20

// Unified renders a unified diff with the given number of context lines.
// Identical inputs produce the empty string.
func Unified(before, after string, context int) string {
	if before == after {
		return ""
	}
	if context < 0 {
		context = 0
	}
	oldLines, newLines := splitLines(before), splitLines(after)

	edits := diffLines(oldLines, newLines)
	return render(edits, context)
}

// splitLines splits on newlines without inventing a trailing empty line for
// text that ends in one — a phantom line shows up in a diff as a change nobody
// made.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// kind is what happened to one line.
type kind uint8

const (
	same kind = iota
	removed
	added
)

type edit struct {
	kind kind
	text string
}

// diffLines produces the edit script.
func diffLines(oldLines, newLines []string) []edit {
	// Shared prefix and suffix are the bulk of any real edit, and taking them
	// out first is what keeps the quadratic step small enough to be honest.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	oldMid := oldLines[prefix : len(oldLines)-suffix]
	newMid := newLines[prefix : len(newLines)-suffix]

	edits := make([]edit, 0, len(oldLines)+len(newLines))
	for _, line := range oldLines[:prefix] {
		edits = append(edits, edit{same, line})
	}
	edits = append(edits, middle(oldMid, newMid)...)
	for _, line := range oldLines[len(oldLines)-suffix:] {
		edits = append(edits, edit{same, line})
	}
	return edits
}

// middle diffs the part that actually changed.
func middle(oldMid, newMid []string) []edit {
	switch {
	case len(oldMid) == 0 && len(newMid) == 0:
		return nil
	case len(oldMid) == 0:
		return allOf(added, newMid)
	case len(newMid) == 0:
		return allOf(removed, oldMid)
	case len(oldMid)*len(newMid) > maxLCSCells:
		// Too big to align line by line. Reporting it as a replacement is
		// accurate and instant; a person reading a rewrite of this size is
		// reading "everything changed" either way.
		return append(allOf(removed, oldMid), allOf(added, newMid)...)
	}
	return lcsEdits(oldMid, newMid)
}

func allOf(k kind, lines []string) []edit {
	edits := make([]edit, len(lines))
	for i, line := range lines {
		edits[i] = edit{k, line}
	}
	return edits
}

// lcsEdits walks a longest-common-subsequence table into an edit script.
func lcsEdits(oldMid, newMid []string) []edit {
	rows, cols := len(oldMid)+1, len(newMid)+1
	table := make([]int, rows*cols)
	at := func(i, j int) int { return i*cols + j }

	for i := len(oldMid) - 1; i >= 0; i-- {
		for j := len(newMid) - 1; j >= 0; j-- {
			if oldMid[i] == newMid[j] {
				table[at(i, j)] = table[at(i+1, j+1)] + 1
				continue
			}
			table[at(i, j)] = max(table[at(i+1, j)], table[at(i, j+1)])
		}
	}

	var edits []edit
	i, j := 0, 0
	for i < len(oldMid) && j < len(newMid) {
		switch {
		case oldMid[i] == newMid[j]:
			edits = append(edits, edit{same, oldMid[i]})
			i, j = i+1, j+1
		case table[at(i+1, j)] >= table[at(i, j+1)]:
			edits = append(edits, edit{removed, oldMid[i]})
			i++
		default:
			edits = append(edits, edit{added, newMid[j]})
			j++
		}
	}
	edits = append(edits, allOf(removed, oldMid[i:])...)
	return append(edits, allOf(added, newMid[j:])...)
}

// render turns the edit script into hunks with located headers.
//
// Unchanged runs longer than twice the context are cut: forty identical lines
// between two edits are not context, they are the reason people stop reading
// diffs.
func render(edits []edit, context int) string {
	var out strings.Builder
	oldLine, newLine := 1, 1

	for index := 0; index < len(edits); {
		if edits[index].kind == same {
			oldLine, newLine = oldLine+1, newLine+1
			index++
			continue
		}

		// Walk back over the context this hunk should open with.
		start := index
		for start > 0 && edits[start-1].kind == same && index-start < context {
			start--
		}
		// And forward to the end of the changed run plus its trailing context.
		end := index
		for end < len(edits) {
			if edits[end].kind != same {
				end++
				continue
			}
			run := 0
			for end+run < len(edits) && edits[end+run].kind == same {
				run++
			}
			if run > context && end+run < len(edits) {
				end += context
				break
			}
			if end+run >= len(edits) {
				end += min(run, context)
				break
			}
			end += run
		}

		oldStart, newStart := oldLine-(index-start), newLine-(index-start)
		oldCount, newCount := 0, 0
		var body strings.Builder
		for _, e := range edits[start:end] {
			switch e.kind {
			case same:
				oldCount, newCount = oldCount+1, newCount+1
				body.WriteString(" " + e.text + "\n")
			case removed:
				oldCount++
				body.WriteString("-" + e.text + "\n")
			default:
				newCount++
				body.WriteString("+" + e.text + "\n")
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		out.WriteString(body.String())

		for _, e := range edits[index:end] {
			switch e.kind {
			case same:
				oldLine, newLine = oldLine+1, newLine+1
			case removed:
				oldLine++
			default:
				newLine++
			}
		}
		index = end
	}
	return out.String()
}

// Truncate keeps the head and the tail of a long diff and says how much is
// missing.
//
// The middle goes, not the tail. The last hunk matters as much as the first,
// and a preview that always drops the end teaches people that the end does not
// matter — which is exactly wrong when what they are approving is a change to
// their files.
func Truncate(text string, maxLines int) string {
	if maxLines < 4 {
		maxLines = 4
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) <= maxLines {
		return text
	}
	head := maxLines / 2
	tail := maxLines - head
	hidden := len(lines) - head - tail

	var out strings.Builder
	for _, line := range lines[:head] {
		out.WriteString(line + "\n")
	}
	fmt.Fprintf(&out, "… %d lines not shown …\n", hidden)
	for _, line := range lines[len(lines)-tail:] {
		out.WriteString(line + "\n")
	}
	return out.String()
}
