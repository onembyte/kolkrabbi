package agentcli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// copilotFrame is one line of `copilot --output-format json` as observed on
// 2026-09-06 (CLI 1.0.82). Only the fields kolk reads are named; everything
// else is left where it is.
type copilotFrame struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	SessionID string          `json:"sessionId"`
	ExitCode  *int            `json:"exitCode"`
}

type copilotMessage struct {
	Model        string `json:"model"`
	Content      string `json:"content"`
	DeltaContent string `json:"deltaContent"`
	ToolRequests []struct {
		ToolCallID string          `json:"toolCallId"`
		Name       string          `json:"name"`
		Arguments  json.RawMessage `json:"arguments"`
	} `json:"toolRequests"`
}

type copilotToolStart struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
	Model      string          `json:"model"`
}

type copilotToolComplete struct {
	ToolCallID string `json:"toolCallId"`
	Model      string `json:"model"`
	Success    bool   `json:"success"`
	Result     *struct {
		Content string `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

type copilotCallSuccess struct {
	ResponseUsage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens        int `json:"cached_tokens"`
			CacheCreationTokens int `json:"cache_creation_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"responseUsage"`
}

type copilotAutoMode struct {
	ChosenModel     string   `json:"chosenModel"`
	AvailableModels []string `json:"availableModels"`
}

// TranslateCopilot turns one JSONL line into kolk's events. The reply is the
// message deltas, confirmed by assistant.message; a tool's start and result
// are one tool event each, a denied result an error the person can see;
// usage comes from the model call; the terminal `result` carries the session
// handle and the exit code, a non-zero one being the turn's error. Lines of
// kinds kolk does not read yield nothing.
func TranslateCopilot(line []byte) ([]Event, error) {
	line = bytesTrimmed(line)
	if len(line) == 0 {
		return nil, nil
	}
	// The CLI prints the odd non-JSON line (update notices); it is not the
	// reply and not an error, so only a JSON object is read as a frame.
	if !json.Valid(line) || line[0] != '{' {
		return nil, nil
	}
	var frame copilotFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return nil, fmt.Errorf("copilot frame: %w", err)
	}
	switch frame.Type {
	case "session.auto_mode_resolved":
		var auto copilotAutoMode
		_ = json.Unmarshal(frame.Data, &auto)
		if auto.ChosenModel != "" {
			return []Event{{Kind: EventInit, Model: auto.ChosenModel, Text: strings.Join(auto.AvailableModels, ",")}}, nil
		}
	case "assistant.message_delta":
		var m copilotMessage
		_ = json.Unmarshal(frame.Data, &m)
		if m.DeltaContent != "" {
			return []Event{{Kind: EventMessageDelta, Text: m.DeltaContent}}, nil
		}
	case "assistant.message":
		var m copilotMessage
		_ = json.Unmarshal(frame.Data, &m)
		// A message that only carries tool requests is not the reply; the
		// tool events say what happened. The final message is.
		if m.Content != "" {
			return []Event{{Kind: EventMessageCompleted, Model: m.Model, Text: m.Content}}, nil
		}
	case "tool.execution_start":
		var t copilotToolStart
		_ = json.Unmarshal(frame.Data, &t)
		return []Event{{Kind: EventTool, Model: t.Model, ToolName: t.ToolName, ToolCallID: t.ToolCallID, ToolInput: rawText(t.Arguments)}}, nil
	case "tool.execution_complete":
		var t copilotToolComplete
		_ = json.Unmarshal(frame.Data, &t)
		event := Event{Kind: EventTool, Model: t.Model, ToolCallID: t.ToolCallID}
		if t.Success && t.Result != nil {
			event.ToolOutput = t.Result.Content
		} else if t.Error != nil {
			event.ToolIsError = true
			event.ToolOutput = t.Error.Message
			if t.Error.Code != "" {
				event.ToolOutput += " (" + t.Error.Code + ")"
			}
		} else {
			event.ToolIsError = true
			event.ToolOutput = "tool failed without a message"
		}
		return []Event{event}, nil
	case "model.model_call_success":
		var c copilotCallSuccess
		_ = json.Unmarshal(frame.Data, &c)
		if c.ResponseUsage != nil {
			return []Event{{Kind: EventUsage,
				InputTokens: c.ResponseUsage.PromptTokens, OutputTokens: c.ResponseUsage.CompletionTokens,
				CacheRead: c.ResponseUsage.PromptTokensDetails.CachedTokens, CacheCreation: c.ResponseUsage.PromptTokensDetails.CacheCreationTokens}}, nil
		}
	case "result":
		events := []Event{{Kind: EventInit, SessionID: frame.SessionID}}
		if frame.ExitCode != nil && *frame.ExitCode != 0 {
			events = append(events, Event{Kind: EventError, Error: fmt.Sprintf("copilot exited with code %d", *frame.ExitCode)})
		}
		return events, nil
	}
	return nil, nil
}

// CopilotToolsDenied reports whether the run had a tool refused because the
// non-interactive CLI could not ask — the observed shape of a turn run
// without --allow-all-tools, where the model still answers as if it had done
// the work.
func CopilotToolsDenied(events []Event) bool {
	for _, event := range events {
		if event.Kind == EventTool && event.ToolIsError && strings.Contains(event.ToolOutput, "(denied)") {
			return true
		}
	}
	return false
}

// rawText renders a tool's arguments for the transcript: a JSON string as
// its text, anything else as its JSON.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
