package continuity

import "github.com/onembyte/kolkrabbi/internal/provider"

// TaskState is the scheduler fact needed to resume without guessing whether
// an empty outcome means "not started" or "was active when the boundary was
// written". Old journals omit it and are interpreted by the resume validator.
const (
	TaskQueued  = "queued"
	TaskRunning = "running"
	TaskWaiting = "waiting"
	TaskSettled = "settled"
)

// Run is the execution journal of one accepted request. The working transcript
// can be compacted independently; this record retains the request, plan and
// child work needed to continue after a limit without doing completed work twice.
type Run struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Input   string `json:"input"`
	Prompt  string `json:"prompt"`
	Root    string `json:"root"`
	Mode    string `json:"mode"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Phase   string `json:"phase"`
	Tasks   []Task `json:"tasks,omitempty"`
	Main    Task   `json:"main"`
	// Recovery names a completed exceptional boundary. It is retired durably
	// before subsequent work; running task state alone cannot prove a hard exit.
	Recovery  string `json:"recovery,omitempty"`
	LastPause *Pause `json:"last_pause,omitempty"`
	Spend     Spend  `json:"spend"`
}

// Task preserves a resolved route and its private conversation as well as the
// final result. Empty Status means work remains; settled outcomes have a status.
type Task struct {
	ID            string `json:"id,omitempty"`
	Sequence      uint64 `json:"sequence,omitempty"`
	Title         string `json:"title"`
	Kind          string `json:"kind,omitempty"`
	Level         string `json:"level,omitempty"`
	Needs         []int  `json:"needs,omitempty"`
	Model         string `json:"model"`
	Vendor        string `json:"vendor,omitempty"`
	Effort        string `json:"effort"`
	CeilingEffort string `json:"ceiling_effort,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	ChildTurn     string `json:"child_turn,omitempty"`
	Status        string `json:"status,omitempty"`
	State         string `json:"state,omitempty"`
	Result        string `json:"result,omitempty"`
	Reason        string `json:"reason,omitempty"`
	// PartialOutput is every byte emitted by the provider call that did not
	// reach a final assistant message. It is cleared only after that message is
	// committed, so an exceptional snapshot never loses words already shown.
	PartialOutput string             `json:"partial_output,omitempty"`
	Messages      []provider.Message `json:"messages,omitempty"`
	Archives      []string           `json:"archives,omitempty"`
	Rounds        int                `json:"rounds,omitempty"`
	ProviderState string             `json:"provider_state,omitempty"`
	// ProviderConfirmed distinguishes a handle acknowledged by the vendor from
	// one minted locally before the provider accepted the conversation.
	ProviderConfirmed bool `json:"provider_confirmed,omitempty"`
	// ProviderOwned distinguishes vendor tools from native HTTP tool proposals,
	// including a provider that failed before it returned a conversation handle.
	ProviderOwned bool `json:"provider_owned,omitempty"`
	// ProviderResumable is the adapter's own word that it continues a
	// confirmed conversation by its handle. Without it an accepted turn has no
	// proven way back, and recovery refuses rather than start the work over.
	ProviderResumable bool `json:"provider_resumable,omitempty"`
	// ProviderNeverStarted is the adapter's proof that the task's latest
	// turn's prompt never reached a vendor process. Missing output after
	// delivery is not such proof: a process can act and lose its frames.
	ProviderNeverStarted bool `json:"provider_never_started,omitempty"`
	// ProviderDelivered records that some attempt of this task in this run
	// reached a vendor process. It is never unlearned: a later attempt that
	// never arrived cannot make an earlier one that did look unstarted.
	ProviderDelivered bool `json:"provider_delivered,omitempty"`
	// ProviderTurnClosed is the vendor's own word that it ended the latest
	// turn: its result, turn.completed or turn.failed frame arrived. Only a
	// closed turn can be continued by a new message; an open one may still
	// hold actions the vendor started.
	ProviderTurnClosed bool `json:"provider_turn_closed,omitempty"`
	// ProviderInFlight says the latest provider call did not reach a committed
	// response. SafeBoundary remains the prior proven boundary in that case.
	ProviderInFlight bool `json:"provider_in_flight,omitempty"`
	// SafeBoundary names the last journaled provider/tool boundary. It is a
	// fact for resume validation, not permission to replay an uncertain action.
	SafeBoundary string `json:"safe_boundary,omitempty"`
	// VendorTools is what a provider-owned turn reported about its own
	// tools, which only its live stream says: the last one it finished and
	// the ones it started without finishing. Resume reads it to know which
	// vendor actions completed; nothing in it is ever replayed.
	VendorTools *VendorToolBoundary `json:"vendor_tools,omitempty"`
	Checkpoint  *int                `json:"checkpoint,omitempty"`
	Loop        ToolLoop            `json:"loop"`
}

// VendorToolBoundary is a vendor-run turn's own tool record.
type VendorToolBoundary struct {
	LastFinishedID     string   `json:"last_finished_id,omitempty"`
	LastFinishedName   string   `json:"last_finished_name,omitempty"`
	LastFinishedFailed bool     `json:"last_finished_failed,omitempty"`
	Unfinished         []string `json:"unfinished,omitempty"`
}

// ToolLoop retains the repetition guard across a wait; a quota reset does not
// buy permission to repeat a tool that was already stuck.
type ToolLoop struct {
	Last     string   `json:"last,omitempty"`
	Repeats  int      `json:"repeats,omitempty"`
	Reported bool     `json:"reported,omitempty"`
	Denied   bool     `json:"denied,omitempty"`
	Recent   []string `json:"recent,omitempty"`
}

func (t Task) Clone() Task {
	if t.Checkpoint != nil {
		value := *t.Checkpoint
		t.Checkpoint = &value
	}
	t.Needs = append([]int(nil), t.Needs...)
	t.Archives = append([]string(nil), t.Archives...)
	t.Messages = append([]provider.Message(nil), t.Messages...)
	for i := range t.Messages {
		t.Messages[i].ToolCalls = append([]provider.ToolCall(nil), t.Messages[i].ToolCalls...)
	}
	t.Loop.Recent = append([]string(nil), t.Loop.Recent...)
	if t.VendorTools != nil {
		tools := *t.VendorTools
		tools.Unfinished = append([]string(nil), tools.Unfinished...)
		t.VendorTools = &tools
	}
	return t
}

// Spend carries the run's existing admission budget across a pause. In-flight
// reservations are not stored: all children have reached a boundary before a
// pause is saved.
type Spend struct {
	USD     float64 `json:"usd"`
	Limit   float64 `json:"limit"`
	Calls   int     `json:"calls"`
	Worst   float64 `json:"worst"`
	Billing string  `json:"billing,omitempty"`
}

// Clone gives a session or worker its own slices. No stored snapshot aliases
// messages, tool calls or dependency slices being mutated by an active worker.
func (r *Run) Clone() *Run {
	if r == nil {
		return nil
	}
	out := *r
	out.Main = r.Main.Clone()
	if r.LastPause != nil {
		pause := *r.LastPause
		out.LastPause = &pause
	}
	out.Tasks = append([]Task(nil), r.Tasks...)
	for i := range out.Tasks {
		out.Tasks[i] = out.Tasks[i].Clone()
	}
	return &out
}
