package agentcli

import (
	"os"
	"path/filepath"
	"slices"
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
