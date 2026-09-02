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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

func newTestAuthenticatedClient(t testing.TB, baseURL, key string) *Client {
	t.Helper()
	auth, err := secret.NewAuthTransport(secret.New(key), baseURL, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Transport:     auth,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		AppName: "Kolkrabbi",
		auth:    auth,
	}
}

func newCanonicalOpenRouterClient(t testing.TB, key string) *Client {
	t.Helper()
	client, err := NewOpenRouterClient(DefaultBaseURL, key)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

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
	fmt.Fprint(w, `data: {"model":"any/model","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":40,"cost":0.0042,"prompt_tokens_details":{"cached_tokens":90}}}`+"\n\n")
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

	c := newTestAuthenticatedClient(t, srv.URL, "test-key")

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
	// 90 of those 120 prompt tokens were served from cache and cost a fraction
	// of the rest. A chart that cannot see that cannot explain the bill.
	if meta.CacheReadTokens != 90 {
		t.Errorf("cache read tokens = %d, want 90", meta.CacheReadTokens)
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

	c := newTestAuthenticatedClient(t, srv.URL, "bad-key")

	_, _, err := c.StreamChat(context.Background(), "any/model", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil {
		t.Fatal("expected an error for HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention 401", err)
	}
}

func TestClientDoesNotForwardCredentialsAcrossRedirects(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	var sourceCalls int
	var sourceAuth string
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		sourceAuth = r.Header.Get("Authorization")
		http.Redirect(w, r, target.URL+"/chat/completions", http.StatusFound)
	}))
	defer redirect.Close()

	const key = "sk-or-v1-redirect-canary"
	c := newTestAuthenticatedClient(t, redirect.URL, key)
	_, _, err := c.StreamChat(context.Background(), "any/model", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("StreamChat error = %v, want the refused redirect response", err)
	}
	if sourceCalls != 1 || sourceAuth != "Bearer "+key {
		t.Fatalf("redirect source calls/auth = %d/%q, want one authenticated request", sourceCalls, sourceAuth)
	}
	if targetCalls != 0 {
		t.Fatalf("redirect target received %d requests, want 0", targetCalls)
	}
}

func TestClientRefusesCredentialAfterBaseURLMutationBeforeNetwork(t *testing.T) {
	const key = "sk-or-v1-origin-mutation-canary"
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	client := newCanonicalOpenRouterClient(t, key)
	client.BaseURL = srv.URL
	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("mutated authenticated client contacted the replacement origin")
	}
	if calls != 0 {
		t.Fatalf("replacement origin received %d requests, want 0", calls)
	}
	if !errors.Is(err, secret.ErrCredentialOrigin) {
		t.Fatalf("ListModels error = %v, want ErrCredentialOrigin", err)
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("origin refusal leaked the credential: %v", err)
	}
}

func TestNewOpenRouterClientBindsCredentialToCanonicalOrigin(t *testing.T) {
	const key = "sk-or-v1-canonical-origin-canary"
	recorder := &recordingTransport{}
	client := newCanonicalOpenRouterClient(t, key)
	client.auth.Base = recorder

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recorder.last == nil {
		t.Fatal("canonical OpenRouter request did not reach the base transport")
	}
	if got := recorder.last.URL.String(); got != DefaultBaseURL+"/models" {
		t.Fatalf("request URL = %q, want canonical OpenRouter models endpoint", got)
	}
	if got := recorder.last.Header.Get("Authorization"); got != "Bearer "+key {
		t.Fatalf("canonical request Authorization = %q", got)
	}
}

func TestNewOpenRouterClientRejectsANonOpenRouterEndpoint(t *testing.T) {
	client, err := NewOpenRouterClient("https://untrusted.invalid/api/v1", "sk-or-v1-constructor-canary")
	if client != nil {
		t.Fatal("foreign endpoint received an OpenRouter client")
	}
	if !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("constructor error = %v, want ErrCredentialBinding", err)
	}
}

