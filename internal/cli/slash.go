package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/commands"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

type slashCommand struct {
	name    string
	args    string
	summary string
}

var slashCommandTable = []slashCommand{
	{"key", "<api-key> | - | <provider> <key>", "add an API key for any supported provider"},
	{"mode", "<chat|code|agent>", "switch mode (agent = orchestrated; code is default)"},
	{"effort", "<low|medium|high|max>", "select model tier and orchestration width"},
	{"model", "[id]", "list available models or switch this session"},
	{"plans", "[filter] | login <provider> <plan>", "list plans or start provider-owned login"},
	{"plogin", "[filter]", "search plans and start provider-owned login"},
	{"pmodels", "[filter]", "list models and effort levels exposed by plan connectors"},
	{"localia", "[models [filter] | plan <model> | pull [--yes] <model>]", "local hardware, model catalog, fit plans, and pulls"},
	{"compact", "[undo]", "shrink the conversation now, or put back the last one"},
	{"remember", "[--project] <note>", "add one line of standing guidance"},
	{"config", "[get <k> | set <k> <v> | unset <k> | show]", "read and write saved settings"},
	{"update", "", "install the latest verified release"},
	{"stats", "[--json]", "100% local usage and rating dashboard"},
	{"dash", "[--addr 127.0.0.1:0]", "open the local usage dashboard in a browser"},
	{"version", "[--json]", "print the running build"},
	{"rate", "<1-5>", "rate the last turn for local stats"},
	{"permissions", "[ask|auto-approve|full-auto]", "see or choose how much may happen without asking"},
	{"ask", "", "confirm before changing a file or running a command"},
	{"auto-approve", "", "edit inside the project without asking; still ask before commands"},
	{"full-auto", "", "stop asking; the floor still refuses"},
	{"new", "", "start a fresh saved session"},
	{"clear", "", "alias for /new"},
	{"session", "", "show the current session id and file"},
	{"changes", "", "list files modified by this session"},
	{"diff", "[path]", "show what this session changed, as a diff"},
	{"plan", "[off]", "read-only: explore and propose, without writing or running anything"},
	{"saga", "[goal | run | resume | status | stop | rewind]", "careful-progression autonomous loop"},
	{"devices", "[revoke <id>]", "list paired devices, or revoke one without stopping the session"},
	{"undo", "[task <n>]", "take back the last turn, or one subagent's file changes alone"},
	{"rewind", "", "restore the last turn's files only, leaving the conversation"},
	{"commit", "", "draft a commit message from the staged diff, and stop"},
	{"pr", "", "draft a pull request title and body, and hand over `gh pr create`"},
	{"doctor", "", "check keys, directories, terminal and network"},
	{"help", "", "show all slash commands"},
	{"exit", "", "quit Kolkrabbi"},
	{"quit", "", "alias for /exit"},
}

func slashSuggestions() []tui.CommandSpec {
	commands := make([]tui.CommandSpec, 0, len(slashCommandTable))
	for _, command := range slashCommandTable {
		usage := "/" + command.name
		if command.args != "" {
			usage += " " + command.args
		}
		commands = append(commands, tui.CommandSpec{
			Name: command.name, Usage: usage, Summary: command.summary,
		})
	}
	return commands
}

func printSlashHelp(out interface{ Write([]byte) (int, error) }) {
	for _, command := range slashCommandTable {
		usage := "/" + command.name
		if command.args != "" {
			usage += " " + command.args
		}
		_, _ = fmt.Fprintf(out, "%-42s %s\n", usage, command.summary)
	}
}

// reportAgentLane says what an orchestrated run may spend on.
//
// The roster is printed on entering agent mode, before it can cost anything.
// A limit nobody can see is one people meet by being surprised, and this one is
// worth seeing twice: it names the ceiling, and it names what is below it, so
// the difference between "I picked a cheap model" and "cheap work will run
// cheaply" is visible rather than assumed.
func (a *app) reportAgentLane(ag *engine.Agent) {
	if ag.Mode != engine.ModeAgent {
		return
	}
	roster := ag.Roster(a.rungAvailable())
	blocked := engine.ModelsAboveCeiling(ag.SessionModel())
	if len(roster.Rungs) < 2 && len(blocked) == 0 {
		// On no ranked ladder, or already at the top with nothing cheaper
		// reachable. Neither is worth a line: the first would be a claim kolk
		// cannot make, the second says nothing the user did not just choose.
		return
	}

	models := make([]string, 0, len(roster.Rungs))
	for _, rung := range roster.Rungs {
		models = append(models, rung.Model)
	}
	fmt.Fprintf(a.stdout, "agent lane: %s\n", strings.Join(models, " → "))
	if len(blocked) > 0 {
		fmt.Fprintf(a.stdout, "  capped at %s — %s out of reach\n",
			ag.SessionModel(), strings.Join(blocked, " and "))
	}
	if len(roster.Rungs) == 1 && len(blocked) > 0 {
		// Being told what is refused without being told what is available is
		// half an answer, and this is the half people act on.
		fmt.Fprintln(a.stdout, "  nothing cheaper is signed in, so every task runs on your model")
	}
}

