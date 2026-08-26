package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

// A Rule is one standing answer to a question the user got tired of being
// asked — or one they want asked every time regardless of tier.
//
// The grammar is a single line, because a permission list people cannot read
// at a glance is a permission list people stop reading:
//
//	allow bash(git *)
//	deny  write(*/migrations/*)
//	ask   write(*)
type Rule struct {
	Decision Verdict
	Family   string // bash | read | write | *
	Pattern  string
	Source   string // the line as written, so a message can quote it back
}

// Rules are consulted in order and the last match wins, the way every
// allow/deny list people already know behaves: later lines refine earlier ones.
type Rules []Rule

// ruleFamilies maps what a person writes to the tools it covers. A rule names
// a family rather than a tool because "write" is the thing being permitted;
// whether it arrives as write_file or edit_file is Kolkrabbi's business.
var ruleFamilies = map[string][]string{
	"bash":  {"bash"},
	"shell": {"bash"},
	"read":  {"read_file", "list_dir"},
	"write": {"write_file", "edit_file"},
	"edit":  {"write_file", "edit_file"},
	"any":   nil, // nil means every tool
	"*":     nil,
}

// ParseRules reads a whole list, reporting the first line that does not parse.
func ParseRules(lines []string) (Rules, error) {
	var rules Rules
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rule, err := ParseRule(trimmed)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ParseRule reads one line. Every error quotes the line back, because a
// permission file that fails to load without saying where is one people fix by
// deleting.
func ParseRule(line string) (Rule, error) {
	trimmed := strings.TrimSpace(line)
	bad := func(what string) (Rule, error) {
		return Rule{}, fmt.Errorf("%s in permission rule %q (expected: allow|ask|deny tool(pattern))", what, trimmed)
	}

	decision, rest, found := strings.Cut(trimmed, " ")
	if !found {
		return bad("missing what to apply it to")
	}
	verdict, ok := parseRuleDecision(decision)
	if !ok {
		return bad("unknown decision " + decision)
	}

	rest = strings.TrimSpace(rest)
	family, pattern, found := strings.Cut(rest, "(")
	if !found || !strings.HasSuffix(pattern, ")") {
		return bad("missing (pattern)")
	}
	family = strings.ToLower(strings.TrimSpace(family))
	if _, known := ruleFamilies[family]; !known {
		return bad("unknown tool " + family)
	}
	pattern = strings.TrimSuffix(pattern, ")")
	if strings.TrimSpace(pattern) == "" {
		return bad("empty pattern")
	}

	return Rule{Decision: verdict, Family: family, Pattern: expandHome(pattern), Source: trimmed}, nil
}

func parseRuleDecision(word string) (Verdict, bool) {
	switch strings.ToLower(word) {
	case "allow":
		return VerdictAllow, true
	case "ask", "confirm":
		return VerdictAsk, true
	case "deny", "refuse":
		return VerdictDeny, true
	default:
		return 0, false
	}
}

// expandHome resolves a leading ~ so `deny write(~/.ssh/*)` means what it looks
// like it means on the machine it was written on.
func expandHome(pattern string) string {
	if pattern != "~" && !strings.HasPrefix(pattern, "~/") {
		return pattern
	}
	home, err := paths.UserHomeDir()
	if err != nil {
		return pattern
	}
	return filepath.ToSlash(home) + strings.TrimPrefix(pattern, "~")
}

// match reports whether this rule covers the request.
func (r Rule) match(request tools.Request) bool {
	names := ruleFamilies[r.Family]
	if names != nil && !contains(names, request.Tool) {
		return false
	}
	for _, target := range r.targets(request) {
		if target != "" && globMatch(r.Pattern, target) {
			return true
		}
	}
	return false
}

// targets are the strings a pattern is tried against. A path rule is matched
// against both spellings of the path so that `write(src/*)` written the way the
// user sees it and `write(/home/me/p/src/*)` both work.
func (r Rule) targets(request tools.Request) []string {
	if request.Command != "" {
		if r.Family == "bash" || ruleFamilies[r.Family] == nil {
			return []string{request.Command}
		}
		return nil
	}
	return []string{request.Display, filepath.ToSlash(request.Path)}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// Match returns the decision of the last rule that covers the request.
func (rs Rules) Match(request tools.Request) (Rule, bool) {
	matched := Rule{}
	found := false
	for _, rule := range rs {
		if rule.match(request) {
			matched, found = rule, true
		}
	}
	return matched, found
}

// globMatch is a deliberately small glob: `*` stands for any run of characters,
// including `/`, and everything else is literal. People writing a permission
// line reach for `*` and nothing else, and a pattern language with more corners
// than that is one where a rule can mean something its author did not intend.
func globMatch(pattern, s string) bool {
	// Iterative backtracking: linear in the common case, no recursion depth to
	// worry about on a hostile pattern.
	var star, mark int
	star = -1
	i, j := 0, 0
	for i < len(s) {
		switch {
		case j < len(pattern) && (pattern[j] == s[i]):
			i++
			j++
		case j < len(pattern) && pattern[j] == '*':
			star, mark = j, i
			j++
		case star >= 0:
			j = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for j < len(pattern) && pattern[j] == '*' {
		j++
	}
	return j == len(pattern)
}
