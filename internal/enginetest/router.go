// Package mockrouter provides a scripted, in-process fake of the OpenRouter
// chat-completions endpoint for sandboxed end-to-end testing: no network, no
// API key, fully deterministic. Each Step is one assistant response; the
// server streams them back in order as SSE, deliberately fragmenting content
// and tool-call arguments across chunks the way real providers do.
package enginetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Step is one scripted assistant response. If ToolCalls is non-empty the
// stream ends with finish_reason "tool_calls", otherwise "stop". Usage is
// reported in a final usage chunk like real providers; zero values get
// synthetic defaults.
type Step struct {
	Text             string
	ToolCalls        []provider.ToolCall
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	StatusCode       int
	RetryAfter       string
	ErrorBody        string
	StreamError      string
}

type Server struct {
	*httptest.Server

	mu       sync.Mutex
	steps    []Step
	i        int
	Requests [][]provider.Message // messages array of every request received, in order
	Tools    []int                // number of tool schemas each request carried
	Models   []string             // the model each request asked for, in order
}

func New(steps ...Step) *Server {
	s := &Server{steps: steps}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// delta mirrors the wire shape of a single streaming chunk.
type delta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []provider.ToolCall `json:"tool_calls,omitempty"`
}

type wireUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

type chunk struct {
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Delta        delta   `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage,omitempty"`
	Error *wireError `json:"error,omitempty"`
}

type wireError struct {
	Message string `json:"message"`
}

func mkChunk(d delta, finish *string) chunk {
	var c chunk
	c.Choices = make([]struct {
		Delta        delta   `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	}, 1)
	c.Choices[0].Delta = d
	c.Choices[0].FinishReason = finish
	return c
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []provider.Message `json:"messages"`
		Tools    []provider.Tool    `json:"tools"`
		Model    string             `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.Requests = append(s.Requests, req.Messages)
	s.Tools = append(s.Tools, len(req.Tools))
	s.Models = append(s.Models, req.Model)
	if s.i >= len(s.steps) {
		s.mu.Unlock()
		http.Error(w, `{"error":{"message":"mockrouter: no more scripted steps"}}`, http.StatusInternalServerError)
		return
	}
	step := s.steps[s.i]
	s.i++
	s.mu.Unlock()

	if step.StatusCode != 0 && step.StatusCode != http.StatusOK {
		if step.RetryAfter != "" {
			w.Header().Set("Retry-After", step.RetryAfter)
		}
		w.WriteHeader(step.StatusCode)
		_, _ = fmt.Fprint(w, step.ErrorBody)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	emit := func(c chunk) {
		b, _ := json.Marshal(c)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}

	emit(mkChunk(delta{Role: "assistant"}, nil))
	if step.StreamError != "" {
		emit(chunk{Error: &wireError{Message: step.StreamError}})
		return
	}

	// stream content in small fragments, like a real provider
	for _, frag := range fragments(step.Text, 7) {
		emit(mkChunk(delta{Content: frag}, nil))
	}

	// stream each tool call: header chunk (id/type/name), then argument fragments
	for idx, tc := range step.ToolCalls {
		emit(mkChunk(delta{ToolCalls: []provider.ToolCall{{
			Index: idx, ID: tc.ID, Type: "function",
			Function: provider.FunctionCall{Name: tc.Function.Name},
		}}}, nil))
		for _, frag := range fragments(tc.Function.Arguments, 9) {
			emit(mkChunk(delta{ToolCalls: []provider.ToolCall{{
				Index:    idx,
				Function: provider.FunctionCall{Arguments: frag},
			}}}, nil))
		}
	}

	finish := "stop"
	if len(step.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	emit(mkChunk(delta{}, &finish))

	// final usage chunk (empty choices), as real providers send with
	// stream_options.include_usage
	u := wireUsage{PromptTokens: step.PromptTokens, CompletionTokens: step.CompletionTokens, Cost: step.Cost}
	if u.PromptTokens == 0 {
		u.PromptTokens = 100
	}
	if u.CompletionTokens == 0 {
		u.CompletionTokens = len(step.Text)/4 + 8
	}
	if u.Cost == 0 {
		u.Cost = 0.001
	}
	uc := chunk{Model: "mock/model", Usage: &u}
	b, _ := json.Marshal(uc)
	fmt.Fprintf(w, "data: %s\n\n", b)
	if fl != nil {
		fl.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// fragments splits s into chunks of roughly n runes, always cutting on rune
// boundaries: real providers emit valid-UTF-8 JSON strings per chunk, and
// json.Marshal would corrupt a fragment that ends mid-rune.
func fragments(s string, n int) []string {
	if s == "" {
		return nil
	}
	var out []string
	start, count := 0, 0
	for i := range s { // i iterates rune start offsets
		if count == n {
			out = append(out, s[start:i])
			start, count = i, 0
		}
		count++
	}
	return append(out, s[start:])
}
