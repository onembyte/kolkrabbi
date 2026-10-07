package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// WorkRecord describes an observed tool result. Arguments supply a public
// purpose and target; success, output and file changes come from execution.
type WorkRecord struct {
	Agent                              int
	Name, Arguments, Output, Error     string
	Path, Diff                         string
	Added, Removed                     int
	Changed, Created, Failed, Provider bool
	Pending, Warning                   bool
}

const workOutputLines = 8

// LogWork appends an entire outcome under one lock, so concurrent agents'
// headers, diffs and output cannot interleave.
func (r *Runtime) LogWork(record WorkRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	text, group := workRecordText(record, r.workGroup)
	r.workGroup = group
	r.controller.AppendTranscript(text)
	r.renderLocked()
}

func workRecordText(record WorkRecord, previousGroup string) (string, string) {
	var args struct {
		Command     string `json:"command"`
		Cmd         string `json:"cmd"`
		Path        string `json:"path"`
		FilePath    string `json:"file_path"`
		Pattern     string `json:"pattern"`
		Purpose     string `json:"purpose"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal([]byte(record.Arguments), &args)
	path := record.Path
	if path == "" {
		path = args.Path
	}
	if path == "" {
		path = args.FilePath
	}
	command := args.Command
	if command == "" {
		command = args.Cmd
	}
	if command == "" && !strings.HasPrefix(strings.TrimSpace(record.Arguments), "{") {
		command = record.Arguments
	}
	purpose := args.Purpose
	if purpose == "" {
		purpose = args.Description
	}
	owner := "kolk"
	if record.Agent > 0 {
		owner = fmt.Sprintf("agent %d", record.Agent)
	}
	kind, detail := "Used", record.Name
	ranCommand := false
	switch strings.ReplaceAll(strings.ToLower(record.Name), "-", "_") {
	case "bash", "exec_command", "shell", "command_execution":
		kind, detail, ranCommand = "Ran", command, true
		// Codex reads and searches through its shell: a command that only
		// reads joins the exploration group like a dedicated read would.
		if explored, ok := shellExploration(command); ok {
			kind, detail = "Explored", explored
		}
	case "read", "read_file":
		kind, detail = "Explored", "Read "+path
	case "list_dir", "glob", "ls":
		kind, detail = "Explored", "Listed "+path
		if args.Pattern != "" {
			detail = "Found " + args.Pattern
		}
	case "grep", "search", "find":
		kind, detail = "Explored", "Searched "+args.Pattern
		if path != "" {
			detail += " in " + path
		}
	case "edit", "edit_file", "write", "write_file", "apply_patch", "file_change":
		kind, detail = "Edited", path
		if detail == "" && command != "" {
			detail = command
		}
		if record.Created {
			kind = "Created"
		}
	}
	if detail == "" {
		detail = record.Name
	}
	if record.Failed || record.Error != "" {
		kind = "Failed"
	}
	if record.Pending {
		kind = "Unfinished"
	}
	if record.Warning {
		kind = "Warning"
	}
	var out strings.Builder
	group := ""
	if kind == "Explored" {
		group = owner + ":explore"
		if group != previousGroup {
			fmt.Fprintf(&out, "\n• Explored · %s\n", owner)
		}
		fmt.Fprintf(&out, "  └ %s\n", workLine(detail))
	} else {
		fmt.Fprintf(&out, "\n• %s %s", kind, workLine(detail))
		if record.Changed {
			fmt.Fprintf(&out, " (+%d -%d)", record.Added, record.Removed)
		}
		fmt.Fprintf(&out, " · %s\n", owner)
	}
	if purpose != "" && workLine(purpose) != workLine(detail) {
		fmt.Fprintf(&out, "    ↳ %s\n", workLine(purpose))
	}
	if record.Error != "" {
		fmt.Fprintf(&out, "  └ × %s\n", workLine(record.Error))
	} else if record.Changed {
		if record.Diff == "" && record.Added+record.Removed > 0 {
			// Counts were reported but no excerpt: not the same as nothing.
			out.WriteString("  └ (no excerpt reported)\n")
		} else if record.Diff == "" {
			out.WriteString("  └ no text changes\n")
		} else {
			// Every raw row is signed or a hunk header; payload fences cannot close
			// the block. Bound it before storage as well as at render time.
			out.WriteString("```diff\n")
			out.WriteString(workExcerpt(record.Diff, 48, ""))
			out.WriteString("```\n")
		}
	} else if kind != "Explored" {
		output := record.Output
		if strings.TrimSpace(output) == "" {
			// Only a provider tool that settled normally completed; one that
			// failed, never finished or warned is not called complete.
			switch {
			case record.Provider && !record.Failed && !record.Pending && !record.Warning:
				output = "(completed; no output reported)"
			case record.Provider:
				output = "(no output reported)"
			default:
				output = "(no output)"
			}
		}
		if record.Failed && ranCommand {
			out.WriteString("  └ × command failed\n")
		} else if record.Failed {
			out.WriteString("  └ × failed\n")
		}
		out.WriteString(workExcerpt(output, workOutputLines, "  └ "))
	}
	return out.String(), group
}