func (a *app) slash(ctx context.Context, ag *engine.Agent, line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, cmd))

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/commit":
		a.runCommitDraft(ctx, ag)
	case "/pr":
		a.runPullRequestDraft(ctx, ag)
	case "/doctor":
		if err := a.runDoctor(ctx, nil); err != nil {
			fmt.Fprintln(a.stdout, err)
		}
	case "/help":
		printSlashHelp(a.stdout)
		// Listed after the built-ins and marked, so a reader can tell what came
		// from this machine's files from what kolk ships.
		for _, command := range a.markdownCommands() {
			summary := command.Description
			if summary == "" {
				summary = "from " + command.Source
			}
			fmt.Fprintf(a.stdout, "%-42s %s\n", "/"+command.Name, summary)
		}
		fmt.Fprintln(a.stdout, "\nKeys: ↑ last message · Shift+Enter newline · Ctrl+C clear input (twice exits)")
	case "/mode":
		if arg == "" {
			fmt.Fprintf(a.stdout, "mode: %s (chat|code|agent)\n", ag.Mode)
			break
		}
		// A provider CLI owns its own tool loop, so the tool set and the
		// permission mode it should run under are flags on its process — which
		// replays no argv. Mode is part of that contract, exactly like effort:
		// a change means a new process.
		//
		// Agent mode is no longer refused here. It used to be, for every plan
		// connector at once, on the grounds that the vendor schedules its own
		// subagents — but the vendor's scheduler is off (its Task tool is not
		// in the tool set), and kolk's agent mode spawns kolk's own children,
		// which kolk starts and can stop. Whether a given connector is ready
		// is the adapter's question, not this one's: both shipped plan
		// adapters accept agent mode, and any connector-specific startup
		// error surfaces from the restart below.
		if plan, ok := ag.SessionBackend().(*verifyingBackend); ok {
			if err := ag.SetMode(arg); err != nil {
				fmt.Fprintln(a.stdout, err)
				break
			}
			fmt.Fprintf(a.stdout, "mode: %s\n", ag.Mode)
			if plan.mode != ag.Mode {
				model, connector := plan.plan.Model, plan.plan.Connector
				label, err := a.switchModel(ctx, ag, model)
				if err != nil {
					fmt.Fprintf(a.stdout, "could not restart %s in %s mode: %v\n", connector, ag.Mode, err)
					fmt.Fprintf(a.stdout, "  re-run /model %s to retry\n", model)
					break
				}
				fmt.Fprintf(a.stdout, "%s restarted in %s mode (%s)\n", connector, ag.Mode, label)
			}
			a.reportAgentLane(ag)
			break
		}
		if err := ag.SetMode(arg); err != nil {
			fmt.Fprintln(a.stdout, err)
		} else {
			fmt.Fprintf(a.stdout, "mode: %s\n", ag.Mode)
			a.reportAgentLane(ag)
		}
	case "/effort":
		if arg == "" {
			fmt.Fprintf(a.stdout, "effort: %s (low|medium|high|max)\n", ag.Effort)
			break
		}
		if err := ag.SetEffort(arg); err != nil {
			fmt.Fprintln(a.stdout, err)
			break
		}
		if ag.Sess != nil {
			ag.Sess.SetEffort(ag.Effort)
		}
		m := ag.ModelForEffort(ag.Effort)
		fmt.Fprintf(a.stdout, "effort: %s → %s\n", ag.Effort, m)
		// A provider CLI is started with its effort and replays no argv, so a
		// new level means a new process. The restart is the dial's job, not a
		// second command the user has to remember.
		if plan, ok := ag.SessionBackend().(*verifyingBackend); ok {
			if plan.effort == ag.Effort {
				break
			}
			model, connector := plan.plan.Model, plan.plan.Connector
			label, err := a.switchModel(ctx, ag, model)
			if err != nil {
				fmt.Fprintf(a.stdout, "could not restart %s at %s effort: %v\n", connector, ag.Effort, err)
				fmt.Fprintf(a.stdout, "  re-run /model %s to retry\n", model)
				break
			}
			fmt.Fprintf(a.stdout, "%s restarted at %s effort (%s)\n", connector, ag.Effort, label)
		}
	case "/rate":
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintln(a.stdout, "usage: /rate <1-5>")
			break
		}
		if err := ag.RateLast(n); err != nil {
			fmt.Fprintln(a.stdout, err)
		} else {
			fmt.Fprintf(a.stdout, "rated %d★ — see `kolk stats`\n", n)
		}
	case "/new", "/clear":
		sess := session.New(a.dirs.Sessions(), ag.SessionModel())
		ckpt, err := checkpoint.Open(sess.CkptDir())
		if err != nil {
			ckpt = nil
		}
		if ckpt != nil {
			// A new session in the same project gets its own store. The notice
			// is not reprinted: whatever the answer is here, the session this
			// one replaced already said it.
			ckpt.UseShadow(ctx, projectRoot())
		}
		opts := ag.Options
		opts.Sess = sess
		opts.Ckpt = ckpt
		*ag = *engine.New(opts)
		fmt.Fprintf(a.stdout, "new session: %s\n", sess.ID)
	case "/session":
		sessID := ""
		if ag.Sess != nil {
			sessID = ag.Sess.SessionID()
		}
		fmt.Fprintf(a.stdout, "id:    %s\nfile:  %s\n", sessID, a.dirs.Session(sessID))
	case "/changes":
		if ag.Ckpt == nil {
			fmt.Fprintln(a.stdout, "checkpointing is not enabled.")
			break
		}
		ck, ok := ag.Ckpt.(*checkpoint.Store)
		if !ok {
			fmt.Fprintln(a.stdout, "checkpoint store not available.")
			break
		}
		ch := ck.Changes()
		if len(ch) == 0 {
			fmt.Fprintln(a.stdout, "no file changes recorded this session.")
			break
		}
		for _, e := range ch {
			verb := "edited"
			if !e.Existed {
				verb = "created"
			}
			fmt.Fprintf(a.stdout, "turn %-3d %-8s %s (%s)\n", e.Turn, verb, e.Path, e.Tool)
		}
	case "/plan":
		a.planMode(ag, arg)
	case "/diff":
		if ag.Ckpt == nil {
			fmt.Fprintln(a.stdout, "checkpointing is not enabled, so there is nothing to compare against.")
			break
		}
		store, ok := ag.Ckpt.(*checkpoint.Store)
		if !ok {
			fmt.Fprintln(a.stdout, "checkpoint store not available.")
			break
		}
		a.printSessionDiff(store, strings.TrimSpace(arg))
	case "/devices":
		// The same listing as `kolk devices`, from inside a session: noticing a
		// device you do not recognise should not mean stopping to deal with it.
		if err := a.runDevices(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "devices: %v\n", err)
		}
	case "/undo":
		// `/undo task <n>` takes back one writing subagent and leaves the rest
		// of the turn standing (A33.8). The bare form is unchanged: the whole
		// turn, files and conversation together.
		if rest := strings.TrimSpace(arg); strings.HasPrefix(rest, "task") {
			a.undoTask(ctx, ag, strings.TrimSpace(strings.TrimPrefix(rest, "task")))
			break
		}
		result, err := ag.Undo(ctx)
		if err != nil {
			fmt.Fprintf(a.stderr, "undo failed: %v\n", err)
			if len(result.Files) > 0 {
				fmt.Fprintln(a.stderr, "some files were restored before it stopped; the conversation is unchanged.")
				for _, p := range result.Files {
					fmt.Fprintln(a.stderr, "  "+p)
				}
			}
			break
		}
		if result.Messages == 0 && len(result.Files) == 0 {
			fmt.Fprintln(a.stdout, "nothing to undo.")
			break
		}
		fmt.Fprintf(a.stdout, "undid the last turn: %d messages", result.Messages)
		if len(result.Files) == 0 {
			fmt.Fprintln(a.stdout, ", no file changes.")
			break
		}
		fmt.Fprintf(a.stdout, ", %d file(s) restored:\n", len(result.Files))
		for _, p := range result.Files {
			fmt.Fprintln(a.stdout, "  "+p)
		}
	case "/rewind":
		restored, err := ag.Rewind(ctx)
		if err != nil {
			fmt.Fprintf(a.stderr, "rewind failed: %v\n", err)
			break
		}
		if restored == nil {
			fmt.Fprintln(a.stdout, "nothing to rewind.")
			break
		}
		fmt.Fprintln(a.stdout, "restored:")
		for _, p := range restored {
			fmt.Fprintln(a.stdout, "  "+p)
		}
		fmt.Fprintln(a.stdout, "\033[2mnote: files only — the conversation still describes these edits. /undo takes back both.\033[0m")
	case "/permissions", "/permission":
		if strings.TrimSpace(arg) == "" {
			a.showPermissions(ag)
			break
		}
		if looksLikeRule(arg) {
			a.editRule(ag, arg)
			break
		}
		a.setPermission(ag, strings.TrimSpace(arg))
	case "/ask", "/auto-approve", "/full-auto":
		a.setPermission(ag, strings.TrimPrefix(cmd, "/"))
	case "/model":
		if arg == "" {
			fmt.Fprintf(a.stdout, "current model: %s\n", ag.SessionModel())
			d, _ := a.locate()
			if err := a.printModelCatalog(ctx, ag.Client, d.CatalogFile(), false, ""); err != nil {
				fmt.Fprintf(a.stderr, "could not list models: %v\n", err)
			}
			fmt.Fprintln(a.stdout, "\nswitch: /model <name|alias>")
		} else if planModel, err := a.namedPlanModel(arg); err != nil {
			// A plan model the user cannot use yet is a refusal with a reason,
			// never a catalog search that ends in an OpenRouter error.
			fmt.Fprintf(a.stdout, "%v\n", err)
		} else if planModel || strings.Contains(arg, "/") || provider.ResolveModelAlias(arg) != arg {
			label, err := a.switchModel(ctx, ag, arg)
			if err != nil {
				fmt.Fprintf(a.stdout, "%v\n", err)
				break
			}
			fmt.Fprintf(a.stdout, "model set to %s\n", label)
		} else {
			d, _ := a.locate()
			if err := a.printModelCatalog(ctx, ag.Client, d.CatalogFile(), false, arg); err != nil {
				fmt.Fprintf(a.stderr, "could not list models: %v\n", err)
			}
			fmt.Fprintf(a.stdout, "\nswitch: /model <id|alias>\n")
		}
	case "/plans":
		if err := a.runPlans(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "plans error: %v\n", err)
		}
		if a.pendingLogin != nil {
			// End the session so the provider CLI gets a terminal nobody else
			// is reading; finishSession signs in and comes back.
			return true
		}
	case "/plogin":
		if err := a.runPlanLogin(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "plan login error: %v\n", err)
		}
		if a.pendingLogin != nil {
			return true
		}
	case "/remember":
		if err := a.runRemember(strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "remember error: %v\n", err)
		}
	case "/compact":
		if strings.TrimSpace(arg) == "undo" {
			if ag.RestoreCompaction() {
				fmt.Fprintln(a.stdout, "restored the conversation replaced by the last compaction")
			} else {
				fmt.Fprintln(a.stdout, "no compaction to undo in this session")
			}
			break
		}
		target := int(float64(ag.ContextWindow) * 0.5)
		result, changed := ag.CompactNow(ctx, target)
		if !changed {
			fmt.Fprintln(a.stdout, "nothing to compact yet; the recent turns are kept as they are")
			break
		}
		fmt.Fprintf(a.stdout, "compacted %d messages (%s), freeing about %d tokens · undo with /compact undo\n",
			result.Replaced, result.Stage, result.FreedTokens)
	case "/localia":
		if err := a.runLocalia(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "localia error: %v\n", err)
		}
	case "/pmodels":
		if err := a.runPlanModels(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "pmodels error: %v\n", err)
		}
	case "/key":
		if err := a.runKey(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "key error: %v\n", err)
		}
	case "/config":
		if err := a.runConfig(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "config error: %v\n", err)
		}
	case "/dash":
		addr, err := dashAddrFrom(strings.Fields(arg))
		if err == nil {
			err = a.startDashInSession(addr)
		}
		if err != nil {
			fmt.Fprintf(a.stderr, "dash error: %v\n", err)
		}
	case "/stats":
		if err := a.runStats(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "stats error: %v\n", err)
		}
	case "/version":
		if err := a.runVersion(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "version error: %v\n", err)
		}
	case "/update":
		if arg != "" {
			fmt.Fprintln(a.stdout, "usage: /update")
			break
		}
		if err := a.applyUpdate(ctx, true); err != nil {
			fmt.Fprintf(a.stderr, "update failed: %v\n", err)
		}
		if a.restartInto != "" {
			// End the session so the screen comes down and the terminal is
			// restored; tuiRepl performs the handover from there.
			return true
		}
	case "/saga":
		if err := a.runSaga(ctx, strings.Fields(arg)); err != nil {
			fmt.Fprintf(a.stderr, "saga error: %v\n", err)
		}
	default:
		// A markdown command is not a built-in and cannot shadow one: this is
		// reached only after every built-in has been tried. It expands to a
		// prompt and runs as a user turn, so what the model then does is judged
		// exactly as if the person had typed it.
		if command, found := a.markdownCommand(strings.TrimPrefix(cmd, "/")); found {
			if err := ag.RunTurn(ctx, command.Prompt(arg)); err != nil {
				fmt.Fprintf(a.stderr, "error: %v\n", err)
				writeAdvice(a.stderr, err)
			}
			break
		}
		fmt.Fprintln(a.stdout, "unknown command, /help for a list")
	}
	return false
}

