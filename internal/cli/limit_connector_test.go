package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// limitBackend fails every turn with one classified limit.
type limitBackend struct{ limit provider.Limit }

func (b limitBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, b.limit
}

// The engine keys a limit to the connector its model runs through. The CLI
// says which that is: a vendor's own model belongs to its CLI connector, and
// anything else, a gateway id included, is left to the keyed endpoint.
func TestTheAgentKnowsWhichConnectorAModelRunsThrough(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	for model, want := range map[string]string{
		"claude-opus":         "claude",
		"gpt-5.6-luna":        "codex",
		"openai/gpt-5.6-luna": "",
		"anthropic/claude-3":  "",
		"vendor/paid":         "",
		"":                    "",
	} {
		if got := a.modelConnector(model); got != want {
			t.Errorf("modelConnector(%q) = %q, want %q", model, got, want)
		}
	}
}

// For the session's own model the session answers, "" included; any other
// model, a child's, is named by the vendor catalogues.
func TestTheSessionNamesItsOwnModelsConnector(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	for _, c := range []struct {
		model, connector string
		ask              map[string]string
	}{
		{"auto", "copilot", map[string]string{"auto": "copilot", "claude-opus": "claude", "gpt-5.6-luna": "codex"}},
		{"claude-sonnet", "copilot", map[string]string{"claude-sonnet": "copilot", "claude-opus": "claude"}},
		{"claude-opus", "", map[string]string{"claude-opus": "", "gpt-5.6-luna": "codex"}},
		{"claude-opus", "claude", map[string]string{"CLAUDE-OPUS ": "claude", "vendor/model": ""}},
	} {
		sess := session.New(t.TempDir(), c.model)
		sess.SetConnector(c.connector)
		for model, want := range c.ask {
			if got := a.connectorIn(sess, model); got != want {
				t.Errorf("session %s on %q: %q runs through %q, want %q", c.model, c.connector, model, got, want)
			}
		}
	}
	if got := a.connectorIn(nil, "claude-opus"); got != "claude" {
		t.Errorf("no session: %q", got)
	}
}

// A limit met on a Claude session cools the Claude connector, for as long as
// the vendor said, and the pause records it there. Left unwired, the engine
// keyed it to the gateway: the gateway stayed cooling for days while the
// exhausted plan was offered as the way on.
func TestAPlanLimitCoolsTheConnectorItCameThrough(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	stored := session.New(dirs.Sessions(), "claude-opus")
	stored.SetConnector("claude")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: stored.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	if ag.ConnectorName == nil {
		t.Fatal("the agent was built without ConnectorName")
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the Claude session's", ag.Backend)
	}
	resets := time.Now().Add(96 * time.Hour).Truncate(time.Second)
	// No connector named: the engine has to ask the CLI.
	wrapped.inner = limitBackend{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
		ResetAt: resets, Message: "usage limit reached", Source: "phrase"}}
	ag.OnSubscriptionLimit = engine.OnLimitStop

	var paused *engine.PausedError
	if err := ag.RunTurn(context.Background(), "keep going"); !errors.As(err, &paused) {
		t.Fatalf("did not pause: %v", err)
	}
	if paused.Pause.Connector != "claude" {
		t.Fatalf("pause connector = %q, want claude", paused.Pause.Connector)
	}
	active := ag.Cooldowns.Active()
	if len(active) == 0 {
		t.Fatal("the limit left no cooldown")
	}
	for _, cd := range active {
		if cd.Connector != "claude" || !cd.Until.Equal(resets) {
			t.Errorf("cooldown %s until %v, want claude's, until the vendor's %v", cd.Key, cd.Until, resets)
		}
	}
}

// autoResume watches ag's pause the way a session does, with the wait cut to
// nothing once. It reports each sign-in the probe asks for, the turn it hands
// back, and whether the monitor re-armed instead (a second wait).
type autoResume struct {
	signIns   chan string
	delivered chan string
	rearmed   chan struct{}
	waits     atomic.Int32
}

