package cli

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// The internal child entry is gated on an environment variable, not an argv
// verb, so the closed four-command surface stays closed and `kolk help` shows
// nothing new. Where the kernel cannot be Landlock -- everywhere but linux --
// the entry refuses with a sentence that says so, and never runs the command.
func TestLandlockChildEntryRefusesOffLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("on linux the entry applies a ruleset; that is V34.1e.2b's test")
	}
	t.Setenv("KOLK_LANDLOCK_CHILD", "1")
	t.Setenv("KOLK_LANDLOCK_POLICY", `{"Root":"/tmp","Temp":"/tmp"}`)
	a, _, stderr := newTestApp(t, "")
	code := a.main(context.Background(), []string{"bash", "-c", "echo RAN"})
	if code != 125 {
		t.Fatalf("exit %d, want 125 (the child could not confine and did not run)", code)
	}
	if !strings.Contains(stderr.String(), "linux") {
		t.Fatalf("stderr = %q, want it to name linux as the only platform for this entry", stderr.String())
	}
	if strings.Contains(stderr.String(), "RAN") {
		t.Fatal("the command ran")
	}
}

// With the variable unset, Main is exactly what it was: `help` is a command.
func TestMainDispatchesNormallyWithoutTheChildVariable(t *testing.T) {
	a, _, stderr := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("kolk help exited %d: %s", code, stderr.String())
	}
}
