package provider

import "strings"

// subscriptionModelAliases are deliberately qualified by the product tier.
// `pro` already means Gemini Pro, so reusing it for ChatGPT Pro would make a
// one-word shortcut change providers depending on which backend is enabled.
var subscriptionModelAliases = map[string]string{
	"claude-pro":     "claude-sonnet",
	"claude-max":     "claude-opus",
	"gpt-plus":       "ChatGPT Plus/gpt-5.6-sol",
	"gpt-plus-sol":   "ChatGPT Plus/gpt-5.6-sol",
	"gpt-plus-terra": "ChatGPT Plus/gpt-5.6-terra",
	"gpt-plus-luna":  "ChatGPT Plus/gpt-5.6-luna",
	"gpt-pro":        "ChatGPT Pro/gpt-5.6-pro",
	"gpt-pro-sol":    "ChatGPT Pro/gpt-5.6-sol",
	"gpt-pro-terra":  "ChatGPT Pro/gpt-5.6-terra",
	"gpt-pro-luna":   "ChatGPT Pro/gpt-5.6-luna",
	"terra":          "gpt-5.6-terra",
	"luna":           "gpt-5.6-luna",
}

var subscriptionModelAliasOrder = []string{
	"claude-pro", "claude-max", "gpt-plus", "gpt-plus-sol", "gpt-plus-terra", "gpt-plus-luna",
	"gpt-pro", "gpt-pro-sol", "gpt-pro-terra", "gpt-pro-luna", "terra", "luna",
}

// StandardModelAliases maps friendly user shorthands to canonical provider
// model IDs or, for subscription aliases, a plan-qualified model reference.
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
	"claude-pro":  "claude-sonnet",
	"claude-max":  "claude-opus",
	"gpt-plus":    "ChatGPT Plus/gpt-5.6-sol",
	"gpt-pro":     "ChatGPT Pro/gpt-5.6-pro",
	"terra":       "gpt-5.6-terra",
	"luna":        "gpt-5.6-luna",
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

// SubscriptionModelShortcutFor returns the concise, plan-qualified shortcut
// for one catalog row. Shared model ids need this plan context; otherwise the
// picker could print a shortcut that resolves to the wrong subscription.
func SubscriptionModelShortcutFor(plan, model string) string {
	plan = strings.ToLower(strings.TrimSpace(plan))
	model = strings.ToLower(strings.TrimSpace(model))
	for _, alias := range subscriptionModelAliasOrder {
		qualifier, target := subscriptionTarget(subscriptionModelAliases[alias])
		if target != model || (qualifier != "" && qualifier != plan) {
			continue
		}
		return alias
	}
	return ""
}

func subscriptionTarget(target string) (qualifier, model string) {
	target = strings.ToLower(strings.TrimSpace(target))
	if plan, rest, ok := strings.Cut(target, "/"); ok {
		return plan, rest
	}
	return "", target
}