// markdownCommand looks one up by name.
//
// The directories are read on demand rather than cached, and the measurement
// says that is fine: a lookup costs one ReadDir of a directory that usually
// does not exist. Caching would mean a file added mid-session did not work
// until the session restarted, which is the wrong trade for something a person
// edits while using it.
func (a *app) markdownCommand(name string) (commands.Command, bool) {
	for _, command := range a.markdownCommands() {
		if command.Name == name {
			return command, true
		}
	}
	return commands.Command{}, false
}

func (a *app) markdownCommands() []commands.Command {
	d, err := a.resolve()
	if err != nil {
		return nil
	}
	return commands.Load(projectRoot(), d.Config)
}

// permissionTiers is the whole permission model, in the order a person should
// read it: safest first.
var permissionTiers = []struct {
	tier    engine.Permission
	summary string
}{
	{engine.PermissionAsk, "asks before changing a file or running a command"},
	{engine.PermissionAutoApprove, "edits inside the project without asking; still asks before running a command or leaving the project"},
	{engine.PermissionFullAuto, "stops asking; still refuses the floor and logs every step outside the project"},
}

// showPermissions lists the tiers and marks the active one.
func (a *app) showPermissions(ag *engine.Agent) {
	fmt.Fprintln(a.stdout, "permission tiers — choose with /permissions <tier>, or /ask, /auto-approve, /full-auto")
	for _, entry := range permissionTiers {
		marker := "  "
		if ag.Permission == entry.tier {
			marker = "→ "
		}
		fmt.Fprintf(a.stdout, "%s%-13s %s\n", marker, entry.tier, entry.summary)
	}
	fmt.Fprintln(a.stdout, "\nno tier removes the floor: credential files, system directories, sudo,")
	fmt.Fprintln(a.stdout, "piping a download into a shell and unrecoverable deletes are refused in all three.")
	a.printRules(ag)
}

// setPermission moves the session to one tier and says what that now means.
func (a *app) setPermission(ag *engine.Agent, name string) {
	tier, ok := engine.NormalizePermission(name)
	if !ok {
		fmt.Fprintf(a.stdout, "%q is not a tier.\n", name)
		a.showPermissions(ag)
		return
	}
	ag.Permission = tier
	for _, entry := range permissionTiers {
		if entry.tier == tier {
			fmt.Fprintf(a.stdout, "permission: %s — %s\n", tier, entry.summary)
		}
	}
	if tier == engine.PermissionFullAuto {
		// The moment to say it is not unlimited is the moment someone asks for
		// the most permissive tier.
		fmt.Fprintln(a.stdout, "Kolkrabbi still refuses credential files, system directories, sudo, downloads piped into a shell and unrecoverable deletes.")
	}
	fmt.Fprintln(a.stdout, "this process only; start another with `kolk --permission "+string(tier)+"`")
}
