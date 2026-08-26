package engine

import (
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// SessionPort is the session handle interface consumed by the engine.
type SessionPort interface {
	SessionID() string
	SessionTitle() string
	ModelName() string
	SetModelName(string)
	SetTitleFromInput(string)
	GetMessages() []provider.Message
	SetMessages([]provider.Message)
	AppendMessage(provider.Message)
	Save() error
}

// Checkpointer is the pre-write snapshot port.
type Checkpointer interface {
	BeginTurn()
	Record(tool, path string) error
	RewindLastTurn() ([]string, error)
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
	Cost             float64
	Ms               int64
	ToolCalls        int
}

// Recorder is the accounting port.
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
