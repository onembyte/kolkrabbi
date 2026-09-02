package cli

import (
	"context"
	"testing"
)

// runRetiredVerb drives a command that stopped being an outside-session verb
// on 2026-09-02 (docs/plan/09, "the outside-session surface is closed").
//
// Nothing about these commands moved except the entry point: /key, /config,
// /localia and the rest call exactly the functions below, so a test that used
// to run `kolk key …` still covers the behaviour by running the function the
// session runs. That the *session* reaches them is a separate guarantee, and
// TestEverySessionCommandIsReachable is where it lives.
//
// The exit code is reconstructed the way main did, so each test keeps the
// assertion it was written with.
func runRetiredVerb(t *testing.T, a *app, args ...string) int {
	t.Helper()
	ctx := context.Background()
	rest := args[1:]
	var err error
	switch args[0] {
	case "key":
		err = a.runKey(ctx, rest)
	case "config":
		err = a.runConfig(ctx, rest)
	case "models":
		err = a.runModels(ctx, rest)
	case "plans":
		err = a.runPlans(ctx, rest)
	case "pmodels":
		err = a.runPlanModels(ctx, rest)
	case "localia":
		err = a.runLocalia(ctx, rest)
	case "stats":
		err = a.runStats(ctx, rest)
	case "devices":
		err = a.runDevices(ctx, rest)
	case "version":
		err = a.runVersion(ctx, rest)
	case "doctor":
		err = a.runDoctor(ctx, rest)
	default:
		t.Fatalf("runRetiredVerb: %q is not a retired verb; if it is still a verb use a.main, and if it no longer exists at all the test should say so", args[0])
	}
	code := exitCode(err)
	a.printFailure(err, code)
	return code
}

// runUpdateInSession drives the update the way /update does. `kolk update`
// retired on 2026-09-02; applyUpdate is what the slash command calls, and the
// exit code is reconstructed so the tests keep their assertions.
func runUpdateInSession(t *testing.T, a *app) int {
	t.Helper()
	err := a.applyUpdate(context.Background(), true)
	code := exitCode(err)
	a.printFailure(err, code)
	return code
}
