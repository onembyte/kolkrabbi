package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer answers /chat/completions with the given SSE lines and records
// what it was sent.
func sseServer(t *testing.T, lines []string, status int) (*httptest.Server, *http.Header) {
	t.Helper()
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized","type":"api_error"},"signin_url":"https://ollama.com/connect?name=box"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func hostAddr(server *httptest.Server) string { return strings.TrimPrefix(server.URL, "http://") }

// E5. A local server takes no key, and must be sent none: the only credential
// kolk holds is the OpenRouter key, and a Bearer header carrying it to a
// process on this machine is a credential leaving the service it belongs to.
func TestHostClientStreamsWithoutAKeyAndSendsNone(t *testing.T) {
	server, seen := sseServer(t, []string{
		`{"choices":[{"delta":{"content":"hel"}}]}`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
	}, http.StatusOK)
	client := NewHostClient(hostAddr(server))

	var streamed strings.Builder
	msg, _, err := client.StreamChat(context.Background(), "qwen2.5-coder:7b", []Message{{Role: "user", Content: "hi"}}, nil, func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatalf("a keyless host client refused to stream: %v", err)
	}
	if msg.Content != "hello" || streamed.String() != "hello" {
		t.Errorf("content = %q streamed = %q, want hello", msg.Content, streamed.String())
	}
	if seen.Get("Authorization") != "" {
		t.Fatalf("a credential was sent to a local server: %q", seen.Get("Authorization"))
	}
}

// A cold 7B on a CPU takes minutes to its first token. The gateway client's
// 60 s first-byte timeout is right for a data centre and wrong here; the turn's
// own context bounds the wait instead.
func TestHostClientHasNoFirstByteTimeout(t *testing.T) {
	client := NewHostClient("127.0.0.1:11434")
	transport, ok := client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("host client transport is %T, want a bare *http.Transport with nothing to add", client.HTTPClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 0 {
		t.Fatalf("ResponseHeaderTimeout = %v, want none", transport.ResponseHeaderTimeout)
	}
}

// The guard that matters for the user: an error from the local server must not
// read as an OpenRouter error with OpenRouter remedies. A signed-out cloud
// model is a 401 from Ollama, and "run kolk key" is the wrong fix.
func TestHostClientErrorsNameTheirOriginAndItsRemedy(t *testing.T) {
	server, _ := sseServer(t, nil, http.StatusUnauthorized)
	client := NewHostClient(hostAddr(server))
	_, _, err := client.StreamChat(context.Background(), "gpt-oss:120b-cloud", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil {
		t.Fatal("a 401 was not an error")
	}
	if strings.Contains(err.Error(), "openrouter") {
		t.Errorf("a local server's error is labelled openrouter: %q", err)
	}
	if !strings.HasPrefix(err.Error(), "ollama:") {
		t.Errorf("error %q does not name its origin", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Origin != "ollama" {
		t.Fatalf("error does not carry its origin: %+v", httpErr)
	}
	advice, ok := Advise(err)
	if !ok {
		t.Fatal("no advice for an Ollama 401")
	}
	if strings.Contains(advice.Summary+advice.NextAction, "OpenRouter") || strings.Contains(advice.NextAction, "kolk key") {
		t.Errorf("advice sends the user to fix OpenRouter for an Ollama sign-in: %+v", advice)
	}
	if !strings.Contains(advice.NextAction, "ollama signin") {
		t.Errorf("advice does not name the command that signs in: %+v", advice)
	}
	if !strings.Contains(advice.NextAction, "https://ollama.com/connect?name=box") {
		t.Errorf("the sign-in URL the server offered was dropped: %+v", advice)
	}
}

func TestHostClientNotFoundNamesThePull(t *testing.T) {
	err := &HTTPError{StatusCode: http.StatusNotFound, Origin: "ollama", Message: `model "qwen2.5-coder:7b" not found`}
	advice, ok := Advise(err)
	if !ok || !strings.Contains(advice.NextAction, "ollama pull") {
		t.Fatalf("a missing local model does not say how to pull it: %+v", advice)
	}
}

// Two complete tool calls can arrive in separate chunks that both carry index
// 0 — an absent index decodes as 0. Merging them by index concatenates two
// calls into one with garbage arguments; a new id at the same index is a new
// call.
func TestStreamKeepsTwoToolCallsApartWhenTheIndexRepeats(t *testing.T) {
	server, _ := sseServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_b","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"b\"}"}}]}}]}`,
	}, http.StatusOK)
	client := NewHostClient(hostAddr(server))
	msg, _, err := client.StreamChat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2: %+v", len(msg.ToolCalls), msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Arguments != `{"path":"a"}` || msg.ToolCalls[1].Function.Arguments != `{"path":"b"}` {
		t.Fatalf("arguments were merged across calls: %+v", msg.ToolCalls)
	}
}

// A cloud usage limit is advice about time, not money: it resets, and a local
// model has no limit at all.
func TestHostClientRateLimitAdviceSaysItResets(t *testing.T) {
	err := &HTTPError{StatusCode: http.StatusTooManyRequests, Origin: HostOrigin, Message: "you have reached your session usage limit"}
	advice, ok := Advise(err)
	if !ok {
		t.Fatal("no advice for an Ollama 429")
	}
	text := advice.Summary + " " + advice.NextAction
	if !strings.Contains(text, "resets") || strings.Contains(text, "OpenRouter") || strings.Contains(text, "credit") {
		t.Fatalf("advice = %+v, want a resetting limit and nothing about the gateway or money", advice)
	}
}
