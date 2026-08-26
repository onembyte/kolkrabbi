package cli

import (
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// planRules are what plan mode adds: read-only, for this session only.
//
// Built out of E13's rules rather than a new mode flag, so there stays exactly
// one place where "may I do this" is answered and `/permissions` can show a
// plan-mode session precisely what is refusing it. A second permission system
// with its own precedence is how two systems end up disagreeing.
var planRules = []string{"deny write(*)", "deny bash(*)"}

// planInstruction tells the model what the refusals are for.
//
// Refusing the tools without saying why produces a model that keeps trying them
// and reporting failures. What is wanted is one that explores and proposes.
const planInstruction = `You are in plan mode. Read and explore as much as you need, but you cannot write files or run commands: those tools will refuse. Produce a concrete plan — the files you would change, what each change does, and anything you are unsure about — and stop there. The user leaves plan mode when they are ready for you to act.`

// planMode enters or leaves read-only planning.
func (a *app) planMode(ag *engine.Agent, arg string) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "off", "exit", "done":
		a.leavePlanMode(ag)
	default:
		a.enterPlanMode(ag)
	}
}

func (a *app) enterPlanMode(ag *engine.Agent) {
	if a.inPlanMode() {
		fmt.Fprintln(a.stdout, "already in plan mode. /plan off when you are ready to act.")
		return
	}
	a.sessionRules = append(a.sessionRules, planRules...)
	a.applyRules(ag)
	ag.SetExtraSystem(planInstruction)

	fmt.Fprintln(a.stdout, "plan mode: reading only. Writes and shell commands are refused, whatever the tier.")
	fmt.Fprintln(a.stdout, "\033[2m/permissions shows the rules doing it · /plan off to act on the plan\033[0m")
}

func (a *app) leavePlanMode(ag *engine.Agent) {
	if !a.inPlanMode() {
		fmt.Fprintln(a.stdout, "not in plan mode.")
		return
	}
	// Drops the rules plan mode added and nothing else: a session rule someone
	// wrote themselves is not plan mode's to remove.
	kept := a.sessionRules[:0]
	for _, rule := range a.sessionRules {
		if !isPlanRule(rule) {
			kept = append(kept, rule)
		}
	}
	a.sessionRules = kept
	a.applyRules(ag)
	ag.SetExtraSystem("")

	fmt.Fprintf(a.stdout, "plan mode off. Back to %s.\n", ag.Permission)
}

func (a *app) inPlanMode() bool {
	for _, rule := range a.sessionRules {
		if isPlanRule(rule) {
			return true
		}
	}
	return false
}

func isPlanRule(rule string) bool {
	for _, planRule := range planRules {
		if rule == planRule {
			return true
		}
	}
	return false
}
