package engine

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// P5. Vendor identity. The main conversation "main-h" was saved while the
// model ran through the claude connector (journal Main.Vendor). After a
// restart the same model runs through another vendor's adapter that was
// handed the same handle string. The engine records the vendor but never
// compares it; only the handle string is compared.
func TestRound2TheSavedConversationStaysWithItsVendor(t *testing.T) {
	for _, boundary := range []string{"pause", "error"} {
		t.Run(boundary, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_probe_vendor_identity", "mock/model")}
			root := t.TempDir()
			stop := error(errors.New("the vendor process exited"))
			if boundary == "pause" {
				stop = provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
					Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}
			}
			main := &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true, stop: stop}
			first := New(Options{Backend: resumableVendorMain{main}, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard,
				ConnectorName: func(string) string { return "claude" }})
			if err := first.RunTurn(context.Background(), "do the thing"); err == nil {
				t.Fatal("setup: the vendor stop did not stop the turn")
			}
			_ = first.Close()
			if run := sess.RunState(); run == nil || run.Main.Vendor != "claude" {
				t.Fatalf("setup: journal main vendor = %+v", run)
			}
			other := &probeVendorMain{handle: "main-h"}
			second := New(Options{Backend: other, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard,
				ConnectorName: func(string) string { return "copilot" }})
			defer second.Close()
			pending, ok := second.Resume()
			if !ok {
				t.Fatal("nothing to resume")
			}
			err := second.RunTurn(context.Background(), pending)
			if n := other.calls(); n > 0 {
				t.Fatalf("IDENTITY: the claude conversation main-h was continued through the copilot adapter (%d calls, err=%v)", n, err)
			}
		})
	}
}
