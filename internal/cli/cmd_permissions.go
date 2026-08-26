package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
)

// A rule's scope is how long the user meant it to last. The three that persist
// nothing, persist here, and persist everywhere are the whole set: anything
// finer is a distinction people have to remember instead of read.
const (
	scopeSession = "session"
	scopeProject = "project"
	scopeAlways  = "always"
)

// scopedRule is one rule as it will be shown: the line the user wrote and
// where it came from. Listing a rule without its scope invites someone to
// delete a global rule believing it was this project's.
type scopedRule struct {
	line  string
	scope string
}

// permissionRuleWords are the openings that make a /permissions argument a rule
// rather than a tier name.
var permissionRuleWords = map[string]bool{
	"allow": true, "ask": true, "deny": true, "confirm": true, "refuse": true,
	"forget": true, "remove": true,
}

// looksLikeRule reports whether an argument to /permissions is a rule edit.
func looksLikeRule(arg string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(arg), " ")
	return permissionRuleWords[strings.ToLower(first)] && strings.Contains(arg, " ")
}

// editRule adds or forgets one rule and puts the result into effect now.
func (a *app) editRule(ag *engine.Agent, arg string) {
	verb, rest, _ := strings.Cut(strings.TrimSpace(arg), " ")
	if verb = strings.ToLower(verb); verb == "forget" || verb == "remove" {
		a.forgetRule(ag, strings.TrimSpace(rest))
		return
	}
	a.addRule(ag, strings.TrimSpace(arg))
}

// addRule stores a rule after checking that it parses. A permission file that
// accepts a line Kolkrabbi will later refuse to read is a file that silently
// stops protecting anything.
func (a *app) addRule(ag *engine.Agent, arg string) {
	line, scope := splitScope(arg)
	if _, err := engine.ParseRule(line); err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return
	}

	if scope == scopeSession {
		a.sessionRules = append(a.sessionRules, line)
	} else {
		root := ag.Root
		if scope == scopeAlways {
			root = ""
		}
		if err := a.updateStoredRules(func(stored *config.Permissions) { stored.Add(line, root) }); err != nil {
			fmt.Fprintf(a.stderr, "could not save the rule: %v\n", err)
			return
		}
	}

	a.applyRules(ag)
	fmt.Fprintf(a.stdout, "rule added (%s): %s\n", scope, line)
	if scope == scopeSession {
		fmt.Fprintln(a.stdout, "\033[2mthis process only — add it again with `always` or `project` to keep it.\033[0m")
	}
}

// forgetRule removes a rule by the number shown in the listing. Numbers are
// used because retyping a glob exactly is how people end up with two rules and
// delete the wrong one.
func (a *app) forgetRule(ag *engine.Agent, which string) {
	rules := a.activeRules(ag)
	index, err := strconv.Atoi(which)
	if err != nil || index < 1 || index > len(rules) {
		fmt.Fprintf(a.stderr, "%q is not one of the %d rules; run /permissions to see them.\n", which, len(rules))
		return
	}
	target := rules[index-1]

	if target.scope == scopeSession {
		kept := a.sessionRules[:0]
		for _, line := range a.sessionRules {
			if line != target.line {
				kept = append(kept, line)
			}
		}
		a.sessionRules = kept
	} else {
		root := ag.Root
		if target.scope == scopeAlways {
			root = ""
		}
		if err := a.updateStoredRules(func(stored *config.Permissions) { stored.Remove(target.line, root) }); err != nil {
			fmt.Fprintf(a.stderr, "could not save the change: %v\n", err)
			return
		}
	}

	a.applyRules(ag)
	fmt.Fprintf(a.stdout, "rule removed (%s): %s\n", target.scope, target.line)
}

// splitScope separates a trailing scope word from the rule itself. The scope
// goes last because the rule is what someone is thinking about when they type.
func splitScope(arg string) (line, scope string) {
	close := strings.LastIndex(arg, ")")
	if close < 0 {
		return arg, scopeProject
	}
	trailing := strings.ToLower(strings.TrimSpace(arg[close+1:]))
	switch trailing {
	case scopeSession, scopeProject, scopeAlways:
		return strings.TrimSpace(arg[:close+1]), trailing
	case "once":
		// "once" is what the confirmation prompt offers; from a command it can
		// only mean the rest of this session.
		return strings.TrimSpace(arg[:close+1]), scopeSession
	default:
		return strings.TrimSpace(arg), scopeProject
	}
}

// activeRules is every rule in effect, in the order they are applied: global,
// then this project's, then this session's. The order is not cosmetic — the
// last match wins, so a listing in any other order would misrepresent what
// actually happens.
func (a *app) activeRules(ag *engine.Agent) []scopedRule {
	stored, err := a.storedRules()
	if err != nil {
		fmt.Fprintf(a.stderr, "could not read the rule file: %v\n", err)
		stored = &config.Permissions{}
	}
	rules := make([]scopedRule, 0, len(stored.Always)+len(a.sessionRules))
	for _, line := range stored.Always {
		rules = append(rules, scopedRule{line, scopeAlways})
	}
	if ag.Root != "" {
		for _, line := range stored.Projects[ag.Root] {
			rules = append(rules, scopedRule{line, scopeProject})
		}
	}
	for _, line := range a.sessionRules {
		rules = append(rules, scopedRule{line, scopeSession})
	}
	return rules
}

// applyRules puts the current rule set into the running session. A rule the
// user has to restart to feel is a rule they will assume did not work.
func (a *app) applyRules(ag *engine.Agent) {
	var parsed engine.Rules
	for _, rule := range a.activeRules(ag) {
		one, err := engine.ParseRule(rule.line)
		if err != nil {
			// One unreadable line must not silently disable the others,
			// including the ones that refuse things.
			fmt.Fprintf(a.stderr, "ignoring a permission rule: %v\n", err)
			continue
		}
		parsed = append(parsed, one)
	}
	ag.Rules = parsed
}

func (a *app) storedRules() (*config.Permissions, error) {
	d, err := a.locate()
	if err != nil {
		return nil, err
	}
	return config.LoadPermissions(config.PermissionsFile(d.Config))
}

func (a *app) updateStoredRules(change func(*config.Permissions)) error {
	d, err := a.locate()
	if err != nil {
		return err
	}
	file := config.PermissionsFile(d.Config)
	stored, err := config.LoadPermissions(file)
	if err != nil {
		return err
	}
	change(stored)
	return config.SavePermissions(file, stored)
}

// printRules lists what is in effect beneath the tier summary.
func (a *app) printRules(ag *engine.Agent) {
	rules := a.activeRules(ag)
	if len(rules) == 0 {
		fmt.Fprintln(a.stdout, "\nno rules. add one with /permissions allow bash(git *) [session|project|always]")
		return
	}
	fmt.Fprintln(a.stdout, "\nrules, in the order they apply — the last one that matches wins:")
	for i, rule := range rules {
		fmt.Fprintf(a.stdout, "  %d. %-34s %s\n", i+1, rule.line, rule.scope)
	}
	fmt.Fprintln(a.stdout, "\nadd: /permissions allow bash(git *) [session|project|always]")
	fmt.Fprintln(a.stdout, "drop: /permissions forget <number>")
}
