package engine

import (
	"context"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// SessionPort is the session handle interface consumed by the engine.
type SessionPort interface {
	SessionID() string
	SessionTitle() string
	ModelName() string
	SetModelName(string)
	// SetEffort and SetConnector keep the session file describing the run it
	// actually is: the dial level, and the subscription connector answering
	// for it, when one does. A resumed run reads both back.
	SetEffort(string)
	SetConnector(string)
	SessionEffort() string
	ConnectorName() string
	// ProviderState carries the provider-side state worth resuming across
	// Kolkrabbi restarts — for Claude, the vendor conversation handle. Empty
	// means "start a fresh one".
	ProviderStateName() string
	SetProviderStateName(string)
	SetTitleFromInput(string)
	// TitleIsAuto reports whether the title is still Kolkrabbi's own guess, and
	// so may be improved. It is asked before the improvement is generated,
	// because generating one costs a model call.
	TitleIsAuto() bool
	// SetAutoTitle offers a better derived title, and reports whether it was
	// taken. It must never overwrite a title the user chose.
	SetAutoTitle(string) bool
	GetMessages() []provider.Message
	SetMessages([]provider.Message)
	AppendMessage(provider.Message)
	Save() error
}

// Checkpointer is the pre-write snapshot port.
type Checkpointer interface {
	BeginTurn(context.Context)
	Record(tool, path string) error
	RewindLastTurn(context.Context) ([]string, error)
	// BeginTask and EndTask bracket one writing subagent, so a task that makes
	// a mess can be taken back without touching the tasks around it (A33.8).
	// A store that cannot snapshot the whole tree returns -1 and does nothing,
	// which is why neither returns an error: this must never fail a run.
	BeginTask(ctx context.Context, title string) int
	EndTask(ctx context.Context, handle int)
}

// CallRecord is the un-marshaled usage record passed to Recorder.
type CallRecord struct {
	Session          string
	Turn             string
	Mode             string
	Effort           string
	Role             string
	Model            string
	PromptTokens     int
	CompletionTokens int
	// Cache tokens, when the provider reports them.
	CacheReadTokens     int
	CacheCreationTokens int
	Cost                float64
	Ms                  int64
	ToolCalls           int
}

// Recorder is the accounting port.
//
// Implementations must be safe for concurrent use: an orchestrated run records
// from several subagents at once. The shipped store appends one line per call
// with a single O_APPEND write, which is atomic for a regular file and so is
// already safe across goroutines and across processes.
type Recorder interface {
	RecordCall(r CallRecord) error
	RecordRating(session, turn string, rating int) error
}

// Clock is a monotonic timestamp source.
type Clock func() time.Time

// QualityGate describes one auto-detected verification command.
type QualityGate struct {
	Name    string // human label, e.g. "go test"
	Command string // shell command, e.g. "go vet ./... && go test ./..."
}

// QualityGateDetector discovers project quality gates from repo markers.
type QualityGateDetector interface {
	// Detect inspects repoDir for project files and returns the gates
	// that apply. The order is deterministic: Go, Node, Rust, Make.
	Detect(repoDir string) []QualityGate
}

// GateResult is the outcome of running one quality gate.
type GateResult struct {
	Gate   QualityGate
	Passed bool
	Output string // combined stdout+stderr, truncated to a reasonable cap
}

// QualityGateRunner executes a set of quality gates against the repo.
type QualityGateRunner interface {
	RunGates(repoDir string, gates []QualityGate) []GateResult
}

// GitCheckpointer creates atomic saga commits and rollbacks.
type GitCheckpointer interface {
	// CommitChapter stages all changes and creates a saga commit.
	// Returns the short commit hash on success.
	CommitChapter(repoDir string, chapterNum int, summary string) (string, error)

	// RollbackChapter discards all uncommitted changes in the repo.
	RollbackChapter(repoDir string) error

	// HasChanges reports whether the working tree has uncommitted modifications.
	HasChanges(repoDir string) (bool, error)
}
