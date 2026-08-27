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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/lock"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/selfupdate"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/term"
)

// defaultModel is OpenRouter's guaranteed zero-cost router. Startup normally
// picks a stronger free coding model from the live catalog; this remains the
// safe answer when the catalog is empty or unavailable. Override with -m or
// `kolk config set-model`.
const defaultModel = "openrouter/free"

// app is one CLI process. Commands write through its streams rather than to
// os.Stdout directly, so the whole surface can be exercised in-process by a
// test instead of a subprocess.
type app struct {
	stdout io.Writer
	stderr io.Writer
	// dirs is resolved lazily and once. `kolk help` and `kolk version` must
	// work on a machine where the home directory cannot be found at all, so
	// nothing touches the filesystem until a command actually needs it.
	dirs     paths.Dirs
	migrated bool
	// sessionHold marks this session live while the process runs it, so a
	// dashboard can tell which sessions are actually going.
	sessionHold *lock.File
	debugLog    *debugLog
	// projectHooksApproved remembers this session's answer per hooks-file
	// fingerprint. Session-scoped on purpose: see approveProjectHooks.
	projectHooksApproved map[string]bool
	// sessionRules are permission rules the user added for this process only.
	// They are deliberately not written anywhere: a rule that outlives the
	// session someone scoped it to is a rule nobody consented to.
	sessionRules []string
	// in is the one shared stdin reader. The REPL and the engine's tool
	// confirmations both read lines from it; two readers would each buffer and
	// one would eat the other's input.
	in *bufio.Reader
	// terminalInput/output are retained separately from the interface streams:
	// raw mode and live size probes require real file descriptors. Tests and
	// redirected invocations leave them nil and use the byte-identical REPL.
	terminalInput  *os.File
	terminalOutput *os.File

	// Credential operations are narrow function seams rather than replaceable
	// app services. Tests can prove the command refuses unsafe input before a
	// network or filesystem effect; production still gets exactly one verifier
	// and the one T0.1 store.
	verifyOpenRouter verifyOpenRouterFunc
	setCredential    setCredentialFunc
	update           func(context.Context) (selfupdate.Result, error)
	currentVersion   func() string
	now              func() time.Time
	canAnimate       func() bool
	newActivity      func(io.Writer) engine.ActivityIndicator
	chooseDefault    func(context.Context, *provider.Client) defaultModelChoice
	enterRaw         func(*os.File) (func() error, error)
	terminalOwned    func() bool
	probeHardware    func(context.Context, string) local.Hardware
	catalog          []provider.ModelInfo
	dashURL          string
	terminalSize     func(*os.File) (int, int)
	isStdinPiped     func() bool
	handover         func(context.Context, string, []string, string) error
}

func newApp() *app {
	a := &app{
		stdout: os.Stdout, stderr: os.Stderr, in: bufio.NewReader(os.Stdin),
		terminalInput: os.Stdin, terminalOutput: os.Stdout,
	}
	a.initKeyDependencies()
	a.update = selfupdate.Update
	a.currentVersion = func() string { return buildinfo.Get().Version }
	a.canAnimate = term.CanAnimate
	a.newActivity = func(out io.Writer) engine.ActivityIndicator {
		return newOctopusActivity(out, term.Color())
	}
	a.chooseDefault = discoverDefaultModel
	a.enterRaw = term.EnterRaw
	a.terminalSize = term.Size
	a.isStdinPiped = func() bool {
		stat, err := os.Stdin.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) == 0
	}
	a.handover = shell.Handover
	return a
}

// Main runs one kolk invocation and returns the process exit code. It never
// calls os.Exit, so cmd/kolk stays a four-line shim and tests stay in-process.
func Main(ctx context.Context, args []string) int {
	return newApp().main(ctx, args)
}

func (a *app) main(ctx context.Context, args []string) int {
	err := a.dispatch(ctx, args)
	code := exitCode(err)
	a.printFailure(err, code)
	return code
}

