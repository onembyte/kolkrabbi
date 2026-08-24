package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// simulates a real OpenRouter/OpenAI streaming response: content in a few
// chunks, then a tool call whose name and arguments arrive fragmented across
// several deltas (as they do in real streaming responses).
func mockSSEHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")

	events := []streamChunk{
		mkChunk("Sure, ", nil, nil),
		mkChunk("let me check that.\n", nil, nil),
		mkChunk("", []ToolCall{{Index: 0, ID: "call_abc123", Type: "function", Function: FunctionCall{Name: "bash", Arguments: ""}}}, nil),
		mkChunk("", []ToolCall{{Index: 0, Function: FunctionCall{Arguments: `{"command": "`}}}, nil),
		mkChunk("", []ToolCall{{Index: 0, Function: FunctionCall{Arguments: `ls -la", "description"`}}}, nil),
		mkChunk("", []ToolCall{{Index: 0, Function: FunctionCall{Arguments: `: "list files"}`}}}, nil),
		mkChunk("", nil, strPtr("tool_calls")),
	}
	fl, _ := w.(http.Flusher)
	for _, e := range events {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	fmt.Fprint(w, `data: {"model":"any/model","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":40,"cost":0.0042}}`+"\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func strPtr(s string) *string { return &s }

func mkChunk(content string, toolCalls []ToolCall, finishReason *string) streamChunk {
	var c streamChunk
	c.Choices = make([]struct {
		Delta struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	}, 1)
	c.Choices[0].Delta.Content = content
	c.Choices[0].Delta.ToolCalls = toolCalls
	c.Choices[0].FinishReason = finishReason
	return c
}

func TestStreamChat_ToolCallAccumulation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(mockSSEHandler))
	defer srv.Close()

	c := NewClient("test-key")
	c.BaseURL = srv.URL

	var tokens strings.Builder
	msg, meta, err := c.StreamChat(context.Background(), "any/model", []Message{{Role: "user", Content: "list files"}}, nil, func(tok string) {
		tokens.WriteString(tok)
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if meta.PromptTokens != 120 || meta.CompletionTokens != 40 || meta.Cost != 0.0042 {
		t.Errorf("meta = %+v, want usage 120/40 cost 0.0042", meta)
	}
	if meta.Elapsed <= 0 {
		t.Error("meta.Elapsed not measured")
	}

	wantContent := "Sure, let me check that.\n"
	if msg.Content != wantContent {
		t.Errorf("content = %q, want %q", msg.Content, wantContent)
	}
	if tokens.String() != wantContent {
		t.Errorf("streamed tokens = %q, want %q", tokens.String(), wantContent)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("tool call ID = %q, want call_abc123", tc.ID)
	}
	if tc.Function.Name != "bash" {
		t.Errorf("tool call name = %q, want bash", tc.Function.Name)
	}
	wantArgs := `{"command": "ls -la", "description": "list files"}`
	if tc.Function.Arguments != wantArgs {
		t.Errorf("tool call args = %q, want %q", tc.Function.Arguments, wantArgs)
	}
}

func TestStreamChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := NewClient("bad-key")
	c.BaseURL = srv.URL

	_, _, err := c.StreamChat(context.Background(), "any/model", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention 401", err)
	}
}

func TestStreamChat_HTTPErrorPreservesRateLimitClassification(t *testing.T) {
	const echoedKey = "sk-or-v1-0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Provider returned error ` + echoedKey + `","metadata":{"provider_name":"Stealth","limit_source":"upstream_provider_shared_pool","remedy_hint":"Retry shortly"}}}`))
	}))
	defer srv.Close()

	c := NewClient("test-key")
	c.BaseURL = srv.URL
	_, _, err := c.StreamChat(context.Background(), "stealth/ox-alpha", []Message{{Role: "user", Content: "continue"}}, nil, nil)
	if err == nil {
		t.Fatal("expected an HTTP error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error type = %T, want *HTTPError: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests || httpErr.RetryAfter != 3*time.Second {
		t.Fatalf("HTTP error status/retry = %d/%v", httpErr.StatusCode, httpErr.RetryAfter)
	}
	if httpErr.ProviderName != "Stealth" || httpErr.LimitSource != "upstream_provider_shared_pool" || httpErr.RemedyHint != "Retry shortly" {
		t.Fatalf("HTTP error metadata = %+v", httpErr)
	}
	if strings.Contains(err.Error(), echoedKey) || strings.Contains(httpErr.ResponseBody, echoedKey) {
		t.Fatalf("typed HTTP error leaked echoed credential: %v / %q", err, httpErr.ResponseBody)
	}
}

// The credential must reach the server and nothing else.
//
// Before secret.AuthTransport, StreamChat built the Authorization header on the
// request it owned, so any error path or debug line that printed that request
// with %+v published the key — http.Header is a plain map and cannot redact.
func TestKeyNeverAppearsInAnythingPrintable(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef"

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := NewClient(key)
	c.BaseURL = srv.URL

	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer "+key {
		t.Errorf("the server received %q; authentication did not arrive", got)
	}

	// The precise invariant: bypass the auth transport entirely and inspect
	// the request this package actually built. If any code here sets the
	// header itself, it shows up on the request the caller holds — and that is
	// the request that lands in a log line or an error with %+v.
	bare := NewClient(key)
	bare.BaseURL = srv.URL
	rec := &recordingTransport{}
	bare.HTTPClient = &http.Client{Transport: rec}

	_, _ = bare.ListModels(context.Background())
	if h := rec.last.Header.Get("Authorization"); h != "" {
		t.Errorf("ListModels put the credential on its own request: %q", h)
	}
	_, _, _ = bare.StreamChat(context.Background(), "m", []Message{{Role: "user", Content: "x"}}, nil, nil)
	if h := rec.last.Header.Get("Authorization"); h != "" {
		t.Errorf("StreamChat put the credential on its own request: %q", h)
	}

	// Every way someone might print the client while debugging a failed call.
	for name, dump := range map[string]string{
		"%v":         fmt.Sprintf("%v", c),
		"%+v":        fmt.Sprintf("%+v", c),
		"%#v":        fmt.Sprintf("%#v", c),
		"transport":  fmt.Sprintf("%+v", c.HTTPClient.Transport),
		"key":        fmt.Sprintf("%+v", c.Key()),
		"httpclient": fmt.Sprintf("%+v", c.HTTPClient),
	} {
		if strings.Contains(dump, key) {
			t.Errorf("printing the client with %s leaked the key:\n%s", name, dump)
		}
	}
	if !c.HasKey() {
		t.Error("HasKey() = false after NewClient with a key")
	}
}

func TestListModelsRankedRequestsIntelligenceAndToolFiltering(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"free/code","name":"Free Code","description":"coding","supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0","request":"0","internal_reasoning":"0"},"context_length":200000}]}`)
	}))
	defer srv.Close()

	client := NewClient("test-key")
	client.BaseURL = srv.URL
	models, err := client.ListModelsRanked(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawQuery, "sort=intelligence-high-to-low") ||
		!strings.Contains(rawQuery, "supported_parameters=tools") ||
		!strings.Contains(rawQuery, "output_modalities=text") {
		t.Fatalf("ranked models query = %q", rawQuery)
	}
	if len(models) != 1 || models[0].ID != "free/code" || models[0].Pricing.Request != "0" ||
		models[0].Pricing.InternalReasoning != "0" || len(models[0].SupportedParameters) != 1 {
		t.Fatalf("ranked models = %#v", models)
	}
}

// A gateway that rejects a request will happily echo the Authorization header
// it received straight back in the error body.
func TestProviderErrorsAreScrubbed(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key: ` + key + `"}}`))
	}))
	defer srv.Close()

	c := NewClient(key)
	c.BaseURL = srv.URL

	_, err := c.ListModels(context.Background())
	if err == nil {
		t.Fatal("a 401 should be an error")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the provider error echoed the key back: %v", err)
	}

	_, _, err = c.StreamChat(context.Background(), "m", []Message{{Role: "user", Content: "x"}}, nil, nil)
	if err == nil {
		t.Fatal("a 401 should be an error")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the streaming error echoed the key back: %v", err)
	}
}

// recordingTransport keeps the last request it was handed, unmodified.
type recordingTransport struct{ last *http.Request }

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.last = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		Request:    req,
	}, nil
}
