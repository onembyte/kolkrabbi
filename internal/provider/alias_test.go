package provider_test

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestResolveModelAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sonnet", "anthropic/claude-3-7-sonnet"},
		{"claude", "anthropic/claude-3-7-sonnet"},
		{"haiku", "anthropic/claude-3-5-haiku"},
		{"opus", "anthropic/claude-3-opus"},
		{"gpt", "openai/gpt-4o"},
		{"gpt-4o", "openai/gpt-4o"},
		{"mini", "openai/gpt-4o-mini"},
		{"gpt-4o-mini", "openai/gpt-4o-mini"},
		{"o3", "openai/o3-mini"},
		{"o3-mini", "openai/o3-mini"},
		{"flash", "google/gemini-2.5-flash"},
		{"gemini", "google/gemini-2.5-flash"},
		{"pro", "google/gemini-2.5-pro"},
		{"deepseek", "deepseek/deepseek-r1"},
		{"r1", "deepseek/deepseek-r1"},
		{"coder", "qwen/qwen-2.5-coder-32b-instruct"},
		{"free", "openrouter/free"},
		{"auto", "openrouter/auto"},
		// Passthrough for full vendor IDs or unrecognized names
		{"meta-llama/llama-3.3-70b-instruct", "meta-llama/llama-3.3-70b-instruct"},
		{"custom/model-id", "custom/model-id"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := provider.ResolveModelAlias(tt.input)
			if got != tt.want {
				t.Errorf("ResolveModelAlias(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
