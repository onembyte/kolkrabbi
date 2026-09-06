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
	"sync"
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
	// claudeBypassNoted: the full-auto loss on a Claude child is said once.
	claudeBypassNoted bool
	// sandboxNudged: the /sandbox suggestion on full-auto is said once.
	sandboxNudged bool
	// unknownAskedNoted: a vendor answering for a model it does not list is said once per pair.
	unknownAskedNoted map[string]bool
	stdout            io.Writer
	stderr            io.Writer
	// dirs is resolved lazily and once. `kolk help` and `kolk version` must
	// work on a machine where the home directory cannot be found at all, so
	// nothing touches the filesystem until a command actually needs it.
	dirs     paths.Dirs
	migrated bool
	// sessionHold marks this session live while the process runs it, so a
	// dashboard can tell which sessions are actually going.
	sessionHold *lock.File

	// background owns every goroutine the CLI starts that outlives the call
	// that started it — today, the catalog refresh. One owner, joined at exit,
	// per docs/plan/02-architecture.md §10. Its parent context is detached from
	// the run's cancellation so a Ctrl+C that ends a turn cannot tear a
	// half-written cache out from under a refresh; joinBackground cancels it
	// instead, so exit never waits on a provider's clock.
	backgroundMu     sync.Mutex
	backgroundCtx    context.Context
	backgroundCancel context.CancelFunc
	background       sync.WaitGroup
	debugLog         *debugLog
	// projectHooksApproved remembers this session's answer per hooks-file
	// fingerprint. Session-scoped on purpose: see approveProjectHooks.
	projectHooksApproved map[string]bool
	// sagaWake is a narrow test seam for the inline SAGA boundary. Production
	// uses runSagaLoop with the current agent; tests can prove routing and
	// posture restoration without opening a provider or changing a chapter.
	sagaWake func(context.Context, *engine.Agent) error
	// modelLister is a test seam over the connector→lister registry, so a test
	// can discover from a fake vendor instead of running the installed CLI.
	// Nil means the registry.
	modelLister func(connector string, gateway []provider.ModelInfo) provider.ModelLister
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
	chooseDefault    func([]provider.ModelInfo) defaultModelChoice
	// discoverHost finds the user's own Ollama. Injected so a test never
	// probes the real loopback port, which on the owner's machine has one.
	discoverHost func(context.Context) local.Host
	// listHostModels reads what that Ollama serves; injected for the same
	// reason.
	listHostModels func(ctx context.Context, addr, cacheFile string) ([]local.HostModel, error)
	// listCloudCatalog reads the public Ollama Cloud metadata list without
	// credentials; listCloudModels proves candidates through the local server.
	// Both are separate seams so host rows remain useful when optional Cloud
	// discovery is unavailable, and tests never touch a real endpoint.
	listCloudCatalog func(context.Context) ([]local.CloudCatalogModel, error)
	listCloudModels  func(ctx context.Context, addr, version, cacheFile string, catalog []local.CloudCatalogModel) ([]local.HostModel, error)
	// signIn asks that Ollama whether it is signed in to ollama.com, and
	// signInBudget is how long a login waits for the browser half to finish.
	// warmHost loads a host model ahead of its first turn; injected so a test
	// can see the request without a goroutine.
	warmHost     func(context.Context, modelWarmer, string)
	signIn       func(context.Context, string) local.SignInState
	signInBudget time.Duration
	// startHost brings up an idle Ollama for one command; injected so a test
	// never starts a process.
	startHost func(context.Context, local.Host) (string, func(), error)
	// pulledNames reads the host store manifest tree; injected so a test never
	// reads the real ~/.ollama.
	pulledNames   func() map[string]bool
	enterRaw      func(*os.File) (func() error, error)
	terminalOwned func() bool
	// readHidden reads a credential through the TUI's masked overlay while the
	// TUI owns the terminal; nil everywhere else.
	readHidden     func(ctx context.Context, prompt string) (string, bool)
	probeHardware  func(context.Context, string) local.Hardware
	catalog        []provider.ModelInfo
	dashURL        string
	terminalSize   func(*os.File) (int, int)
	resizeNotifier func(*os.File) (<-chan struct{}, func())
	// restartInto is the version an accepted in-session update wants to hand
	// over to. The exec happens after the screen is torn down and the terminal
	// restored, never from inside the slash handler: replacing the process
	// image while the renderer owns a raw terminal leaves the shell unusable
	// if anything goes wrong.
	restartInto string
	// pendingLogin is a provider sign-in a session asked for. It cannot run
	// while the screen is up — the input pump owns the keyboard and would eat
	// the provider CLI's keystrokes — so it runs once the screen is down, and
	// the session is resumed afterwards.
	pendingLogin   *provider.Plan
	replaceSelf    func(path string, args []string, env []string) error
	executablePath func() (string, error)
	isStdinPiped   func() bool
	handover       func(context.Context, string, []string, string) error
	// handoverWindow runs a provider login in a terminal window kolk opens
	// itself, so a session never has to step down for its user to sign in.
	// Nil means this kolk build has no such path and the screen-down flow
	// below is the only way.
	handoverWindow func(context.Context, string, []string) error
	// loginInSession runs a provider login on a pty inside the running session,
	// which is where a person asking to sign in expects it to happen. It is
	// nil outside a TUI session, and preferred over every other runner when
	// set: a window kolk opens is a second place to look, and on a stock macOS
	// there is no emulator on PATH for it to open at all.
	loginInSession func(context.Context, string, []string) error
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
	a.discoverHost = func(ctx context.Context) local.Host {
		return local.DiscoverHost(ctx, local.HostDiscovery{Addr: local.DefaultHostAddr, LookPath: shell.LookPath})
	}
	a.listHostModels = local.ListHostModels
	a.listCloudCatalog = local.ListCloudCatalog
	a.listCloudModels = local.ListCloudModels
	a.signIn = local.SignIn
	a.pulledNames = func() map[string]bool { return local.PulledNames(local.HostModelDir(os.Environ())) }
	a.signInBudget = 2 * time.Minute
	a.newActivity = func(out io.Writer) engine.ActivityIndicator {
		return newOctopusActivity(out, term.Color())
	}
	a.chooseDefault = chooseDefaultModel
	a.enterRaw = term.EnterRaw
	a.terminalSize = term.Size
	a.resizeNotifier = term.ResizeNotifier
	a.replaceSelf = shell.Replace
	a.executablePath = shell.SelfPath
	a.isStdinPiped = func() bool {
		stat, err := os.Stdin.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) == 0
	}
	a.handover = shell.Handover
	a.handoverWindow = shell.LoginWindow
	return a
}

