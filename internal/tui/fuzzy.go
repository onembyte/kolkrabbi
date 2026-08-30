package tui

import "strings"

// fuzzyScore reports whether haystack matches query the way every picker in
// the app now agrees a match looks: every whitespace-separated token of query
// must be a case-insensitive subsequence of haystack, in any order across
// tokens. Empty query matches everything with a zero score, preserving
// matchesFilter's existing "no filter shows all" behaviour — this replaces
// matchesFilter rather than sitting beside it, so every surface that filtered
// on a whole substring per token now tolerates a scattered one too: "cld"
// finds "claude" the way "claude" itself already did.
//
// ok reports whether every token matched. When ok, score ranks the match —
// higher is better — so the row meant usually sits on top without arrowing
// down to it, the same shape Claude Code's and Codex's own pickers rank by: a
// run of contiguous characters outscores the same characters scattered, and a
// match starting at a word boundary (the start of the string, or right after
// a space, `/`, `-`, `_`, `.` or `:`) outscores one starting mid-word.
func fuzzyScore(haystack, query string) (score int, ok bool) {
	return fuzzyScoreFields([]string{haystack}, query)
}

// fuzzyMatches is the boolean-only shape most call sites want, kept separate
// from fuzzyScore so a caller that only filters — not ranks — never has to
// name a score it will not use.
func fuzzyMatches(haystack, query string) bool {
	_, ok := fuzzyScore(haystack, query)
	return ok
}

// fuzzyScoreFields is fuzzyScore's contract extended over several distinct
// fields rather than one haystack: every whitespace token of query must be a
// subsequence of at least one field on its own, never of their concatenation.
// A caller with fields worth keeping apart — a setting's key, summary and
// value; a plan's provider and name — must join them for search without a
// single token being able to thread its subsequence through the join: two
// unrelated fields that each carry one 'f' must not let "eff" match by taking
// one 'f' from each. Different tokens of the same query may still land in
// different fields — "anthropic max" still finds a plan whose provider is
// "anthropic" and whose name is "Claude Max" — because each token is judged
// against every field independently and only needs one field to win.
func fuzzyScoreFields(fields []string, query string) (score int, ok bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, true
	}
	for _, token := range strings.Fields(strings.ToLower(query)) {
		best, found := 0, false
		for _, field := range fields {
			tokenScore, matched := subsequenceScore(strings.ToLower(field), token)
			if matched && (!found || tokenScore > best) {
				best, found = tokenScore, true
			}
		}
		if !found {
			return 0, false
		}
		score += best
	}
	return score, true
}

// boundaryBonus rewards a token's first character landing right where a
// person's eye would look for it — the start of the field, or the start of a
// word inside it — over the same character buried mid-word.
const boundaryBonus = 8

// subsequenceScore finds token as a subsequence of haystack — both already
// lowercased — scanning once and greedily taking the earliest occurrence of
// each rune in turn. That greedy choice is what makes an earlier, tighter run
// score higher: taking a rune as soon as it appears leaves the most room for
// the runes after it to land close behind, rather than search for a
// theoretically tighter run further in and risk finding none at all.
//
// Every character after the first costs the size of the gap since the one
// before it — zero for a character that continues right on from its
// predecessor, negative for one that does not. A separate bonus for
// contiguity on top of that would be redundant: a run that never gaps already
// costs nothing, which is already enough to outscore the same letters found
// scattered.
func subsequenceScore(haystack, token string) (score int, ok bool) {
	h := []rune(haystack)
	needle := []rune(token)
	previous := -1
	needleIndex := 0
	for position, r := range h {
		if needleIndex >= len(needle) {
			break
		}
		if r != needle[needleIndex] {
			continue
		}
		if needleIndex == 0 {
			if position == 0 || isWordBoundary(h[position-1]) {
				score += boundaryBonus
			}
		} else {
			score -= position - previous - 1
		}
		previous = position
		needleIndex++
	}
	return score, needleIndex == len(needle)
}

func isWordBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '/', '-', '_', '.', ':':
		return true
	}
	return false
}
