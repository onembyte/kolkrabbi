package cli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/tui"
)

// Every slash command whose usage names fixed words offers those words for
// completion: what the help line says you may type, the composer will finish
// for you. The expectation is derived from the usage text itself, so a new
// command or a new word cannot be added to one without the other.
func TestEveryCommandWithFixedWordsOffersThemForCompletion(t *testing.T) {
	specs := map[string]tui.CommandSpec{}
	for _, spec := range slashSuggestions() {
		specs[spec.Name] = spec
	}
	for _, command := range slashCommandTable {
		want := literalWords(command.args)
		if len(want) == 0 {
			continue
		}
		spec := specs[command.name]
		offered := map[string]bool{}
		for _, choice := range spec.Choices {
			for _, word := range choice.Words {
				offered[word] = true
			}
		}
		for _, word := range want {
			if !offered[word] {
				t.Errorf("/%s usage says %q may be typed, but completion never offers it (offers %v)", command.name, word, offered)
			}
		}
	}
	// Dynamic vocabularies ride along: themes and keyed vendors.
	for _, word := range []string{"kolkrabbi", "nord", "quiet"} {
		if !hasWord(specs["theme"], word) {
			t.Errorf("/theme does not offer %q", word)
		}
	}
	for _, word := range []string{"openrouter", "google", "xai"} {
		if !hasWord(specs["key"], word) {
			t.Errorf("/key does not offer %q", word)
		}
	}
}

func hasWord(spec tui.CommandSpec, word string) bool {
	for _, choice := range spec.Choices {
		for _, w := range choice.Words {
			if w == word {
				return true
			}
		}
	}
	return false
}

// literalWords reads the fixed words out of a usage string: the alternatives
// inside <a|b> and [a|b], and bare words such as "login" or "--json". A
// placeholder — anything in angle brackets, or a bracketed name such as
// [filter], [path], [n] — names nothing fixed.
func literalWords(args string) []string {
	var words []string
	seen := map[string]bool{}
	placeholders := map[string]bool{"filter": true, "path": true, "n": true, "effort": true, "id": true, "alias": true, "inline": true}
	add := func(token string) {
		token = strings.TrimSpace(token)
		if token == "" || strings.HasPrefix(token, "<") {
			return
		}
		w := strings.Trim(token, "[]")
		if w == "" || seen[w] || placeholders[w] || strings.ContainsAny(w, "<>[]…") {
			return
		}
		seen[w] = true
		words = append(words, w)
	}
	first := func(alt string) string {
		fields := strings.Fields(strings.TrimSpace(alt))
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
	for _, group := range regexpAlternatives(args) {
		for _, alt := range strings.Split(group, "|") {
			add(first(alt))
		}
	}
	for _, alt := range strings.Split(args, "|") {
		add(first(alt))
	}
	return words
}

func regexpAlternatives(args string) []string {
	var groups []string
	depth, start := 0, -1
	for i, r := range args {
		switch r {
		case '<', '[':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case '>', ']':
			depth--
			if depth == 0 && start >= 0 {
				inner := args[start:i]
				if strings.Contains(inner, "|") {
					groups = append(groups, inner)
				}
				start = -1
			}
		}
	}
	return groups
}