// Main runs one kolk invocation and returns the process exit code. It never
// calls os.Exit, so cmd/kolk stays a four-line shim and tests stay in-process.
func Main(ctx context.Context, args []string) int {
	return newApp().main(ctx, args)
}

func (a *app) main(ctx context.Context, args []string) int {
	// The sandbox's confined child is kolk re-executed with an environment
	// variable set, never an argv verb: the four-command surface stays four.
	if handled, code := shell.MaybeRunAsLandlockChild(args, a.stderr); handled {
		return code
	}
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

// commandTable is the closed outside-session surface.
//
// Amended 2026-09-02 (docs/plan/09 §"the outside-session surface is closed"):
// the session is the product, so a verb out here has to be something a session
// cannot do. Four are. Everything else that used to live here — key, model,
// effort, mode, config, models, plans, pmodels, localia, update, stats, dash,
// devices, version, doctor, completion — is a slash command and only a slash
// command.
//
// Opening a session is not a verb and is not in this table: bare `kolk`,
// `kolk -r`, `kolk "<prompt>"` and the flags are the ways in, and dispatch
// reaches them by falling through.
//
// **Nothing may be added here.** A fifth verb fails
// TestOutsideSessionSurfaceIsClosed, and the owner is to be asked twice before
// that test is ever edited.
func commandTable() []command {
	return []command{
		{"sessions", "[search <text> | rename <id> <title> | fork <id> | export <id> [--json] | rm <id> | clear]",
			"list this folder's saved sessions, or search, fork, export or delete one", (*app).runSessions},
		{"serve", "[--addr <addr>] [--pair] [--stdio]",
			"host a session for a client: pick one to serve, or start a new one", (*app).runServe},
		{"uninstall", "[--keep-data] [--yes]", "remove kolk and everything it stored", (*app).runUninstall},
		{"help", "", "what Kolkrabbi is, and every command inside and outside a session", (*app).runHelp},
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

// retiredVerbs are the commands that moved into the session on 2026-09-02, and
// what to type instead.
//
// They exist because of what dispatch does with an unrecognised word. Treating
// one as a prompt is right for `kolk fix the failing test` and catastrophic for
// `kolk version`: the word becomes a turn, the turn reaches a model, and a
// command that used to print one line spends the user's plan. That is not a
// hypothetical — `make budgets` measures cold start by running `kolk version`
// twenty times, and after these verbs were removed it sent seventy-four turns
// to a real subscription before the budget gate's own timing caught it.
//
// So a retired name is refused with the spelling that works, for free. Only an
// exact single-word match: `kolk "config the model"` is still a prompt, because
// the whole sentence is one argument and never equals a verb.
var retiredVerbs = map[string]string{
	"key": "/key", "model": "/model", "effort": "/effort", "mode": "/mode",
	"config": "/config", "models": "/model", "plans": "/plans", "pmodels": "/pmodels",
	"localia": "/localia", "update": "/update", "stats": "/stats", "dash": "/dash",
	"devices": "/devices", "version": "/version", "doctor": "/doctor", "resume": "/resume",
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
		if slash, retired := retiredVerbs[args[0]]; retired {
			return &GuidedError{
				Msg: fmt.Sprintf("`kolk %s` is a session command now: %s", args[0], slash),
				Hint: []string{
					"kolk            open a session",
					slash + "   then run it there",
					"",
					"`kolk help` lists every command, in and out of a session.",
					fmt.Sprintf("To send %q to a model as a prompt, quote it: kolk %q", args[0], args[0]),
				},
			}
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

// usageLine is the usage a command prints when it is used wrongly, generated
// from the registry the command actually lives in.
//
// Most commands live in the session now (docs/plan/09, 2026-09-02), so the
// slash registry is consulted first: `/config set-everything` must answer with
// `usage: /config …`, not with a `kolk config` that no longer exists. The four
// outside-session verbs still generate from the command table.
func usageLine(name string) string {
	if c := lookupCommand(name); c != nil {
		return strings.TrimSpace("usage: kolk " + c.name + " " + c.args)
	}
	for _, sc := range slashCommandTable {
		if sc.name == name {
			return strings.TrimSpace("usage: /" + sc.name + " " + sc.args)
		}
	}
	return "usage: /" + name
}

// printUsage is the front door.
//
// `kolk help` is the one command someone runs before they know anything, and
// since 2026-09-02 it has a second job: the session is where the commands are,
// so help has to say that plainly or a new user will look for `kolk model` and
// conclude it was removed rather than moved. It prints what Kolkrabbi is, the
// build and licence, both surfaces in full, the flags, and where its state
// lives.
func (a *app) printUsage() {
	build := buildinfo.Get()
	fmt.Fprintf(a.stdout, `kolk — chat, code, and ordered agents in one terminal, on any model

  Kolkrabbi runs a conversation that can read and write files, run commands,
  and split a long job into ordered tasks. It works against OpenRouter, any
  OpenAI-compatible endpoint, a Claude or Codex subscription through its own
  CLI, or a model on this machine. Every call is logged locally so you can see
  what each model costs and how well it did; nothing is sent anywhere else.

  %s
  Apache-2.0   ·   https://github.com/onembyte/kolkrabbi

Open a session — this is the normal way in:
  kolk                          a session, in code mode
  kolk "do the thing"           one turn, then exit
  kolk -r                       reopen the most recent session
  kolk --mode chat              start in another mode

Inside the session, everything is a /command:
`, build.String())

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, c := range slashCommandTable {
		usage := "/" + c.name
		if c.args != "" {
			usage += " " + c.args
		}
		fmt.Fprintf(w, "  %s\t%s\n", usage, c.summary)
	}
	_ = w.Flush() // a failed write to a terminal is not actionable

	fmt.Fprint(a.stdout, `
Outside a session there are four commands and no more, because only these are
things a session cannot do:
`)
	// Name and summary only: `kolk sessions` grammar is long enough to push
	// every summary off the right edge, and `kolk help <command>` is where a
	// grammar belongs.
	w = tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, c := range commandTable() {
		fmt.Fprintf(w, "  kolk %s\t%s\n", c.name, c.summary)
	}
	_ = w.Flush()

	fmt.Fprint(a.stdout, "\nFlags:\n")
	w = tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	for _, f := range flagTable {
		name := "    --" + f.long
		if f.short != "" {
			name = "-" + f.short + ", --" + f.long
		}
		fmt.Fprintf(w, "  %s\t%s\n", strings.TrimSpace(name+" "+f.arg), f.summary)
	}
	_ = w.Flush()

	fmt.Fprint(a.stdout, `
What it can do:
  three modes        /mode chat (no tools) · code (the tool loop) · agent
                     (plans tasks, routes each to a model, runs them, answers once)
  an effort dial     /effort low|medium|high|max picks the model tier, the tool-round
                     limit, the shell timeout, and how wide an agent run may go
  any provider       OpenRouter, any OpenAI-compatible URL (Ollama, LiteLLM, vLLM),
                     a Claude or Codex subscription through its own CLI, or local Ollama
  your money's rules a model you pick is a ceiling: an orchestrated run may route to
                     something cheaper, never to something dearer
  permission tiers   /permissions ask · auto-approve · full-auto — and a floor no tier
                     removes: credential files, system paths, sudo, curl-into-shell
  local accounting   every call's tokens, cost and latency in ~/.config/kolk/stats.jsonl;
                     /rate 1-5 adds your judgement, /stats and /dash read it back
  checkpoints        /diff, /changes, /undo and /rewind take back what a turn wrote
  careful progression append /saga to a request to work it in committed chapters
  project memory     KOLKRABBI.md or AGENTS.md in the working directory joins the prompt

Env:
  OPENROUTER_API_KEY            overrides the stored OpenRouter key
  OPENROUTER_BASE_URL           overrides the saved base URL
  KOLK_CONFIG_DIR / _DATA_DIR / _CACHE_DIR
                                override where settings, sessions and caches live

Run 'kolk help <command>' for one of the four commands above; inside a session,
/help lists every command with its arguments.
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

// startBackground runs fn on a goroutine the app owns. ctx is the caller's:
// its values carry over, its cancellation does not.
func (a *app) startBackground(ctx context.Context, fn func(context.Context)) {
	a.backgroundMu.Lock()
	if a.backgroundCtx == nil {
		a.backgroundCtx, a.backgroundCancel = context.WithCancel(context.WithoutCancel(ctx))
	}
	parent := a.backgroundCtx
	a.backgroundMu.Unlock()
	a.background.Add(1)
	go func() {
		defer a.background.Done()
		fn(parent)
	}()
}

// joinBackground cancels and then waits for everything startBackground
// started. Cancel first: a refresh that has not finished by exit is abandoned,
// and the stale cache it would have replaced still serves the next start.
func (a *app) joinBackground() {
	a.backgroundMu.Lock()
	cancel := a.backgroundCancel
	a.backgroundMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.background.Wait()
}
