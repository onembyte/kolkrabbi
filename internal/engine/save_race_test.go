package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// countingSession is the port with a lock around it, so `go test -race` is
// measuring the engine's coalescing state rather than the fake's.
type countingSession struct {
	mu      sync.Mutex
	durable int
	interim int
}

func (s *countingSession) SessionID() string               { return "s_race" }
func (s *countingSession) SessionTitle() string            { return "" }
func (s *countingSession) ModelName() string               { return "mock/model" }
func (s *countingSession) SetModelName(string)             {}
func (s *countingSession) SessionEffort() string           { return "" }
func (s *countingSession) SetEffort(string)                {}
func (s *countingSession) SessionMode() string             { return "" }
func (s *countingSession) SetMode(string)                  {}
func (s *countingSession) ConnectorName() string           { return "" }
func (s *countingSession) SetConnector(string)             {}
func (s *countingSession) ProviderStateName() string       { return "" }
func (s *countingSession) SetProviderStateName(string)     {}
func (s *countingSession) SetTitleFromInput(string)        {}
func (s *countingSession) TitleIsAuto() bool               { return false }
func (s *countingSession) SetAutoTitle(string) bool        { return false }
func (s *countingSession) GetMessages() []provider.Message { return nil }
func (s *countingSession) SetMessages([]provider.Message)  {}
func (s *countingSession) AppendMessage(provider.Message)  {}
func (s *countingSession) Paused() *continuity.Pause       { return nil }
func (s *countingSession) SetPaused(*continuity.Pause)     {}

// failingCountingSession is the same port with a disk that refuses, which is
// the only path that touches the once-per-session warning.
type failingCountingSession struct{ countingSession }

func (s *failingCountingSession) Save() error {
	_ = s.countingSession.Save()
	return errors.New("disk is read-only")
}

func (s *failingCountingSession) SaveInterim() error { return s.Save() }

func (s *countingSession) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durable++
	return nil
}

func (s *countingSession) SaveInterim() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interim++
	return nil
}

func (s *countingSession) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.durable, s.interim
}

// The dirty flag is reached from more goroutines than the loop that owns it:
// with no isolator, every orchestrated task shares the session's root and so
// shares the pre-write hook. Under -race this fails on any unguarded field.
func TestTheDirtyFlagSurvivesConcurrentMarkersAndFlushers(t *testing.T) {
	sess := &countingSession{}
	agent := &Agent{Options: Options{Out: &strings.Builder{}, Sess: sess}}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				agent.markDirty()
				agent.saveFor(saveToolRound)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				agent.noteFileWrite()
				agent.flush(saveTurnEnd)
			}
		}()
	}
	wg.Wait()

	// The invariant coalescing must not break: whatever is left pending is
	// written by the next boundary, and after that boundary nothing is owed.
	agent.markDirty()
	agent.flush(saveTurnEnd)
	agent.saveState.mu.Lock()
	pending := agent.saveState.pending
	agent.saveState.mu.Unlock()
	if pending {
		t.Error("the session is still dirty after a flush; a write was lost")
	}
	if durable, interim := sess.counts(); durable+interim == 0 {
		t.Error("nothing was written at all")
	}
}

// A flush with nothing pending writes nothing, which is what lets RunTurn defer
// one and a nested RunTurn cost one write rather than two.
func TestFlushingATwiceCleanSessionWritesOnce(t *testing.T) {
	sess := &countingSession{}
	agent := &Agent{Options: Options{Out: &strings.Builder{}, Sess: sess}}

	agent.markDirty()
	agent.flush(saveTurnEnd)
	agent.flush(saveTurnEnd)
	agent.flush(saveTurnEnd)

	if durable, interim := sess.counts(); durable != 1 || interim != 0 {
		t.Errorf("writes = %d durable, %d interval; want exactly one durable", durable, interim)
	}
}

// markDirty is the half of the pair that must never touch the disk: it is
// called once per streamed message, which is the whole point of O3.
func TestMarkDirtyWritesNothing(t *testing.T) {
	sess := &countingSession{}
	agent := &Agent{Options: Options{Out: &strings.Builder{}, Sess: sess}}

	for range 100 {
		agent.markDirty()
	}

	if durable, interim := sess.counts(); durable != 0 || interim != 0 {
		t.Errorf("markDirty wrote %d durable and %d interval times, want none", durable, interim)
	}
}

// The interval save is the only one allowed to skip the directory fsync, and
// only when the round left the working tree alone.
func TestTheIntervalSaveIsInterimAndTheFileWriteRoundIsNot(t *testing.T) {
	sess := &countingSession{}
	agent := &Agent{Options: Options{Out: &strings.Builder{}, Sess: sess}}

	agent.saveFor(saveToolRound) // first round: nothing written yet, so it writes
	if durable, interim := sess.counts(); durable != 0 || interim != 1 {
		t.Fatalf("first tool round wrote %d durable, %d interval; want one interval save", durable, interim)
	}

	agent.saveFor(saveToolRound) // inside the interval, tree untouched: coalesced
	if durable, interim := sess.counts(); durable != 0 || interim != 1 {
		t.Fatalf("second tool round wrote %d durable, %d interval; want it coalesced", durable, interim)
	}

	agent.noteFileWrite()
	agent.saveFor(saveToolRound) // the tree changed: durable, interval or not
	if durable, interim := sess.counts(); durable != 1 || interim != 1 {
		t.Fatalf("the file-writing round wrote %d durable, %d interval; want a durable save", durable, interim)
	}
}

// The warning is printed once even when a read-only disk is met by several
// goroutines at the same moment. Before O3 nothing concurrent reached it; the
// pre-write hook does, and with no isolator every orchestrated task shares it.
func TestTheFailedSaveWarningIsPrintedOnceUnderConcurrency(t *testing.T) {
	var out lockedBuilder
	agent := &Agent{Options: Options{Out: &out, Sess: &failingCountingSession{}}}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				agent.saveFor(saveTurnEnd)
			}
		}()
	}
	wg.Wait()

	if got := strings.Count(out.String(), "could not save session"); got != 1 {
		t.Fatalf("warned %d times, want once", got)
	}
}

// lockedBuilder is a writer several goroutines may share, so the test measures
// the engine's warning rather than strings.Builder's lack of a lock.
type lockedBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *lockedBuilder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedBuilder) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}
