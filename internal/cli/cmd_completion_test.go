package cli

import (
	"context"
	"strings"
	"testing"
)

func TestCompletionBashGeneratesValidScript(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	code := a.main(context.Background(), []string{"completion", "bash"})
	if code != ExitOK {
		t.Fatalf("kolk completion bash exit = %d (stderr: %s)", code, errOut.String())
	}
	output := out.String()
	for _, want := range []string{"_kolk_completions", "complete -F _kolk_completions kolk", "model", "effort", "mode", "sonnet"} {
		if !strings.Contains(output, want) {
			t.Errorf("bash completion missing %q: %q", want, output)
		}
	}
}

func TestCompletionZshGeneratesValidScript(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	code := a.main(context.Background(), []string{"completion", "zsh"})
	if code != ExitOK {
		t.Fatalf("kolk completion zsh exit = %d (stderr: %s)", code, errOut.String())
	}
	output := out.String()
	for _, want := range []string{"#compdef kolk", "_kolk", "_arguments", "model", "effort"} {
		if !strings.Contains(output, want) {
			t.Errorf("zsh completion missing %q: %q", want, output)
		}
	}
}

func TestCompletionFishGeneratesValidScript(t *testing.T) {
	a, out, errOut := newTestApp(t, "")
	code := a.main(context.Background(), []string{"completion", "fish"})
	if code != ExitOK {
		t.Fatalf("kolk completion fish exit = %d (stderr: %s)", code, errOut.String())
	}
	output := out.String()
	for _, want := range []string{"complete -c kolk", "model", "effort", "mode"} {
		if !strings.Contains(output, want) {
			t.Errorf("fish completion missing %q: %q", want, output)
		}
	}
}

func TestCompletionUnknownShellReturnsUsageError(t *testing.T) {
	a, _, errOut := newTestApp(t, "")
	code := a.main(context.Background(), []string{"completion", "powershell"})
	if code != ExitUsage {
		t.Fatalf("kolk completion powershell exit = %d, want ExitUsage", code)
	}
	if !strings.Contains(errOut.String(), "usage: kolk completion") {
		t.Errorf("stderr = %q, want usage string", errOut.String())
	}
}

func TestCompletionNoArgReturnsUsageError(t *testing.T) {
	a, _, errOut := newTestApp(t, "")
	code := a.main(context.Background(), []string{"completion"})
	if code != ExitUsage {
		t.Fatalf("kolk completion exit = %d, want ExitUsage", code)
	}
	if !strings.Contains(errOut.String(), "usage: kolk completion") {
		t.Errorf("stderr = %q, want usage string", errOut.String())
	}
}
