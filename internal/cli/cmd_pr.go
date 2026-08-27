package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// maxDraftLogBytes bounds the branch log sent for drafting. A PR description is
// a paragraph; a thousand commit subjects would cost more than it is worth.
const maxDraftLogBytes = 16 * 1024

const pullRequestDraftSystem = `You write pull request descriptions. Given a list of commit subjects, reply with a title on the first line, a blank line, then a short body of one or two paragraphs saying what changed and why. No markdown fences, no preamble, no bullet list of the commits themselves.`

// runPullRequestDraft drafts a title and body from the branch's commits and
// hands over `gh pr create`.
//
// It does not open anything. Drafting is where the model helps; running it is a
// confirmation like any other, and the same reasoning that keeps `/commit` from
// committing keeps this from opening a pull request nobody read.
//
// GitHub only, through `gh`. Bitbucket and Azure DevOps are refused in writing
// (item 28): each is another REST client, auth flow and set of fixtures for a
// user this project does not have, and anyone who wants them can run their own
// CLI through `bash` — which is exactly what Kolkrabbi would be doing for them.
func (a *app) runPullRequestDraft(ctx context.Context, ag *engine.Agent) {
	root := projectRoot()
	sh := shell.New()

	run := func(command string) (string, bool) {
		result, err := sh.Run(ctx, shell.Cmd{Command: command, Dir: root})
		if err != nil || !result.OK() || result.ExitCode != 0 {
			return "", false
		}
		return strings.TrimSpace(result.Output), true
	}

	if _, ok := run("command -v gh"); !ok {
		a.reportMissingGh()
		return
	}
	branch, ok := run("git rev-parse --abbrev-ref HEAD")
	if !ok || branch == "" || branch == "HEAD" {
		fmt.Fprintln(a.stdout, "not on a branch — a pull request needs one.")
		return
	}
	if _, ok := run("git rev-parse --abbrev-ref --symbolic-full-name @{upstream}"); !ok {
		a.reportNoUpstream(branch)
		return
	}

	base := defaultBase(run)
	log, ok := run("git log --no-merges --format=%h %s " + shell.Quote(base+"..HEAD"))
	if !ok {
		fmt.Fprintln(a.stdout, "could not read this branch's commits — is this a git repository?")
		return
	}
	if strings.TrimSpace(log) == "" {
		a.reportNothingToPropose(base)
		return
	}

	draft, err := ag.FastLaneChat(ctx, pullRequestDraftSystem, prepareLogForDrafting(log))
	if err != nil {
		fmt.Fprintf(a.stdout, "could not draft a description: %v\n", err)
		return
	}
	title, body, _ := strings.Cut(strings.TrimSpace(draft), "\n")
	a.showPullRequestDraft(base, strings.TrimSpace(title), strings.TrimSpace(body))
}

// defaultBase asks the remote what it considers the default branch, and falls
// back to main. Guessing "master" on a repository that uses "main" produces a
// diff of the entire history, which is a confusing way to fail.
func defaultBase(run func(string) (string, bool)) string {
	if head, ok := run("git symbolic-ref --quiet refs/remotes/origin/HEAD"); ok {
		if _, name, found := strings.Cut(head, "refs/remotes/origin/"); found && name != "" {
			return name
		}
	}
	return "main"
}

func (a *app) reportMissingGh() {
	fmt.Fprintln(a.stdout, "`gh` is not on this machine, and pull requests go through it.")
	fmt.Fprintln(a.stdout, "install it from cli.github.com, or open the pull request in a browser.")
}

func (a *app) reportNoUpstream(branch string) {
	fmt.Fprintf(a.stdout, "%s has never been pushed, so there is nothing to open a pull request against.\n", branch)
	fmt.Fprintf(a.stdout, "  git push -u origin %s\n", branch)
}

func (a *app) reportNothingToPropose(base string) {
	fmt.Fprintf(a.stdout, "this branch has no commits that %s does not — nothing to propose yet.\n", base)
}

// showPullRequestDraft prints the draft and the command that would use it.
func (a *app) showPullRequestDraft(base, title, body string) {
	fmt.Fprintf(a.stdout, "\n%s\n\n%s\n\n", title, body)
	fmt.Fprintln(a.stdout, "nothing was opened. to use this draft:")
	fmt.Fprintf(a.stdout, "  gh pr create --base %s --title %s --body %s\n",
		base, shell.Quote(title), shell.Quote(body))
}

// prepareLogForDrafting makes a branch log safe and affordable to send.
//
// Scrubbed first: a commit subject can name a key as readily as a diff line
// can. Truncated visibly second, so a model handed part of a branch does not
// describe it as the whole of one.
func prepareLogForDrafting(log string) string {
	safe := redact.Scrub(log)
	if len(safe) <= maxDraftLogBytes {
		return safe
	}
	return safe[:maxDraftLogBytes] + "\n\n[log truncated: only the most recent commits are shown]"
}
