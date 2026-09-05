//go:build linux

package shell

import (
	"strings"
	"testing"
)

// Landlock reports a refused open as EACCES.
const osRefusalPhrase = "Permission denied"

func TestProbeReportsLandlockWithAnABI(t *testing.T) {
	name, err := mechanism()
	if err != nil {
		t.Skipf("no Landlock on this kernel: %v", err)
	}
	if !strings.HasPrefix(name, "landlock v") {
		t.Fatalf("mechanism() = %q, want landlock vN", name)
	}
}

// A kernel below ABI 4 cannot enforce a TCP deny. The plan says refuse, never
// approximate: the parent declines before any child exists and names ABI 4.
func TestNetworkDenyIsRefusedBelowLandlockABI4(t *testing.T) {
	previous := landlockABIProbe
	landlockABIProbe = func() (int, error) { return 3, nil }
	defer func() { landlockABIProbe = previous }()

	_, err := prepareSandbox(Sandbox{Root: t.TempDir(), Temp: t.TempDir(), Network: NetworkDeny})
	if err == nil || !strings.Contains(err.Error(), "ABI 4") {
		t.Fatalf("want a refusal naming ABI 4, got %v", err)
	}
	// allow is unaffected by the ABI
	if _, err := prepareSandbox(Sandbox{Root: t.TempDir(), Temp: t.TempDir(), Network: NetworkAllow}); err != nil {
		t.Fatalf("network=allow must not depend on ABI 4: %v", err)
	}
}
