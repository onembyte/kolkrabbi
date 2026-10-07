package session

import (
	"sync"
	"testing"
)

// The model and its connector are read off the turn's goroutine: the resume
// monitor asks which connector a paused model runs through, and saves from
// there, while a /model switch writes both. They share the lock a save
// snapshots under. Meaningful under -race.
func TestTheModelAndConnectorAreSafeToReadWhileSwitching(t *testing.T) {
	s := New(t.TempDir(), "claude-opus")
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				s.SetModelName("claude-opus")
				s.SetConnector("claude")
			} else {
				s.SetModelName("vendor/model")
				s.SetConnector("")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = s.ModelName(), s.ConnectorName()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := s.Save(); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}

// A model and its connector are one fact. A reader never sees a new model
// beside the old connector: "auto" via claude names a CLI that model does not
// run through.
func TestTheRouteIsReadAndWrittenAsOnePair(t *testing.T) {
	s := New(t.TempDir(), "claude-opus")
	s.SetRoute("claude-opus", "claude")
	pairs := map[string]string{"claude-opus": "claude", "auto": "copilot", "vendor/model": ""}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			for model, connector := range pairs {
				s.SetRoute(model, connector)
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		model, connector := s.Route()
		if want, ok := pairs[model]; !ok || connector != want {
			t.Fatalf("read %q via %q", model, connector)
		}
	}
}
