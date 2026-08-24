package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

type slashCommand struct {
	name    string
	args    string
	summary string
}

var slashCommandTable = []slashCommand{
	{"mode", "<chat|code|agent>", "switch mode (agent = orchestrated; code is default)"},
	{"effort", "<quick|standard|deep|ultra>", "select model tier and orchestration width"},
	{"model", "[id]", "list available models or switch this session"},
	{"update", "", "install the latest verified release"},
	{"rate", "<1-5>", "rate the last turn for local stats"},
	{"auto-approve", "[on|off]", "control tool confirmations for this session"},
	{"yolo", "", "toggle auto-approval for this session"},
	{"new", "", "start a fresh saved session"},
	{"clear", "", "alias for /new"},
	{"session", "", "show the current session id and file"},
	{"changes", "", "list files modified by this session"},
	{"rewind", "", "undo the last turn's file changes"},
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

// slash handles a /command typed in the REPL. It returns true when the REPL
// should exit.
func (a *app) slash(ctx context.Context, ag *engine.Agent, line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, cmd))

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		printSlashHelp(a.stdout)
		fmt.Fprintln(a.stdout, "\nKeys: ↑ last message · Shift+Enter newline · Ctrl+C clear input (twice exits)")
	case "/mode":
		if arg == "" {
			fmt.Fprintf(a.stdout, "mode: %s (chat|code|agent)\n", ag.Mode)
			break
		}
		if err := ag.SetMode(arg); err != nil {
			fmt.Fprintln(a.stdout, err)
		} else {
			fmt.Fprintf(a.stdout, "mode: %s\n", ag.Mode)
		}
	case "/effort":
		if arg == "" {
			fmt.Fprintf(a.stdout, "effort: %s (quick|standard|deep|ultra)\n", ag.Effort)
			break
		}
		if err := ag.SetEffort(arg); err != nil {
			fmt.Fprintln(a.stdout, err)
		} else {
			m := ag.Model
			if t, ok := ag.Tiers[ag.Effort]; ok && t != "" {
				m = t
			}
			fmt.Fprintf(a.stdout, "effort: %s → %s\n", ag.Effort, m)
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
		sess := session.New(a.dirs.Sessions(), ag.Model)
		ckpt, err := checkpoint.Open(sess.CkptDir())
		if err != nil {
			ckpt = nil
		}
		opts := ag.Options
		opts.Sess = sess
		opts.Ckpt = ckpt
		*ag = *engine.New(opts)
		fmt.Fprintf(a.stdout, "new session: %s\n", sess.ID)
	case "/session":
		fmt.Fprintf(a.stdout, "id:    %s\nfile:  %s\n", ag.Sess.ID, a.dirs.Session(ag.Sess.ID))
	case "/changes":
		if ag.Ckpt == nil {
			fmt.Fprintln(a.stdout, "checkpointing is not enabled.")
			break
		}
		ch := ag.Ckpt.Changes()
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
	case "/rewind":
		restored, err := ag.Rewind()
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
		fmt.Fprintln(a.stdout, "\033[2mnote: files only — the conversation history is unchanged.\033[0m")
	case "/yolo":
		ag.Yolo = !ag.Yolo
		if ag.Yolo {
			fmt.Fprintln(a.stdout, "yolo mode: true — this process only; start another with `kolk --yolo`")
		} else {
			fmt.Fprintln(a.stdout, "yolo mode: false — tool actions will ask first")
		}
	case "/auto-approve":
		switch arg {
		case "", "on":
			a.setAutoApprove(ag, true)
		case "off":
			a.setAutoApprove(ag, false)
		default:
			fmt.Fprintln(a.stdout, "usage: /auto-approve [on|off]")
		}
	case "/model":
		if arg == "" {
			fmt.Fprintf(a.stdout, "current model: %s\n", ag.Model)
			if err := a.printModelCatalog(ctx, ag.Client, ""); err != nil {
				fmt.Fprintf(a.stderr, "could not list models: %v\n", err)
			}
		} else {
			ag.Model = arg
			ag.Sess.Model = arg
			fmt.Fprintf(a.stdout, "model set to %s\n", arg)
		}
	case "/update":
		if arg != "" {
			fmt.Fprintln(a.stdout, "usage: /update")
			break
		}
		if err := a.applyUpdate(ctx, true); err != nil {
			fmt.Fprintf(a.stderr, "update failed: %v\n", err)
		}
	default:
		fmt.Fprintln(a.stdout, "unknown command, /help for a list")
	}
	return false
}

func (a *app) setAutoApprove(ag *engine.Agent, enabled bool) {
	ag.Yolo = enabled
	if enabled {
		fmt.Fprintln(a.stdout, "auto-approve: on — tool actions will run without confirmation; this process only; start another with `kolk --yolo`")
		return
	}
	fmt.Fprintln(a.stdout, "auto-approve: off — tool actions will ask first")
}
