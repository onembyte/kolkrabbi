package protocol

import (
	"encoding/json"
	"fmt"
)

// EventType is the language-neutral event name carried in Envelope.Type.
// Decoders retain syntactically valid unknown values for forward compatibility.
type EventType string

const (
	// EventHello announces the protocol, server identity, and capabilities.
	EventHello EventType = "hello"
	// EventMessageDelta carries display-ready assistant text as it streams.
	EventMessageDelta EventType = "message.delta"
	// EventMessageCompleted carries the authoritative final assistant-text snapshot.
	EventMessageCompleted EventType = "message.completed"
	// EventReasoningDelta carries display-ready reasoning text as it streams.
	EventReasoningDelta EventType = "reasoning.delta"
	// EventToolRequested announces one complete tool invocation.
	EventToolRequested EventType = "tool.requested"
	// EventToolStarted announces that one requested invocation began execution.
	EventToolStarted EventType = "tool.started"
	// EventToolOutput carries the complete display-ready output of one invocation.
	EventToolOutput EventType = "tool.output"
	// EventToolFinished carries one invocation's terminal outcome.
	EventToolFinished EventType = "tool.finished"
	// EventPermissionRequested asks a client to decide one Kolkrabbi-run action.
	EventPermissionRequested EventType = "permission.requested"
	// EventPermissionResolved records the decision for one permission request.
	EventPermissionResolved EventType = "permission.resolved"
	// EventSubagentStarted announces one child turn owned by a parent turn.
	EventSubagentStarted EventType = "subagent.started"
	// EventSubagentFinished records one child turn's terminal outcome.
	EventSubagentFinished EventType = "subagent.finished"
	// EventWorkUpdated records one observed main-agent or subagent work step.
	EventWorkUpdated EventType = "work.updated"
	// EventUsageReported carries one model row for one physical attempt.
	EventUsageReported EventType = "usage.reported"
	// EventScoreRecorded carries one typed evaluation of a protocol target.
	EventScoreRecorded EventType = "score.recorded"
	// EventCheckpointCreated announces one durable pre-write snapshot entry.
	EventCheckpointCreated EventType = "checkpoint.created"
	// EventError carries one terminal failure using the shared error entity.
	EventError EventType = "error"
	// EventLog carries one structured non-error diagnostic.
	EventLog EventType = "log"
	// EventSessionStarted announces the initial live-session projection.
	EventSessionStarted EventType = "session.started"
	// EventSessionUpdated carries a non-empty patch to the live-session projection.
	EventSessionUpdated EventType = "session.updated"
	// EventSessionEnded announces why a live session ended.
	EventSessionEnded EventType = "session.ended"
	// EventTurnStarted records the request projection used to begin a turn.
	EventTurnStarted EventType = "turn.started"
	// EventTurnFinished records why a turn completed.
	EventTurnFinished EventType = "turn.finished"
	// EventTurnCancelled records why a turn was cancelled.
	EventTurnCancelled EventType = "turn.cancelled"
	// EventProviderLimit records one classified limit a model hit and what kolk
	// did about it (plan 35 §2.1).
	EventProviderLimit EventType = "provider.limit"
)

var knownEventTypes = []EventType{
	EventHello,
	EventMessageDelta,
	EventMessageCompleted,
	EventReasoningDelta,
	EventToolRequested,
	EventToolStarted,
	EventToolOutput,
	EventToolFinished,
	EventPermissionRequested,
	EventPermissionResolved,
	EventSubagentStarted,
	EventSubagentFinished,
	EventWorkUpdated,
	EventUsageReported,
	EventScoreRecorded,
	EventCheckpointCreated,
	EventError,
	EventLog,
	EventSessionStarted,
	EventSessionUpdated,
	EventSessionEnded,
	EventTurnStarted,
	EventTurnFinished,
	EventTurnCancelled,
	EventProviderLimit,
}

