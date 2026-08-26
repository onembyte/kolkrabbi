package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSystemPromptIncludesBothMemoryLayers(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	if err := os.WriteFile("AGENTS.md", []byte("project rule: run make check"), 0o600); err != nil {
		t.Fatal(err)
	}
	userMemory := filepath.Join(t.TempDir(), "memory.md")
	if err := os.WriteFile(userMemory, []byte("user rule: prefer table tests"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{Options: Options{UserMemoryFile: userMemory}}

	prompt := agent.systemPrompt(ModeCode)

	if !strings.Contains(prompt, "prefer table tests") {
		t.Fatal("the user's own notes were not included")
	}
	if !strings.Contains(prompt, "run make check") {
		t.Fatal("the project's notes were not included")
	}
	// The project layer comes last so it wins a contradiction by being nearer
	// the task.
	if strings.Index(prompt, "prefer table tests") > strings.Index(prompt, "run make check") {
		t.Fatal("project notes must come after user notes")
	}
}

func TestSystemPromptWithoutAnyMemoryIsUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())
	agent := &Agent{Options: Options{UserMemoryFile: filepath.Join(t.TempDir(), "absent.md")}}

	prompt := agent.systemPrompt(ModeCode)

	if strings.Contains(prompt, "notes") {
		t.Fatalf("prompt mentions notes with none present: %q", prompt)
	}
}

func TestOversizedMemoryIsCutAtALineBoundaryAndSaysSo(t *testing.T) {
	// Multibyte content, so a byte-offset cut would produce invalid UTF-8 in
	// the system prompt — a corrupt request rather than a long one.
	line := "π approximates 3.14159 — a line with multibyte characters in it\n"
	body := strings.Repeat(line, (maxMemoryBytes/len(line))+50)
	path := filepath.Join(t.TempDir(), "memory.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{Options: Options{UserMemoryFile: path}}

	prompt := agent.systemPrompt(ModeChat)

	if !utf8.ValidString(prompt) {
		t.Fatal("truncation produced invalid UTF-8")
	}
	if !strings.Contains(prompt, "truncated") {
		t.Fatalf("an oversized memory file was cut without saying so")
	}
	if len(prompt) > maxMemoryBytes*3 {
		t.Fatalf("prompt is %d bytes; the cap did not apply", len(prompt))
	}
}

func TestMemoryIsNotCutMidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	body := strings.Repeat("a line that is long enough to matter for the boundary\n", 400)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{Options: Options{UserMemoryFile: path}}

	prompt := agent.systemPrompt(ModeChat)
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "a line") && !strings.HasSuffix(line, "boundary") {
			t.Fatalf("a memory line was cut in half: %q", line)
		}
	}
}
