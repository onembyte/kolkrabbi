package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/term"
)

// runDoctor reports what kolk can see of the machine it is running on.
//
// The rule it follows everywhere: it prints what it found, never what it found
// *with*. A diagnostic exists to be pasted into a bug report, so a key appears
// as the last four characters `kolk key` already shows and a directory appears
// with the home path collapsed to `~`. Anything else would make the useful
// thing — sharing it — the dangerous thing.
//
// It also never fails the command. Someone runs `kolk doctor` because something
// is already wrong; exiting at the first failed check would hide the rest of
// the report exactly when it is wanted.
func (a *app) runDoctor(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return usagef("%s", usageLine("doctor"))
	}

	fmt.Fprintf(a.stdout, "kolk %s\n\n", buildinfo.Get().Version)

	home, _ := paths.UserHomeDir()

	fmt.Fprintln(a.stdout, "keys")
	a.doctorKeys(ctx)

	fmt.Fprintln(a.stdout, "\ndirectories")
	d, err := a.resolve()
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ could not locate them: %v\n", err)
	} else {
		for _, dir := range []struct {
			label string
			path  string
		}{
			{"config", d.Config},
			{"data", d.Data},
			{"cache", d.Cache},
		} {
			fmt.Fprintf(a.stdout, "  %s %-7s %s\n",
				tick(writable(dir.path)), dir.label, compactWorkingFolder(dir.path, home))
		}
	}

	fmt.Fprintln(a.stdout, "\nterminal")
	a.doctorTerminal()

	fmt.Fprintln(a.stdout, "\nnetwork")
	a.doctorNetwork(ctx)

	return nil
}

func (a *app) doctorKeys(ctx context.Context) {
	if env := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); env != "" {
		fmt.Fprintf(a.stdout, "  ✓ openrouter  %s  from OPENROUTER_API_KEY\n", redact.Mask(env))
		return
	}
	d, err := a.resolve()
	if err == nil {
		if cred, err := resolveOpenRouterCredential(ctx, filepath.Join(d.Config, "keys.json")); err == nil && cred.Reveal() != "" {
			fmt.Fprintf(a.stdout, "  ✓ openrouter  %s  from the key store\n", redact.Mask(cred.Reveal()))
			return
		}
	}
	fmt.Fprintln(a.stdout, "  ✗ openrouter  no key found — add one with `kolk key <API_KEY>`")
}

func (a *app) doctorTerminal() {
	stdout, isFile := a.stdout.(*os.File)
	if !isFile {
		fmt.Fprintln(a.stdout, "  · output is not a terminal (piped or captured)")
		return
	}
	// These are facts, not verdicts. A piped kolk is not a broken kolk, and a
	// ✗ beside "interactive terminal" sends a person looking for a fault that
	// is not there — which is the specific way a diagnostic wastes someone's
	// evening.
	interactive := term.IsTerminal(stdout)
	if interactive {
		fmt.Fprintln(a.stdout, "  · interactive terminal")
	} else {
		fmt.Fprintln(a.stdout, "  · output is redirected, not a terminal")
	}
	width, height := term.Size(stdout)
	fmt.Fprintf(a.stdout, "  · %d columns × %d rows\n", width, height)
	switch {
	case !interactive:
		fmt.Fprintf(a.stdout, "  · colour off while redirected (TERM=%s)\n", os.Getenv("TERM"))
	case os.Getenv("NO_COLOR") != "":
		fmt.Fprintln(a.stdout, "  · colour off because NO_COLOR is set")
	default:
		fmt.Fprintf(a.stdout, "  · colour on (TERM=%s)\n", os.Getenv("TERM"))
	}
}

// doctorNetwork asks whether OpenRouter answers at all. It deliberately does
// not spend a turn: "can this machine reach the provider" is the question, and
// a model call would answer a different one at a price.
func (a *app) doctorNetwork(ctx context.Context) {
	base := strings.TrimSpace(os.Getenv("OPENROUTER_BASE_URL"))
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	// Short, because someone with no network is the expected caller and a
	// diagnostic that hangs is worse than one that says "unreachable".
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimSuffix(base, "/")+"/models", nil)
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ %s is not a usable address\n", base)
		return
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ %s unreachable\n", base)
		fmt.Fprintln(a.stdout, "  · a proxy, a VPN, or a --base-url pointing at something that is not running are the usual causes")
		return
	}
	defer func() { _ = response.Body.Close() }()
	fmt.Fprintf(a.stdout, "  %s %s answered HTTP %d\n", tick(response.StatusCode < 500), base, response.StatusCode)
}

func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".kolk-doctor-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

func tick(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}
