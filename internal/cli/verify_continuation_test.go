package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

type proofBackend struct{ neverStarted, closed, resumes, confirmed bool }

func (proofBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}
func (b proofBackend) TurnNeverStarted() bool        { return b.neverStarted }
func (b proofBackend) TurnClosed() bool              { return b.closed }
func (b proofBackend) ResumesConversation() bool     { return b.resumes }
func (b proofBackend) ProviderHandleConfirmed() bool { return b.confirmed }

type noProofBackend struct{}

func (noProofBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}

// The plan decorator proves nothing itself: it says exactly what the adapter
// it wraps says, fact by fact, and nothing for one that says nothing. A
// recovery decided on the decorator's word must be the adapter's word.
func TestThePlanDecoratorForwardsTheAdaptersProofs(t *testing.T) {
	for _, inner := range []proofBackend{
		{neverStarted: true}, {closed: true}, {resumes: true}, {confirmed: true}, {true, true, true, true},
	} {
		wrapped := &verifyingBackend{inner: inner}
		if wrapped.TurnNeverStarted() != inner.neverStarted || wrapped.TurnClosed() != inner.closed ||
			wrapped.ResumesConversation() != inner.resumes || wrapped.ProviderHandleConfirmed() != inner.confirmed {
			t.Errorf("%+v: the decorator said never started %v, closed %v, resumes %v, confirmed %v", inner,
				wrapped.TurnNeverStarted(), wrapped.TurnClosed(), wrapped.ResumesConversation(), wrapped.ProviderHandleConfirmed())
		}
	}
	bare := &verifyingBackend{inner: noProofBackend{}}
	if bare.TurnNeverStarted() || bare.TurnClosed() || bare.ResumesConversation() || bare.ProviderHandleConfirmed() {
		t.Error("the decorator claimed proofs its adapter never gave")
	}
}

// Found by the vendor-recovery verifier. The decorator every production main
// vendor backend is wrapped in passes the vendor's own tool reports through;
// without it the journal never hears what a vendor ran in the main session.
func TestThePlanDecoratorPassesVendorToolReportsThrough(t *testing.T) {
	var wrapped engine.ChatBackend = &verifyingBackend{inner: noProofBackend{}}
	if _, ok := wrapped.(provider.ObservedChatBackend); !ok {
		t.Fatal("verifyingBackend is not a provider.ObservedChatBackend")
	}
}

// Found by the vendor-recovery verifier, end to end on the real composition:
// a real Claude adapter inside the real plan decorator drives a fake claude.
// Claude confirms the conversation, runs a tool with a side effect, and the
// process dies before its result frame. The resume is refused: the vendor
// acted and never closed its turn, so neither continuing nor starting over is
// safe.
func TestADecoratedClaudeMainThatActedAndDiedIsNotReplayed(t *testing.T) {
	// Named apart from the commands: a "$" in a subtest name reaches the temp
	// dir, where the fake's shell would expand it.
	for _, c := range []struct{ name, death string }{{"exited", "exit 3"}, {"killed", "kill -9 $$"}} {
		t.Run(c.name, func(t *testing.T) { decoratedClaudeMainResume(t, c.death) })
	}
}

func decoratedClaudeMainResume(t *testing.T, death string) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	side := filepath.Join(bin, "side-effects.log")
	script := `#!/bin/sh
sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
if [ "$n" = "1" ]; then
  printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"deploy"}}]}}'
  echo "deployed by $sid" >> "` + side + `"
  printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"deployed"}]}}'
  ` + death + `
fi
echo "deployed by $sid" >> "` + side + `"
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"deployed again"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"deployed again\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	sess := enginetest.NewFakeSession("s_probe_decorated", "claude-opus")
	root := t.TempDir()
	decorated := func() engine.ChatBackend {
		state := sess.ProviderStateName()
		inner, err := agentcli.NewClaudeBackendFromHandleWithOptions("claude-opus", engine.ModeCode, "high", state, state != "",
			agentcli.ExecutionOptions{BypassPermissions: true})
		if err != nil {
			t.Fatal(err)
		}
		return &verifyingBackend{inner: inner, note: sess.SetProviderStateName, explain: func(error) {}, confirm: func(context.Context) {}}
	}
	opts := engine.Options{Backend: decorated(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
	first := engine.New(opts)
	firstErr := first.RunTurn(context.Background(), "deploy the service")
	_ = first.Close()
	if firstErr == nil {
		t.Fatal("the dying vendor process did not fail the turn")
	}
	run := sess.RunState()
	if run == nil {
		t.Fatal("no journal")
	}
	if run.Main.VendorTools == nil || run.Main.VendorTools.LastFinishedID != "toolu_1" {
		t.Fatalf("the vendor's tool never reached the journal through the decorator: %+v", run.Main.VendorTools)
	}

	opts.Backend = decorated()
	second := engine.New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing offered for resume")
	}
	resumeErr := second.RunTurn(context.Background(), pending)
	sent, _ := os.ReadFile(requests)
	done, _ := os.ReadFile(side)
	lines := strings.Split(strings.TrimSpace(string(sent)), "\n")
	if len(lines) > 1 {
		t.Fatalf("REPLAY: the vendor had already run its tool (%q), and the resume (err=%v) sent the request again to a %s:\n%s",
			strings.TrimSpace(string(done)), resumeErr, "new conversation", lines[len(lines)-1])
	}
	if resumeErr == nil || !strings.Contains(resumeErr.Error(), "repeat what the vendor already did") {
		t.Fatalf("resume = %v; want a refusal that names the risk of repeating the vendor's work", resumeErr)
	}
}

type confirmedHandleBackend struct{ stubHandleBackend }

func (confirmedHandleBackend) ProviderHandleConfirmed() bool { return true }

// A failed turn notes a handle only once the vendor confirmed it: a restart
// recovering that turn must reach the same conversation. An unconfirmed one
// stays unnoted (TestAFailedTurnNotesNothing).
func TestAFailedTurnNotesAConfirmedHandle(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	noted := ""
	backend := a.verifyingBackend(confirmedHandleBackend{stubHandleBackend{stubBackend: stubBackend{err: errors.New("boom")}, handle: "vendor-conv-1"}},
		planModel, "code", "high", func(state string) { noted = state })
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err == nil {
		t.Fatal("the underlying failure must reach the caller")
	}
	if noted != "vendor-conv-1" {
		t.Fatalf("a failed turn on a confirmed conversation noted %q, want its handle", noted)
	}
}

// End to end on the real composition, the recovery the owner asked for: the
// vendor closes a turn with an error result, and after a restart /resume
// continues that same conversation with --resume and a new message, never a
// fresh one and never the request sent as if new.
func TestADecoratedClaudeMainTheVendorClosedContinuesAfterARestart(t *testing.T) {
	bin := t.TempDir()
	requests := filepath.Join(bin, "requests.log")
	argv := filepath.Join(bin, "argv.log")
	script := `#!/bin/sh
sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid="$a"; fi
  prev="$a"
done
printf '%s\n' "$*" >> "` + argv + `"
IFS= read -r line
printf '%s\n' "$line" >> "` + requests + `"
n=$(wc -l < "` + requests + `" | tr -d ' ')
printf '%s\n' "{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"opus\",\"session_id\":\"$sid\"}"
if [ "$n" = "1" ]; then
  printf '%s\n' "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"is_error\":true,\"result\":\"the vendor stopped\",\"session_id\":\"$sid\"}"
  exit 1
fi
printf '%s\n' '{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"continued"}]}}'
printf '%s\n' "{\"type\":\"result\",\"result\":\"continued\",\"subtype\":\"success\",\"session_id\":\"$sid\"}"
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sess := enginetest.NewFakeSession("s_closed_decorated", "claude-opus")
	root := t.TempDir()
	decorated := func() engine.ChatBackend {
		state := sess.ProviderStateName()
		inner, err := agentcli.NewClaudeBackendFromHandleWithOptions("claude-opus", engine.ModeCode, "high", state, state != "",
			agentcli.ExecutionOptions{BypassPermissions: true})
		if err != nil {
			t.Fatal(err)
		}
		return &verifyingBackend{inner: inner, note: sess.SetProviderStateName, explain: func(error) {}, confirm: func(context.Context) {}}
	}
	opts := engine.Options{Backend: decorated(), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
	first := engine.New(opts)
	if err := first.RunTurn(context.Background(), "deploy the service"); err == nil {
		t.Fatal("the vendor's error result did not fail the turn")
	}
	_ = first.Close()
	handle := sess.ProviderStateName()
	if run := sess.RunState(); run == nil || !run.Main.ProviderTurnClosed || !run.Main.ProviderConfirmed || handle == "" {
		t.Fatalf("journal main = %+v, noted handle %q; want a closed, confirmed turn on a noted conversation", run.Main, handle)
	}
	opts.Backend = decorated()
	second := engine.New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing offered for resume")
	}
	if err := second.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("resume: %v", err)
	}
	sent, _ := os.ReadFile(requests)
	calls, _ := os.ReadFile(argv)
	lines := strings.Split(strings.TrimSpace(string(sent)), "\n")
	spawns := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "Continue this unfinished request") {
		t.Fatalf("requests = %q; want the original and one continuation", lines)
	}
	if len(spawns) != 2 || !strings.Contains(spawns[1], "--resume "+handle) {
		t.Fatalf("spawns = %q; want the second to resume %s", spawns, handle)
	}
}

type retiredHandleBackend struct {
	stubHandleBackend
	retired bool
}

func (b retiredHandleBackend) ProviderHandleRetired() bool { return b.retired }

// A retired conversation is forgotten by the session file too, so no later
// kolk process resumes a conversation whose turn was left unfinished. An
// adapter that holds no handle yet, and retired none, leaves the session's
// handle alone.
func TestARetiredConversationLeavesTheSessionFile(t *testing.T) {
	for _, c := range []struct {
		name    string
		retired bool
		want    string
	}{{"retired", true, ""}, {"none yet", false, "kept"}} {
		t.Run(c.name, func(t *testing.T) {
			a, planModel := unverifiedClaude(t)
			noted := "kept"
			backend := a.verifyingBackend(retiredHandleBackend{stubHandleBackend{stubBackend: stubBackend{err: errors.New("killed")}}, c.retired},
				planModel, "code", "high", func(state string) { noted = state })
			_, _, _ = backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil)
			if noted != c.want {
				t.Fatalf("the session file holds %q, want %q", noted, c.want)
			}
		})
	}
}

type forgettingBackend struct {
	stubHandleBackend
	forgot *bool
}

func (b forgettingBackend) ForgetConversation() { *b.forgot = true }

// /new reaches the adapter through the plan decorator.
func TestThePlanDecoratorForwardsForgetConversation(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	forgot := false
	backend := a.verifyingBackend(forgettingBackend{stubHandleBackend{handle: "h"}, &forgot}, planModel, "code", "high", func(string) {})
	forget, ok := engine.ChatBackend(backend).(interface{ ForgetConversation() })
	if !ok {
		t.Fatal("the decorator hides ForgetConversation")
	}
	forget.ForgetConversation()
	if !forgot {
		t.Fatal("the adapter was not asked to forget its conversation")
	}
}
