package shell

import (
	"strings"
	"testing"
)

// The bounded diagnostic: one line, only when a sandboxed command failed with
// the kernel's refusal phrase in its output, naming what is confined and the
// one switch -- and never a claim about cause.
func TestSandboxDiagnosticIsOneLineOnlyOnAKernelRefusal(t *testing.T) {
	p := Sandbox{Root: "/w/proj", Temp: "/tmp/k", Network: NetworkAllow}
	refused := Result{Output: "bash: /etc/x: Operation not permitted\n", ExitCode: 1, Failure: "exit status 1"}
	got := SandboxDiagnostic(p, refused)
	if got == "" {
		t.Fatal("no diagnostic on a kernel refusal")
	}
	if strings.Count(strings.TrimSuffix(got, "\n"), "\n") != 0 {
		t.Fatalf("diagnostic must be exactly one line:\n%q", got)
	}
	for _, want := range []string{"/w/proj", "/tmp/k", "network allowed", "/sandbox off"} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic %q lacks %q", got, want)
		}
	}
	denied := SandboxDiagnostic(Sandbox{Root: "/w", Temp: "/t", Network: NetworkDeny},
		Result{Output: "connect: Permission denied\n", ExitCode: 1, Failure: "exit status 1"})
	if !strings.Contains(denied, "network denied") {
		t.Fatalf("network=deny must be named: %q", denied)
	}
}

func TestSandboxDiagnosticStaysSilentOtherwise(t *testing.T) {
	p := Sandbox{Root: "/w", Temp: "/t", Network: NetworkAllow}
	for name, res := range map[string]Result{
		"success":             {Output: "ok\n", ExitCode: 0},
		"plain non-zero exit": {Output: "tests failed\n", ExitCode: 3, Failure: "exit status 3"},
		"kolk's own refusal":  {ExitCode: -1, Failure: Refusal(ErrSandboxUnsupported)},
		"timeout":             {Output: "", ExitCode: -1, TimedOut: true, Failure: "command timed out after 1s"},
	} {
		if got := SandboxDiagnostic(p, res); got != "" {
			t.Errorf("%s: unexpected diagnostic %q", name, got)
		}
	}
}