// KnownEventTypes returns the ordered event vocabulary shipped by this
// protocol binding. The returned slice is a copy and may be modified freely.
func KnownEventTypes() []EventType {
	return append([]EventType(nil), knownEventTypes...)
}

// HelloData is the payload of EventHello and the future /v1/hello response.
type HelloData struct {
	Protocol     string   `json:"protocol"`
	Server       string   `json:"server"`
	Capabilities []string `json:"capabilities"`
}

// MessageDeltaData is the payload of EventMessageDelta.
type MessageDeltaData struct {
	Text string `json:"text"`
}

// MessageCompletedData is the payload of EventMessageCompleted.
type MessageCompletedData struct {
	Text string `json:"text"`
}

// ReasoningDeltaData is the payload of EventReasoningDelta.
type ReasoningDeltaData struct {
	Text string `json:"text"`
}

// ToolExecutor identifies who executes a requested tool.
type ToolExecutor string

const (
	// ToolExecutorKolk routes the invocation through Kolkrabbi's tool boundary.
	ToolExecutorKolk ToolExecutor = "kolk"
	// ToolExecutorProvider reports an invocation the backend already executed.
	ToolExecutorProvider ToolExecutor = "provider"
)

func validToolExecutor(executor ToolExecutor) bool {
	return executor == ToolExecutorKolk || executor == ToolExecutorProvider
}

// ToolRequestedData is the payload of EventToolRequested. Arguments retains
// the provider's complete JSON text without normalization.
type ToolRequestedData struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Arguments string       `json:"arguments"`
	Executor  ToolExecutor `json:"executor"`
	TaskID    string       `json:"task_id,omitempty"`
	ChildTurn string       `json:"child_turn,omitempty"`
}

// ToolStartedData is the payload of EventToolStarted.
type ToolStartedData struct {
	ID        string       `json:"id"`
	Executor  ToolExecutor `json:"executor"`
	TaskID    string       `json:"task_id,omitempty"`
	ChildTurn string       `json:"child_turn,omitempty"`
}

// ToolOutputData is the payload of EventToolOutput. Output may be empty when
// the tool completed without display text.
type ToolOutputData struct {
	ID        string       `json:"id"`
	Output    string       `json:"output"`
	Executor  ToolExecutor `json:"executor"`
	TaskID    string       `json:"task_id,omitempty"`
	ChildTurn string       `json:"child_turn,omitempty"`
}

// ToolFinishedData is the payload of EventToolFinished. OK reports whether
// the invocation produced a valid tool result.
type ToolFinishedData struct {
	ID        string       `json:"id"`
	OK        bool         `json:"ok"`
	Executor  ToolExecutor `json:"executor"`
	TaskID    string       `json:"task_id,omitempty"`
	ChildTurn string       `json:"child_turn,omitempty"`
}

