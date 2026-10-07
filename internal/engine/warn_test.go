package engine

import (
	"errors"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type failingSaveSession struct {
	messages []provider.Message
	saves    int
}

func (s *failingSaveSession) SessionID() string                { return "s1" }
func (s *failingSaveSession) SessionTitle() string             { return "t" }
func (s *failingSaveSession) ModelName() string                { return "vendor/model" }
func (s *failingSaveSession) SetModelName(string)              {}
func (s *failingSaveSession) SessionEffort() string            { return "" }
func (s *failingSaveSession) SetEffort(string)                 {}
func (s *failingSaveSession) SessionMode() string              { return "" }
func (s *failingSaveSession) SetMode(string)                   {}
func (s *failingSaveSession) ConnectorName() string            { return "" }
func (s *failingSaveSession) SetConnector(string)              {}
func (s *failingSaveSession) Route() (string, string)          { return "vendor/model", "" }
func (s *failingSaveSession) SetRoute(string, string)          {}
func (s *failingSaveSession) ProviderStateName() string        { return "" }
func (s *failingSaveSession) SetProviderStateName(string)      {}
func (s *failingSaveSession) SetTitleFromInput(string)         {}
func (s *failingSaveSession) TitleIsAuto() bool                { return false }
func (s *failingSaveSession) SetAutoTitle(string) bool         { return false }
func (s *failingSaveSession) GetMessages() []provider.Message  { return s.messages }
func (s *failingSaveSession) SetMessages(m []provider.Message) { s.messages = m }
func (s *failingSaveSession) AppendMessage(m provider.Message) { s.messages = append(s.messages, m) }
func (s *failingSaveSession) Save() error {
	s.saves++
	return errors.New("disk is read-only")
}

func (s *failingSaveSession) SaveInterim() error        { return s.Save() }
func (s *failingSaveSession) SaveRecovery(string) error { return s.Save() }
func (s *failingSaveSession) ArchiveMessages([]provider.Message) (string, error) {
	return "memory:failing-save", nil
}

// The engine writes everything through Options.Out: in a session that is the
// terminal renderer, which owns the screen. A warning printed straight to
// os.Stderr lands outside the renderer's rows and scribbles over the composer.
func TestSaveWarningGoesThroughTheConfiguredWriter(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Sess: &failingSaveSession{}}}

	agent.saveFor(saveTurnEnd)

	if !strings.Contains(out.String(), "could not save session") {
		t.Fatalf("out = %q, want the warning where every other engine message goes", out.String())
	}
}

func TestSaveWarningIsPrintedOnlyOnce(t *testing.T) {
	var out strings.Builder
	session := &failingSaveSession{}
	agent := &Agent{Options: Options{Out: &out, Sess: session}}

	for range 4 {
		agent.saveFor(saveTurnEnd)
	}

	// A failing disk must not fill the transcript with the same line.
	if got := strings.Count(out.String(), "could not save session"); got != 1 {
		t.Fatalf("warned %d times, want once", got)
	}
	if session.saves != 4 {
		t.Fatalf("saved %d times, want it to keep trying", session.saves)
	}
}

func TestRestoringACompactionReportsAFailedSave(t *testing.T) {
	var out strings.Builder
	session := &failingSaveSession{}
	agent := &Agent{Options: Options{Out: &out, Sess: session}}
	agent.preCompact = []provider.Message{{Role: "user", Content: "before"}}

	if agent.RestoreCompaction() {
		t.Fatal("a failed durable restore must not report success")
	}
	if len(session.messages) != 0 || agent.preCompact == nil {
		t.Fatal("failed restore changed working messages or consumed the saved history")
	}
	// Telling the user their conversation is back while it is not on disk is
	// the kind of quiet half-success this session keeps finding.
	if !strings.Contains(out.String(), "could not save") {
		t.Fatalf("out = %q, want the failed save surfaced", out.String())
	}
}

func (s *failingSaveSession) Paused() *continuity.Pause   { return nil }
func (s *failingSaveSession) SetPaused(*continuity.Pause) {}
func (s *failingSaveSession) RunState() *continuity.Run   { return nil }
func (s *failingSaveSession) SetRunState(*continuity.Run) {}

// flakySaveSession fails its saves only while fail is set: the shape of a
// disk that fills, is cleared, and fills again.
type flakySaveSession struct {
	failingSaveSession
	fail bool
}

func (s *flakySaveSession) Save() error {
	s.saves++
	if s.fail {
		return errors.New("no space left on device")
	}
	return nil
}

func (s *flakySaveSession) SaveInterim() error { return s.Save() }

// One warning per failing streak, not per session: a disk that recovers and
// fails again is a new failure the person has not been told about.
func TestSaveWarningReturnsAfterTheDiskRecovers(t *testing.T) {
	var out strings.Builder
	session := &flakySaveSession{fail: true}
	agent := &Agent{Options: Options{Out: &out, Sess: session}}

	agent.saveFor(saveTurnEnd)
	agent.saveFor(saveTurnEnd)
	session.fail = false
	agent.saveFor(saveTurnEnd)
	session.fail = true
	agent.saveFor(saveTurnEnd)
	agent.saveFor(saveTurnEnd)

	if got := strings.Count(out.String(), "could not save session"); got != 2 {
		t.Fatalf("warned %d times, want once per failing streak (2):\n%s", got, out.String())
	}
}

// A pause's own message promises a later /resume. When the pause itself
// could not be saved, that promise holds only while this session runs, so it
// is said every time, even in the middle of a streak already reported.
func TestAFailedPauseSaveIsAlwaysReported(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Sess: &failingSaveSession{}}}

	agent.saveFor(saveTurnEnd)
	agent.saveFor(savePause)
	agent.saveFor(saveTurnEnd)

	text := out.String()
	if got := strings.Count(text, "could not save session"); got != 2 {
		t.Fatalf("warned %d times, want the streak once and the pause once:\n%s", got, text)
	}
	if !strings.Contains(text, "at the pause") || !strings.Contains(text, "only until this session exits") {
		t.Fatalf("the pause warning does not say what is at stake:\n%s", text)
	}
}
