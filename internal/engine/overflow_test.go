package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestIsContextOverflowRecognisesTheUsualRefusals(t *testing.T) {
	for _, message := range []string{
		"This model's maximum context length is 128000 tokens",
		"context_length_exceeded",
		"prompt is too long: 210000 tokens > 200000 maximum",
		"Please reduce the length of the messages",
		"input length exceeds context window",
	} {
		err := &provider.HTTPError{StatusCode: http.StatusBadRequest, Message: message}
		if !IsContextOverflow(err) {
			t.Fatalf("%q was not recognised as an over-long request", message)
		}
	}
}

func TestIsContextOverflowIgnoresOtherFailures(t *testing.T) {
	for _, err := range []error{
		errors.New("network is unreachable"),
		&provider.HTTPError{StatusCode: http.StatusTooManyRequests, Message: "rate limited"},
		&provider.HTTPError{StatusCode: http.StatusUnauthorized, Message: "invalid api key"},
		&provider.HTTPError{StatusCode: http.StatusBadRequest, Message: "unknown model"},
		&provider.HTTPError{StatusCode: http.StatusInternalServerError, Message: "context length is fine"},
	} {
		if IsContextOverflow(err) {
			t.Fatalf("%v was mistaken for an over-long request", err)
		}
	}
}

func TestIsContextOverflowReadsTheResponseBodyToo(t *testing.T) {
	// Some providers put the reason only in the raw body.
	err := &provider.HTTPError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: `{"error":{"code":"context_length_exceeded"}}`,
	}
	if !IsContextOverflow(err) {
		t.Fatal("the reason was in the body and was missed")
	}
}

func TestIsContextOverflowAcceptsPayloadTooLarge(t *testing.T) {
	err := &provider.HTTPError{StatusCode: http.StatusRequestEntityTooLarge, Message: "request too large"}
	if !IsContextOverflow(err) {
		t.Fatal("413 with a size complaint is the same problem")
	}
}

type overflowThenOKBackend struct {
	calls    int
	sawSizes []int
}

func (b *overflowThenOKBackend) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	b.sawSizes = append(b.sawSizes, estimateTokens(messages))
	if b.calls == 1 {
		return provider.Message{}, provider.Meta{}, &provider.HTTPError{
			StatusCode: http.StatusBadRequest,
			Message:    "This model's maximum context length is 20000 tokens",
		}
	}
	return provider.Message{Role: "assistant", Content: "ok"}, provider.Meta{PromptTokens: 100}, nil
}

// A provider refusing for length used to end the turn. It is the one failure
// Kolkrabbi can actually do something about.
func TestATurnRecoversOnceFromAnOverLongRequest(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(longSession())
	backend := &overflowThenOKBackend{}
	var out strings.Builder
	agent := New(Options{
		Out: &out, Mode: ModeChat, Sess: session, Model: "vendor/model",
		ContextWindow: 20_000, Backend: backend, Yolo: true,
	})

	if err := agent.RunTurn(context.Background(), "carry on"); err != nil {
		t.Fatalf("the turn was lost instead of recovered: %v", err)
	}
	if backend.calls != 2 {
		t.Fatalf("provider called %d times, want one refusal and one retry", backend.calls)
	}
	if backend.sawSizes[1] >= backend.sawSizes[0] {
		t.Fatalf("retried with %d tokens after being refused %d; it must be smaller",
			backend.sawSizes[1], backend.sawSizes[0])
	}
	if !strings.Contains(out.String(), "too long") || !strings.Contains(out.String(), "retrying once") {
		t.Fatalf("output = %q, want the recovery explained", out.String())
	}
}

type alwaysOverflowBackend struct{ calls int }

func (b *alwaysOverflowBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	return provider.Message{}, provider.Meta{}, &provider.HTTPError{
		StatusCode: http.StatusBadRequest,
		Message:    "context_length_exceeded",
	}
}

func TestATurnDoesNotRetryOverflowForever(t *testing.T) {
	session := enginetest.NewFakeSession("s1", "vendor/model")
	session.SetMessages(longSession())
	backend := &alwaysOverflowBackend{}
	var out strings.Builder
	agent := New(Options{
		Out: &out, Mode: ModeChat, Sess: session, Model: "vendor/model",
		ContextWindow: 20_000, Backend: backend, Yolo: true,
	})

	if err := agent.RunTurn(context.Background(), "carry on"); err == nil {
		t.Fatal("a request that cannot be made to fit must fail")
	}
	// Once. Trying again would spend money to fail the same way.
	if backend.calls != 2 {
		t.Fatalf("provider called %d times, want exactly one retry", backend.calls)
	}
}