// PermissionRequestedData is the payload of EventPermissionRequested. Diff is
// omitted when the action has no separate diff preview.
type PermissionRequestedData struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`
	Detail string `json:"detail"`
	Diff   string `json:"diff,omitempty"`
}

// PermissionDecision is the closed decision vocabulary for a permission
// round-trip.
type PermissionDecision string

const (
	// PermissionDecisionAllow approves only the correlated request.
	PermissionDecisionAllow PermissionDecision = "allow"
	// PermissionDecisionAllowSession approves with session-scoped retention.
	PermissionDecisionAllowSession PermissionDecision = "allow_session"
	// PermissionDecisionDeny rejects the correlated request.
	PermissionDecisionDeny PermissionDecision = "deny"
)

func validPermissionDecision(decision PermissionDecision) bool {
	return decision == PermissionDecisionAllow ||
		decision == PermissionDecisionAllowSession ||
		decision == PermissionDecisionDeny
}

// PermissionResolvedData is the payload of EventPermissionResolved. Reason is
// optional and explains decisions such as a timeout-driven deny.
type PermissionResolvedData struct {
	ID       string             `json:"id"`
	Decision PermissionDecision `json:"decision"`
	Reason   string             `json:"reason,omitempty"`
}

// SubagentStartedData correlates a parent turn with one child task and turn.
// Index and Total are 1-based presentation coordinates, not scheduler state.
type SubagentStartedData struct {
	ID        string `json:"id"`
	ChildTurn string `json:"child_turn"`
	Task      string `json:"task"`
	Mode      string `json:"mode"`
	Index     int    `json:"index"`
	Total     int    `json:"total"`
	// Level is how much capability the planner judged this task to need, and
	// Model is the rung that answer resolved to. Both omitted when unstated: a
	// planner that says nothing and a run with no ladder are different from a
	// task that was judged trivial, and an empty string would flatten them.
	Level string `json:"level,omitempty"`
	Model string `json:"model,omitempty"`
}

// SubagentFinishedData records the outcome of one correlated child turn. The
// child turn's completed message and diagnostics own result and error text.
type SubagentFinishedData struct {
	ID        string `json:"id"`
	ChildTurn string `json:"child_turn"`
	Mode      string `json:"mode"`
	OK        bool   `json:"ok"`
	// Model is the rung that actually ran it, which is not always the rung it
	// started on: a cheaper one that would not spawn falls back to the ceiling.
	Model string `json:"model,omitempty"`
}

// WorkRole identifies which execution scope owns one observed work step.
type WorkRole string

const (
	WorkRoleMain     WorkRole = "main"
	WorkRoleSubagent WorkRole = "subagent"
)

func validWorkRole(role WorkRole) bool {
	return role == WorkRoleMain || role == WorkRoleSubagent
}

// WorkState is observed task state, never an estimated completion value.
type WorkState string

const (
	WorkStateQueued  WorkState = "queued"
	WorkStateWaiting WorkState = "waiting"
	WorkStateWorking WorkState = "working"
	WorkStateDone    WorkState = "done"
	WorkStateFailed  WorkState = "failed"
	WorkStateBlocked WorkState = "blocked"
)

func validWorkState(state WorkState) bool {
	switch state {
	case WorkStateQueued, WorkStateWaiting, WorkStateWorking,
		WorkStateDone, WorkStateFailed, WorkStateBlocked:
		return true
	default:
		return false
	}
}

// WorkPhase says which broad execution boundary owns the observed step.
type WorkPhase string

const (
	WorkPhasePlanning   WorkPhase = "planning"
	WorkPhaseSchedule   WorkPhase = "schedule"
	WorkPhaseProvider   WorkPhase = "provider"
	WorkPhaseTool       WorkPhase = "tool"
	WorkPhaseCheckpoint WorkPhase = "checkpoint"
	WorkPhaseSynthesis  WorkPhase = "synthesis"
	WorkPhaseComplete   WorkPhase = "complete"
)

func validWorkPhase(phase WorkPhase) bool {
	switch phase {
	case WorkPhasePlanning, WorkPhaseSchedule, WorkPhaseProvider, WorkPhaseTool,
		WorkPhaseCheckpoint, WorkPhaseSynthesis, WorkPhaseComplete:
		return true
	default:
		return false
	}
}

// WorkUpdatedData is one ordered, durable step. Main work uses the parent turn
// as ID and omits child coordinates. Subagent work uses its task ID and repeats
// child-turn/index/total correlation so a standalone journal row is useful.
type WorkUpdatedData struct {
	ID        string    `json:"id"`
	ChildTurn string    `json:"child_turn,omitempty"`
	Role      WorkRole  `json:"role"`
	State     WorkState `json:"state"`
	Phase     WorkPhase `json:"phase"`
	Step      string    `json:"step"`
	Sequence  uint64    `json:"sequence"`
	Index     int       `json:"index,omitempty"`
	Total     int       `json:"total,omitempty"`
	Model     string    `json:"model,omitempty"`
	Effort    string    `json:"effort,omitempty"`
}

// CheckpointCreatedData identifies one durable pre-write snapshot without
// exposing backup storage details or file content.
type CheckpointCreatedData struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Tool    string `json:"tool"`
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
}

// LogLevel is the closed severity vocabulary for non-error diagnostics.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
)

func validLogLevel(level LogLevel) bool {
	return level == LogLevelDebug || level == LogLevelInfo || level == LogLevelWarn
}

// LogCode is the closed machine-readable diagnostic vocabulary.
type LogCode string

const (
	LogCodeToolsDropped      LogCode = "tools_dropped"
	LogCodeToolsUnverified   LogCode = "tools_unverified"
	LogCodeModelIgnored      LogCode = "model_ignored"
	LogCodeModelRotated      LogCode = "model_rotated"
	LogCodeEffortClamped     LogCode = "effort_clamped"
	LogCodeEffortUnsupported LogCode = "effort_unsupported"
	LogCodeCacheUnsupported  LogCode = "cache_unsupported"
	LogCodeHistoryTruncated  LogCode = "history_truncated"
	LogCodeHistoryLost       LogCode = "history_lost"
	LogCodeFallbackIgnored   LogCode = "fallback_ignored"
	LogCodeUsageUnavailable  LogCode = "usage_unavailable"
	LogCodeCostUnavailable   LogCode = "cost_unavailable"
	LogCodeParamDropped      LogCode = "param_dropped"
	LogCodeToolCallTruncated LogCode = "tool_call_truncated"
	LogCodeToolIDRewritten   LogCode = "tool_id_rewritten"
	LogCodeDeltasDropped     LogCode = "deltas_dropped"
)

func validLogCode(code LogCode) bool {
	switch code {
	case LogCodeToolsDropped, LogCodeToolsUnverified, LogCodeModelIgnored, LogCodeModelRotated,
		LogCodeEffortClamped, LogCodeEffortUnsupported, LogCodeCacheUnsupported,
		LogCodeHistoryTruncated, LogCodeHistoryLost, LogCodeFallbackIgnored,
		LogCodeUsageUnavailable, LogCodeCostUnavailable, LogCodeParamDropped,
		LogCodeToolCallTruncated, LogCodeToolIDRewritten, LogCodeDeltasDropped:
		return true
	default:
		return false
	}
}

// LogData is one structured non-error diagnostic. Was and Became describe a
// transition only when Field names the affected request or runtime field.
type LogData struct {
	Level   LogLevel `json:"level"`
	Code    LogCode  `json:"code"`
	Field   string   `json:"field,omitempty"`
	Was     string   `json:"was,omitempty"`
	Became  string   `json:"became,omitempty"`
	Message string   `json:"message,omitempty"`
}

// SessionStartedData is the payload of EventSessionStarted.
type SessionStartedData struct {
	Model  string `json:"model"`
	Mode   string `json:"mode"`
	Effort string `json:"effort"`
	CWD    string `json:"cwd"`
}

// SessionUpdatedData is the payload of EventSessionUpdated. At least one
// known or future field must be present in its wire object.
type SessionUpdatedData struct {
	Model  string `json:"model,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Effort string `json:"effort,omitempty"`
	Title  string `json:"title,omitempty"`
}

