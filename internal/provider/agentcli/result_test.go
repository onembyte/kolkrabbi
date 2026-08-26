package agentcli

import (
	"strings"
	"testing"
	"time"
)

func TestCollectBuildsProviderMessageAndMetadata(t *testing.T) {
	message, meta, err := Collect([]Event{
		{Kind: EventInit, Model: "opus"},
		{Kind: EventMessageDelta, Model: "opus", Text: "hello "},
		{Kind: EventMessageCompleted, Text: "hello world"},
		{Kind: EventUsage, Model: "opus", InputTokens: 3, OutputTokens: 4, CostUSD: 0.2},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if message.Role != "assistant" || message.Content != "hello world" ||
		meta.Model != "opus" || meta.PromptTokens != 3 || meta.CompletionTokens != 4 ||
		meta.Cost != 0.2 || meta.Elapsed != time.Second {
		t.Fatalf("message=%+v meta=%+v", message, meta)
	}
}

func TestCollectReturnsTranslatedProviderError(t *testing.T) {
	_, _, err := Collect([]Event{{Kind: EventError, Error: "login required"}}, 0)
	if err == nil || !strings.Contains(err.Error(), "login required") {
		t.Fatalf("error = %v", err)
	}
}
