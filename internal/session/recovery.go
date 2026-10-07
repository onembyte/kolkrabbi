package session

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

const (
	recoveryVersion = 1
	maxRecoveryFile = 256 << 20
	maxRecoveryBody = 768 << 20
)

type recoveryFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}

type recoveryEnvelope struct {
	Version       int             `json:"version"`
	ID            string          `json:"id"`
	Revision      uint64          `json:"revision"`
	Reason        string          `json:"reason"`
	SessionSHA256 string          `json:"session_sha256"`
	Session       json.RawMessage `json:"session"`
	Archives      []recoveryFile  `json:"archives,omitempty"`
}

func recoveryPath(dir, id string) string { return filepath.Join(dir, id+".resume.json.gz") }

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SaveRecovery writes a complete, compressed recovery point before its JSON
// mirror. A failed mirror leaves the recovery point readable and reports the
// failure; a failed recovery write never claims a durable boundary.
func (s *Session) SaveRecovery(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("recovery reason is empty")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := validateSessionID(s.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	s.messagesMu.Lock()
	if s.Revision == ^uint64(0) {
		s.messagesMu.Unlock()
		return errors.New("session revision exhausted")
	}
	if err := s.archiveExecutionsLocked(); err != nil {
		s.messagesMu.Unlock()
		return fmt.Errorf("archiving execution history: %w", err)
	}
	s.UpdatedAt = time.Now()
	s.Revision++
	s.RecoveryVersion = recoveryVersion
	state, err := json.Marshal(s)
	header := s.meta()
	s.messagesMu.Unlock()
	if err != nil {
		return err
	}
	var frozen Session
	if err := json.Unmarshal(state, &frozen); err != nil {
		return err
	}
	frozen.dir = s.dir
	archives, err := s.recoveryArchives()
	if err != nil {
		return err
	}
	if err := validateRecoveryClosure(&frozen, archives); err != nil {
		return err
	}
	envelope := recoveryEnvelope{Version: recoveryVersion, ID: s.ID,
		Revision: s.Revision, Reason: reason, SessionSHA256: digest(state),
		Session: state, Archives: archives}
	plain, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if len(plain) > maxRecoveryBody {
		return errors.New("recovery point exceeds expanded size limit")
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if compressed.Len() > maxRecoveryFile {
		return errors.New("recovery point exceeds compressed size limit")
	}
	if err := atomicfile.Write(recoveryPath(s.dir, s.ID), compressed.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing compressed recovery point: %w", err)
	}
	// Keep the public session format readable by older installations. It is a
	// mirror of the same revision, not the authority for this boundary.
	if err := atomicfile.Write(s.path(), state, 0o600); err != nil {
		return &RecoveryMirrorError{Part: "JSON mirror", Err: err}
	}
	if err := writeMetaChecked(s.dir, header); err != nil {
		return &RecoveryMirrorError{Part: "session header", Err: err}
	}
	return nil
}

// RecoveryMirrorError is a recovery point that reached disk, followed by a
// failure to update its JSON mirror or header. The compressed file is the
// authority for that boundary and loads; the failure is still worth saying,
// and says it is not the boundary's.
type RecoveryMirrorError struct {
	Part string
	Err  error
}

func (e *RecoveryMirrorError) Error() string {
	return fmt.Sprintf("recovery point saved but %s failed: %v", e.Part, e.Err)
}

func (e *RecoveryMirrorError) Unwrap() error { return e.Err }

// RecoveryDurable reports that the recovery point itself was written.
func (e *RecoveryMirrorError) RecoveryDurable() bool { return true }

func (s *Session) recoveryArchives() ([]recoveryFile, error) {
	var files []recoveryFile
	total := 0
	add := func(path string) error {
		name, err := filepath.Rel(s.dir, path)
		if err != nil || !safeRecoveryName(s.ID, name) {
			return fmt.Errorf("unsafe recovery archive path: %s", path)
		}
		data, err := readRecoveryBytes(s.dir, name)
		if err != nil {
			return err
		}
		file := recoveryFile{Name: name, SHA256: digest(data), Data: data}
		if err := validateRecoveryArchive(s.ID, file); err != nil {
			return err
		}
		total += len(data)
		if total > maxRecoveryBody/2 {
			return errors.New("recovery archives exceed expanded size limit")
		}
		files = append(files, file)
		return nil
	}
	for _, dir := range []string{s.compactionDir(), s.executionDir()} {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("recovery archive store is not a directory: %s", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".json" {
				if err := add(filepath.Join(dir, entry.Name())); err != nil {
					return nil, err
				}
			}
		}
	}
	legacy, err := CompactionArchives(s.dir, s.ID)
	if err != nil {
		return nil, err
	}
	for _, path := range legacy {
		if err := add(path); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func safeRecoveryName(id, name string) bool {
	if filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, "..") {
		return false
	}
	parts := strings.Split(name, string(filepath.Separator))
	if len(parts) == 1 {
		return strings.HasPrefix(parts[0], id+".pre-compact-") && strings.HasSuffix(parts[0], ".json")
	}
	if len(parts) != 2 || !strings.HasSuffix(parts[1], ".json") {
		return false
	}
	return parts[0] == id+".compactions" || parts[0] == id+".executions"
}

func readRecovery(dir, id string) (*Session, []recoveryFile, error) {
	path := recoveryPath(dir, id)
	compressed, err := readRecoveryBytes(dir, filepath.Base(path))
	if err != nil {
		return nil, nil, err
	}
	reader := bytes.NewReader(compressed)
	zr, err := gzip.NewReader(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("reading recovery point %s: %w", path, err)
	}
	zr.Multistream(false)
	plain, err := io.ReadAll(io.LimitReader(zr, maxRecoveryBody+1))
	if err == nil {
		err = zr.Close()
	}
	if err != nil || len(plain) > maxRecoveryBody || reader.Len() != 0 {
		return nil, nil, fmt.Errorf("recovery point %s is corrupt, oversized, or has trailing data", path)
	}
	var envelope recoveryEnvelope
	if err := json.Unmarshal(plain, &envelope); err != nil {
		return nil, nil, fmt.Errorf("decoding recovery point %s: %w", path, err)
	}
	if envelope.Version != recoveryVersion || envelope.ID != id || envelope.Revision == 0 ||
		strings.TrimSpace(envelope.Reason) == "" || digest(envelope.Session) != envelope.SessionSHA256 {
		return nil, nil, fmt.Errorf("recovery point %s has invalid identity or checksum", path)
	}
	var s Session
	if err := json.Unmarshal(envelope.Session, &s); err != nil || s.ID != id || s.Revision != envelope.Revision || s.RecoveryVersion != recoveryVersion {
		return nil, nil, fmt.Errorf("recovery point %s has invalid session state", path)
	}
	seen := map[string]bool{}
	for _, file := range envelope.Archives {
		if seen[file.Name] {
			return nil, nil, fmt.Errorf("recovery point %s has invalid archive %q", path, file.Name)
		}
		if err := validateRecoveryArchive(id, file); err != nil {
			return nil, nil, err
		}
		seen[file.Name] = true
	}
	s.dir = dir
	if err := validateRecoveryClosure(&s, envelope.Archives); err != nil {
		return nil, nil, err
	}
	return &s, envelope.Archives, nil
}

func restoreRecoveryArchives(dir string, files []recoveryFile) error {
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if data, err := readRecoveryBytes(dir, file.Name); err == nil {
			if digest(data) != file.SHA256 {
				return fmt.Errorf("recovery archive differs from saved point: %s", path)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicfile.WriteBeneath(dir, path, file.Data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// readRecoveryBytes keeps reads inside the data directory, including while a
// directory entry changes. Recovery never reads a linked archive or a device.
func readRecoveryBytes(dir, name string) ([]byte, error) {
	if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid recovery path %q", name)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("recovery archive store is not a directory: %s", parent)
		}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecoveryFile {
		return nil, fmt.Errorf("recovery file is not a bounded regular file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("recovery file changed while opening: %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRecoveryFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRecoveryFile {
		return nil, fmt.Errorf("recovery file is too large: %s", name)
	}
	return data, nil
}

func validateRecoveryArchive(id string, file recoveryFile) error {
	if !safeRecoveryName(id, file.Name) || len(file.Data) > maxRecoveryFile || digest(file.Data) != file.SHA256 {
		return fmt.Errorf("recovery point has invalid archive %q", file.Name)
	}
	parent, base := filepath.Dir(file.Name), filepath.Base(file.Name)
	if parent == id+".executions" {
		var run continuity.Run
		if err := json.Unmarshal(file.Data, &run); err != nil || executionFile(run.ID) != base {
			return fmt.Errorf("recovery point has invalid execution archive %q", file.Name)
		}
		return nil
	}
	var messages []provider.Message
	if err := json.Unmarshal(file.Data, &messages); err != nil ||
		(parent == id+".compactions" && base != digest(file.Data)+".json") {
		return fmt.Errorf("recovery point has invalid conversation archive %q", file.Name)
	}
	return nil
}

func (s *Session) validateRunArchives() error {
	files, err := s.recoveryArchives()
	if err != nil {
		return err
	}
	return validateRecoveryClosure(s, files)
}

// Every conversation referenced by a current or archived run must be inside
// this bundle. Existing disk files cannot make an incomplete bundle complete.
func validateRecoveryClosure(s *Session, files []recoveryFile) error {
	known := make(map[string]bool, len(files))
	runs := append([]continuity.Run(nil), s.Executions...)
	for _, file := range files {
		known[file.Name] = true
		if filepath.Dir(file.Name) == s.ID+".executions" {
			var run continuity.Run
			if err := json.Unmarshal(file.Data, &run); err != nil {
				return err
			}
			runs = append(runs, run)
		}
	}
	for _, run := range runs {
		tasks := append([]continuity.Task{run.Main}, run.Tasks...)
		for _, task := range tasks {
			for _, path := range task.Archives {
				if filepath.Dir(path) != s.compactionDir() {
					return fmt.Errorf("saved conversation archive is outside its session: %s", path)
				}
				name, err := filepath.Rel(s.dir, path)
				if err != nil {
					return err
				}
				if !known[name] {
					return fmt.Errorf("recovery point is missing conversation archive %s", path)
				}
			}
		}
	}
	return nil
}
