//go:build darwin

package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CheckOllamaPlatform checks the official native bundle's OS minimum before a
// potentially large download. The platform layer owns the machine version probe.
func CheckOllamaPlatform(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sw_vers", "-productVersion")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	version, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("checking macOS compatibility for Localia: %w", err)
	}
	return checkOllamaMacOS(strings.TrimSpace(string(version)))
}
