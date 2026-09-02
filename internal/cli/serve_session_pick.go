package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

// pickServedSession decides which conversation `kolk serve` hosts.
//
// Serving used to mint a fresh session id every time, so a client that
// attached found an empty conversation and the sessions already on the machine
// were unreachable through it. The server hosts a session, so which one is a
// question, and the answer belongs to the person starting it.
//
// The list is machine-wide on purpose, unlike `kolk sessions`: a client
// attaches to this server from somewhere else, and the folder the server
// happens to be started in is not the client's folder.
//
// Nothing here blocks a script. An explicit --session or --new answers the
// question up front, and when stdin is not a terminal the answer is a new
// session, which is what serving did before this existed.
func (a *app) pickServedSession(sessionsDir, explicit string, forceNew bool) (string, error) {
	if strings.TrimSpace(explicit) != "" && forceNew {
		return "", usagef("--session and --new both name what to serve; pass one")
	}
	if id := strings.TrimSpace(explicit); id != "" {
		if _, err := session.Load(sessionsDir, id); err != nil {
			return "", fmt.Errorf("no saved session %s to serve: %w", id, err)
		}
		fmt.Fprintf(a.stdout, "serving session %s\n", id)
		return id, nil
	}
	if forceNew {
		return a.newServedSession(), nil
	}

	saved, err := session.List(sessionsDir)
	if err != nil {
		return "", err
	}
	if len(saved) == 0 {
		fmt.Fprintln(a.stdout, "no saved sessions on this machine; serving a new one.")
		return a.newServedSession(), nil
	}
	// Newest first: the one you are most likely to want is the one you were
	// last in.
	sort.SliceStable(saved, func(i, j int) bool { return saved[i].UpdatedAt.After(saved[j].UpdatedAt) })

	if !a.canPromptForServedSession() {
		fmt.Fprintf(a.stdout, "%d saved sessions; stdin is not a terminal, so a new one is served.\n", len(saved))
		fmt.Fprintln(a.stdout, "name one with `kolk serve --session <id>`, or pass --new to say so explicitly.")
		return a.newServedSession(), nil
	}

	fmt.Fprintln(a.stdout, "which session should this server host?")
	for i, candidate := range saved {
		title := candidate.Title
		if strings.TrimSpace(title) == "" {
			title = "(untitled)"
		}
		where := candidate.CWD
		if strings.TrimSpace(where) == "" {
			where = "folder not recorded"
		}
		fmt.Fprintf(a.stdout, "  %2d. %-22s %s  %-28s %s\n",
			i+1, candidate.ID, candidate.UpdatedAt.Format("2006-01-02 15:04"), title, where)
	}
	fmt.Fprintf(a.stdout, "  %2d. new session\n", len(saved)+1)
	fmt.Fprintf(a.stdout, "choose [1-%d, default %d = new]: ", len(saved)+1, len(saved)+1)

	choice := strings.TrimSpace(a.readServeChoice())
	if choice == "" {
		return a.newServedSession(), nil
	}
	number, err := strconv.Atoi(choice)
	if err != nil || number < 1 || number > len(saved)+1 {
		// Refused rather than defaulted: serving the wrong conversation to a
		// client is not a mistake to make on someone's behalf.
		return "", fmt.Errorf("%q is not one of 1-%d", choice, len(saved)+1)
	}
	if number == len(saved)+1 {
		return a.newServedSession(), nil
	}
	chosen := saved[number-1]
	fmt.Fprintf(a.stdout, "serving session %s — %s\n", chosen.ID, chosen.Title)
	return chosen.ID, nil
}

func (a *app) newServedSession() string {
	id := xid.New(xid.Session)
	fmt.Fprintf(a.stdout, "serving a new session %s\n", id)
	return id
}

// canPromptForServedSession is false whenever asking would hang: no reader, or
// a stdin that is a pipe rather than a person.
func (a *app) canPromptForServedSession() bool {
	if a.in == nil {
		return false
	}
	if a.isStdinPiped != nil && a.isStdinPiped() {
		return false
	}
	return true
}

func (a *app) readServeChoice() string {
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return line
}
