package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/selfupdate"
)

// A restart is a process replacement, so the conditions under which it happens
// are worth pinning: only in a session, only after a real update, and only when
// the user asked for it.
func TestUpdateArmsARestartOnlyWhenConfigured(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setting   *bool
		inSession bool
		updated   bool
		wantArmed bool
	}{
		{"off by default", nil, true, true, false},
		{"explicitly off", boolPtr(false), true, true, false},
		{"on, in session, updated", boolPtr(true), true, true, true},
		{"on but nothing was updated", boolPtr(true), true, false, false},
		{"on but not in a session", boolPtr(true), false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dirs := storeFirstRunKey(t)
			if tc.setting != nil {
				if err := config.Save(dirs.ConfigFile(), &config.Config{AutoRestartAfterUpdate: tc.setting}); err != nil {
					t.Fatal(err)
				}
			}
			a, _, _ := newTestApp(t, "")
			a.currentVersion = func() string { return "1.0.0" }
			a.update = func(context.Context) (selfupdate.Result, error) {
				return selfupdate.Result{Current: "1.0.0", Latest: "1.1.0", Updated: tc.updated, Path: "/tmp/kolk"}, nil
			}
			if err := a.applyUpdate(context.Background(), tc.inSession); err != nil {
				t.Fatal(err)
			}
			if armed := a.restartInto != ""; armed != tc.wantArmed {
				t.Fatalf("armed = %v, want %v", armed, tc.wantArmed)
			}
		})
	}
}

// The restart has to land the user back where they were, or it is just an exit.
func TestRestartCarriesTheSessionModeEffortAndTier(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.restartInto = "1.1.0"
	a.executablePath = func() (string, error) { return "/usr/local/bin/kolk", nil }

	var gotPath string
	var gotArgs []string
	a.replaceSelf = func(path string, args []string, _ []string) error {
		gotPath, gotArgs = path, args
		return nil
	}

	agent := &engine.Agent{}
	agent.Mode, agent.Effort, agent.Permission = "agent", "high", engine.PermissionAutoApprove
	a.performRestart(agent)

	if gotPath != "/usr/local/bin/kolk" {
		t.Fatalf("path = %q", gotPath)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"--mode agent", "--effort high", "--permission auto-approve"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("restart args %q dropped %q", joined, want)
		}
	}
}

// A restart that cannot happen must say so: silently continuing on the old
// version while having announced the new one is the one unacceptable outcome.
func TestRestartFailureIsReported(t *testing.T) {
	a, _, errOut := newTestApp(t, "")
	a.restartInto = "1.1.0"
	a.executablePath = func() (string, error) { return "/usr/local/bin/kolk", nil }
	a.replaceSelf = func(string, []string, []string) error { return errors.New("permission denied") }

	a.performRestart(&engine.Agent{})
	if !strings.Contains(errOut.String(), "could not restart") || !strings.Contains(errOut.String(), "Run kolk again") {
		t.Fatalf("silent restart failure: %q", errOut.String())
	}
}

func boolPtr(v bool) *bool { return &v }
