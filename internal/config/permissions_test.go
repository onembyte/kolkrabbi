package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingPermissionFileIsAnEmptyList(t *testing.T) {
	// Nobody should have to create a file to start with no rules.
	stored, err := LoadPermissions(filepath.Join(t.TempDir(), "permissions.json"))
	if err != nil {
		t.Fatalf("loading a file that is not there: %v", err)
	}
	if len(stored.For("/anywhere")) != 0 {
		t.Fatalf("got %v, want no rules", stored.For("/anywhere"))
	}
}

func TestProjectRulesApplyAfterGlobalOnes(t *testing.T) {
	// Ordering is the whole semantics downstream: last match wins, so the
	// project's own rules must be the later ones.
	stored := &Permissions{
		Always:   []string{"allow bash(git *)"},
		Projects: map[string][]string{"/p": {"deny bash(git push *)"}},
	}
	got := stored.For("/p")
	want := []string{"allow bash(git *)", "deny bash(git push *)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(stored.For("/other")) != 1 {
		t.Fatalf("another project saw %v, want only the global rule", stored.For("/other"))
	}
}

func TestAddingARuleSurvivesAReload(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "permissions.json")
	stored := &Permissions{}
	stored.Add("allow bash(go test *)", "/p")
	stored.Add("deny write(*.pem)", "")
	if err := SavePermissions(file, stored); err != nil {
		t.Fatalf("saving: %v", err)
	}

	reloaded, err := LoadPermissions(file)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	got := strings.Join(reloaded.For("/p"), "|")
	if got != "deny write(*.pem)|allow bash(go test *)" {
		t.Fatalf("got %q", got)
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	// A permission list is worth exactly as much as its integrity.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
}

func TestTheSameRuleIsNotStoredTwice(t *testing.T) {
	stored := &Permissions{}
	stored.Add("allow bash(git *)", "/p")
	stored.Add("allow bash(git *)", "/p")
	if got := stored.For("/p"); len(got) != 1 {
		t.Fatalf("got %v, want one copy", got)
	}
}

func TestRemovingARuleTakesItOutOfBothScopes(t *testing.T) {
	stored := &Permissions{}
	stored.Add("deny write(*.pem)", "")
	stored.Add("deny write(*.pem)", "/p")
	if !stored.Remove("deny write(*.pem)", "/p") {
		t.Fatal("Remove reported nothing was removed")
	}
	if got := stored.For("/p"); len(got) != 1 {
		t.Fatalf("got %v, want the global rule to remain", got)
	}
	if !stored.Remove("deny write(*.pem)", "") {
		t.Fatal("Remove reported nothing was removed globally")
	}
	if got := stored.For("/p"); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
	if stored.Remove("deny write(*.pem)", "") {
		t.Fatal("removing a rule that is gone reported success")
	}
}

func TestAnUnreadablePermissionFileNamesItself(t *testing.T) {
	file := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPermissions(file)
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("err = %v, want it to name %s", err, file)
	}
}
