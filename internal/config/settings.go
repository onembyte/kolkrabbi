package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Setting is one row of `kolk config`: the key a person types, the value in
// effect, and what it does.
//
// Value is what the session will actually use, not what the file happens to
// contain — an unset key reports the default it inherits. A settings list that
// shows blanks for everything a user has not touched answers the wrong
// question: they want to know what kolk is doing, not what they typed.
type Setting struct {
	Key     string
	Value   string
	Default bool // true when the value is inherited rather than configured
	Summary string
}

// settingRows are the fixed settings, in display order. Map-valued sections
// (tiers, slots) expand after these, because their keys depend on the file.
func (c *Config) settingRows(defaultModel, defaultBaseURL string) []Setting {
	if c == nil {
		c = &Config{}
	}
	text := func(value, fallback string) (string, bool) {
		if strings.TrimSpace(value) == "" {
			return fallback, true
		}
		return value, false
	}

	model, modelDefault := text(c.Model, defaultModel)
	baseURL, baseURLDefault := text(c.BaseURL, defaultBaseURL)
	mode, modeDefault := text(c.Mode, "code")
	onLimit, onLimitDefault := text(c.Routing.OnSubscriptionLimit, "ask")
	onFree, onFreeDefault := text(c.Routing.OnFreeExhausted, "free")
	effort, effortDefault := text(c.Effort, "medium")

	cost, costDefault := "no ceiling", true
	if c.MaxRunCostUSD > 0 {
		cost, costDefault = fmt.Sprintf("$%.2f", c.MaxRunCostUSD), false
	}
	tasks, tasksDefault := "3", true
	if c.MaxConcurrentTasks > 0 {
		tasks, tasksDefault = strconv.Itoa(c.MaxConcurrentTasks), false
	}

	return []Setting{
		{"model", model, modelDefault, "the model a new session starts on"},
		{"mode", mode, modeDefault, "chat = no tools · code = tool loop · agent = orchestrated"},
		{"effort", effort, effortDefault, "model tier, tool-round limit and orchestration width"},
		{"base_url", baseURL, baseURLDefault, "any OpenAI-compatible endpoint (Ollama, LiteLLM, vLLM)"},
		{"auto_restart_after_update", onOff(c.AutoRestartAfterUpdate), c.AutoRestartAfterUpdate == nil,
			"restart into the new version after `kolk update`, keeping the session"},
		{"max_run_cost_usd", cost, costDefault, "stop an orchestrated run once it has cost this much"},
		{"max_concurrent_tasks", tasks, tasksDefault, "how many orchestrated tasks may run at once"},
		{"routing.on_subscription_limit", onLimit, onLimitDefault,
			"when a subscription runs out: ask · switch to a metered model · stop"},
		{"routing.on_free_exhausted", onFree, onFreeDefault,
			"when no free model can serve: free (stay free) · paid · stop"},
	}
}

// Settings lists every setting with the value in effect, including the
// per-effort tiers and per-role slots the file defines.
func (c *Config) Settings(defaultModel, defaultBaseURL string) []Setting {
	rows := c.settingRows(defaultModel, defaultBaseURL)
	rows = append(rows, mapSettings("effort.", c.Tiers, "model used at this effort")...)
	rows = append(rows, mapSettings("slot.", c.Slots, "model used for this orchestration role")...)
	rows = append(rows, c.Local.settings()...)
	return rows
}

func mapSettings(prefix string, values map[string]string, summary string) []Setting {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]Setting, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, Setting{Key: prefix + key, Value: values[key], Summary: summary})
	}
	return rows
}

func onOff(value *bool) string {
	if value != nil && *value {
		return "on"
	}
	return "off"
}

// ParseOnOff reads the two words a toggle accepts, plus the spellings people
// reach for first.
func ParseOnOff(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "yes", "1", "enabled":
		return true, nil
	case "off", "false", "no", "0", "disabled":
		return false, nil
	}
	return false, fmt.Errorf("expected on or off, got %q", raw)
}
