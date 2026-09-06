package engine

import (
	"encoding/json"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// publishLimit puts one classified limit and the action taken on it on the bus
// (plan 35 §2.1). One event per decision: the TUI, the streams and any future
// dashboard learn what stopped, what it is keyed on, when it lifts, and what
// kolk did -- the same facts the transcript line states, in a shape a program
// can read. Nothing is persisted here; the owner asked that switches be shown,
// not stored.
func (a *Agent) publishLimit(limit provider.Limit, action string) {
	if a.Bus == nil || a.lastTurnID == "" {
		return
	}
	data := protocol.ProviderLimitData{
		Kind: string(limit.Kind), Scope: string(limit.Scope), Action: action,
		Model: limit.Model, Connector: limit.Connector, Message: limit.Message, Source: limit.Source,
	}
	if !limit.ResetAt.IsZero() {
		data.ResetAt = limit.ResetAt.UTC().Format(time.RFC3339)
	}
	if limit.RetryAfter > 0 {
		data.RetryAfterMs = limit.RetryAfter.Milliseconds()
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: protocol.EventProviderLimit, Data: encoded})
}
