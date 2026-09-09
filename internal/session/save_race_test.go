package session

import (
	"fmt"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Save reads the message slice to marshal it; AppendMessage writes it under
// the messages mutex. A turn appends while an autosave runs, so the two meet
// in practice, and without the same lock on both sides the snapshot on disk is
// whatever the race produced. The race detector is the assertion.
func TestSaveAndAppendShareOneSynchronization(t *testing.T) {
	s := New(t.TempDir(), "model")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.AppendMessage(provider.Message{Role: "user", Content: fmt.Sprintf("message %d", i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if err := s.Save(); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}

// SaveInterim takes the same snapshot under the same lock as Save; only the
// directory fsync differs. The engine calls it from the tool loop while a turn
// appends, so the two meet exactly as Save and AppendMessage do, and the same
// assertion applies (OPTIMIZATION_PLAN.md O3).
func TestSaveInterimSharesTheSameSynchronizationAsSave(t *testing.T) {
	s := New(t.TempDir(), "model")
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.AppendMessage(provider.Message{Role: "user", Content: fmt.Sprintf("message %d", i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if err := s.SaveInterim(); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			if err := s.Save(); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}
