package agentcli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// claudeRejection runs one turn against a vendor that rejects it on the plan
// limit, resetting at resets (zero: the frame names no reset).
func claudeRejection(t *testing.T, resets int64) error {
	t.Helper()
	frame := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"seven_day"}}`
	if resets > 0 {
		frame = fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"seven_day","resetsAt":%d}}`, resets)
	}
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(frame),
		[]byte(`{"type":"result","subtype":"error_during_execution","errors":["usage limit reached"],"is_error":true}`),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := newClaudeSession(ctx, "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	_, _, turnErr := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", nil)
	if turnErr == nil {
		t.Fatal("a rejected plan limit must fail the turn")
	}
	return turnErr
}

// The vendor says when its window resets. That time is the pause's, so the
// session waits for the reset once instead of re-sending the turn every
// fifteen minutes into the same wall, and says "reset at", not "retry at".
func TestAClaudePlanLimitCarriesTheVendorsReset(t *testing.T) {
	resets := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	turnErr := claudeRejection(t, resets.Unix())

	limit, ok := provider.Classify(turnErr)
	if !ok {
		t.Fatalf("the plan limit was not classified: %v", turnErr)
	}
	if limit.Kind != provider.LimitSubscriptionAllowance || limit.Scope != provider.ScopeAccount {
		t.Fatalf("limit = %+v, want the account's subscription allowance", limit)
	}
	if !limit.ResetAt.Equal(resets) {
		t.Fatalf("ResetAt = %v, want the vendor's %v", limit.ResetAt, resets)
	}
	// The limit names its own connector: left to the engine, an unwired
	// session keys the cooldown to the gateway, which then stays cooling for
	// days while the plan that actually ran out is offered as the way on.
	if limit.Connector != "claude" {
		t.Fatalf("Connector = %q, want claude", limit.Connector)
	}
	if limit.Source != "vendor-frame" {
		t.Fatalf("Source = %q, want vendor-frame: the time is the vendor's word, not a phrase match", limit.Source)
	}
	// What a person reads is unchanged: the plan limit speaks first, and the
	// limit's own message carries no kind prefix.
	if !strings.HasPrefix(turnErr.Error(), "claude plan limit reached") {
		t.Fatalf("error = %q, want the plan limit speaking first", turnErr)
	}
	if !strings.HasPrefix(limit.Message, "claude plan limit reached") || !strings.Contains(limit.Message, "seven-day window is fully used") {
		t.Fatalf("Message = %q", limit.Message)
	}
}

// A rejection that names no reset is still the plan's limit; the pause then
// makes its own estimate, as before.
func TestAClaudePlanLimitWithoutAResetStaysAnAllowance(t *testing.T) {
	limit, ok := provider.Classify(claudeRejection(t, 0))
	if !ok || limit.Kind != provider.LimitSubscriptionAllowance {
		t.Fatalf("limit = %+v %v, want a subscription allowance", limit, ok)
	}
	if !limit.ResetAt.IsZero() {
		t.Fatalf("ResetAt = %v, want none: the vendor named none", limit.ResetAt)
	}
}

// A Limit's message is scrubbed: classification passes a Limit through as it
// is, so whatever the vendor's cause carried is scrubbed here, where the
// sentence is built, whichever path handed the cause in.
func TestAPlanLimitsMessageIsScrubbed(t *testing.T) {
	session := &ClaudeSession{rejectedLimit: Event{Kind: EventLimit, LimitWindow: "five_hour"}}
	leaked := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	err := session.classifyLimitFailure(fmt.Errorf("stream ended: %w", &providerError{message: "token " + leaked + " was refused"}))
	limit, ok := provider.Classify(err)
	if !ok {
		t.Fatalf("not classified: %v", err)
	}
	if strings.Contains(limit.Message, leaked) || strings.Contains(err.Error(), leaked) {
		t.Fatalf("the vendor's cause leaked a secret: %q", limit.Message)
	}
	if !strings.Contains(limit.Message, "was refused") {
		t.Fatalf("Message = %q, want the cause kept, scrubbed", limit.Message)
	}
}

// A window of one model family (seven_day_opus) caps that model, not the
// account: every other Claude model stays usable, so the cooldown and the
// pause belong to the model.
func TestAPlanLimitOnAModelsOwnWindowIsThatModels(t *testing.T) {
	for window, want := range map[string]provider.LimitScope{
		"seven_day_opus":  provider.ScopeModel,
		"five_hour_fable": provider.ScopeModel,
		"seven_day":       provider.ScopeAccount,
		"five_hour":       provider.ScopeAccount,
		"monthly":         provider.ScopeAccount,
		"seven_day_":      provider.ScopeAccount,
		"":                provider.ScopeAccount,
	} {
		session := &ClaudeSession{rejectedLimit: Event{Kind: EventLimit, LimitWindow: window}}
		limit, ok := provider.Classify(session.classifyLimitFailure(&providerError{message: "usage limit reached"}))
		if !ok || limit.Scope != want {
			t.Errorf("window %q: scope %q (%v), want %q", window, limit.Scope, ok, want)
		}
	}
}

// A rejection explains the failure of its own turn and no other. Once the
// user has resumed (extra usage, a new window), a later turn's unrelated
// failure is that failure, not the old plan limit with its days-away reset.
func TestARejectionClassifiesOnlyItsOwnTurn(t *testing.T) {
	resets := time.Now().Add(6 * 24 * time.Hour).Unix()
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"seven_day","resetsAt":%d}}`, resets)),
		[]byte(`{"type":"result","subtype":"error_during_execution","errors":["usage limit reached"],"is_error":true}`),
		[]byte(`{"type":"result","result":"done","subtype":"success"}`),
		[]byte(`{"type":"result","subtype":"error_during_execution","errors":["API Error: 529 overloaded"],"is_error":true}`),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := newClaudeSession(ctx, "opus", "code", "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	turn := func(text string) error {
		_, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: text}}, "opus", nil)
		return err
	}
	if limit, ok := provider.Classify(turn("one")); !ok || limit.Kind != provider.LimitSubscriptionAllowance {
		t.Fatalf("turn one = %+v %v, want the plan limit", limit, ok)
	}
	if err := turn("two"); err != nil {
		t.Fatalf("turn two, after the resume: %v", err)
	}
	third := turn("three")
	if third == nil {
		t.Fatal("turn three must fail")
	}
	if strings.HasPrefix(third.Error(), "claude plan limit reached") {
		t.Fatalf("an unrelated failure was read as the old limit: %v", third)
	}
	if limit, ok := provider.Classify(third); ok && !limit.ResetAt.IsZero() {
		t.Fatalf("turn three carries the old reset: %+v", limit)
	}
}
