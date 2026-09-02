package agentcli

import (
	"context"
	"errors"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeExecutionOptionsPinsTheNestedWorkspaceAndExplicitSiblingOnly(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested-checkout")
	sibling := filepath.Join(parent, "sibling-checkout")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "workspace-link")
	if err := os.Symlink(nested, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := normalizeExecutionOptions(ExecutionOptions{
		Workspace:      link,
		AdditionalDirs: []string{sibling, sibling},
		NetworkAccess:  true,
		Provider:       "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	wantSibling, err := filepath.EvalSymlinks(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace != wantWorkspace {
		t.Fatalf("workspace = %q, want canonical nested checkout %q", got.Workspace, wantWorkspace)
	}
	if !slices.Equal(got.AdditionalDirs, []string{wantSibling}) {
		t.Fatalf("additional directories = %q, want only explicit sibling %q", got.AdditionalDirs, wantSibling)
	}
	if !got.NetworkAccess || got.Provider != "codex" {
		t.Fatalf("capabilities = %+v, want network enabled and provider preserved", got)
	}
}

func TestNormalizeExecutionOptionsRejectsAFileAsWorkspace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeExecutionOptions(ExecutionOptions{Workspace: file}); err == nil {
		t.Fatal("a regular file was accepted as the provider workspace")
	}
}

// The envelope is validated once, at construction, and the per-turn path
// trusts that. Proved by taking the directories away afterwards: a turn that
// re-validated would fail on a workspace that no longer exists, and a turn
// that uses the canonical path it was given does not.
//
// This is not a claim that a deleted workspace is fine — the child will fail
// to start, with the vendor's own error, which is the truthful place for it.
// It is a claim that kolk does not pay for that check on every turn.
func TestOptionsAreNormalizedOnceAtConstruction(t *testing.T) {
	workspace := t.TempDir()
	backend, err := NewCodexBackendFromHandleWithOptions("gpt-5.6-sol", "code", "high", "", false,
		ExecutionOptions{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if !backend.execution.normalized {
		t.Fatal("the constructor did not record that it validated the envelope")
	}
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatal(err)
	}

	invocation, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "code", "high", "", false, "go", backend.execution)
	if err != nil {
		t.Fatalf("a turn re-validated an envelope the constructor had already checked: %v", err)
	}
	if !slices.Contains(invocation.Args, "--cd") {
		t.Fatalf("the canonical workspace did not reach the argv: %v", invocation.Args)
	}

	// An envelope nobody validated is still validated: the marker is
	// unexported, so a literal from outside this package cannot claim it.
	if _, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "code", "high", "", false, "go",
		ExecutionOptions{Workspace: filepath.Join(t.TempDir(), "not-there")}); err == nil {
		t.Fatal("an unvalidated envelope was accepted as canonical")
	}
}

// The persistent Claude path holds one process for the whole session, so a
// turn on it must not build the one-shot invocation — it was built and thrown
// away, envelope validation and all, on every turn of every ordinary session.
func TestPersistentClaudeTurnBuildsNoInvocation(t *testing.T) {
	workspace := t.TempDir()
	backend, err := NewClaudeBackendFromHandleWithOptions("claude-fable", "code", "high", "", false,
		ExecutionOptions{Workspace: workspace, NetworkAccess: true})
	if err != nil {
		t.Fatal(err)
	}
	// A persistent session, and a workspace that has since gone. Building the
	// unused invocation would validate it and fail the turn before the session
	// process was ever asked.
	// startWithOptions is the seam the workspace-carrying path uses, so the
	// shell's own working-directory check is not what this test measures.
	backend.start = func(context.Context, string, []string) (lineProcess, error) {
		return nil, errors.New("reached the session process")
	}
	backend.startWithOptions = func(context.Context, string, []string, shell.ProcessOptions) (lineProcess, error) {
		return nil, errors.New("reached the session process")
	}
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatal(err)
	}

	_, _, err = backend.StreamChat(context.Background(), "claude-fable",
		[]provider.Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "reached the session process") {
		t.Fatalf("turn error = %v, want the session process to have been the thing that failed", err)
	}
}
