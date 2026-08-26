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
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// sh runs every command the bash tool is asked to run. It is a package-level
// value rather than a parameter so that Execute's signature is unchanged —
// the tool tests are the most valuable coverage in the repo and this refactor
// has no business touching them.
var sh = shell.New()

// Confirm is called before any side-effecting action (bash, write, edit).
// It should return true if the action is allowed to proceed.
type Confirm func(action, detail string) bool

// Request describes one tool action so a policy can judge it.
//
// Tools stay mechanical: they resolve what is being asked for and hand it up.
// Whether it needs a prompt, is allowed outright, or must be refused is a
// question about permission tiers and rules, which is not a tool's business.
type Request struct {
	Tool    string // bash | read_file | write_file | edit_file | list_dir
	Path    string // absolute, symlinks resolved; file tools only
	Display string // the path as a person should read it
	Outside bool   // resolves outside the project root
	Command string // bash only
	Summary string // the model's own description of what it is doing
	Detail  string // preview shown when asking
}

// Guard decides whether one action may proceed. A nil guard allows everything,
// which is what a caller with no policy — a test, a script — expects.
type Guard func(Request) bool

// Options carries the policy and the project root into a tool call.
type Options struct {
	// Root confines file paths. Empty disables confinement.
	Root     string
	Guard    Guard
	PreWrite PreWrite
}

func (o Options) allow(r Request) bool {
	if o.Guard == nil {
		return true
	}
	return o.Guard(r)
}

// fileRequest resolves a path once, so the guard judges and the tool operates
// on exactly the same location. Judging one path and then opening another is
// how confinement checks are defeated.
func (o Options) fileRequest(tool, path, detail string) (Request, error) {
	absolute, outside, err := resolvePath(o.Root, path)
	if err != nil {
		return Request{}, err
	}
	return Request{
		Tool:    tool,
		Path:    absolute,
		Display: describePath(o.Root, absolute, outside),
		Outside: outside,
		Detail:  detail,
	}, nil
}

// truncateRunes caps a preview without splitting a rune.
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut
}

// PreWrite is called for file-modifying tools (write_file, edit_file) after
// the user has confirmed but before the file is touched — the checkpoint
// hook. A non-nil error aborts the operation.
type PreWrite func(tool, path string) error

const maxOutput = 12000 // chars; keeps huge command/file output from blowing up context

// truncate caps tool output without corrupting it.
//
// Cutting at a byte offset can split a UTF-8 rune, and this is the hottest path
// in the product: every file read and every command result larger than the cap
// goes through it. A file containing an accented name, a smart quote or an
// emoji would otherwise put invalid bytes into the conversation, which is then
// sent to the provider and saved in the session.
func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	cut := s[:maxOutput]
	// A line boundary is better than a rune boundary: half a line at the cut
	// reads as though the file itself is broken. Only accept one that is not
	// throwing away most of the output.
	if index := strings.LastIndexByte(cut, '\n'); index > maxOutput/2 {
		cut = cut[:index+1]
	} else {
		cut = trimPartialRune(cut)
	}
	return cut + fmt.Sprintf("\n... [truncated, %d more chars]", len(s)-len(cut))
}

