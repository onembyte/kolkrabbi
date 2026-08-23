package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// slash handles a /command typed in the REPL. It returns true when the REPL
// should exit.
func (a *app) slash(ag *engine.Agent, line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, cmd))

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		fmt.Fprint(a.stdout, `/mode <chat|code|agent>   switch mode (agent = orchestrated; code is the default)
/effort <quick|standard|deep|ultra>   select model tier and agent orchestration width
/model <id>    override the base model for this session
/rate <1-5>    rate the last turn (feeds the local dashboard)
/yolo          toggle auto-approve of tool actions
/new           start a fresh session (current one stays saved)
/session       show current session id and file
/changes       list files this session has modified
/rewind        undo the last turn's file changes (repeatable, files only)
/exit          quit
`)
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
		fmt.Fprintf(a.stdout, "yolo mode: %v\n", ag.Yolo)
	case "/model":
		if arg == "" {
			fmt.Fprintln(a.stdout, "usage: /model <model-id>")
		} else {
			ag.Model = arg
			ag.Sess.Model = arg
			fmt.Fprintf(a.stdout, "model set to %s\n", arg)
		}
	default:
		fmt.Fprintln(a.stdout, "unknown command, /help for a list")
	}
	return false
}
