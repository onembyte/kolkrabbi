package session

import (
	"errors"
	"os"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Once the compressed recovery point is renamed into place it is the
// boundary: a JSON mirror or header that then fails to update is a separate,
// visible failure, and says so, so the engine keeps the recovery point it has.
func TestAMirrorFailureAfterTheRecoveryPointIsReportedAsDurable(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "mock/model")
	s.AppendMessage(provider.Message{Role: "user", Content: "before"})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	// The header's path becomes a directory: its write can no longer land.
	if err := os.Remove(metaPath(dir, s.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metaPath(dir, s.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	s.AppendMessage(provider.Message{Role: "user", Content: "after"})
	err := s.SaveRecovery("pause")
	var durable interface{ RecoveryDurable() bool }
	if err == nil || !errors.As(err, &durable) || !durable.RecoveryDurable() {
		t.Fatalf("SaveRecovery = %v; want a failure that says the recovery point itself is durable", err)
	}
	loaded, err := Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if msgs := loaded.GetMessages(); len(msgs) == 0 || msgs[len(msgs)-1].Content != "after" {
		t.Fatalf("the durable recovery point did not load: %+v", msgs)
	}
}

// The JSON mirror is written after the recovery point too: its failure is the
// same kind, durable and named.
func TestAJSONMirrorFailureAfterTheRecoveryPointIsReportedAsDurable(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "mock/model")
	s.AppendMessage(provider.Message{Role: "user", Content: "before"})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	// The mirror's path becomes a directory: its rename can no longer land.
	if err := os.Remove(s.path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.path(), 0o700); err != nil {
		t.Fatal(err)
	}
	s.AppendMessage(provider.Message{Role: "user", Content: "after"})
	err := s.SaveRecovery("pause")
	var mirror *RecoveryMirrorError
	if err == nil || !errors.As(err, &mirror) || mirror.Part != "JSON mirror" || !mirror.RecoveryDurable() {
		t.Fatalf("SaveRecovery = %v; want the JSON mirror's failure, reported as durable", err)
	}
	if _, statErr := os.Stat(recoveryPath(dir, s.ID)); statErr != nil {
		t.Fatalf("the recovery point is not on disk: %v", statErr)
	}
}

// A failure before the rename is not durable, and does not say it is.
func TestAFailureBeforeTheRecoveryPointIsNotDurable(t *testing.T) {
	s := New(t.TempDir(), "mock/model")
	err := s.SaveRecovery("  ")
	var durable interface{ RecoveryDurable() bool }
	if err == nil || errors.As(err, &durable) && durable.RecoveryDurable() {
		t.Fatalf("SaveRecovery = %v; an unwritten recovery point must not claim to be durable", err)
	}
}
