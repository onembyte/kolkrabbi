// Package tools defines the agentic tools exposed to the model (bash
// execution, file read/write/edit, directory listing) and executes them
// locally, gating side-effecting actions behind a caller-supplied confirm
// callback.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/diff"
	"github.com/onembyte/kolkrabbi/internal/ports"
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
	// PostWrite is called after a file-modifying tool succeeded, so a hook can
	// react to work that is already done. It mirrors PreWrite deliberately:
	// one seam in, one seam out, and neither can veto — a hook that could stop
	// a tool call would be a second permission system.
	PostWrite PostWrite
}

// postWrite fires the after-the-fact seam, if there is one.
func (o Options) postWrite(tool, path string) {
	if o.PostWrite != nil {
		o.PostWrite(tool, path)
	}
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

// PostWrite is called after a file-modifying tool has changed something. It
// returns nothing: a hook reports and never fails the edit that happened.
type PostWrite func(tool, path string)

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
			Description: "Read a text file's contents from disk, with line numbers. Large files are truncated; use start_line and end_line to page through the rest instead of reaching for shell tools.",
			Parameters: schema(`{
				"path":{"type":"string","description":"Path to the file, absolute or relative to the working directory"},
				"start_line":{"type":"integer","description":"First line to read, 1-based. Omit to start at the beginning"},
				"end_line":{"type":"integer","description":"Last line to read, inclusive. Omit to read to the end"},
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
		// Taken before, compared after: what this command started, not what was
		// already running. Two reads of a small kernel table, and silent on a
		// machine that will not answer.
		listeningBefore := ports.Snapshot()
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
		// A task starts a dev server; the port it chose is the one fact the
		// user needs and the one thing the terminal does not tell them.
		for _, listener := range ports.Opened(listeningBefore, ports.Snapshot()) {
			result += "\n[" + ports.Describe(listener) + "]"
		}
		return result, nil

	case "read_file":
		var a struct {
			Path      string `json:"path"`
			Purpose   string `json:"purpose"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		request, err := o.fileRequest("read_file", a.Path, "")
		if err != nil {
			return "", err
		}
		request.Summary = a.Purpose
		if !o.allow(request) {
			return "", fmt.Errorf("reading %s was not allowed", request.Display)
		}
		b, err := os.ReadFile(request.Path)
		if err != nil {
			return "", err
		}
		if isBinary(b) {
			// Sending a binary wastes the window and carries bytes no provider
			// can represent. What the model can act on is that it exists.
			return fmt.Sprintf("[binary file: %s, %d bytes — not shown]", request.Display, len(b)), nil
		}
		return renderLines(string(b), request.Display, a.StartLine, a.EndLine), nil

	case "write_file":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Purpose string `json:"purpose"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
		request, err := o.fileRequest("write_file", a.Path, "")
		if err != nil {
			return "", err
		}
		request.Summary = a.Purpose
		request.Detail = writePreview(request.Path, a.Content)
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
		o.postWrite("write_file", request.Path)
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
		updated := strings.Replace(content, a.OldStr, a.NewStr, 1)
		request.Detail = changePreview(content, updated)
		if !o.allow(request) {
			return "", fmt.Errorf("editing %s was not allowed", request.Display)
		}
		if err := snapshot("edit_file", request.Path); err != nil {
			return "", err
		}
		if err := os.WriteFile(request.Path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		o.postWrite("edit_file", request.Path)
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

// previewContext is how many unchanged lines surround each change in a
// confirmation. Enough to tell the reader which occurrence this is; not so many
// that the change is buried.
const previewContext = 3

// previewLines bounds a confirmation preview. A prompt someone has to scroll
// is a prompt they answer without reading.
const previewLines = 40

// writePreview describes what writing this content to this path would do.
//
// A create and an overwrite must not look the same. The old preview showed the
// first 400 characters of the new content either way, so replacing a file was
// indistinguishable from adding one — and the thing being destroyed never
// appeared at all.
func writePreview(path, content string) string {
	existing, err := os.ReadFile(path)
	if err != nil {
		// Unreadable for any reason — absent, a directory, no permission — is
		// reported as new rather than guessed at. Being wrong in the direction
		// of "this file does not exist yet" is the safe wrong.
		return "new file\n" + diff.Truncate(prefixLines(content, "+"), previewLines)
	}
	return changePreview(string(existing), content)
}

// changePreview renders a before/after as a diff a person can act on.
func changePreview(before, after string) string {
	if before == after {
		// An empty diff shown as an empty prompt reads as a bug.
		return "no change: the file already has these contents"
	}
	return diff.Truncate(diff.Unified(before, after, previewContext), previewLines)
}

// prefixLines marks every line of a new file, so a create reads like the diff
// it is rather than like a block of text.
func prefixLines(content, marker string) string {
	if content == "" {
		return marker + "\n"
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(marker + line + "\n")
	}
	return b.String()
}

// binarySniffBytes is how much of a file decides whether it is text. A NUL in
// the first few KiB is the same test `file` and git use, and it is the one
// thing no text encoding produces.
const binarySniffBytes = 8 << 10

func isBinary(body []byte) bool {
	head := body
	if len(head) > binarySniffBytes {
		head = head[:binarySniffBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// renderLines numbers a file, optionally narrowed to a range.
//
// Line numbers stay absolute even inside a range: an edit built from a
// renumbered listing lands in the wrong place, and the model has no way to
// know it happened.
func renderLines(content, display string, start, end int) string {
	lines := strings.Split(content, "\n")
	// A trailing newline produces a final empty element that is not a line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)

	if start > 0 || end > 0 {
		if start < 1 {
			start = 1
		}
		if end < 1 || end > total {
			end = total
		}
		if start > total {
			return fmt.Sprintf("[%s has %d lines; requested %d-%d starts past the end]",
				display, total, start, end)
		}
		return numbered(lines[start-1:end], start) +
			fmt.Sprintf("[showing lines %d-%d of %d]\n", start, end, total)
	}

	rendered := numbered(lines, 1)
	if len(rendered) <= maxOutput {
		return rendered
	}
	// Truncation that does not say how to continue leaves the model guessing,
	// and it usually guesses "run grep in bash", which is slower and needs a
	// command confirmation for a read.
	head := truncate(rendered)
	shown := strings.Count(head, "\n")
	return head + fmt.Sprintf("\n[%s has %d lines; showed about %d. Read more with start_line and end_line.]\n",
		display, total, shown)
}

func numbered(lines []string, first int) string {
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%5d\t%s\n", first+i, line)
	}
	return b.String()
}
