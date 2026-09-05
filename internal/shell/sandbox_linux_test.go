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