// printFailure is the one place a failure becomes words. It is separate from
// main so the error matrix can be tested without dispatching a command.
func (a *app) printFailure(err error, code int) {
	var guided *GuidedError
	switch {
	case err == nil:
	// errors.As, not a type switch: a GuidedError wrapped on its way up still
	// has to print the commands that fix it, or the guidance is lost exactly
	// when the failure got complicated enough to need it.
	case errors.As(err, &guided):
		fmt.Fprintf(a.stderr, "%s\n", guided.Msg)
		for _, h := range guided.Hint {
			fmt.Fprintf(a.stderr, "  %s\n", h)
		}
	case code == ExitInterrupt:
		fmt.Fprintln(a.stderr, "(interrupted)")
	default:
		fmt.Fprintf(a.stderr, "error: %v\n", err)
		// A provider failure is the one class of error where the raw message
		// is rarely enough: 401, 402, 404 and 429 all read as "it broke" to
		// someone who did not write the client. Advise adds the sentence that
		// says which of them it was and what to do about it.
		writeAdvice(a.stderr, err)
		if code == ExitUsage {
			fmt.Fprintln(a.stderr, "run `kolk help` for usage.")
		}
	}
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
		{"key", "<api-key> | - | <provider> <api-key|->",
			"add an API key for any supported provider", (*app).runKey},
		{"model", "[id | alias]", "switch model or list available models", (*app).runModel},
		{"effort", "[low|medium|high|max]", "set default effort level", (*app).runEffort},
		{"mode", "[chat|code|agent]", "set default operational mode", (*app).runMode},
		{"config", "[get <k> | set <k> <v> | unset <k> | set-tier <effort> <id> | show]",
			"read and write saved settings", (*app).runConfig},
		{"models", "[filter]", "list models with context size and $/1M pricing", (*app).runModels},
		{"plans", "[filter] | login <provider> <plan>", "list plans or start provider-owned login", (*app).runPlans},
		{"pmodels", "[filter]", "list models and effort levels exposed by plan connectors", (*app).runPlanModels},
		{"localia", "[models [filter] | plan <model> | pull [--yes] <model>]", "local hardware, model catalog, fit plans, and pulls", (*app).runLocalia},
		{"update", "", "install the latest verified release", (*app).runUpdate},
		{"sessions", "[search <text> | rename <id> <title> | fork <id> | export <id> [--json] | rm <id> | clear]", "list, search, fork, export or delete saved sessions", (*app).runSessions},
		{"stats", "[--json]", "100% local usage and rating dashboard", (*app).runStats},
		{"dash", "[--addr 127.0.0.1:0]", "open the local usage dashboard in a browser", (*app).runDash},
		{"serve", "[--addr <addr>] [--token <tok>] [--stdio]", "start headless event server or stdio bridge", (*app).runServe},
		{"version", "[--json]", "print the running build", (*app).runVersion},
		{"saga", "[goal | run | resume | status | stop | rewind]", "careful-progression autonomous loop", (*app).runSaga},
		{"completion", "<bash|zsh|fish>", "generate shell completions", (*app).runCompletion},
		{"doctor", "", "check keys, directories, terminal and network", (*app).runDoctor},
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
	_ = w.Flush() // a failed write to a terminal is not actionable

	fmt.Fprint(a.stdout, "\nFlags:\n")
	w = tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, f := range flagTable {
		name := "    --" + f.long
		if f.short != "" {
			name = "-" + f.short + ", --" + f.long
		}
		fmt.Fprintf(w, "  %s\t%s\n", strings.TrimSpace(name+" "+f.arg), f.summary)
	}
	_ = w.Flush() // a failed write to a terminal is not actionable

	fmt.Fprint(a.stdout, `
Run 'kolk help <command>' for a command's arguments.

Env:
  OPENROUTER_API_KEY            overrides the stored OpenRouter key
  OPENROUTER_BASE_URL           overrides the saved base URL

Effort tiers map effort levels to models (quick→cheap, deep→frontier).
Unset tiers fall back to the session model, so zero config always works.
In agent mode, effort also scales orchestration width.

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

// locate resolves kolk's paths without creating or migrating anything. The
// first-run credential check uses this read-only half so learning that a key
// is missing cannot leave state behind.
func (a *app) locate() (paths.Dirs, error) {
	if a.dirs.Data != "" {
		return a.dirs, nil
	}
	d, err := paths.Resolve()
	if err != nil {
		return paths.Dirs{}, err
	}
	a.dirs = d
	return d, nil
}

// resolve locates kolk's directories, once per process, and performs the
// one-time move of prototype-era state out of the config directory.
//
// The migration runs here rather than at startup so that a command which needs
// no state — help, version — neither triggers it nor can be broken by it.
func (a *app) resolve() (paths.Dirs, error) {
	d, err := a.locate()
	if err != nil {
		return paths.Dirs{}, err
	}
	if a.migrated {
		return d, nil
	}

	// Establish the data directory — and its .gitignore — before anything can
	// write a session, a usage record or a key into it. KOLK_DATA_DIR makes it
	// legal to point state inside a repository, and the failure mode of getting
	// that wrong is a published API key, so the guard goes in first.
	//
	// A failure here is reported but not fatal: a read-only command still has
	// useful answers, and the commands that write will fail loudly on their own.
	if err := d.EnsureData(); err != nil {
		fmt.Fprintf(a.stderr, "warning: %v\n", err)
	}

	a.migrated = true
	moved, err := d.Migrate()
	if len(moved) > 0 {
		fmt.Fprintf(a.stderr, "moved your %s to %s\n", strings.Join(moved, " and "), d.Data)
	}
	if err != nil {
		// Not fatal: kolk works from the new location either way, and the
		// old files are still on disk. Say so and continue.
		fmt.Fprintf(a.stderr, "note: %v\n", err)
	}
	return d, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
