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
	resume, resumeDefault := text(c.Continuity.Resume, "auto")
	theme, themeDefault := text(c.Theme, "kolkrabbi")
	effective := c.EffectiveContinuity()
	continuityMode, continuityModeDefault := text(c.Continuity.Mode, effective.Mode)
	selection, selectionDefault := text(c.Continuity.Select, "auto")
	preferred, preferredDefault := strings.Join(c.Continuity.Preferred, ", "), len(c.Continuity.Preferred) == 0
	if preferredDefault {
		preferred = "(none)"
	}
	order, orderDefault := strings.Join(effective.Order, ", "), len(c.Continuity.Order) == 0
	effort, effortDefault := text(c.Effort, "medium")

	cost, costDefault := "no ceiling", true
	if c.MaxRunCostUSD > 0 {
		cost, costDefault = fmt.Sprintf("$%.2f", c.MaxRunCostUSD), false
	}
	tasks, tasksDefault := "3", true
	if c.MaxConcurrentTasks > 0 {
		tasks, tasksDefault = strconv.Itoa(c.MaxConcurrentTasks), false
	}
	network, networkDefault := text(c.SubagentNetwork, "auto")
	sandbox, sandboxDefault := text(c.Sandbox, "off")

	return []Setting{
		{"model", model, modelDefault, "the model a new session starts on"},
		{"mode", mode, modeDefault, "chat = no tools · code = tool loop · agent = orchestrated"},
		{"effort", effort, effortDefault, "model tier, tool-round limit and orchestration width"},
		{"base_url", baseURL, baseURLDefault, "any OpenAI-compatible endpoint (Ollama, LiteLLM, vLLM); used without a key unless it is openrouter.ai"},
		{"auto_restart_after_update", onOff(c.AutoRestartAfterUpdate), c.AutoRestartAfterUpdate == nil,
			"restart into the new version after `/update`, keeping the session"},
		{"max_run_cost_usd", cost, costDefault, "stop an orchestrated run once it has cost this much"},
		{"max_concurrent_tasks", tasks, tasksDefault, "how many orchestrated tasks may run at once"},
		{"subagent_network", network, networkDefault,
			"network for orchestrated children: auto (research tasks; claude has no switch) · on · off (strict)"},
		{"sandbox", sandbox, sandboxDefault,
			"confine bash commands to the project and temp (OS sandbox): on · off; /sandbox switches it for the session"},
		{"routing.on_subscription_limit", onLimit, onLimitDefault,
			"deprecated, an alias of continuity.mode (switch = on, stop = off); removed in the release after this one"},
		{"routing.on_free_exhausted", onFree, onFreeDefault,
			"deprecated, an alias of continuity.order (paid = paid before free); removed in the release after this one"},
		{"continuity.mode", continuityMode, continuityModeDefault,
			"when the model behind the session hits a limit: off (pause, resume on the same model) · on (walk the chain)"},
		{"continuity.select", selection, selectionDefault,
			"which chain, when mode is on: auto (equivalents, in continuity.order) · preferred (your list, as written) · ask (a question, once per run)"},
		{"continuity.preferred", preferred, preferredDefault,
			"your own models to continue on, plan-qualified or bare, comma-separated; the only way a free model joins a chain"},
		{"continuity.order", order, orderDefault,
			"the groups to try, in order: subscription · paid · free"},
		{"theme", theme, themeDefault, "the terminal look: kolkrabbi · nord · quiet (appearance only; NO_COLOR still wins); /theme tries one for the session"},
		{"continuity.resume", resume, resumeDefault,
			"a session paused on a limit: auto (comes back when the limit lifts, no tokens spent) · manual (waits for /resume)"},
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
