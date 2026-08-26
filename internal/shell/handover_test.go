package shell

import (
	"context"
	"strings"
	"testing"
)

func TestHandoverRejectsMissingProviderCLI(t *testing.T) {
	err := Handover(context.Background(), "kolk-provider-does-not-exist", nil, "")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Handover error = %v, want missing executable", err)
	}
}

func TestHandoverHonoursCancelledContextBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Handover(ctx, "kolk-provider-does-not-exist", nil, ""); err != context.Canceled {
		t.Fatalf("Handover error = %v, want context cancellation", err)
	}
}
