package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonExecutableHelp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "kolkd")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, binary, "--help").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "-stdio") || !strings.Contains(string(out), "-token") {
		t.Fatalf("help: %v\n%s", err, out)
	}
}
