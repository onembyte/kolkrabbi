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
