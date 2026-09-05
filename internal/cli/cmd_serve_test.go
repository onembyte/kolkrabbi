package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServeHelpAndUsage(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if err := a.runHelp(context.Background(), []string{"serve"}); err != nil {
		t.Fatalf("runHelp serve: %v", err)
	}
	if !strings.Contains(out.String(), "usage: kolk serve") {
		t.Errorf("missing usage line: %s", out.String())
	}
}

func TestServeStdioShutdownCleanly(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := a.runServe(ctx, []string{"--stdio"})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("runServe --stdio error: %v", err)
	}
}

// A bearer token on the command line is in `ps` for every user on the machine
// and in the shell's history file afterwards. The environment and pairing both
// exist; the flag form is refused, and the refusal names them.
func TestServeRefusesABearerTokenOnTheCommandLine(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := a.runServe(ctx, []string{"--token", "bearer-secret-on-argv", "--stdio"})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("serve ran with a token on argv (err=%v); it must refuse before doing anything", err)
	}
	for _, want := range []string{"KOLK_AUTH_TOKEN", "--pair"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "bearer-secret-on-argv") {
		t.Errorf("refusal echoes the token: %v", err)
	}
}

// The environment form keeps working: that is the way out the refusal names.
func TestServeStillTakesTheTokenFromTheEnvironment(t *testing.T) {
	t.Setenv("KOLK_AUTH_TOKEN", "from-the-environment")
	a, _, _ := newTestApp(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := a.runServe(ctx, []string{"--stdio"})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("serve with the token in the environment: %v", err)
	}
}
