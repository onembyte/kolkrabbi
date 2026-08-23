// Package cli is kolk's terminal surface: the command table, flag parsing and
// the REPL that turn a command line into engine turns.
//
// The CLI is a client of the engine, not the program. It owns no behaviour the
// daemon or the desktop shell could not also want, which is what keeps those
// two additions of a directory rather than refactors of this tree.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// defaultModel is OpenRouter's auto-router: the zero-config answer to "which
// model?", so a brand-new user never has to pick one. Override with -m or
// `kolk config set-model`.
const defaultModel = "openrouter/auto"

// app is one CLI process. Commands write through its streams rather than to
// os.Stdout directly, so the whole surface can be exercised in-process by a
// test instead of a subprocess.
type app struct {
	stdout io.Writer
	stderr io.Writer
	// in is the one shared stdin reader. The REPL and the engine's tool
	// confirmations both read lines from it; two readers would each buffer and
	// one would eat the other's input.
	in *bufio.Reader
}

func newApp() *app {
	return &app{stdout: os.Stdout, stderr: os.Stderr, in: bufio.NewReader(os.Stdin)}
}

// Main runs one kolk invocation and returns the process exit code. It never
// calls os.Exit, so cmd/kolk stays a four-line shim and tests stay in-process.
func Main(ctx context.Context, args []string) int {
	return newApp().main(ctx, args)
}

func (a *app) main(ctx context.Context, args []string) int {
	err := a.dispatch(ctx, args)
	code := exitCode(err)
	switch e := err.(type) {
	case nil:
	case *GuidedError:
		fmt.Fprintf(a.stderr, "%s\n", e.Msg)
		for _, h := range e.Hint {
			fmt.Fprintf(a.stderr, "  %s\n", h)
		}
	default:
		if code == ExitInterrupt {
			fmt.Fprintln(a.stderr, "(interrupted)")
			break
		}
		fmt.Fprintf(a.stderr, "error: %v\n", err)
		if code == ExitUsage {
			fmt.Fprintln(a.stderr, "run `kolk help` for usage.")
		}
	}
	return code
}

// command is one top-level verb. This table is the single source for dispatch,
// `kolk help` and — once they exist — shell completions and the /slash twins,
// so the parity rule holds by construction rather than by discipline.
type command struct {
	name    string
	args    string // argument grammar, shown in help
	summary string
	run     func(a *app, ctx context.Context, args []string) error
}

func commandTable() []command {
	return []command{
		{"config", "[set-key <key> | set-model <id> | set-base-url <url> | set-tier <effort> <id> | show]",
			"read and write saved settings", (*app).runConfig},
		{"models", "[filter]", "list models with context size and $/1M pricing", (*app).runModels},
		{"sessions", "[rm <id> | clear]", "list or delete saved sessions", (*app).runSessions},
		{"stats", "[--json]", "100% local usage and rating dashboard", (*app).runStats},
		{"help", "", "show this help", (*app).runHelp},
	}
}

func lookupCommand(name string) *command {
	table := commandTable()
	for i := range table {
		if table[i].name == name {
			return &table[i]
		}
	}
	return nil
}

// dispatch routes a command line. An unrecognised first word is deliberately
// NOT an error: `kolk fix the failing test` must be a prompt, not a typo.
func (a *app) dispatch(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			return a.runHelp(ctx, nil)
		}
		if c := lookupCommand(args[0]); c != nil {
			return c.run(a, ctx, args[1:])
		}
	}
	return a.runDefault(ctx, args)
}

func (a *app) runHelp(_ context.Context, args []string) error {
	if len(args) == 0 {
		a.printUsage()
		return nil
	}
	c := lookupCommand(args[0])
	if c == nil {
		return usagef("no such command %q", args[0])
	}
	fmt.Fprintf(a.stdout, "%s\n  %s\n", usageLine(c.name), c.summary)
	return nil
}

// usageLine renders a command's argument grammar from the table, so a "usage:"
// message can never drift from what dispatch actually accepts.
func usageLine(name string) string {
	c := lookupCommand(name)
	if c == nil {
		return "usage: kolk " + name
	}
	return strings.TrimSpace("usage: kolk " + c.name + " " + c.args)
}

func (a *app) printUsage() {
	fmt.Fprint(a.stdout, `kolk — chat / code / agent in one CLI, any model, any provider

Usage:
  kolk                          interactive session (mode: code)
  kolk "do the thing"           single-shot: one turn, then exit
  kolk <command> [args]

Commands:
`)
	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, c := range commandTable() {
		fmt.Fprintf(w, "  %s\t%s\n", c.name, c.summary)
	}
	w.Flush()

	fmt.Fprint(a.stdout, "\nFlags:\n")
	w = tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, f := range flagTable {
		name := "    --" + f.long
		if f.short != "" {
			name = "-" + f.short + ", --" + f.long
		}
		fmt.Fprintf(w, "  %s\t%s\n", strings.TrimSpace(name+" "+f.arg), f.summary)
	}
	w.Flush()

	fmt.Fprint(a.stdout, `
Run 'kolk help <command>' for a command's arguments.

Env:
  OPENROUTER_API_KEY            overrides the saved config key
  OPENROUTER_BASE_URL           overrides the saved base URL

Effort tiers map effort levels to models (quick→cheap, deep→frontier).
Unset tiers fall back to the session model, so zero config always works.
In agent mode, effort also scales orchestration width (2–6 subagent tasks).

Project memory: KOLKRABBI.md or AGENTS.md in the working directory is added to
the system prompt (like CLAUDE.md in Claude Code).
`)
}

func (a *app) printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, string(b))
	return nil
}

// configDir and sessionsDir are the prototype's hardcoded paths, moved here
// verbatim. They become internal/paths at migration step 5, which is where the
// XDG split and the Windows Known Folders live; until then, one place.
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kolk"
	}
	return filepath.Join(home, ".config", "kolk")
}

func sessionsDir() string { return filepath.Join(configDir(), "sessions") }

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
