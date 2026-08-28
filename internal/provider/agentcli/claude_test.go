package agentcli

import (
	"strings"
	"testing"
)

func TestBuildClaudeInvocationKeepsPromptOutOfArgv(t *testing.T) {
	invocation, err := BuildClaudeInvocation("opus", "code", "high", "inspect this repository")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(invocation.Args, "\x00")
	if strings.Contains(joined, invocation.Prompt) {
		t.Fatalf("prompt leaked into argv: %q", joined)
	}
	for _, required := range []string{"-p", "--verbose", "--output-format", "stream-json", "--safe-mode"} {
		if !strings.Contains(joined, required) {
			t.Errorf("argv missing %q: %q", required, joined)
		}
	}
	if !strings.Contains(joined, "--effort") || !strings.Contains(joined, "high") {
		t.Errorf("argv missing effort selection: %q", joined)
	}
}

func TestBuildClaudeInvocationRejectsEmptyPrompt(t *testing.T) {
	if _, err := BuildClaudeInvocation("opus", "code", "high", "  "); err == nil {
		t.Fatal("empty prompt should be rejected")
	}
}
