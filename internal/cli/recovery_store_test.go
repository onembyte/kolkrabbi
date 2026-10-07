package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestRecoveryOnlySessionForkAndExportRetainAllHistory(t *testing.T) {
	dirs, source := seedSessions(t)
	archive, err := source.ArchiveMessages([]provider.Message{{Role: "user", Content: "complete saved evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetRunState(&continuity.Run{Version: 1, ID: "request", Phase: "tasks",
		Main:  continuity.Task{Archives: []string{archive}},
		Tasks: []continuity.Task{{Title: "child", Archives: []string{archive}}}})
	if err := source.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dirs.Sessions(), source.ID+".json")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dirs.Sessions(), source.ID+".compactions")); err != nil {
		t.Fatal(err)
	}
	a, out, _ := newTestApp(t, "")
	if err := a.forkSession(dirs.Sessions(), source.ID); err != nil {
		t.Fatal(err)
	}
	all, err := session.List(dirs.Sessions())
	if err != nil {
		t.Fatal(err)
	}
	var fork string
	for _, item := range all {
		if strings.Contains(item.Title, "(fork)") {
			fork = item.ID
		}
	}
	if fork == "" {
		t.Fatal("missing fork")
	}
	if err := session.Delete(dirs.Sessions(), source.ID); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.exportSession(dirs.Sessions(), fork, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "complete saved evidence") || strings.Contains(out.String(), archive) {
		t.Fatalf("fork lost or retained source ownership of history: %s", out.String())
	}
}
