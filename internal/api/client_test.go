package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
