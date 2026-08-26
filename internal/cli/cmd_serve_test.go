package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServeHelpAndUsage(t *testing.T) {
	a, out, _ := newTestApp("")
	if err := a.runHelp(context.Background(), []string{"serve"}); err != nil {
		t.Fatalf("runHelp serve: %v", err)
	}
	if !strings.Contains(out.String(), "usage: kolk serve") {
		t.Errorf("missing usage line: %s", out.String())
	}
}

func TestServeStdioShutdownCleanly(t *testing.T) {
	a, _, _ := newTestApp("")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := a.runServe(ctx, []string{"--stdio"})
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("runServe --stdio error: %v", err)
	}
}
