// Package hooks runs a shell command at a named moment.
//
// The permission story is the design, not a wrapper around it. Item 15 sent
// formatter-after-edit here rather than building it there for exactly this
// reason: a formatter that runs silently after every edit is a shell command
// executing with nobody at the prompt.
package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// Event is a moment where something already happened.
type Event string

const (
	// PostEdit fires after an edit_file that was applied.
	PostEdit Event = "post-edit"
	// PostWrite fires after a write_file that was applied.
	PostWrite Event = "post-write"
	// SessionEnd fires once, as a session closes.
	SessionEnd Event = "session-end"
)

// Events is the whole vocabulary.
//
// **There is no `pre-tool` hook**, and that is a decision rather than an
// omission: a hook that can veto a tool call is a second permission system, and
// E13 exists so there is exactly one. Every event here names something that has
// already happened, so a hook can react and cannot arbitrate.
func Events() []Event { return []Event{PostEdit, PostWrite, SessionEnd} }

// Config is what a hooks file declares.
type Config struct {
	PostEdit   []string `json:"post-edit,omitempty"`
	PostWrite  []string `json:"post-write,omitempty"`
	SessionEnd []string `json:"session-end,omitempty"`
}

func (c Config) commandsFor(event Event) []string {
	switch event {
	case PostEdit:
		return c.PostEdit
	case PostWrite:
		return c.PostWrite
	case SessionEnd:
		return c.SessionEnd
	default:
		return nil
	}
}

// Result is what one hook did. It is reported and never returned as an error,
// because a hook cannot fail a turn.
type Result struct {
	Command string
	Output  string
	Failure string
}

// Runner runs the hooks for one session.
type Runner struct {
	Shell   shell.Shell
	Config  Config
	Session string
	Timeout time.Duration

	// Allowed is the floor. A hook is judged by `hardline` like any other
	// command — no sudo, no credential paths, no piping a download into a
	// shell — and a refusal here is never offered to the user as a choice.
	Allowed func(command string) bool

	// Confirm asks about one command, once.
	Confirm func(command string) bool

	// answered remembers this session's decisions, keyed by the command itself.
	// Once per *distinct command*, not once per event or once per file: the
	// same shape as a permission rule, because it is one.
	answered map[string]bool
}

// Run fires every hook for an event and returns what each one did.
//
// It never returns an error. A formatter that is not installed must not fail
// the edit that already happened — the whole point of these events being
// *post* is that the work is done before a hook is consulted, so a broken hook
// can cost its own output and nothing else.
func (r *Runner) Run(ctx context.Context, event Event, file string) []Result {
	commands := r.Config.commandsFor(event)
	if len(commands) == 0 {
		return nil
	}
	if r.answered == nil {
		r.answered = map[string]bool{}
	}

	var results []Result
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if r.Allowed != nil && !r.Allowed(command) {
			// Not offered as a question: the floor is not a thing a prompt can
			// lift, so asking would imply it could.
			results = append(results, Result{Command: command, Failure: "refused by the permission floor"})
			continue
		}
		allowed, decided := r.answered[command]
		if !decided {
			allowed = r.Confirm == nil || r.Confirm(command)
			// A decline is remembered too. Being asked again on every edit is
			// how a person ends up saying yes to make it stop.
			r.answered[command] = allowed
		}
		if !allowed {
			continue
		}
		results = append(results, r.run(ctx, command, file))
	}
	return results
}

func (r *Runner) run(ctx context.Context, command, file string) Result {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	result, err := r.Shell.Run(ctx, shell.Cmd{
		Command: command,
		Timeout: timeout,
		// Two variables and nothing else. Not the user's whole environment,
		// and never a credential: a hook is somebody's one-line script, and the
		// blast radius of handing it everything is the same as the blast radius
		// of that script being wrong.
		Env: []string{
			"KOLK_FILE=" + file,
			"KOLK_SESSION=" + r.Session,
		},
	})
	if err != nil {
		return Result{Command: command, Failure: fmt.Sprintf("could not run: %v", err)}
	}
	// Scrubbed like any tool result: a hook prints whatever it prints, and a
	// key in that output would otherwise reach a transcript.
	return Result{
		Command: command,
		Output:  redact.Scrub(strings.TrimSpace(result.Output)),
		Failure: redact.Scrub(result.Failure),
	}
}
