package engine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type fallbackVendor struct {
	vendorChild
	calls int
	meta  provider.Meta
}

func (b *fallbackVendor) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, token func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, messages, tools, token, nil)
}

func (b *fallbackVendor) StreamChatObserved(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, token func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	b.calls++
	if b.calls > 1 {
		return provider.Message{Role: "assistant", Content: "replayed"}, provider.Meta{}, nil
	}
	message, _, err := b.vendorChild.StreamChatObserved(ctx, model, messages, tools, token, observe)
	return message, b.meta, err
}

func TestVendorFailureCannotReplayWorkThroughFallback(t *testing.T) {
	for _, policy := range []string{OnLimitSwitch, OnLimitAsk} {
		for _, state := range []struct {
			name                 string
			closed, neverStarted bool
			events               []provider.ProgressEvent
			toolCalls            int
		}{
			{name: "delivered open turn"},
			{name: "closed turn with completed tool", closed: true, events: finishedTool},
			{name: "closed turn with unfinished tool", closed: true, events: finishedTool[:1]},
			{name: "contradictory delivery proof", neverStarted: true, events: finishedTool},
			{name: "closed turn with metadata tool report", closed: true, toolCalls: 1},
		} {
			t.Run(policy+"/"+state.name, func(t *testing.T) {
				backend := &fallbackVendor{vendorChild: vendorChild{handle: "saved", confirmed: true,
					closed: state.closed, neverStarted: state.neverStarted, events: state.events, err: vendorLimit}, meta: provider.Meta{ToolCalls: state.toolCalls}}
				chooser := &stubChooser{answer: "Continue on paid/model", ok: true}
				a := New(Options{Backend: backend, Model: "vendor", Out: io.Discard,
					OnSubscriptionLimit: policy, MeteredModel: func() string { return "paid/model" }, Ask: chooser})
				_, _, err := a.streamChat(context.Background(), "reply", "vendor", nil, nil, nil)
				var limit provider.Limit
				if err == nil || !errors.As(err, &limit) {
					t.Fatalf("fallback lost the original limit: %v", err)
				}
				if backend.calls != 1 || a.SessionModel() != "vendor" || len(chooser.asked) != 0 {
					t.Fatalf("unsafe call was retried/switched/asked: calls=%d model=%q questions=%d", backend.calls, a.SessionModel(), len(chooser.asked))
				}
			})
		}
	}
}

func TestVendorRateLimitCannotRotateOrRetryAnOpenTurn(t *testing.T) {
	backend := &fallbackVendor{vendorChild: vendorChild{handle: "saved", confirmed: true,
		err: &provider.HTTPError{StatusCode: http.StatusTooManyRequests}}}
	a := New(Options{Backend: backend, Model: "vendor/model:free", Out: io.Discard,
		FreeModels: []string{"vendor/model:free", "other/model:free"}})
	_, _, err := a.streamChat(context.Background(), "reply", "vendor/model:free", nil, nil, nil)
	if err == nil || backend.calls != 1 || a.SessionModel() != "vendor/model:free" {
		t.Fatalf("open vendor turn rotated/retried: err=%v calls=%d model=%q", err, backend.calls, a.SessionModel())
	}
}

func TestVendorFallbackIsAllowedOnlyAtAProvenEmptyBoundary(t *testing.T) {
	for _, state := range []struct {
		name                 string
		closed, neverStarted bool
	}{
		{name: "never delivered", neverStarted: true},
		{name: "closed without tools", closed: true},
	} {
		t.Run(state.name, func(t *testing.T) {
			backend := &fallbackVendor{vendorChild: vendorChild{handle: "saved", confirmed: true,
				closed: state.closed, neverStarted: state.neverStarted, err: vendorLimit}}
			a := New(Options{Backend: backend, Model: "vendor", Out: io.Discard,
				OnSubscriptionLimit: OnLimitSwitch, MeteredModel: func() string { return "paid/model" }})
			_, _, err := a.streamChat(context.Background(), "reply", "vendor", nil, nil, nil)
			if err != nil || backend.calls != 2 || a.SessionModel() != "paid/model" {
				t.Fatalf("safe boundary could not fall back: err=%v calls=%d model=%q", err, backend.calls, a.SessionModel())
			}
		})
	}
}

func TestCoordinatedChildLimitCannotSwitchEvenAtSafeBoundary(t *testing.T) {
	backend := &fallbackVendor{vendorChild: vendorChild{neverStarted: true, err: vendorLimit}}
	gate := &executionPause{}
	ctx := context.WithValue(context.Background(), executionPauseKey{}, gate)
	a := New(Options{Backend: backend, Model: "vendor", Out: io.Discard,
		OnSubscriptionLimit: OnLimitSwitch, MeteredModel: func() string { return "paid/model" }})
	_, _, err := a.streamChat(ctx, "working", "vendor", nil, nil, nil)
	if err == nil || gate.stopped() == nil || backend.calls != 1 || a.SessionModel() != "vendor" {
		t.Fatalf("child escaped pause admission gate: err=%v calls=%d model=%q", err, backend.calls, a.SessionModel())
	}
}