func watchAutoResume(t *testing.T, ctx context.Context, ag *engine.Agent, signedIn func(string) bool) *autoResume {
	t.Helper()
	w := &autoResume{signIns: make(chan string, 8), delivered: make(chan string, 1), rearmed: make(chan struct{}, 1)}
	ag.HandoverSignedIn = func(name string) bool {
		ok := signedIn(name)
		w.signIns <- fmt.Sprintf("%s=%v", name, ok)
		return ok
	}
	ag.ResumeWait = func(ctx context.Context, _ time.Duration) error {
		if w.waits.Add(1) == 1 {
			return nil
		}
		select {
		case w.rearmed <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ag.ResumeReady = func(_ context.Context, pending string) bool { w.delivered <- pending; return true }
	t.Cleanup(ag.WatchPauses(ctx))
	return w
}

// outcome waits for the monitor's decision: the turn handed back, or a
// re-arm. It returns the handed-back turn ("" for a re-arm) and every
// sign-in asked on the way.
func (w *autoResume) outcome(t *testing.T) (string, []string) {
	t.Helper()
	var asked []string
	deadline := time.After(10 * time.Second)
	for {
		select {
		case pending := <-w.delivered:
			return pending, append(asked, drain(w.signIns)...)
		case name := <-w.signIns:
			asked = append(asked, name)
		case <-w.rearmed:
			return "", append(asked, drain(w.signIns)...)
		case <-deadline:
			t.Fatal("the monitor neither resumed nor re-armed")
		}
	}
}

func drain(c chan string) []string {
	var out []string
	for {
		select {
		case s := <-c:
			out = append(out, s)
		default:
			return out
		}
	}
}

// A pause on a gateway or compatible endpoint is lifted by asking that
// endpoint. It is never a handover: a sign-in check for "openrouter" answers
// no forever, and the session would wait for /resume at every interval.
func TestAGatewayPauseIsProbedAtItsEndpoint(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusPaymentRequired} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			dirs := storeFirstRunKey(t)
			if err := dirs.EnsureData(); err != nil {
				t.Fatal(err)
			}
			var lists atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/models") {
					lists.Add(1)
					_, _ = io.WriteString(w, `{"data":[{"id":"vendor/model"}]}`)
					return
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"busy or out of credit"}}`)
			}))
			defer srv.Close()
			a, _, _ := newTestApp(t, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ag, err := a.newAgent(ctx, &options{model: "vendor/model"})
			if err != nil {
				t.Fatal(err)
			}
			defer ag.Close()
			client := provider.NewCompatibleClient(srv.URL)
			ag.Client, ag.Backend = client, client
			ag.OnSubscriptionLimit = engine.OnLimitStop
			w := watchAutoResume(t, ctx, ag, a.connectorSignedIn)

			var paused *engine.PausedError
			if err := ag.RunTurn(ctx, "hello"); !errors.As(err, &paused) {
				t.Fatalf("did not pause: %v", err)
			}
			pending, asked := w.outcome(t)
			if pending != "hello" {
				t.Fatalf("the endpoint answers again, yet the turn was not resumed (sign-ins asked %v, /models asked %d times)", asked, lists.Load())
			}
			if len(asked) != 0 || lists.Load() == 0 {
				t.Fatalf("a gateway pause asked sign-ins %v and /models %d times; want the endpoint only", asked, lists.Load())
			}
		})
	}
}

// A pause on a vendor CLI is lifted by that connector's sign-in, the check it
// already has: signed in, the waiting turn comes back; signed out, it waits.
func TestAHandoverPauseIsProbedThroughItsSignIn(t *testing.T) {
	for _, signedIn := range []bool{true, false} {
		t.Run(fmt.Sprintf("signed in %v", signedIn), func(t *testing.T) {
			dirs := storeFirstRunKey(t)
			if err := dirs.EnsureData(); err != nil {
				t.Fatal(err)
			}
			enablePlanConnector(t, dirs)
			stored := session.New(dirs.Sessions(), "claude-opus")
			stored.SetConnector("claude")
			if err := stored.Save(); err != nil {
				t.Fatal(err)
			}
			a, _, _ := newTestApp(t, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ag, err := a.newAgent(ctx, &options{session: stored.ID})
			if err != nil {
				t.Fatal(err)
			}
			defer ag.Close()
			ag.Backend.(*verifyingBackend).inner = limitBackend{provider.Limit{Kind: provider.LimitSubscriptionAllowance,
				Scope: provider.ScopeAccount, Connector: "claude", ResetAt: time.Now().Add(time.Hour), Message: "usage limit reached", Source: "vendor-frame"}}
			ag.OnSubscriptionLimit = engine.OnLimitStop
			w := watchAutoResume(t, ctx, ag, func(name string) bool { return signedIn && a.connectorSignedIn(name) })

			var paused *engine.PausedError
			if err := ag.RunTurn(ctx, "keep going"); !errors.As(err, &paused) {
				t.Fatalf("did not pause: %v", err)
			}
			pending, asked := w.outcome(t)
			if len(asked) == 0 || !strings.HasPrefix(asked[0], "claude=") {
				t.Fatalf("sign-ins asked %v; want claude's", asked)
			}
			if want := map[bool]string{true: "keep going", false: ""}[signedIn]; pending != want {
				t.Fatalf("handed back %q, want %q", pending, want)
			}
		})
	}
}

// A Copilot session names its connector itself: its model ("auto") is no
// vendor's catalogue row, and a Claude or GPT model offered through Copilot
// belongs to Copilot's plan, not to the vendor's own CLI.
func TestACopilotSessionKeysItsLimitToCopilot(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "github", Plan: "Copilot Pro", Name: "copilot", LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	stored := session.New(dirs.Sessions(), "auto")
	stored.SetConnector("copilot")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ag, err := a.newAgent(ctx, &options{session: stored.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer ag.Close()
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want Copilot's", ag.Backend)
	}
	wrapped.inner = limitBackend{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
		Message: "monthly quota exceeded", Source: "phrase"}}
	ag.OnSubscriptionLimit = engine.OnLimitStop
	w := watchAutoResume(t, ctx, ag, a.connectorSignedIn)

	var paused *engine.PausedError
	if err := ag.RunTurn(ctx, "go"); !errors.As(err, &paused) {
		t.Fatalf("did not pause: %v", err)
	}
	if paused.Pause.Connector != "copilot" {
		t.Fatalf("pause connector = %q, want copilot", paused.Pause.Connector)
	}
	for _, cd := range ag.Cooldowns.Active() {
		if cd.Connector != "copilot" {
			t.Errorf("cooldown %s, want copilot's", cd.Key)
		}
	}
	if pending, asked := w.outcome(t); pending != "go" || len(asked) == 0 || asked[0] != "copilot=true" {
		t.Fatalf("handed back %q after sign-ins %v; want copilot's sign-in, then the turn", pending, asked)
	}
}

// /new and /clear replace the session and keep the backend, so the new
// session records the same connector, and the connector the agent names is
// always the current session's: never the one it started with.
func TestNewKeepsTheSessionsConnectorCurrent(t *testing.T) {
	t.Run("a plan session", func(t *testing.T) {
		dirs := storeFirstRunKey(t)
		if err := dirs.EnsureData(); err != nil {
			t.Fatal(err)
		}
		enablePlanConnector(t, dirs)
		stored := session.New(dirs.Sessions(), "claude-opus")
		stored.SetConnector("claude")
		if err := stored.Save(); err != nil {
			t.Fatal(err)
		}
		a, _, _ := newTestApp(t, "")
		ag, err := a.newAgent(context.Background(), &options{session: stored.ID})
		if err != nil {
			t.Fatal(err)
		}
		defer ag.Close()
		lane := ag.FastLaneModel()
		if quit := a.slash(context.Background(), ag, "/new"); quit {
			t.Fatal("/new ended the session")
		}
		if ag.Sess.SessionID() == stored.ID {
			t.Fatal("/new kept the old session")
		}
		if got := ag.Sess.ConnectorName(); got != "claude" {
			t.Fatalf("the new session's connector = %q, want claude: the backend is still Claude", got)
		}
		if got := ag.ConnectorName(ag.SessionModel()); got != "claude" {
			t.Fatalf("ConnectorName = %q, want claude", got)
		}
		// With the connector lost, the fast lane read the session as a gateway
		// one and chose a free gateway model for the Claude backend.
		if got := ag.FastLaneModel(); got != lane {
			t.Fatalf("fast lane %q after /new, %q before", got, lane)
		}
	})
	t.Run("a switch after /new", func(t *testing.T) {
		dirs := storeFirstRunKey(t)
		if err := dirs.EnsureData(); err != nil {
			t.Fatal(err)
		}
		if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
			Provider: "github", Plan: "Copilot Pro", Name: "copilot", LoginOwner: "provider-cli", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		a, _, _ := newTestApp(t, "")
		ctx := context.Background()
		ag, err := a.newAgent(ctx, &options{model: "vendor/model"})
		if err != nil {
			t.Fatal(err)
		}
		defer ag.Close()
		if quit := a.slash(ctx, ag, "/new"); quit {
			t.Fatal("/new ended the session")
		}
		if _, err := a.switchModel(ctx, ag, "auto"); err != nil {
			t.Fatal(err)
		}
		if got := ag.ConnectorName("auto"); got != "copilot" {
			t.Fatalf("after /new and /model auto, ConnectorName = %q, want copilot", got)
		}
		wrapped, ok := ag.Backend.(*verifyingBackend)
		if !ok {
			t.Fatalf("backend = %T, want Copilot's", ag.Backend)
		}
		wrapped.inner = limitBackend{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
			Message: "monthly quota exceeded", Source: "phrase"}}
		ag.OnSubscriptionLimit = engine.OnLimitStop
		var paused *engine.PausedError
		if err := ag.RunTurn(ctx, "go"); !errors.As(err, &paused) {
			t.Fatalf("did not pause: %v", err)
		}
		if paused.Pause.Connector != "copilot" {
			t.Fatalf("pause connector = %q, want copilot", paused.Pause.Connector)
		}
	})
}

// /new ends the session, and with it every rule scoped to the session: the
// ones added with /permissions for this session and the ones kept at a
// prompt. Rules stored for good carry on. A rule that outlives the session
// someone scoped it to is a rule nobody consented to.
func TestNewEndsTheRulesKeptForTheSession(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	defer ag.Close()
	if err := a.updateStoredRules(func(stored *config.Permissions) { stored.Add("allow bash(git status)", "") }); err != nil {
		t.Fatal(err)
	}
	a.sessionRules = []string{"allow bash(ls *)"}
	a.applyRules(ag)
	kept, err := engine.ParseRule("allow bash(make *)") // what a prompt's "keep for this session" adds
	if err != nil {
		t.Fatal(err)
	}
	ag.Rules = append(ag.Rules, kept)
	if quit := a.slash(context.Background(), ag, "/new"); quit {
		t.Fatal("/new ended the process")
	}
	var sources []string
	for _, rule := range ag.Rules {
		sources = append(sources, rule.Source)
	}
	if len(sources) != 1 || sources[0] != "allow bash(git status)" {
		t.Fatalf("rules after /new = %q, want only the stored one", sources)
	}
	if len(a.sessionRules) != 0 {
		t.Fatalf("session rules survived /new: %q", a.sessionRules)
	}
}

// Plan mode is a mode, and /new keeps the mode: its refusals stay with its
// instruction, and /new says so. Only the rules someone scoped to the session
// themselves end. Dropping plan mode's refusals while its instruction stayed
// left a session that said "read only" and wrote files.
func TestNewKeepsPlanMode(t *testing.T) {
	a, ag, out := replFixture(t, "")
	defer ag.Close()
	ctx := context.Background()
	a.slash(ctx, ag, "/plan")
	a.sessionRules = append(a.sessionRules, "allow bash(ls *)")
	a.applyRules(ag)
	out.Reset()
	a.slash(ctx, ag, "/new")

	if !a.inPlanMode() || ag.ExtraSystem != planInstruction {
		t.Fatalf("plan mode after /new: rules %q, instruction kept %v", a.sessionRules, ag.ExtraSystem == planInstruction)
	}
	var sources []string
	for _, rule := range ag.Rules {
		sources = append(sources, rule.Source)
	}
	if strings.Join(sources, ",") != strings.Join(planRules, ",") {
		t.Fatalf("rules after /new = %q, want plan mode's alone", sources)
	}
	if !strings.Contains(out.String(), "plan mode is still on") {
		t.Fatalf("/new did not say plan mode stays; output:\n%s", out.String())
	}
	a.slash(ctx, ag, "/plan off")
	if a.inPlanMode() || ag.ExtraSystem != "" || len(ag.Rules) != 0 {
		t.Fatalf("/plan off after /new left rules %q and instruction %q", a.sessionRules, ag.ExtraSystem)
	}
}

// The live marker follows the session the process runs: after /new the new
// session is live and the old one is not.
func TestNewMovesTheLiveMarker(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	defer ag.Close()
	dir := a.dirs.Sessions()
	old := ag.Sess.SessionID()
	held, err := session.Hold(dir, old)
	if err != nil {
		t.Skipf("no session locks here: %v", err)
	}
	a.sessionHold = held
	a.slash(context.Background(), ag, "/new")
	defer func() {
		if a.sessionHold != nil {
			_ = a.sessionHold.Close()
		}
	}()
	if got := session.Live(dir, old); got != session.StateIdle {
		t.Fatalf("the old session reads %s after /new, want idle", got)
	}
	middle := ag.Sess.SessionID()
	if got := session.Live(dir, middle); got != session.StateLive {
		t.Fatalf("the new session reads %s after /new, want live", got)
	}
	// And again: the marker /new took is one it can give back.
	a.slash(context.Background(), ag, "/new")
	if got := session.Live(dir, middle); got != session.StateIdle {
		t.Fatalf("the session before the second /new reads %s, want idle", got)
	}
	if got := session.Live(dir, ag.Sess.SessionID()); got != session.StateLive {
		t.Fatalf("the latest session reads %s, want live", got)
	}
}
