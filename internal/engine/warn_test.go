package engine

import (
	"errors"
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

// The engine writes everything through Options.Out: in a session that is the
// terminal renderer, which owns the screen. A warning printed straight to
// os.Stderr lands outside the renderer's rows and scribbles over the composer.
func TestSaveWarningGoesThroughTheConfiguredWriter(t *testing.T) {
	var out strings.Builder
	agent := &Agent{Options: Options{Out: &out, Sess: &failingSaveSession{}}}

	agent.save()

	if !strings.Contains(out.String(), "could not save session") {
		t.Fatalf("out = %q, want the warning where every other engine message goes", out.String())
	}
}

func TestSaveWarningIsPrintedOnlyOnce(t *testing.T) {
	var out strings.Builder
	session := &failingSaveSession{}
	agent := &Agent{Options: Options{Out: &out, Sess: session}}

	for range 4 {
		agent.save()
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

	if !agent.RestoreCompaction() {
		t.Fatal("the restore itself should report success in memory")
	}
	// Telling the user their conversation is back while it is not on disk is
	// the kind of quiet half-success this session keeps finding.
	if !strings.Contains(out.String(), "could not save") {
		t.Fatalf("out = %q, want the failed save surfaced", out.String())
	}
}