// trimPartialRune drops an incomplete trailing rune, leaving a valid string. A
// real U+FFFD in the content decodes with a size above one and is kept.
func trimPartialRune(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

func schema(props string, required ...string) json.RawMessage {
	if required == nil {
		required = []string{} // marshal as [], not null — some providers reject "required":null
	}
	req, _ := json.Marshal(required)
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":%s,"required":%s}`, props, req))
}

// Definitions returns the OpenAI-compatible tool schemas sent to the model.
func Definitions() []provider.Tool {
	return []provider.Tool{
		{Type: "function", Function: provider.FunctionDef{
			Name:        "bash",
			Description: "Execute a shell command and return its combined stdout/stderr. Use for running builds, tests, git, searching (grep/find), etc.",
			Parameters: schema(`{
				"command":{"type":"string","description":"The shell command to run"},
				"description":{"type":"string","description":"One short sentence: what this command does and why"}
			}`, "command", "description"),
		}},
		{Type: "function", Function: provider.FunctionDef{
			Name:        "read_file",
			Description: "Read a text file's contents from disk, with line numbers.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file, absolute or relative to the working directory"},
				"purpose":{"type":"string","description":"One short phrase saying why this file is needed, shown to the user when the path is outside the project"}
			}`, "path"),
		}},
		{Type: "function", Function: provider.FunctionDef{
			Name:        "write_file",
			Description: "Create a new file or fully overwrite an existing one with the given content.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file to write"},
				"content":{"type":"string","description":"Full file content"},
				"purpose":{"type":"string","description":"One short phrase saying why this file is needed, shown to the user when the path is outside the project"}
			}`, "path", "content"),
		}},
		{Type: "function", Function: provider.FunctionDef{
			Name:        "edit_file",
			Description: "Replace one exact, unique occurrence of old_str with new_str in an existing file. old_str must match the file's current content exactly and appear exactly once.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file to edit"},
				"old_str":{"type":"string","description":"Exact text to find (must be unique in the file)"},
				"new_str":{"type":"string","description":"Text to replace it with"},
				"purpose":{"type":"string","description":"One short phrase saying why this file is needed, shown to the user when the path is outside the project"}
			}`, "path", "old_str", "new_str"),
		}},
		{Type: "function", Function: provider.FunctionDef{
			Name:        "list_dir",
			Description: "List the contents of a directory (non-recursive).",
			Parameters: schema(`{
				"path":{"type":"string","description":"Directory path, defaults to the current directory"},
				"purpose":{"type":"string","description":"One short phrase saying why this file is needed, shown to the user when the path is outside the project"}
			}`),
		}},
	}
}

// Execute runs a tool by name with raw JSON arguments as produced by the
// model, returning the text to feed back as the tool's result. pre (may be
// nil) is invoked before any file modification, for checkpointing.
func Execute(ctx context.Context, name, argsJSON string, o Options) (string, error) {
	snapshot := func(tool, path string) error {
		if o.PreWrite == nil {
			return nil
		}
		if err := o.PreWrite(tool, path); err != nil {
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
		if !o.allow(Request{
			Tool: "bash", Command: a.Command, Summary: a.Description,
			Detail: fmt.Sprintf("%s\n  $ %s", a.Description, a.Command),
		}) {
			return "", fmt.Errorf("this command was not allowed to run")
		}
		res, err := sh.Run(ctx, shell.Cmd{Command: a.Command})
		if err != nil {
			// Only a cancelled turn reaches here. Everything else — a non-zero
			// exit, a timeout — is a result the model should see and react to,
			// not an error that aborts the turn.
			return "", err
		}
		result := truncate(res.Output)
		if !res.OK() {
			// The model sees the failure and reacts to it. A command that exits
			// non-zero is a fact about the world, not a broken tool.
			return fmt.Sprintf("%s\n[exit error: %s]", result, res.Failure), nil
		}
		if result == "" {
			result = "(no output)"
		}
		return result, nil

	case "read_file":
		var a struct {
			Path    string `json:"path"`
			Purpose string `json:"purpose"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		request, err := o.fileRequest("read_file", a.Path, "")
		request.Summary = a.Purpose
		if err != nil {
			return "", err
		}
		if !o.allow(request) {
			return "", fmt.Errorf("reading %s was not allowed", request.Display)
		}
		b, err := os.ReadFile(request.Path)
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
			Purpose string `json:"purpose"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		preview := a.Content
		if len(preview) > 400 {
			preview = truncateRunes(preview, 400) + "\n... [truncated for preview]"
		}
		request, err := o.fileRequest("write_file", a.Path, preview)
		if err != nil {
			return "", err
		}
		request.Summary = a.Purpose
		if !o.allow(request) {
			return "", fmt.Errorf("writing %s was not allowed", request.Display)
		}
		if err := snapshot("write_file", request.Path); err != nil {
			return "", err
		}
		if dir := filepath.Dir(request.Path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(request.Path, []byte(a.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), request.Display), nil

	case "edit_file":
		var a struct {
			Path    string `json:"path"`
			OldStr  string `json:"old_str"`
			NewStr  string `json:"new_str"`
			Purpose string `json:"purpose"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		request, err := o.fileRequest("edit_file", a.Path, "")
		if err != nil {
			return "", err
		}
		request.Summary = a.Purpose
		b, err := os.ReadFile(request.Path)
		if err != nil {
			return "", err
		}
		content := string(b)
		count := strings.Count(content, a.OldStr)
		if count == 0 {
			return "", fmt.Errorf("old_str not found in %s", request.Display)
		}
		if count > 1 {
			return "", fmt.Errorf("old_str is not unique in %s (%d matches); include more context", request.Display, count)
		}
		request.Detail = fmt.Sprintf("--- old ---\n%s\n--- new ---\n%s", a.OldStr, a.NewStr)
		if !o.allow(request) {
			return "", fmt.Errorf("editing %s was not allowed", request.Display)
		}
		if err := snapshot("edit_file", request.Path); err != nil {
			return "", err
		}
		updated := strings.Replace(content, a.OldStr, a.NewStr, 1)
		if err := os.WriteFile(request.Path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("edited %s", request.Display), nil

	case "list_dir":
		var a struct {
			Path    string `json:"path"`
			Purpose string `json:"purpose"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &a) // path optional
		if a.Path == "" {
			a.Path = "."
		}
		request, err := o.fileRequest("list_dir", a.Path, "")
		if err != nil {
			return "", err
		}
		request.Summary = a.Purpose
		if !o.allow(request) {
			return "", fmt.Errorf("listing %s was not allowed", request.Display)
		}
		entries, err := os.ReadDir(request.Path)
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
