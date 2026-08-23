// Package api implements a minimal client for the OpenRouter chat-completions
// API (OpenAI-compatible), including streamed responses, tool calling, and
// usage/cost accounting.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const DefaultBaseURL = "https://openrouter.ai/api/v1"

// Message is a single chat message. Fields are OpenAI-compatible; unused
// fields are omitted from the wire format via `omitempty`.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
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
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	// AppName/AppURL are sent as OpenRouter's attribution headers.
	AppName string
	AppURL  string
}

func NewClient(apiKey string) *Client {
	// No overall client Timeout: it would cap the total streaming duration.
	// Instead, bound the dial/TLS/first-byte phases and let the caller's
	// context govern the stream itself.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 60 * time.Second
	tr.TLSHandshakeTimeout = 15 * time.Second
	return &Client{
		APIKey:     apiKey,
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Transport: tr},
		AppName:    "Kolkrabbi",
	}
}

// StreamChat sends a chat completion request with streaming enabled. onToken
// is called for every content delta as it arrives (for live terminal output).
// It returns the fully assembled assistant message, including any tool calls,
// plus per-call usage/cost metadata when the server reports it.
func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, tools []Tool, onToken func(string)) (Message, Meta, error) {
	meta := Meta{Model: model}
	if c.APIKey == "" {
		return Message{}, meta, fmt.Errorf("no API key set (run: kolk config set-key <KEY>, or export OPENROUTER_API_KEY)")
	}
	reqBody := chatRequest{
		Model:         model,
		Messages:      messages,
		Tools:         tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	if strings.Contains(c.BaseURL, "openrouter.ai") {
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
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
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
		return Message{}, meta, fmt.Errorf("openrouter: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var contentBuilder strings.Builder
	toolCalls := map[int]*ToolCall{}
	var toolCallOrder []int

	scanner := bufio.NewScanner(resp.Body)
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
			return Message{}, meta, fmt.Errorf("openrouter: %s", chunk.Error.Message)
		}
		if chunk.Model != "" {
			meta.Model = chunk.Model
		}
		if chunk.Usage != nil {
			meta.PromptTokens = chunk.Usage.PromptTokens
			meta.CompletionTokens = chunk.Usage.CompletionTokens
			meta.Cost = chunk.Usage.Cost
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
			existing, ok := toolCalls[tc.Index]
			if !ok {
				cp := tc
				toolCalls[tc.Index] = &cp
				toolCallOrder = append(toolCallOrder, tc.Index)
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
		return Message{}, meta, err
	}
	meta.Elapsed = time.Since(start)

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
	return msg, meta, nil
}

// ModelInfo is a partial view of GET /models, enough for `kolk models`.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pricing     struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	ContextLength int `json:"context_length"`
}

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