// shellExploration recognises a shell command that only reads — a search, a
// read or a listing, alone or piped only into other readers — and says what
// it explored. Every option must be one its tool allows: an option this does
// not know, a clustered one hiding a writer (sed -ni, sort -uo, fd -Hx), a
// redirect, a substitution or a chain keeps the command a command, with its
// output shown.
func shellExploration(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if script, wrapped := unwrapShell(command); wrapped {
		command = script
	}
	stages, ok := shellStages(command)
	if !ok {
		return "", false
	}
	verb, operands := "", []string(nil)
	for index, stage := range stages {
		stageVerb, stageOperands, ok := readerStage(stage)
		if !ok {
			return "", false
		}
		if index == 0 {
			verb, operands = stageVerb, stageOperands
		}
	}
	if tool := stages[0][0]; len(stages) > 1 || verb == "Searched" || tool == "find" || tool == "fd" {
		return verb + " " + command, true
	}
	// A plain read or listing names what it read: its operands.
	if len(operands) == 0 {
		if verb != "Listed" {
			return verb + " " + command, true
		}
		operands = []string{"."}
	}
	return verb + " " + strings.Join(operands, ", "), true
}

// shellStages splits a command into pipeline stages of the words the shell
// will pass to each tool: quotes removed, a | outside quotes between stages.
// Anything the shell would rewrite first — an escape, $…, braces, a glob, a
// subshell, a redirect or a chain — rejects the command, because an option
// can hide in any of them ('-delete', \-delete, ${IFS}-delete, {-delete,}).
func shellStages(command string) ([][]string, bool) {
	var stages [][]string
	var stage []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			stage = append(stage, word.String())
			word.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(command); i++ {
		switch c := command[i]; {
		case c == '\'':
			closing := strings.IndexByte(command[i+1:], '\'')
			if closing < 0 {
				return nil, false
			}
			word.WriteString(command[i+1 : i+1+closing])
			inWord, i = true, i+1+closing
		case c == '"':
			closing := strings.IndexByte(command[i+1:], '"')
			if closing < 0 || strings.ContainsAny(command[i+1:i+1+closing], "$`\\") {
				return nil, false
			}
			word.WriteString(command[i+1 : i+1+closing])
			inWord, i = true, i+1+closing
		case c == ' ' || c == '\t':
			endWord()
		case c == '|':
			endWord()
			if len(stage) == 0 {
				return nil, false
			}
			stages, stage = append(stages, stage), nil
		case strings.IndexByte("\\$`{}*?[]<>;&()!#\n\r", c) >= 0:
			return nil, false
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	endWord()
	if len(stage) == 0 {
		return nil, false
	}
	return append(stages, stage), true
}

// shellDirs are where a wrapper's shell may be: found on the PATH, as a bare
// cat is, or in a system directory.
var shellDirs = []string{"", "/bin/", "/usr/bin/", "/usr/local/bin/", "/opt/homebrew/bin/"}

// unwrapShell is the script a shell wrapper runs, as Codex runs every command
// (`/usr/bin/bash -lc '…'`): exactly a shell, -c or -lc, and one single-quoted
// script with no quote inside it. The script is then held to the same rules
// as a bare command; anything else is not unwrapped and stays a command.
func unwrapShell(command string) (string, bool) {
	shell, rest, ok := strings.Cut(command, " ")
	if !ok {
		return "", false
	}
	// A system shell, not ./bash or /tmp/x/sh: those are any program at all.
	slash := strings.LastIndex(shell, "/") + 1
	if !slices.Contains(shellDirs, shell[:slash]) {
		return "", false
	}
	switch shell[slash:] {
	case "bash", "sh", "zsh":
	default:
		return "", false
	}
	flag, script, ok := strings.Cut(strings.TrimLeft(rest, " "), " ")
	script = strings.TrimSpace(script)
	if !ok || (flag != "-c" && flag != "-lc") || len(script) < 2 || script[0] != '\'' || script[len(script)-1] != '\'' {
		return "", false
	}
	inner := script[1 : len(script)-1]
	if strings.Contains(inner, "'") {
		return "", false
	}
	return inner, true
}

// readerSpec is everything one read-only tool may be given. An option outside
// it — a writer, an executor, or simply one this does not know — makes the
// stage a command.
type readerSpec struct {
	verb string
	// flags take no value; values take the next word (or --flag=value).
	flags, values []string
	// short are the letters allowed in a cluster such as -rn.
	short string
	// digits allows a bare count such as head -20.
	digits bool
	// letterwise is a tool that reads a cluster one letter at a time instead
	// of through getopt (tree): a value letter anywhere in a cluster takes
	// the next word, so a value flag is safe only as its own word, or as -L
	// with attached digits (-L2).
	letterwise bool
}

var readerSpecs = map[string]readerSpec{
	"cat":  {verb: "Read", short: "nbsAETvetu"},
	"head": {verb: "Read", short: "qv", values: []string{"-n", "-c"}, digits: true},
	"tail": {verb: "Read", short: "qvfF", values: []string{"-n", "-c"}, digits: true},
	"wc":   {verb: "Read", short: "lwcmL"},
	"nl":   {verb: "Read", values: []string{"-w", "-s", "-b", "-n", "-v", "-i"}},
	"sort": {verb: "Read", short: "nrufbhdigsV", values: []string{"-k", "-t"}},
	"ls":   {verb: "Listed", short: "laAhR1trSdFGpicugnos", values: []string{"-I"}},
	"tree": {verb: "Listed", short: "adfipsugD", values: []string{"-L", "-I", "-P"}, letterwise: true},
	"rg": {verb: "Searched", short: "niSswvlcFuHNoLx0", values: []string{"-e", "-g", "-t", "-T", "-A", "-B", "-C", "-m", "-M",
		"--glob", "--type", "--type-not", "--max-count", "--regexp", "--color", "--max-columns"},
		flags: []string{"--hidden", "--no-ignore", "--files", "--count", "--json", "--line-number", "--ignore-case",
			"--smart-case", "--fixed-strings", "--word-regexp", "--files-with-matches", "--vimgrep", "--no-heading", "--heading"}},
	"grep": {verb: "Searched", short: "rRniIlLcvwxEFohHsqz", values: []string{"-e", "-A", "-B", "-C", "-m",
		"--include", "--exclude", "--exclude-dir", "--color", "--colour"}},
	"find": {verb: "Listed", flags: []string{"-L", "-H", "-P", "-print", "-print0", "-not", "-o", "-a", "-and", "-or", "-empty", "-follow"},
		values: []string{"-name", "-iname", "-path", "-ipath", "-type", "-maxdepth", "-mindepth", "-newer", "-size",
			"-mtime", "-mmin", "-user", "-perm", "-regex"}},
	"fd": {verb: "Listed", short: "HIuFgaLpsi1", values: []string{"-t", "-e", "-d", "-E", "--type", "--extension", "--max-depth", "--exclude"},
		flags: []string{"--hidden", "--no-ignore", "--glob", "--fixed-strings", "--absolute-path", "--follow", "--full-path"}},
	"git grep": {verb: "Searched", short: "niwlcvEFIhHW", values: []string{"-e", "-A", "-B", "-C", "-m"},
		flags: []string{"--cached", "--untracked", "--no-index", "--heading", "--break", "--full-name"}},
	"git ls-files": {verb: "Listed", short: "comdkstz", values: []string{"-x", "--exclude"},
		flags: []string{"--cached", "--others", "--modified", "--deleted", "--exclude-standard", "--full-name"}},
}

// readerStage says how one pipeline stage explores, and what it names, if
// every option it is given is one its tool allows.
func readerStage(words []string) (string, []string, bool) {
	if len(words) == 0 {
		return "", nil, false
	}
	name, args := words[0], words[1:]
	switch name {
	case "egrep", "fgrep":
		name = "grep"
	case "git":
		// Only a subcommand straight after git: git -c … can set a pager.
		if len(args) == 0 {
			return "", nil, false
		}
		name, args = "git "+args[0], args[1:]
	case "sed":
		// Only printing a line range: exactly -n, one print script, files.
		if len(args) < 2 || args[0] != "-n" || !sedPrintScript(args[1]) {
			return "", nil, false
		}
		for _, arg := range args[2:] {
			if strings.HasPrefix(arg, "-") {
				return "", nil, false
			}
		}
		return "Read", args[2:], true
	}
	spec, known := readerSpecs[name]
	if !known {
		return "", nil, false
	}
	var operands []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		flag, _, withValue := strings.Cut(arg, "=")
		switch {
		case arg == "--" && name == "find":
			// find's -- ends only its leading options: the expression after
			// it still runs (find -- . -delete).
			return "", nil, false
		case arg == "--":
			return spec.verb, append(operands, args[index+1:]...), true
		case !strings.HasPrefix(arg, "-") || arg == "-":
			operands = append(operands, arg)
		case slices.Contains(spec.values, arg):
			index++ // its value, not an operand
		case withValue && !spec.letterwise && slices.Contains(spec.values, flag), slices.Contains(spec.flags, arg):
		case spec.digits && strings.Trim(arg[1:], "0123456789") == "":
		case strings.HasPrefix(arg, "--") || spec.short == "":
			return "", nil, false
		default:
			// A short cluster: every letter allowed, or a value flag whose
			// value is attached (head -n5).
			if slices.Contains(spec.values, arg[:2]) {
				if spec.letterwise && (arg[:2] != "-L" || strings.Trim(arg[2:], "0123456789") != "") {
					return "", nil, false
				}
				continue
			}
			if strings.Trim(arg[1:], spec.short) != "" {
				return "", nil, false
			}
		}
	}
	return spec.verb, operands, true
}

// sedPrintScript is a sed script that only prints an address range, like
// '1,80p' or '$p'.
func sedPrintScript(script string) bool {
	script = strings.Trim(script, `'"`)
	body, printed := strings.CutSuffix(script, "p")
	return printed && body != "" && strings.Trim(body, "0123456789,$") == ""
}

func workLine(s string) string {
	return clipLine(strings.Join(strings.Fields(sanitizeTerminalLine(s)), " "), 220)
}

func workExcerpt(s string, limit int, prefix string) string {
	lines := strings.Split(strings.TrimRight(sanitizeTerminalText(s), "\n"), "\n")
	var out strings.Builder
	for i, line := range lines {
		if i >= limit {
			unit := "lines"
			if len(lines)-i == 1 {
				unit = "line"
			}
			fmt.Fprintf(&out, "%s… %d more %s\n", prefix, len(lines)-i, unit)
			break
		}
		lead := prefix
		if i > 0 && prefix != "" {
			lead = "    "
		}
		fmt.Fprintf(&out, "%s%s\n", lead, clipLine(line, 240))
	}
	return out.String()
}

func writeWorkHeading(out *strings.Builder, text string, styled bool) {
	mark := styleAdd
	if strings.HasPrefix(text, "• Failed") {
		mark = styleDel
	}
	if strings.HasPrefix(text, "• Warning") || strings.HasPrefix(text, "• Unfinished") {
		mark = styleWarn
	}
	if strings.HasPrefix(text, "• ") {
		writeStyled(out, "• ", mark, styled)
		text = strings.TrimPrefix(text, "• ")
	}
	// Colour only numeric change counts; paths and commands stay ordinary text.
	for len(text) > 0 {
		start := strings.Index(text, "(+")
		if start < 0 {
			writeStyled(out, text, styleAction, styled)
			return
		}
		end := strings.IndexByte(text[start:], ')')
		if end < 0 {
			writeStyled(out, text, styleAction, styled)
			return
		}
		end += start
		added, removed, ok := strings.Cut(text[start+2:end], " -")
		_, addErr := strconv.Atoi(added)
		_, delErr := strconv.Atoi(removed)
		if !ok || addErr != nil || delErr != nil {
			writeStyled(out, text[:end+1], styleAction, styled)
			text = text[end+1:]
			continue
		}
		writeStyled(out, text[:start+1], styleAction, styled)
		writeStyled(out, "+"+added, styleAdd, styled)
		out.WriteByte(' ')
		writeStyled(out, "-"+removed, styleDel, styled)
		writeStyled(out, ")", styleAction, styled)
		text = text[end+1:]
	}
}
