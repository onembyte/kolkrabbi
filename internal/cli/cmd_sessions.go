package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/session"
)

func (a *app) runSessions(_ context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}
	sdir := d.Sessions()

	if len(args) > 0 {
		switch args[0] {
		case "rm":
			if len(args) < 2 {
				return usagef("usage: kolk sessions rm <id>")
			}
			if err := session.Delete(sdir, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "deleted session %s\n", args[1])
			return nil
		case "search":
			if len(args) < 2 {
				return usagef("usage: kolk sessions search <text>")
			}
			return a.searchSessions(sdir, strings.Join(args[1:], " "))
		case "rename":
			if len(args) < 3 {
				return usagef("usage: kolk sessions rename <id> <title>")
			}
			return a.renameSession(sdir, args[1], strings.Join(args[2:], " "))
		case "fork":
			if len(args) < 2 {
				return usagef("usage: kolk sessions fork <id>")
			}
			return a.forkSession(sdir, args[1])
		case "export":
			if len(args) < 2 {
				return usagef("usage: kolk sessions export <id> [--json]")
			}
			asJSON := len(args) > 2 && args[2] == "--json"
			return a.exportSession(sdir, args[1], asJSON)
		case "clear":
			if err := session.Clear(sdir); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "all sessions deleted.")
			return nil
		default:
			return usagef("%s", usageLine("sessions"))
		}
	}

	all, err := session.List(sdir)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(a.stdout, "no sessions yet.")
		return nil
	}
	for _, s := range all {
		fmt.Fprintf(a.stdout, "%-22s %s  %-32s msgs:%-4d %s%s\n",
			s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.Model, len(s.Messages),
			snapshotSize(s), s.Title)
	}
	a.warnAboutSharedCheckouts(sdir)
	fmt.Fprintln(a.stdout, "\nresume the latest with `kolk -r`, or a specific one with `kolk -s <id>`")
	return nil
}

// searchSessions matches a phrase against titles and message content, which is
// how a session is actually remembered: by what was said in it, not by its id.
func (a *app) searchSessions(dir, phrase string) error {
	all, err := session.List(dir)
	if err != nil {
		return err
	}
	needle := strings.ToLower(strings.TrimSpace(phrase))
	matched := 0
	for _, candidate := range all {
		if !sessionMatches(candidate, needle) {
			continue
		}
		matched++
		fmt.Fprintf(a.stdout, "%-22s %s  %-32s msgs:%-4d %s\n",
			candidate.ID, candidate.UpdatedAt.Format("2006-01-02 15:04"),
			candidate.Model, len(candidate.Messages), candidate.Title)
	}
	if matched == 0 {
		fmt.Fprintf(a.stdout, "no session matches %q\n", phrase)
	}
	return nil
}

func sessionMatches(s *session.Session, needle string) bool {
	if strings.Contains(strings.ToLower(s.Title), needle) {
		return true
	}
	for _, message := range s.Messages {
		if strings.Contains(strings.ToLower(message.Content), needle) {
			return true
		}
	}
	return false
}

func (a *app) renameSession(dir, id, title string) error {
	loaded, err := loadSession(dir, id)
	if err != nil {
		return err
	}
	loaded.SetTitle(title)
	if err := loaded.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "renamed %s to %q\n", id, title)
	return nil
}

// forkSession copies a session's history into a new one. The original is never
// touched: forking exists precisely so an experiment cannot damage the history
// it started from.
func (a *app) forkSession(dir, id string) error {
	source, err := loadSession(dir, id)
	if err != nil {
		return err
	}
	fork := session.New(dir, source.Model)
	fork.Title = source.Title + " (fork)"
	fork.SetMessages(source.GetMessages())
	if err := fork.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "forked %s into %s\n", id, fork.ID)
	fmt.Fprintf(a.stdout, "resume it with: kolk -s %s\n", fork.ID)
	return nil
}

// exportSession writes a session out for a human or for another tool. Markdown
// elides tool bodies, which are the bulk of a coding session and the least
// readable part of it; --json is the stored record, unaltered.
func (a *app) exportSession(dir, id string, asJSON bool) error {
	loaded, err := loadSession(dir, id)
	if err != nil {
		return err
	}
	if asJSON {
		encoded, err := json.MarshalIndent(loaded, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s\n", encoded)
		return nil
	}
	fmt.Fprintf(a.stdout, "# %s\n\n", loaded.Title)
	fmt.Fprintf(a.stdout, "_%s · %s · %d messages_\n\n",
		loaded.ID, loaded.Model, len(loaded.Messages))
	for _, message := range loaded.Messages {
		switch {
		case message.Role == "tool":
			fmt.Fprintf(a.stdout, "> tool result elided (%d chars)\n\n", len(message.Content))
		case len(message.ToolCalls) > 0:
			names := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				names = append(names, call.Function.Name)
			}
			fmt.Fprintf(a.stdout, "**%s** ran: %s\n\n", message.Role, strings.Join(names, ", "))
		case strings.TrimSpace(message.Content) != "":
			fmt.Fprintf(a.stdout, "**%s**\n\n%s\n\n", message.Role, message.Content)
		}
	}
	return nil
}

// loadSession reads one session by id, reporting a missing one as the ordinary
// mistake it is rather than as a filesystem error with a path in it.
func loadSession(dir, id string) (*session.Session, error) {
	loaded, err := session.Load(dir, id)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no session %q; `kolk sessions` lists them", id)
	}
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// snapshotSize reports what this session's whole-tree snapshots are costing on
// disk, and reports nothing when there are none.
//
// A per-turn snapshot layer is the first thing to suspect when a data directory
// grows and the last thing anyone would guess, so it is worth a column. It is
// worth a column only when it exists: a row of "snap:0B" on every session
// teaches people to stop reading the line.
//
// One stat decides that, before any walking. Most sessions have no store, and a
// listing that walked a directory per session would be the second time a
// convenience made this command slow.
func snapshotSize(s *session.Session) string {
	store := filepath.Join(s.CkptDir(), "shadow.git")
	if info, err := os.Stat(store); err != nil || !info.IsDir() {
		return ""
	}
	var total int64
	err := filepath.WalkDir(store, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry costs its own size, not the total
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return "snap:" + humanBytes(total) + " "
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dKB", n/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// warnAboutSharedCheckouts says once when live sessions overlap.
//
// Item 27 does not refuse two terminals in one repository — people do that on
// purpose. What it refuses is silence: each session's `/undo` restores over the
// other's work, because a snapshot covers a whole tree, and that is a thing to
// be told rather than to discover.
//
// It is best-effort. A listing that failed because liveness could not be read
// would trade a useful command for a warning nobody asked for.
func (a *app) warnAboutSharedCheckouts(dir string) {
	cards, err := session.Overview(dir)
	if err != nil {
		return
	}
	for _, shared := range session.SharedCheckouts(cards) {
		fmt.Fprintf(a.stdout, "\n! %d live sessions are working in the same directory (%s): %s\n",
			len(shared.Sessions), shared.Dir, strings.Join(shared.Sessions, ", "))
		fmt.Fprintln(a.stdout, "  they will edit each other's files, and an /undo in one takes back what the other did.")
	}
}
