package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/stats"
	"github.com/onembyte/kolkrabbi/internal/term"
	"github.com/onembyte/kolkrabbi/internal/tools"
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

	fmt.Fprintln(a.stdout, "\ntools")
	total, perTool := tools.SchemaCost()
	fmt.Fprintf(a.stdout, "  · %d tools, %s of schema on every request\n", len(perTool), humanBytes(int64(total)))

	fmt.Fprintln(a.stdout, "\nnetwork")
	a.doctorNetwork(ctx)

	fmt.Fprintln(a.stdout, "\nlocal models")
	a.doctorLocalModels(ctx)

	fmt.Fprintln(a.stdout, "\nsandbox")
	a.doctorSandbox()

	fmt.Fprintln(a.stdout, "\nlimits")
	a.doctorLimits()

	fmt.Fprintln(a.stdout, "\nsessions")
	a.doctorSessions()

	return nil
}

// doctorSessions checks the derived state beside the transcripts: the header
// every listing reads instead of decoding a session (OPTIMIZATION_PLAN.md O6),
// and the folded ratings startup reads instead of the whole usage log (O5).
//
// Both are caches — the transcript and the usage log are the truth — which is
// exactly why something has to prove they still agree. Doctor is that
// something, and it repairs rather than reports: a header that drifted is put
// back from the transcript it belongs to, which is also the one place an older
// session directory gets its headers written for the first time.
func (a *app) doctorSessions() {
	d, err := a.resolve()
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ could not locate the session directory: %v\n", err)
		return
	}
	all, err := session.List(d.Sessions())
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ sessions could not be listed: %v\n", err)
		return
	}
	repaired := 0
	for _, m := range all {
		if rewritten, err := session.RepairMeta(d.Sessions(), m.ID); err == nil && rewritten {
			repaired++
		}
	}
	switch {
	case len(all) == 0:
		fmt.Fprintln(a.stdout, "  · no sessions on this machine yet")
	case repaired == 0:
		fmt.Fprintf(a.stdout, "  ✓ %d sessions, every header matching its transcript\n", len(all))
	default:
		fmt.Fprintf(a.stdout, "  ✓ %d sessions; %d header(s) rebuilt from their transcript\n", len(all), repaired)
	}
	switch _, fold, err := stats.RatingsByModelFold(d.Data); {
	case err != nil:
		fmt.Fprintf(a.stdout, "  ✗ ratings could not be folded: %v\n", err)
	case fold == stats.FoldRebuilt:
		fmt.Fprintln(a.stdout, "  · ratings cache rebuilt from the usage log")
	default:
		fmt.Fprintln(a.stdout, "  ✓ ratings cache current")
	}
}

// doctorLimits lists the limits kolk remembers for this user -- a plan's window,
// an account out of credit -- with when each lifts. Doctor runs outside any
// session, so this is the user-wide file only; a session's own model and
// endpoint cooldowns are on its status line (V35.1d).
func (a *app) doctorLimits() {
	d, err := a.resolve()
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ %v\n", err)
		return
	}
	active := engine.OpenCooldowns("", d.CooldownsFile()).Active()
	paused := pausedSessions(d.Sessions())
	if len(active) == 0 && len(paused) == 0 {
		fmt.Fprintln(a.stdout, "  ✓ nothing is cooling; no remembered limit on any plan or account")
		return
	}
	for _, cd := range active {
		fmt.Fprintf(a.stdout, "  · %s (%s)\n", cd.Describe(), cd.Source)
	}
	for _, sess := range paused {
		fmt.Fprintf(a.stdout, "  · session %s %s; kolk resumes it by itself, or /resume inside it now\n", sess.ID, sess.Pause.Notice())
	}
}

// pausedSessions lists the sessions on disk whose pause has not yet lifted.
// A sessions directory that cannot be read lists nothing; the rest of /doctor
// still speaks.
//
// Read from the session headers, so a machine with hundreds of transcripts
// answers this without decoding one of them (OPTIMIZATION_PLAN.md O6).
func pausedSessions(dir string) []session.Meta {
	all, err := session.List(dir)
	if err != nil {
		return nil
	}
	var paused []session.Meta
	for _, sess := range all {
		if p := sess.Pause; p != nil && p.ResetAt.After(time.Now()) {
			paused = append(paused, sess)
		}
	}
	return paused
}

// doctorSandbox reports what would enforce a sandbox here and whether a
// network deny could be kept. Machine facts only: the session state is what
// /sandbox prints, and a pasted report should explain why /sandbox on did or
// did not take on this machine.
func (a *app) doctorSandbox() {
	r := shell.Report()
	if r.Err != nil {
		fmt.Fprintf(a.stdout, "  ✗ no sandbox mechanism: %v\n", r.Err)
	} else {
		fmt.Fprintf(a.stdout, "  ✓ %s — writes confined to the project and temp when on\n", r.Mechanism)
	}
	if r.NetworkDenyEnforced {
		fmt.Fprintln(a.stdout, "  ✓ network deny enforceable")
	} else {
		fmt.Fprintf(a.stdout, "  ✗ network deny not enforceable: %s\n", r.NetworkDenyReason)
	}
	fmt.Fprintln(a.stdout, "  · off by default; /sandbox on for this session, /config set sandbox on to persist")
}

