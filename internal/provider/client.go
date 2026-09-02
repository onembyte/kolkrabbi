// Package api implements a minimal client for the OpenRouter chat-completions
// API (OpenAI-compatible), including streamed responses, tool calling, and
// usage/cost accounting.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

const DefaultBaseURL = "https://openrouter.ai/api/v1"

// CompatibleOrigin identifies a user-selected OpenAI-compatible endpoint that
// does not receive OpenRouter credentials or attribution headers.
const CompatibleOrigin = "compatible"

// ErrCredentialBinding reports an attempt to install an OpenRouter credential
// on a client that was not constructed with the canonical OpenRouter binding.
var ErrCredentialBinding = errors.New("provider: client has no OpenRouter credential binding")

// Message is a single chat message. Fields are OpenAI-compatible; unused
// fields are omitted from the wire format via `omitempty`.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index,omitempty"` // only meaningful in streaming deltas
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Meta is per-call accounting extracted from the stream: token counts always
// when the server reports them, exact cost when OpenRouter reports it.
type Meta struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	Cost             float64 // USD; 0 if the server did not report it
	Elapsed          time.Duration
	// Cache accounting, when the provider reports it. A cached turn costs a
	// fraction of an uncached one, so a cost chart that ignores these cannot
	// explain why two turns on one model differ.
	CacheReadTokens     int
	CacheCreationTokens int
	// ToolCalls counts the tool runs inside the turn when the backend knows
	// better than the returned message — a provider-executed tool loop leaves
	// the message carrying none of its calls.
	ToolCalls int
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	// stream_options.include_usage is the OpenAI-compatible way to get a
	// final usage chunk on streams; broadly supported.
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	// usage.include is OpenRouter's extension that additionally reports the
	// exact cost of the call; only sent when talking to openrouter.ai.
	Usage *usageInclude `json:"usage,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type usageInclude struct {
	Include bool `json:"include"`
}

type wireUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
	// OpenAI-compatible cache accounting. Absent from most providers, so it is
	// a pointer: a missing object and a reported zero mean different things.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type Client struct {
	BaseURL string
	// HTTPClient carries the credential in its Transport, not in any request
	// this package builds. Replacing it wholesale removes authentication —
	// which is what a test wants, and what nothing else should do.
	HTTPClient *http.Client
	// AppName/AppURL are sent as OpenRouter's attribution headers.
	AppName string
	AppURL  string

	// Origin names the service this client talks to when it is not the
	// gateway — "ollama" for a server on this machine. It is stamped on every
	// HTTPError so a refusal from a local process never reads as an OpenRouter
	// error with OpenRouter remedies, and it is what waives the key: the
	// gateway is the only origin that needs one.
	Origin string

	// auth holds the key. It is unexported so the only way to read it back is
	// through a Secret, and it is never copied into a request here.
	auth *secret.AuthTransport
}

// HostOrigin is the origin of a client talking to the user's own Ollama.
const HostOrigin = "ollama"

// NewHostClient talks to an Ollama server on this machine through its
// OpenAI-compatible /v1 (E5). Three things differ from the gateway client, each
// for a reason:
//
//   - No key, and no transport that could attach one. The only credential
//     kolk holds is the OpenRouter key, and a Bearer header carrying it to a
//     process on this machine is a credential leaving the service it belongs
//     to. The server needs none; cloud models are signed by the server's own
//     key, which kolk never sees.
//   - No first-byte timeout. A cold 7B on a CPU takes minutes to its first
//     token; the gateway's 60 s is right for a data centre and wrong here. The
//     turn's context bounds the wait instead.
//   - No attribution headers. They are OpenRouter's, and a local server would
//     only ignore them.
func NewHostClient(addr string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 0
	return &Client{
		BaseURL:    "http://" + addr + "/v1",
		HTTPClient: &http.Client{Transport: tr},
		Origin:     HostOrigin,
	}
}

// requiresKey is true for the gateway, which refuses unauthenticated calls,
// and false for a local origin, which has nothing to authenticate.
func (c *Client) requiresKey() bool { return c.Origin == "" }

// IsOpenRouterEndpoint reports whether baseURL has the canonical OpenRouter
// origin. Paths may vary; credential trust is an origin boundary.
func IsOpenRouterEndpoint(baseURL string) bool {
	return secret.SameOrigin(baseURL, DefaultBaseURL)
}

// NewOpenRouterClient constructs an authenticated client only when baseURL is
// on the canonical OpenRouter origin.
func NewOpenRouterClient(baseURL, apiKey string) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !IsOpenRouterEndpoint(baseURL) {
		return nil, ErrCredentialBinding
	}

	// No overall client Timeout: it would cap the total streaming duration.
	// Instead, bound the dial/TLS/first-byte phases and let the caller's
	// context govern the stream itself.
	tr := newProviderTransport()

	// The Authorization header is attached inside secret.AuthTransport, on a
	// clone of the request, so no request this package builds ever contains the
	// key. That matters because %+v on an *http.Request prints Header, and
	// http.Header is a plain map that cannot redact anything — a failing call
	// logged with %+v was, until now, a published key.
	auth := newOpenRouterAuthTransport(secret.New(apiKey), tr)
	return &Client{
		BaseURL: baseURL,
		// Refuse redirects so the auth transport cannot attach the bearer to a
		// different host selected by a response.
		HTTPClient: noRedirectClient(auth),
		AppName:    "Kolkrabbi",
		auth:       auth,
	}, nil
}

// NewCompatibleClient constructs a credentialless client for a user-selected
// OpenAI-compatible endpoint.
func NewCompatibleClient(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: noRedirectClient(newProviderTransport()),
		Origin:     CompatibleOrigin,
	}
}

func newProviderTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 60 * time.Second
	tr.TLSHandshakeTimeout = 15 * time.Second
	return tr
}

func noRedirectClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Key returns the configured credential. It is a Secret, so printing it is
// safe and using it requires calling Reveal.
func (c *Client) Key() secret.Secret {
	if c.auth == nil {
		return secret.Secret{}
	}
	return c.auth.Token()
}

// SetKey replaces the credential on an already-bound OpenRouter client. It
// never converts a keyless compatible or host client into an authenticated
// client, because that would derive credential trust from mutable BaseURL.
func (c *Client) SetKey(key secret.Secret) error {
	if c.auth == nil {
		return ErrCredentialBinding
	}
	c.auth.SetToken(key)
	return nil
}

func newOpenRouterAuthTransport(key secret.Secret, base http.RoundTripper) *secret.AuthTransport {
	auth, err := secret.NewAuthTransport(key, DefaultBaseURL, base)
	if err != nil {
		// DefaultBaseURL is a compile-time constant owned by this package. A
		// malformed value is a programmer error, not runtime configuration.
		panic(fmt.Sprintf("provider: invalid OpenRouter origin: %v", err))
	}
	return auth
}

// HasKey reports whether a credential is configured.
func (c *Client) HasKey() bool { return !c.Key().IsZero() }

// StreamChat sends a chat completion request with streaming enabled. onToken
// is called for every content delta as it arrives (for live terminal output).
// It returns the fully assembled assistant message, including any tool calls,
// plus per-call usage/cost metadata when the server reports it.
// syntheticSlot is where a tool call goes when its index collides with a
// different call's. Far above any real index, so sorting keeps real indexes
// first and collisions in arrival order after them.
const syntheticSlot = 1 << 20

func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, tools []Tool, onToken func(string)) (Message, Meta, error) {
	meta := Meta{Model: model}
	if c.requiresKey() && !c.HasKey() {
		return Message{}, meta, fmt.Errorf("no API key set (run: kolk key <API_KEY>, or export OPENROUTER_API_KEY)")
	}
	reqBody := chatRequest{
		Model:         model,
		Messages:      messages,
		Tools:         tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	// OpenRouter-specific request shape follows the client's origin, not a
	// substring of the URL: a proxy path or lookalike host that happens to
	// contain "openrouter.ai" is a compatible endpoint, not OpenRouter.
	if c.requiresKey() {
		reqBody.Usage = &usageInclude{Include: true}
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, meta, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, meta, err
	}
	req.Header.Set("Content-Type", "application/json")
	// No Authorization here, deliberately: the bound transport adds it.
	if c.AppURL != "" {
		req.Header.Set("HTTP-Referer", c.AppURL)
	}
	if c.AppName != "" {
		req.Header.Set("X-Title", c.AppName)
	}

	start := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Message{}, meta, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		httpErr := newHTTPError(resp.StatusCode, resp.Header, b)
		httpErr.Origin = c.Origin
		return Message{}, meta, secret.ScrubError(httpErr)
	}

	msg, err := readStream(resp.Body, &meta, onToken)
	if err != nil {
		return Message{}, meta, err
	}
	meta.Elapsed = time.Since(start)
	return msg, meta, nil
}

// readStream turns a server-sent event body into one message.
//
// It is a function of its own so it can be fuzzed: this is one of the two
// places where bytes from a third party become control flow, and every
// hand-written fragmentation test here encodes a fragmentation somebody thought
// of (docs/plan/21-quality-testing-security.md).
func readStream(body io.Reader, meta *Meta, onToken func(string)) (Message, error) {
	var contentBuilder strings.Builder
	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow long lines (big tool args)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" || data == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // ignore malformed keep-alive/comment lines
		}
		if chunk.Error != nil {
			return Message{}, fmt.Errorf("openrouter: %s", chunk.Error.Message)
		}
		if chunk.Model != "" {
			meta.Model = chunk.Model
		}
		if chunk.Usage != nil {
			meta.PromptTokens = chunk.Usage.PromptTokens
			meta.CompletionTokens = chunk.Usage.CompletionTokens
			meta.Cost = chunk.Usage.Cost
			if chunk.Usage.PromptTokensDetails != nil {
				meta.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
		}
		if len(chunk.Choices) == 0 {
			continue // usage-only or keep-alive chunk
		}
		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			if onToken != nil {
				onToken(delta.Content)
			}
		}

		for _, tc := range delta.ToolCalls {
			slot := tc.Index
			existing, ok := toolCalls[slot]
			// A new id at an index already holding a call is a new call, not
			// a continuation: an absent index decodes as 0, so a server that
			// sends complete calls without indexes would otherwise have its
			// second call's arguments appended to its first.
			if ok && tc.ID != "" && existing.ID != "" && tc.ID != existing.ID {
				slot = syntheticSlot + len(toolCallOrder)
				existing, ok = toolCalls[slot]
			}
			if !ok {
				cp := tc
				toolCalls[slot] = &cp
				toolCallOrder = append(toolCallOrder, slot)
				continue
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.Function.Name += tc.Function.Name
			}
			existing.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return Message{}, err
	}

	msg := Message{Role: "assistant", Content: contentBuilder.String()}
	if len(toolCalls) > 0 {
		sort.Ints(toolCallOrder)
		for _, i := range toolCallOrder {
			tc := *toolCalls[i]
			tc.Index = 0
			if tc.Type == "" {
				tc.Type = "function"
			}
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}
	return msg, nil
}

// ModelInfo is a partial view of GET /models, enough for `kolk models`.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pricing     struct {
		Prompt            string `json:"prompt"`
		Completion        string `json:"completion"`
		Request           string `json:"request"`
		InternalReasoning string `json:"internal_reasoning"`
	} `json:"pricing"`
	ContextLength       int      `json:"context_length"`
	SupportedParameters []string `json:"supported_parameters"`
}

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return c.listModels(ctx, nil)
}

// ListModelsRanked asks OpenRouter to place the strongest generally capable
// tool models first. Callers still verify pricing and coding suitability;
// server order is only the final quality tie-breaker.
func (c *Client) ListModelsRanked(ctx context.Context) ([]ModelInfo, error) {
	query := url.Values{
		"output_modalities":    {"text"},
		"sort":                 {"intelligence-high-to-low"},
		"supported_parameters": {"tools"},
	}
	return c.listModels(ctx, query)
}

func (c *Client) listModels(ctx context.Context, query url.Values) ([]ModelInfo, error) {
	endpoint := c.BaseURL + "/models"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, secret.ScrubError(
			fmt.Errorf("openrouter: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b))))
	}
	var out struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
