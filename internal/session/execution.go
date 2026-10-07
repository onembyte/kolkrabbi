package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/continuity"
)

func (s *Session) executionDir() string {
	s.mustValidID()
	return filepath.Join(s.dir, s.ID+".executions")
}

func executionFile(id string) string {
	// Never derive a path directly from journal data, including older files.
	return fmt.Sprintf("%x.json", sha256.Sum256([]byte(id)))
}

// archiveExecutionsLocked runs under both writeMu and messagesMu. Superseded
// requests leave the frequently rewritten session only after their complete
// records are durable. Failure retains them in memory and on the old disk file.
func (s *Session) archiveExecutionsLocked() error {
	if len(s.Executions) < 2 {
		return nil
	}
	if err := os.MkdirAll(s.executionDir(), 0o700); err != nil {
		return err
	}
	for _, run := range s.Executions[:len(s.Executions)-1] {
		data, err := json.Marshal(run)
		if err != nil {
			return err
		}
		if err := atomicfile.WriteBeneath(s.dir, filepath.Join(s.executionDir(), executionFile(run.ID)), data, 0o600); err != nil {
			return err
		}
	}
	s.Executions = append([]continuity.Run(nil), s.Executions[len(s.Executions)-1])
	return nil
}

// ExecutionHistory reads complete current and archived requests for an export.
// Routine session saves and model calls never load this historical payload.
// It is all or nothing: the first journal that cannot be read is the error.
func (s *Session) ExecutionHistory() ([]continuity.Run, error) {
	runs, unreadable := s.executionHistory()
	if len(unreadable) > 0 {
		return nil, unreadable[0]
	}
	return runs, nil
}

// ExecutionHistoryReadable is ExecutionHistory for a reader who wants what
// survives: every request that can be read, and why each other journal, or a
// store that could not be listed, cannot, so one damaged record does not
// withhold all the others from an export.
func (s *Session) ExecutionHistoryReadable() ([]continuity.Run, []error) {
	return s.executionHistory()
}

func (s *Session) executionHistory() ([]continuity.Run, []error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	all := map[string]continuity.Run{}
	var unreadable []error
	entries, err := os.ReadDir(s.executionDir())
	if err != nil && !os.IsNotExist(err) {
		unreadable = append(unreadable, err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		run, err := s.readExecution(entry)
		if err != nil {
			unreadable = append(unreadable, err)
			continue
		}
		all[run.ID] = run
	}
	s.messagesMu.Lock()
	for _, run := range s.Executions {
		all[run.ID] = *run.Clone()
	}
	s.messagesMu.Unlock()
	out := make([]continuity.Run, 0, len(all))
	for _, run := range all {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, unreadable
}

// readExecution reads one archived request, naming its journal in any error.
func (s *Session) readExecution(entry os.DirEntry) (continuity.Run, error) {
	path := filepath.Join(s.executionDir(), entry.Name())
	if !entry.Type().IsRegular() {
		return continuity.Run{}, fmt.Errorf("execution archive %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return continuity.Run{}, err
	}
	var run continuity.Run
	if err := json.Unmarshal(data, &run); err != nil {
		return continuity.Run{}, fmt.Errorf("reading execution archive %s: %w", path, err)
	}
	if executionFile(run.ID) != entry.Name() {
		return continuity.Run{}, fmt.Errorf("execution archive identity does not match %s", path)
	}
	return run, nil
}
