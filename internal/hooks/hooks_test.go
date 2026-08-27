package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

type fakeShell struct {
	ran     []string
	env     [][]string
	result  shell.Result
	err     error
	delay   time.Duration
	timeout time.Duration
}

func (f *fakeShell) Name() string { return "fake" }
func (f *fakeShell) Run(ctx context.Context, c shell.Cmd) (shell.Result, error) {
	f.ran = append(f.ran, c.Command)
	f.env = append(f.env, c.Env)
	f.timeout = c.Timeout
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return shell.Result{ExitCode: -1, Failure: "timed out"}, nil
		}
	}
	return f.result, f.err
}

func newRunner(t *testing.T, sh shell.Shell, allow func(string) bool, confirm func(string) bool) *Runner {
	t.Helper()
	return &Runner{
		Shell:   sh,
		Config:  Config{PostEdit: []string{"gofmt -w $KOLK_FILE"}},
		Allowed: allow,
		Confirm: confirm,
		Timeout: 5 * time.Second,
		Session: "s_x",
	}
}

// The confirmation is the design. A formatter that runs silently after every
// edit is a shell command executing with nobody at the prompt.
func TestAHookIsConfirmedBeforeItRuns(t *testing.T) {
	sh := &fakeShell{}
	asked := 0
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { asked++; return true })

	r.Run(context.Background(), PostEdit, "a.go")
	if asked != 1 {
		t.Errorf("asked %d times, want once", asked)
	}
	if len(sh.ran) != 1 {
		t.Fatalf("ran %v, want the hook once", sh.ran)
	}
}

func TestADeclinedHookDoesNotRun(t *testing.T) {
	sh := &fakeShell{}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return false })
	r.Run(context.Background(), PostEdit, "a.go")
	if len(sh.ran) != 0 {
		t.Errorf("a declined hook ran anyway: %v", sh.ran)
	}
}

// Once per distinct command per session, then remembered — the same shape as a
// permission rule, because it is one.
func TestAConfirmedHookIsNotAskedAgain(t *testing.T) {
	sh := &fakeShell{}
	asked := 0
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { asked++; return true })

	for i := 0; i < 5; i++ {
		r.Run(context.Background(), PostEdit, "a.go")
	}
	if asked != 1 {
		t.Errorf("asked %d times across five edits, want once", asked)
	}
	if len(sh.ran) != 5 {
		t.Errorf("ran %d times, want five", len(sh.ran))
	}
}

// A declined hook is remembered too: being asked again on every edit is how a
// person ends up saying yes to make it stop.
func TestADeclinedHookIsNotAskedAgain(t *testing.T) {
	asked := 0
	r := newRunner(t, &fakeShell{}, func(string) bool { return true }, func(string) bool { asked++; return false })
	for i := 0; i < 5; i++ {
		r.Run(context.Background(), PostEdit, "a.go")
	}
	if asked != 1 {
		t.Errorf("asked %d times, want once", asked)
	}
}

// The floor applies. A hook is judged like any other command.
func TestTheFloorRefusesAHookWithoutAsking(t *testing.T) {
	sh := &fakeShell{}
	asked := 0
	r := newRunner(t, sh, func(string) bool { return false }, func(string) bool { asked++; return true })

	results := r.Run(context.Background(), PostEdit, "a.go")
	if len(sh.ran) != 0 {
		t.Errorf("a refused hook ran: %v", sh.ran)
	}
	if asked != 0 {
		t.Error("the user was asked about a command the floor already refused")
	}
	if len(results) != 1 || !strings.Contains(results[0].Failure, "refused") {
		t.Errorf("the refusal was not reported: %#v", results)
	}
}

// Reported, never fatal. A formatter that is not installed must not fail the
// edit that already happened.
func TestAFailingHookIsReportedNotFatal(t *testing.T) {
	sh := &fakeShell{result: shell.Result{ExitCode: 127, Failure: "command not found"}}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return true })

	results := r.Run(context.Background(), PostEdit, "a.go")
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Failure == "" {
		t.Error("a failing hook reported nothing")
	}
}

func TestAHookThatCannotRunAtAllIsAlsoNotFatal(t *testing.T) {
	sh := &fakeShell{err: errors.New("no shell on this machine")}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return true })
	results := r.Run(context.Background(), PostEdit, "a.go")
	if len(results) != 1 || results[0].Failure == "" {
		t.Errorf("a shell that could not run reported %#v", results)
	}
}

// Bounded by effort, like bash. A hook that hangs must not hang the session.
func TestAHookIsBounded(t *testing.T) {
	sh := &fakeShell{}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return true })
	r.Run(context.Background(), PostEdit, "a.go")
	if sh.timeout <= 0 {
		t.Error("the hook ran with no timeout, so one that hangs would hang the session")
	}
}

// $KOLK_FILE and $KOLK_SESSION only. Not the user's whole environment, and
// never a credential.
func TestOnlyTheTwoVariablesArePassed(t *testing.T) {
	sh := &fakeShell{}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return true })
	r.Run(context.Background(), PostEdit, "internal/a.go")

	if len(sh.env) != 1 {
		t.Fatalf("env = %v", sh.env)
	}
	joined := strings.Join(sh.env[0], " ")
	if !strings.Contains(joined, "KOLK_FILE=internal/a.go") {
		t.Errorf("KOLK_FILE was not passed: %v", sh.env[0])
	}
	if !strings.Contains(joined, "KOLK_SESSION=s_x") {
		t.Errorf("KOLK_SESSION was not passed: %v", sh.env[0])
	}
	if len(sh.env[0]) != 2 {
		t.Errorf("the hook was given %d variables, want exactly two: %v", len(sh.env[0]), sh.env[0])
	}
}

// There is no pre-tool hook in v1: a hook that can veto a tool call is a second
// permission system, and E13 exists so there is exactly one.
func TestOnlyThreeEventsExist(t *testing.T) {
	if len(Events()) != 3 {
		t.Fatalf("events = %v, want exactly three", Events())
	}
	for _, event := range Events() {
		if strings.HasPrefix(string(event), "pre-") {
			t.Errorf("%s can veto a tool call, which is a second permission system", event)
		}
	}
}

// Hook output is a tool result by another name and can carry a secret.
func TestHookOutputIsScrubbed(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123"
	sh := &fakeShell{result: shell.Result{Output: "wrote " + key}}
	r := newRunner(t, sh, func(string) bool { return true }, func(string) bool { return true })

	results := r.Run(context.Background(), PostEdit, "a.go")
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if strings.Contains(results[0].Output, key) {
		t.Fatal("a hook's output carried an API key out of the runner")
	}
}
