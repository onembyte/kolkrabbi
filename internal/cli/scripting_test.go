package cli

import (
	"bufio"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/paths"
)

func TestPipedStdinRunsSingleShot(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "Reviewed piped diff cleanly"})
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	// HOME alone isolates nothing on a desktop Linux machine: XDG_DATA_HOME is
	// usually set and wins, so this test used to write sessions, event spills
	// and usage records into the developer's own Kolkrabbi state. The KOLK_*
	// overrides mean the same thing on every platform, which is why they are
	// what isolateHome uses.
	t.Setenv(paths.EnvConfigDir, filepath.Join(home, "config"))
	t.Setenv(paths.EnvDataDir, filepath.Join(home, "data"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(home, "cache"))
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testkey123")

	d, err := paths.Resolve()
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	if err := d.EnsureConfig(); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}

	var stdout, stderr bytes.Buffer
	pipedContent := "diff --git a/main.go b/main.go\n+ func NewFeature() {}"
	app := &app{
		stdout:       &stdout,
		stderr:       &stderr,
		in:           bufio.NewReader(strings.NewReader(pipedContent)),
		isStdinPiped: func() bool { return true },
	}

	args := []string{
		"-p", "review this diff",
		"--base-url", srv.URL,
		"--model", "mock/model",
		"-y",
	}

	if code := app.main(context.Background(), args); code != ExitOK {
		t.Fatalf("app.main exit = %d, want ExitOK (stderr: %s)", code, stderr.String())
	}

	if !strings.Contains(stdout.String(), "Reviewed piped diff cleanly") {
		t.Errorf("stdout = %q, want response containing 'Reviewed piped diff cleanly'", stdout.String())
	}
}

func TestPipedStdinWithoutPromptFlagRunsSingleShot(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "Processed prompt from stdin pipe"})
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	// HOME alone isolates nothing on a desktop Linux machine: XDG_DATA_HOME is
	// usually set and wins, so this test used to write sessions, event spills
	// and usage records into the developer's own Kolkrabbi state. The KOLK_*
	// overrides mean the same thing on every platform, which is why they are
	// what isolateHome uses.
	t.Setenv(paths.EnvConfigDir, filepath.Join(home, "config"))
	t.Setenv(paths.EnvDataDir, filepath.Join(home, "data"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(home, "cache"))
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-testkey123")

	d, err := paths.Resolve()
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	if err := d.EnsureConfig(); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}

	var stdout, stderr bytes.Buffer
	pipedPrompt := "explain the bug in auth"
	app := &app{
		stdout:       &stdout,
		stderr:       &stderr,
		in:           bufio.NewReader(strings.NewReader(pipedPrompt)),
		isStdinPiped: func() bool { return true },
	}

	args := []string{
		"--base-url", srv.URL,
		"--model", "mock/model",
		"-y",
	}

	if code := app.main(context.Background(), args); code != ExitOK {
		t.Fatalf("app.main exit = %d, want ExitOK (stderr: %s)", code, stderr.String())
	}

	if !strings.Contains(stdout.String(), "Processed prompt from stdin pipe") {
		t.Errorf("stdout = %q, want response containing 'Processed prompt from stdin pipe'", stdout.String())
	}
}