// SessionEndedData is the payload of EventSessionEnded.
type SessionEndedData struct {
	Reason string `json:"reason"`
}

// TurnStartedData is the payload of EventTurnStarted.
type TurnStartedData struct {
	Input  string `json:"input"`
	Model  string `json:"model"`
	Mode   string `json:"mode"`
	Effort string `json:"effort"`
}

// TurnFinishedData is the payload of EventTurnFinished. RawReason preserves an
// optional provider-specific finish reason without restricting future values.
type TurnFinishedData struct {
	Reason    string `json:"reason"`
	RawReason string `json:"raw_reason,omitempty"`
}

// TurnCancelledData is the payload of EventTurnCancelled.
type TurnCancelledData struct {
	Reason string `json:"reason"`
}

// ProviderLimitData is the payload of EventProviderLimit: which limit, keyed on
// what, when it lifts if known, and the action kolk took. Kind, scope and
// action are closed vocabularies; the message is already scrubbed.
type ProviderLimitData struct {
	Kind         string `json:"kind"`
	Scope        string `json:"scope"`
	Action       string `json:"action"`
	Model        string `json:"model,omitempty"`
	Connector    string `json:"connector,omitempty"`
	ResetAt      string `json:"reset_at,omitempty"`       // RFC 3339, UTC; absent when unknown
	RetryAfterMs int64  `json:"retry_after_ms,omitempty"` // the provider's own answer, when given
	Message      string `json:"message,omitempty"`
	Source       string `json:"source,omitempty"`
}

