package engine

// SubagentState is the small lifecycle vocabulary exposed to interactive
// surfaces. A task has one working update and one terminal update.
type SubagentState string

const (
	SubagentWorking SubagentState = "working"
	SubagentDone    SubagentState = "done"
	SubagentFailed  SubagentState = "failed"
)

// SubagentStatus is one presentation-safe lifecycle update. It carries only
// facts a surface can act on; provider handles, prompts, output and timings do
// not cross this boundary.
type SubagentStatus struct {
	ID      string
	Index   int
	Total   int
	Model   string
	Effort  string
	Summary string
	State   SubagentState
}
