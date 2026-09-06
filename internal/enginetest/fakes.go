package enginetest

import (
	"context"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// FakeSession is an in-memory session.
type FakeSession struct {
	mu            sync.Mutex
	id            string
	model         string
	title         string
	autoTitled    bool
	effort        string
	mode          string
	connector     string
	providerState string
	messages      []provider.Message
	paused        *continuity.Pause
}

// NewFakeSession creates an in-memory session.
func NewFakeSession(id, model string) *FakeSession {
	return &FakeSession{
		id:    id,
		model: model,
	}
}

func (s *FakeSession) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *FakeSession) SessionTitle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

func (s *FakeSession) ModelName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

func (s *FakeSession) SetModelName(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = m
}

func (s *FakeSession) SessionEffort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effort
}

func (s *FakeSession) SetEffort(level string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.effort = level
}

func (s *FakeSession) SessionMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *FakeSession) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

func (s *FakeSession) ConnectorName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connector
}

func (s *FakeSession) SetConnector(n string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connector = n
}

func (s *FakeSession) ProviderStateName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providerState
}

func (s *FakeSession) SetProviderStateName(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providerState = v
}

func (s *FakeSession) SetTitleFromInput(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = t
}

func (s *FakeSession) TitleIsAuto() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.autoTitled
}

func (s *FakeSession) SetAutoTitle(t string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.autoTitled || t == "" {
		return false
	}
	s.title, s.autoTitled = t, true
	return true
}

func (s *FakeSession) GetMessages() []provider.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]provider.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *FakeSession) SetMessages(msgs []provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = make([]provider.Message, len(msgs))
	copy(s.messages, msgs)
}

func (s *FakeSession) AppendMessage(m provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
}

func (s *FakeSession) Save() error {
	return nil
}

// FakeCheckpointer is an in-memory checkpointer.
type FakeCheckpointer struct {
	mu           sync.Mutex
	Turns        int
	Recorded     []string
	RewoundPaths []string
	// Tasks are the subagent titles bracketed by BeginTask, and Ended the
	// handles returned to EndTask.
	Tasks []string
	Ended []int
}

func (c *FakeCheckpointer) BeginTurn(context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Turns++
}

func (c *FakeCheckpointer) Record(tool, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Recorded = append(c.Recorded, tool+":"+path)
	return nil
}

// BeginTask records the titles bracketed, so a test can assert which subagents
// were given a snapshot without needing a git store.
func (c *FakeCheckpointer) BeginTask(_ context.Context, title string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Tasks = append(c.Tasks, title)
	return len(c.Tasks) - 1
}

func (c *FakeCheckpointer) EndTask(_ context.Context, handle int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Ended = append(c.Ended, handle)
}

func (c *FakeCheckpointer) RewindLastTurn(context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.RewoundPaths, nil
}

// FakeClock returns a deterministic clock advancing on each call.
func FakeClock(start time.Time, step time.Duration) func() time.Time {
	var mu sync.Mutex
	current := start
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := current
		current = current.Add(step)
		return t
	}
}

// Paused and SetPaused mirror the session port for the continuity tests.
func (s *FakeSession) Paused() *continuity.Pause {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused == nil {
		return nil
	}
	copyOfPause := *s.paused
	return &copyOfPause
}

func (s *FakeSession) SetPaused(p *continuity.Pause) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p == nil {
		s.paused = nil
		return
	}
	copyOfPause := *p
	s.paused = &copyOfPause
}