var providerLimitKinds = map[string]bool{"subscription_allowance": true, "account_quota": true, "endpoint_capacity": true, "budget_stop": true, "model_refusal": true, "transport": true}
var providerLimitScopes = map[string]bool{"model": true, "account": true, "endpoint": true}
var providerLimitActions = map[string]bool{"retry": true, "rotate": true, "recommend": true, "ask": true, "switch": true, "pause": true, "resume": true, "stop": true}

func validateEventData(event EventType, raw json.RawMessage) error {
	var text string
	switch event {
	case EventHello:
		var data HelloData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Protocol != Version {
			return fmt.Errorf("protocol: %s data.protocol must be %q", event, Version)
		}
		if data.Server == "" {
			return fmt.Errorf("protocol: %s data.server must be non-empty", event)
		}
		if data.Capabilities == nil {
			return fmt.Errorf("protocol: %s data.capabilities must be an array", event)
		}
		seen := make(map[string]struct{}, len(data.Capabilities))
		for _, capability := range data.Capabilities {
			if capability == "" {
				return fmt.Errorf("protocol: %s capability names must be non-empty", event)
			}
			if _, duplicate := seen[capability]; duplicate {
				return fmt.Errorf("protocol: %s capability %q is duplicated", event, capability)
			}
			seen[capability] = struct{}{}
		}
		return nil
	case EventMessageDelta:
		var data MessageDeltaData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		text = data.Text
	case EventMessageCompleted:
		var data struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Text == nil {
			return fmt.Errorf("protocol: %s data.text must be present and string-valued", event)
		}
		return nil
	case EventReasoningDelta:
		var data ReasoningDeltaData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		text = data.Text
	case EventToolRequested:
		var data ToolRequestedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "id", value: data.ID},
			{name: "name", value: data.Name},
			{name: "arguments", value: data.Arguments},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		if !json.Valid([]byte(data.Arguments)) {
			return fmt.Errorf("protocol: %s data.arguments must contain valid JSON", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return validateToolWorkCorrelation(event, data.TaskID, data.ChildTurn)
	case EventToolStarted:
		var data ToolStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return validateToolWorkCorrelation(event, data.TaskID, data.ChildTurn)
	case EventToolOutput:
		var data struct {
			ID        string       `json:"id"`
			Output    *string      `json:"output"`
			Executor  ToolExecutor `json:"executor"`
			TaskID    string       `json:"task_id"`
			ChildTurn string       `json:"child_turn"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if data.Output == nil {
			return fmt.Errorf("protocol: %s data.output must be present and string-valued", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return validateToolWorkCorrelation(event, data.TaskID, data.ChildTurn)
	case EventToolFinished:
		var data struct {
			ID        string       `json:"id"`
			OK        *bool        `json:"ok"`
			Executor  ToolExecutor `json:"executor"`
			TaskID    string       `json:"task_id"`
			ChildTurn string       `json:"child_turn"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if data.OK == nil {
			return fmt.Errorf("protocol: %s data.ok must be present and boolean-valued", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return validateToolWorkCorrelation(event, data.TaskID, data.ChildTurn)
	case EventPermissionRequested:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		var data struct {
			ID     string  `json:"id"`
			Tool   string  `json:"tool"`
			Detail string  `json:"detail"`
			Diff   *string `json:"diff"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "id", value: data.ID},
			{name: "tool", value: data.Tool},
			{name: "detail", value: data.Detail},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		if _, present := fields["diff"]; present && data.Diff == nil {
			return fmt.Errorf("protocol: %s data.diff must be string-valued when present", event)
		}
		return nil
	case EventPermissionResolved:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		var data PermissionResolvedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if !validPermissionDecision(data.Decision) {
			return fmt.Errorf("protocol: %s data.decision must be %q, %q, or %q", event,
				PermissionDecisionAllow, PermissionDecisionAllowSession, PermissionDecisionDeny)
		}
		if _, present := fields["reason"]; present && data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty and string-valued when present", event)
		}
		return nil
	case EventSubagentStarted:
		var data SubagentStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if err := validateSubagentCorrelation(event, data.ID, data.ChildTurn, data.Mode); err != nil {
			return err
		}
		if data.Task == "" {
			return fmt.Errorf("protocol: %s data.task must be non-empty", event)
		}
		if data.Index < 1 {
			return fmt.Errorf("protocol: %s data.index must be at least 1", event)
		}
		if data.Total < 1 {
			return fmt.Errorf("protocol: %s data.total must be at least 1", event)
		}
		if data.Index > data.Total {
			return fmt.Errorf("protocol: %s data.index must not exceed data.total", event)
		}
		return nil
	case EventSubagentFinished:
		var data struct {
			ID        string `json:"id"`
			ChildTurn string `json:"child_turn"`
			Mode      string `json:"mode"`
			OK        *bool  `json:"ok"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if err := validateSubagentCorrelation(event, data.ID, data.ChildTurn, data.Mode); err != nil {
			return err
		}
		if data.OK == nil {
			return fmt.Errorf("protocol: %s data.ok must be present and boolean-valued", event)
		}
		return nil
	case EventWorkUpdated:
		var data WorkUpdatedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if !validWorkRole(data.Role) {
			return fmt.Errorf("protocol: %s data.role is not defined", event)
		}
		if !validWorkState(data.State) {
			return fmt.Errorf("protocol: %s data.state is not defined", event)
		}
		if !validWorkPhase(data.Phase) {
			return fmt.Errorf("protocol: %s data.phase is not defined", event)
		}
		terminal := data.State == WorkStateDone || data.State == WorkStateFailed || data.State == WorkStateBlocked
		if terminal != (data.Phase == WorkPhaseComplete) {
			return fmt.Errorf("protocol: %s data.state %q is incompatible with phase %q", event, data.State, data.Phase)
		}
		if data.Step == "" {
			return fmt.Errorf("protocol: %s data.step must be non-empty", event)
		}
		if data.Sequence < 1 {
			return fmt.Errorf("protocol: %s data.sequence must be at least 1", event)
		}
		switch data.Role {
		case WorkRoleMain:
			if !validID(data.ID, 't') {
				return fmt.Errorf("protocol: %s main data.id must be a canonical t_ ULID", event)
			}
			if data.ChildTurn != "" || data.Index != 0 || data.Total != 0 {
				return fmt.Errorf("protocol: %s main work must omit child correlation", event)
			}
		case WorkRoleSubagent:
			if !validID(data.ID, 'k') {
				return fmt.Errorf("protocol: %s subagent data.id must be a canonical k_ ULID", event)
			}
			if !validID(data.ChildTurn, 't') {
				return fmt.Errorf("protocol: %s subagent data.child_turn must be a canonical t_ ULID", event)
			}
			if data.Index < 1 || data.Total < 1 || data.Index > data.Total {
				return fmt.Errorf("protocol: %s subagent index/total must be valid one-based coordinates", event)
			}
		}
		return nil
	case EventUsageReported:
		if err := validateUsageEntity(raw); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		return nil
	case EventScoreRecorded:
		if err := validateScoreEntity(raw); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		return nil
	case EventCheckpointCreated:
		var data struct {
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Tool    string `json:"tool"`
			Path    string `json:"path"`
			Existed *bool  `json:"existed"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "id", value: data.ID},
			{name: "reason", value: data.Reason},
			{name: "tool", value: data.Tool},
			{name: "path", value: data.Path},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		if data.Existed == nil {
			return fmt.Errorf("protocol: %s data.existed must be present and boolean-valued", event)
		}
		return nil
	case EventError:
		if err := validateErrorEntity(raw); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		return nil
	case EventLog:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		var data LogData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if !validLogLevel(data.Level) {
			return fmt.Errorf("protocol: %s data.level is not defined", event)
		}
		if !validLogCode(data.Code) {
			return fmt.Errorf("protocol: %s data.code is not defined", event)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "field", value: data.Field},
			{name: "was", value: data.Was},
			{name: "became", value: data.Became},
			{name: "message", value: data.Message},
		} {
			if _, present := fields[field.name]; present && field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty and string-valued when present", event, field.name)
			}
		}
		_, fieldPresent := fields["field"]
		if _, present := fields["was"]; present && !fieldPresent {
			return fmt.Errorf("protocol: %s data.was requires data.field", event)
		}
		if _, present := fields["became"]; present && !fieldPresent {
			return fmt.Errorf("protocol: %s data.became requires data.field", event)
		}
		return nil
	case EventSessionStarted:
		var data SessionStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
			{name: "cwd", value: data.CWD},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventSessionUpdated:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if len(fields) == 0 {
			return fmt.Errorf("protocol: %s data must contain at least one field", event)
		}
		var data SessionUpdatedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
			{name: "title", value: data.Title},
		} {
			if _, present := fields[field.name]; present && field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventSessionEnded:
		var data SessionEndedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		return nil
	case EventTurnStarted:
		var data TurnStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "input", value: data.Input},
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventTurnFinished:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		var data TurnFinishedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		if _, present := fields["raw_reason"]; present && data.RawReason == "" {
			return fmt.Errorf("protocol: %s data.raw_reason must be non-empty when present", event)
		}
		return nil
	case EventTurnCancelled:
		var data TurnCancelledData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		return nil
	case EventProviderLimit:
		var data ProviderLimitData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if !providerLimitKinds[data.Kind] {
			return fmt.Errorf("protocol: %s data.kind %q is not a limit kind", event, data.Kind)
		}
		if !providerLimitScopes[data.Scope] {
			return fmt.Errorf("protocol: %s data.scope %q is not a limit scope", event, data.Scope)
		}
		if !providerLimitActions[data.Action] {
			return fmt.Errorf("protocol: %s data.action %q is not a continuity action", event, data.Action)
		}
		return nil
	default:
		return nil
	}
	if text == "" {
		return fmt.Errorf("protocol: %s data.text must be non-empty", event)
	}
	return nil
}

func validateSubagentCorrelation(event EventType, id, childTurn, mode string) error {
	if !validID(id, 'k') {
		return fmt.Errorf("protocol: %s data.id must be a canonical k_ ULID", event)
	}
	if !validID(childTurn, 't') {
		return fmt.Errorf("protocol: %s data.child_turn must be a canonical t_ ULID", event)
	}
	if mode == "" {
		return fmt.Errorf("protocol: %s data.mode must be non-empty", event)
	}
	return nil
}

// validateToolWorkCorrelation keeps tool events attributable when they belong
// to a concurrent child. Main-tool frames omit both coordinates; a subagent
// frame always carries the task and its child turn together.
func validateToolWorkCorrelation(event EventType, taskID, childTurn string) error {
	if taskID == "" && childTurn == "" {
		return nil
	}
	if taskID == "" || childTurn == "" {
		return fmt.Errorf("protocol: %s tool work correlation requires task_id and child_turn together", event)
	}
	if !validID(taskID, 'k') {
		return fmt.Errorf("protocol: %s data.task_id must be a canonical k_ ULID", event)
	}
	if !validID(childTurn, 't') {
		return fmt.Errorf("protocol: %s data.child_turn must be a canonical t_ ULID", event)
	}
	return nil
}
