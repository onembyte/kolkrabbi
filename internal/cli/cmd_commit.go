package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// maxDraftDiffBytes bounds what is sent to draft a message.
//
// A commit message is one or two sentences; a diff of a megabyte would cost far
// more than the message is worth and may not fit at all. The cap is generous
// enough that ordinary commits pass through whole.
const maxDraftDiffBytes = 24 * 1024

const commitDraftSystem = `You write git commit messages. Given a diff, reply with a message and nothing else: a subject line under 72 characters in the imperative mood, then a blank line, then a short body only if the change needs one. No markdown fences, no preamble, no explanation of what you are doing.`

// runCommitDraft reads the staged diff, drafts a message, and stops.
//
// **It does not commit**, and that is item 28's decision rather than an
// omission: a `/commit` that commits without a confirmation is a shell command
// wearing a costume, and `git commit` is already one keystroke away with the
// message this prints.
//
// It also does not stage. `git add -p` is a conversation, and quietly staging
// everything would surprise anyone who was staging deliberately — which is
// exactly the person who typed `/commit`.
func (a *app) runCommitDraft(ctx context.Context, ag *engine.Agent) {
	root := projectRoot()
	sh := shell.New()

	result, err := sh.Run(ctx, shell.Cmd{Command: "git diff --cached", Dir: root})
	if err != nil || !result.OK() || result.ExitCode != 0 {
		fmt.Fprintln(a.stdout, "could not read the staged diff — is this a git repository?")
		return
	}
	if strings.TrimSpace(result.Output) == "" {
		a.reportNothingStaged()
		return
	}

	draft, err := ag.FastLaneChat(ctx, commitDraftSystem, prepareDiffForDrafting(result.Output))
	if err != nil {
		fmt.Fprintf(a.stdout, "could not draft a message: %v\n", err)
		return
	}
	a.showCommitDraft(strings.TrimSpace(draft))
}

// reportNothingStaged answers the common mistake without calling it an error.
func (a *app) reportNothingStaged() {
	fmt.Fprintln(a.stdout, "nothing is staged, so there is nothing to describe.")
	fmt.Fprintln(a.stdout, "stage what you mean to commit first — `git add -p` for part of a file, `git add <path>` for all of one.")
}

// showCommitDraft prints the message and hands over.
func (a *app) showCommitDraft(message string) {
	fmt.Fprintf(a.stdout, "\n%s\n\n", message)
	fmt.Fprintln(a.stdout, "nothing was committed. to use this message:")
	fmt.Fprintln(a.stdout, "  git commit -F - <<'EOF'")
	fmt.Fprintf(a.stdout, "%s\n", message)
	fmt.Fprintln(a.stdout, "EOF")
}

// prepareDiffForDrafting makes a diff safe and affordable to send.
//
// Scrubbed first, because a diff is the single most likely thing to carry a
// secret into a prompt: it is the literal text of what changed, including the
// line that added a key. Then bounded, and the truncation **says so** — a model
// handed half a change with no notice will describe it as if it were the whole
// one, and the person reading the message would never know.
func prepareDiffForDrafting(diff string) string {
	safe := redact.Scrub(diff)
	if len(safe) <= maxDraftDiffBytes {
		return safe
	}
	return safe[:maxDraftDiffBytes] + "\n\n[diff truncated: only the first part is shown]"
}