func TestNewCompatibleClientStreamsWithoutCredentialOrAttribution(t *testing.T) {
	var authorization string
	var referer string
	var title string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		referer = r.Header.Get("HTTP-Referer")
		title = r.Header.Get("X-Title")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"compatible\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := NewCompatibleClient(srv.URL)
	message, _, err := client.StreamChat(context.Background(), "model", []Message{{Role: "user", Content: "hello"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "compatible" {
		t.Fatalf("content = %q", message.Content)
	}
	if client.HasKey() {
		t.Fatal("compatible client unexpectedly has a credential")
	}
	if authorization != "" || referer != "" || title != "" {
		t.Fatalf("compatible headers Authorization/Referer/Title = %q/%q/%q", authorization, referer, title)
	}
}

func TestSetKeyDoesNotTrustAnExistingCustomBaseURL(t *testing.T) {
	const key = "sk-or-v1-set-key-canary"
	var calls int
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := client.SetKey(secret.New(key))
	if !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("SetKey error = %v, want ErrCredentialBinding", err)
	}
	_, _ = client.ListModels(context.Background())
	if calls != 1 {
		t.Fatalf("custom origin received %d requests, want one keyless request", calls)
	}
	if authorization != "" {
		t.Fatalf("custom origin received Authorization %q", authorization)
	}
	if client.HasKey() {
		t.Fatal("SetKey installed a credential on an unbound client")
	}
	if err != nil && strings.Contains(err.Error(), key) {
		t.Fatalf("origin refusal leaked the credential: %v", err)
	}
}

func TestConcurrentSetKeyOnUnboundClientIsRefusedRaceFree(t *testing.T) {
	client := &Client{
		BaseURL:    "https://untrusted.invalid/api/v1",
		HTTPClient: &http.Client{Transport: &recordingTransport{}},
	}
	originalHTTPClient := client.HTTPClient
	token := secret.New("sk-or-v1-unbound-concurrent-canary")
	var unexpected atomic.Bool

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			for range 10_000 {
				if err := client.SetKey(token); !errors.Is(err, ErrCredentialBinding) {
					unexpected.Store(true)
				}
				if !client.Key().IsZero() {
					unexpected.Store(true)
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	if unexpected.Load() {
		t.Fatal("concurrent SetKey mutated an unbound client")
	}
	if client.HTTPClient != originalHTTPClient {
		t.Fatal("SetKey replaced the HTTP client on an unbound client")
	}
}

func TestConcurrentSetKeyCannotAttachToMutatedBaseURL(t *testing.T) {
	const key = "sk-or-v1-concurrent-set-key-canary"
	var leaked atomic.Bool
	base := verifierTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Request:    req,
		}, nil
	})
	client := newCanonicalOpenRouterClient(t, "")
	client.BaseURL = "https://untrusted.invalid/api/v1"
	client.auth.Base = base
	token := secret.New(key)

	start := make(chan struct{})
	var unexpected atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 10_000 {
			client.SetKey(token)
			client.SetKey(secret.Secret{})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 10_000 {
			_, err := client.ListModels(context.Background())
			if err != nil && !errors.Is(err, secret.ErrCredentialOrigin) {
				unexpected.Store(true)
			}
		}
	}()
	close(start)
	wg.Wait()

	if unexpected.Load() {
		t.Fatal("concurrent ListModels returned an unexpected error")
	}
	if leaked.Load() {
		t.Fatal("concurrent SetKey attached a credential to the mutated BaseURL")
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

	c := newTestAuthenticatedClient(t, srv.URL, "test-key")
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

	c := newTestAuthenticatedClient(t, srv.URL, key)

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
	bare := newCanonicalOpenRouterClient(t, key)
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
		t.Error("HasKey() = false after constructing a keyed OpenRouter client")
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

	client := newTestAuthenticatedClient(t, srv.URL, "test-key")
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

	c := newTestAuthenticatedClient(t, srv.URL, key)

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
