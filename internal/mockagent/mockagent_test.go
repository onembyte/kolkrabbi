//go:build !windows

package mockagent

import (
	"os"
	"strings"
	"testing"
)

func TestSignalFixturesUseBash(t *testing.T) {
	executable, _, err := Write(t.TempDir(), IgnoresInterrupt)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "#!/usr/bin/env bash\n") {
		t.Fatalf("signal fixture interpreter = %q, want explicit Bash", strings.SplitN(string(body), "\n", 2)[0])
	}
}
