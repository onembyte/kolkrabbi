package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A model that reads a .env file used to put its contents into the
// conversation, the session file on disk, and every later provider request.
func TestToolOutputIsScrubbedBeforeItIsKept(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	// Deliberately shorter than a real OpenRouter key, matching the fixture
	// shape the rest of this repository already uses. A fixture built to a
	// vendor's exact length is one a secret scanner cannot tell from the real
	// thing, and GitHub's push protection then blocks every push of the tests
	// that exist to prove secrets never leak.
	secretLine := "OPENROUTER_API_KEY=sk-or-v1-0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(secretLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := enginetest.NewFakeSession("s1", "vendor/model")
	agent := New(Options{
		Sess: session, Out: &strings.Builder{}, Root: root,
		Permission: PermissionFullAuto,
	})

	args, _ := json.Marshal(map[string]string{"path": ".env"})
	result, err := agent.executeTool(context.Background(), provider.ToolCall{
		ID: "call-1", Type: "function",
		Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result, "sk-or-v1-0123456789") {
		t.Fatalf("the key reached the conversation: %q", result)
	}
	if !strings.Contains(result, "redacted") {
		t.Fatalf("the redaction is invisible: %q", result)
	}
	// The rest of the file still has to be usable, or scrubbing breaks the work.
	if !strings.Contains(result, "OPENROUTER_API_KEY=") {
		t.Fatalf("scrubbing destroyed the line: %q", result)
	}
}

func TestOrdinaryToolOutputIsUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	body := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := New(Options{
		Sess: enginetest.NewFakeSession("s1", "m"), Out: &strings.Builder{},
		Root: root, Permission: PermissionFullAuto,
	})

	args, _ := json.Marshal(map[string]string{"path": "main.go"})
	result, err := agent.executeTool(context.Background(), provider.ToolCall{
		ID: "c", Type: "function",
		Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "func main() {}") {
		t.Fatalf("ordinary output was altered: %q", result)
	}
	if strings.Contains(result, "redacted") {
		t.Fatalf("ordinary output was redacted: %q", result)
	}
}

func TestACommandThatPrintsASecretIsScrubbedToo(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	agent := New(Options{
		Sess: enginetest.NewFakeSession("s1", "m"), Out: &strings.Builder{},
		Root: root, Permission: PermissionFullAuto,
	})

	args, _ := json.Marshal(map[string]string{
		"command":     "echo 'ghp_abcdefghijklmnopqrstuvwxyz0123456789AB'",
		"description": "print a token",
	})
	result, err := agent.executeTool(context.Background(), provider.ToolCall{
		ID: "c", Type: "function",
		Function: provider.FunctionCall{Name: "bash", Arguments: string(args)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "ghp_abcdefghijklmnop") {
		t.Fatalf("a token printed by a command reached the conversation: %q", result)
	}
}