// doctorLocalModels reports the user's own Ollama: running and adopted,
// installed but idle, or absent with the one line that installs it. The line
// is named and not run — the Linux installer needs sudo and pipes curl into
// sh, both of which kolk's own hardline refuses.
func (a *app) doctorLocalModels(ctx context.Context) {
	// Guarded like every other reader of this field (run.go, tui_repl.go,
	// cmd_localia.go): host discovery is injected, nil is a supported state,
	// and this was the one call site that assumed otherwise — so /doctor took
	// the whole session down with a nil dereference wherever it was not wired.
	if a.discoverHost == nil {
		fmt.Fprintln(a.stdout, "  · host discovery is not wired in this session")
		return
	}
	host := a.discoverHost(ctx)
	switch host.State {
	case local.HostRunning:
		count := ""
		if models, err := a.listHostModels(ctx, host.Addr, ""); err == nil {
			count = fmt.Sprintf(", %d model(s)", len(models))
		}
		fmt.Fprintf(a.stdout, "  ✓ ollama %s running at %s%s — kolk uses it and never stops it\n", host.Version, host.Addr, count)
	case local.HostInstalled:
		fmt.Fprintf(a.stdout, "  · ollama at %s, not running\n", host.Binary)
	case local.HostAbsent:
		fmt.Fprintln(a.stdout, "  ✗ ollama is not installed")
		fmt.Fprintf(a.stdout, "  · install it with: %s\n", host.InstallHint())
	}
	a.doctorEndpoints(ctx)
}

// doctorEndpoints reports every local endpoint the user has added and
// whether it answers now — the question someone asks doctor precisely when
// a machine that used to be there has gone.
func (a *app) doctorEndpoints(ctx context.Context) {
	dirs, err := a.resolve()
	if err != nil {
		return
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil || len(cfg.Local.Endpoints) == 0 {
		return
	}
	for _, endpoint := range cfg.Local.Endpoints {
		runtime, err := a.identifyEndpoint(ctx, endpoint.Addr)
		if err != nil {
			fmt.Fprintf(a.stdout, "  ✗ %s at %s does not answer: %v\n", endpoint.Name, endpoint.Addr, err)
			continue
		}
		fmt.Fprintf(a.stdout, "  ✓ %s at %s (%s, %d model(s)) — models are %s/<model>\n",
			endpoint.Name, endpoint.Addr, runtime.Kind, len(runtime.Models), endpoint.Name)
	}
}

func (a *app) doctorKeys(ctx context.Context) {
	d, err := a.resolve()
	if err != nil {
		fmt.Fprintf(a.stdout, "  ✗ %v\n", err)
		return
	}
	// The chain, not a second copy of it: what /doctor says is what kolk uses.
	res, err := keystore.Resolve(ctx, keystore.Ref{Provider: "openrouter", Profile: "default"}, os.Getenv, keystore.NewFileStore(d.CredentialsFile()))
	switch {
	case err == nil:
		fmt.Fprintf(a.stdout, "  ✓ openrouter  %s  from %s  (kolk key --why shows the chain)\n", redact.Mask(res.Value.Reveal()), res.Source)
	case errors.Is(err, keystore.ErrNotFound):
		fmt.Fprintln(a.stdout, "  ✗ openrouter  no key found — add one with `/key` (it asks for the key, hidden)")
	default:
		fmt.Fprintf(a.stdout, "  ✗ openrouter  %v\n", err)
		if advice := keyStoreAdvice(err); advice != "" {
			fmt.Fprintf(a.stdout, "    %s\n", advice)
		}
	}
	a.doctorVendorKeys(ctx)
}

// doctorVendorKeys names the owner-chosen vendor keys kolk can see — env first,
// then the store — and says nothing about a vendor with none, since a vendor
// key is optional where the gateway's is not.
func (a *app) doctorVendorKeys(ctx context.Context) {
	d, err := a.resolve()
	if err != nil {
		return
	}
	for _, vendor := range provider.KeyedVendors() {
		env := provider.VendorKeyEnv(vendor)
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			fmt.Fprintf(a.stdout, "  ✓ %-11s %s  from %s\n", vendor, redact.Mask(value), env)
			continue
		}
		if cred, err := resolveVendorCredential(ctx, vendor, filepath.Join(d.Config, "keys.json")); err == nil && cred.Reveal() != "" {
			fmt.Fprintf(a.stdout, "  ✓ %-11s %s  from the key store\n", vendor, redact.Mask(cred.Reveal()))
		}
	}
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
	if err := provider.RefuseCredentialedEndpoint(base); err != nil {
		fmt.Fprintf(a.stdout, "  ✗ refused: %v\n", err)
		return
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
