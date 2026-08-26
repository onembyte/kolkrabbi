package provider

import "strings"

// StandardModelAliases maps friendly user shorthands to canonical provider model IDs.
var StandardModelAliases = map[string]string{
	"sonnet":      "anthropic/claude-3-7-sonnet",
	"claude":      "anthropic/claude-3-7-sonnet",
	"haiku":       "anthropic/claude-3-5-haiku",
	"opus":        "anthropic/claude-3-opus",
	"gpt":         "openai/gpt-4o",
	"gpt-4o":      "openai/gpt-4o",
	"mini":        "openai/gpt-4o-mini",
	"gpt-4o-mini": "openai/gpt-4o-mini",
	"o3":          "openai/o3-mini",
	"o3-mini":     "openai/o3-mini",
	"flash":       "google/gemini-2.5-flash",
	"gemini":      "google/gemini-2.5-flash",
	"pro":         "google/gemini-2.5-pro",
	"deepseek":    "deepseek/deepseek-r1",
	"r1":          "deepseek/deepseek-r1",
	"coder":       "qwen/qwen-2.5-coder-32b-instruct",
	"free":        "openrouter/free",
	"auto":        "openrouter/auto",
}

// ResolveModelAlias resolves a friendly model alias to its canonical model ID.
// If the input is not a recognized alias, it returns the input unchanged.
func ResolveModelAlias(alias string) string {
	cleaned := strings.ToLower(strings.TrimSpace(alias))
	if target, ok := StandardModelAliases[cleaned]; ok {
		return target
	}
	return strings.TrimSpace(alias)
}
