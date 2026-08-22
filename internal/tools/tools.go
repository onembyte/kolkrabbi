// Package tools defines the agentic tools exposed to the model (bash
// execution, file read/write/edit, directory listing) and executes them
// locally, gating side-effecting actions behind a caller-supplied confirm
// callback.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kolkrabbi/internal/api"
)

// Confirm is called before any side-effecting action (bash, write, edit).
// It should return true if the action is allowed to proceed.
type Confirm func(action, detail string) bool

// PreWrite is called for file-modifying tools (write_file, edit_file) after
// the user has confirmed but before the file is touched — the checkpoint
// hook. A non-nil error aborts the operation.
type PreWrite func(tool, path string) error

const maxOutput = 12000 // chars; keeps huge command/file output from blowing up context

func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + fmt.Sprintf("\n... [truncated, %d more chars]", len(s)-maxOutput)
}

func schema(props string, required ...string) json.RawMessage {
	if required == nil {
		required = []string{} // marshal as [], not null — some providers reject "required":null
	}
	req, _ := json.Marshal(required)
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":%s,"required":%s}`, props, req))
}

// Definitions returns the OpenAI-compatible tool schemas sent to the model.
func Definitions() []api.Tool {
	return []api.Tool{
		{Type: "function", Function: api.FunctionDef{
			Name:        "bash",
			Description: "Execute a shell command and return its combined stdout/stderr. Use for running builds, tests, git, searching (grep/find), etc.",
			Parameters: schema(`{
				"command":{"type":"string","description":"The shell command to run"},
				"description":{"type":"string","description":"One short sentence: what this command does and why"}
			}`, "command", "description"),
		}},
		{Type: "function", Function: api.FunctionDef{
			Name:        "read_file",
			Description: "Read a text file's contents from disk, with line numbers.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file, absolute or relative to the working directory"}
			}`, "path"),
		}},
		{Type: "function", Function: api.FunctionDef{
			Name:        "write_file",
			Description: "Create a new file or fully overwrite an existing one with the given content.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file to write"},
				"content":{"type":"string","description":"Full file content"}
			}`, "path", "content"),
		}},
		{Type: "function", Function: api.FunctionDef{
			Name:        "edit_file",
			Description: "Replace one exact, unique occurrence of old_str with new_str in an existing file. old_str must match the file's current content exactly and appear exactly once.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file to edit"},
				"old_str":{"type":"string","description":"Exact text to find (must be unique in the file)"},
				"new_str":{"type":"string","description":"Text to replace it with"}
			}`, "path", "old_str", "new_str"),
		}},
		{Type: "function", Function: api.FunctionDef{
			Name:        "list_dir",
			Description: "List the contents of a directory (non-recursive).",
			Parameters: schema(`{
				"path":{"type":"string","description":"Directory path, defaults to the current directory"}
			}`),
		}},
	}
}

// Execute runs a tool by name with raw JSON arguments as produced by the
// model, returning the text to feed back as the tool's result. pre (may be
// nil) is invoked before any file modification, for checkpointing.
func Execute(ctx context.Context, name, argsJSON string, confirm Confirm, pre PreWrite) (string, error) {
	snapshot := func(tool, path string) error {
		if pre == nil {
			return nil
		}
		if err := pre(tool, path); err != nil {
			return fmt.Errorf("checkpoint failed, aborting %s: %w", tool, err)
		}
		return nil
	}
	switch name {
	case "bash":
		var a struct {
			Command     string `json:"command"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		if !confirm("Run shell command", fmt.Sprintf("%s\n  $ %s", a.Description, a.Command)) {
			return "", fmt.Errorf("user declined to run this command")
		}
		cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, "bash", "-c", a.Command)
		out, err := cmd.CombinedOutput()
		result := truncate(string(out))
		if err != nil {
			return fmt.Sprintf("%s\n[exit error: %v]", result, err), nil
		}
		if result == "" {
			result = "(no output)"
		}
		return result, nil

	case "read_file":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		b, err := os.ReadFile(a.Path)
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(b), "\n")
		var sb strings.Builder
		for i, l := range lines {
			fmt.Fprintf(&sb, "%5d\t%s\n", i+1, l)
		}
		return truncate(sb.String()), nil

	case "write_file":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		preview := a.Content
		if len(preview) > 400 {
			preview = preview[:400] + "\n... [truncated for preview]"
		}
		if !confirm("Write file", fmt.Sprintf("%s\n---\n%s\n---", a.Path, preview)) {
			return "", fmt.Errorf("user declined to write this file")
		}
		if err := snapshot("write_file", a.Path); err != nil {
			return "", err
		}
		if dir := filepath.Dir(a.Path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil

	case "edit_file":
		var a struct {
			Path   string `json:"path"`
			OldStr string `json:"old_str"`
			NewStr string `json:"new_str"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		b, err := os.ReadFile(a.Path)
		if err != nil {
			return "", err
		}
		content := string(b)
		count := strings.Count(content, a.OldStr)
		if count == 0 {
			return "", fmt.Errorf("old_str not found in %s", a.Path)
		}
		if count > 1 {
			return "", fmt.Errorf("old_str is not unique in %s (%d matches); include more context", a.Path, count)
		}
		if !confirm("Edit file", fmt.Sprintf("%s\n--- old ---\n%s\n--- new ---\n%s", a.Path, a.OldStr, a.NewStr)) {
			return "", fmt.Errorf("user declined to edit this file")
		}
		if err := snapshot("edit_file", a.Path); err != nil {
			return "", err
		}
		updated := strings.Replace(content, a.OldStr, a.NewStr, 1)
		if err := os.WriteFile(a.Path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("edited %s", a.Path), nil

	case "list_dir":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &a) // path optional
		if a.Path == "" {
			a.Path = "."
		}
		entries, err := os.ReadDir(a.Path)
		if err != nil {
			return "", err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var sb strings.Builder
		for _, e := range entries {
			kind := "file"
			if e.IsDir() {
				kind = "dir"
			}
			fmt.Fprintf(&sb, "%-4s %s\n", kind, e.Name())
		}
		return truncate(sb.String()), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
