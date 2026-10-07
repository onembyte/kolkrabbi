package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ConversationArchive is a complete transcript before a working-context shrink.
// Path identifies the record referenced by a child execution journal.
type ConversationArchive struct {
	Path     string             `json:"path"`
	Messages []provider.Message `json:"messages"`
}

func (s *Session) compactionDir() string {
	s.mustValidID()
	return filepath.Join(s.dir, s.ID+".compactions")
}

// ArchiveMessages is safe for concurrent children. Content-addressed snapshots
// are immutable and stay outside the frequently rewritten session file.
func (s *Session) ArchiveMessages(messages []provider.Message) (string, error) {
	data, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := os.MkdirAll(s.compactionDir(), 0o700); err != nil {
		return "", err
	}
	if err := s.checkCompactionDir(); err != nil {
		return "", err
	}
	path := filepath.Join(s.compactionDir(), fmt.Sprintf("%x.json", sha256.Sum256(data)))
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("conversation archive is not a regular file: %s", path)
		}
		stored, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if string(stored) != string(data) {
			return "", fmt.Errorf("conversation archive checksum mismatch: %s", path)
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// CompactionHistory includes legacy numbered snapshots and current main/child
// archives. Only explicit history operations load these full payloads. It is
// all or nothing: the first archive that cannot be read is the error.
func (s *Session) CompactionHistory() ([]ConversationArchive, error) {
	history, unreadable := s.compactionHistory()
	if len(unreadable) > 0 {
		return nil, unreadable[0]
	}
	return history, nil
}

// CompactionHistoryReadable is CompactionHistory for a reader who wants what
// survives: every archive that can be read, and why each other one, or a store
// that could not be listed, cannot. An export uses it, so one damaged record
// does not withhold all the others.
func (s *Session) CompactionHistoryReadable() ([]ConversationArchive, []error) {
	return s.compactionHistory()
}

func (s *Session) compactionHistory() (history []ConversationArchive, unreadable []error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	paths, err := CompactionArchives(s.dir, s.ID)
	if err != nil {
		unreadable = append(unreadable, err)
	}
	var entries []os.DirEntry
	if err := s.checkCompactionDir(); err != nil && !os.IsNotExist(err) {
		unreadable = append(unreadable, err)
	} else if entries, err = os.ReadDir(s.compactionDir()); err != nil && !os.IsNotExist(err) {
		unreadable = append(unreadable, err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(s.compactionDir(), entry.Name()))
		}
	}
	for _, path := range paths {
		archive, err := s.readArchive(path)
		if err != nil {
			unreadable = append(unreadable, err)
			continue
		}
		history = append(history, archive)
	}
	return history, unreadable
}

func (s *Session) readArchive(path string) (ConversationArchive, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return ConversationArchive{}, err
	}
	if !info.Mode().IsRegular() {
		return ConversationArchive{}, fmt.Errorf("conversation archive is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ConversationArchive{}, err
	}
	if filepath.Dir(path) == s.compactionDir() && filepath.Base(path) != fmt.Sprintf("%x.json", sha256.Sum256(data)) {
		return ConversationArchive{}, fmt.Errorf("conversation archive checksum mismatch: %s", path)
	}
	archive := ConversationArchive{Path: path}
	if err := json.Unmarshal(data, &archive.Messages); err != nil {
		return ConversationArchive{}, fmt.Errorf("reading conversation archive %s: %w", path, err)
	}
	return archive, nil
}

func (s *Session) checkCompactionDir() error {
	info, err := os.Lstat(s.compactionDir())
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("conversation archive store is not a directory: %s", s.compactionDir())
	}
	return nil
}
