package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/continuity"
)

// The session header, `<id>.meta.json`.
//
// Every listing on this machine — `kolk -r`, `kolk sessions`, the serve
// picker, `kolk doctor`, the dashboard — used to answer "which session?" by
// decoding every transcript in the directory. A few hundred sessions of a few
// hundred KB is tens of megabytes of JSON parsed to print one line each, and
// `kolk -r` paid it before showing anything (OPTIMIZATION_PLAN.md O6).
//
// So each save writes a few hundred bytes beside the transcript with the
// fields a listing actually shows. The transcript remains the source of truth:
// the header is derived, never authoritative, and a session without one is
// listed exactly as before by decoding it. That is what makes this safe to
// delete, and what makes an older session still appear.

const metaVersion = 1

// Meta is one session as every listing sees it: who, when, where, and how
// much — never what was said.
//
// Deliberately not a *Session. Loading a megabyte of transcript to render one
// line is the difference between a listing that can be run in a loop and one
// that cannot, and a type that cannot carry a transcript cannot leak one into
// a view by accident.
type Meta struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Model   string `json:"model,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	// Effort and Connector are here for the same reason as the rest: the
	// dashboard's cards show them, and a field the header lacks is a field
	// that costs a full decode to read.
	Effort    string `json:"effort,omitempty"`
	Connector string `json:"connector,omitempty"`
	// Pause is the limit this session is stopped on, when it is. `kolk doctor`
	// reports every paused session on the machine, and that question has no
	// cheaper answer than asking each one.
	Pause        *continuity.Pause `json:"pause,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at"`
	MessageCount int               `json:"message_count"`
}

// Name is what to show for a session, titled or not. An untitled session is
// the normal state until the fast lane names one, and a row with no name is a
// row nobody can pick out of a list.
func (m Meta) Name() string {
	if title := strings.TrimSpace(m.Title); title != "" {
		return title
	}
	return m.ID
}

// metaPath is where a session's header lives.
func metaPath(dir, id string) string { return filepath.Join(dir, id+".meta.json") }

// meta is the header for this session as it stands. Called under the same lock
// the transcript snapshot is taken under, so the two describe one state.
func (s *Session) meta() Meta {
	return Meta{
		Version:      metaVersion,
		ID:           s.ID,
		Title:        s.Title,
		Model:        s.Model,
		CWD:          s.CWD,
		Effort:       s.Effort,
		Connector:    s.Connector,
		Pause:        s.Pause,
		UpdatedAt:    s.UpdatedAt,
		MessageCount: len(s.Messages),
	}
}

// writeMeta replaces the header.
//
// Atomically, so a listing never reads half a header, but with neither fsync a
// transcript gets: this file is derived from one, so the worst a power cut can
// cost is a stale line in a listing, which the next save corrects and `kolk
// doctor` repairs. Measured: an fsync here cost 4 ms on every save of a 100 KB
// session, which is half the save. Best effort for the same reason — a save
// that wrote the transcript has done the thing that matters, and must not
// report failure because a cache beside it could not be replaced.
func writeMeta(dir string, m Meta) {
	m.Version = metaVersion
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = atomicfile.WriteWith(metaPath(dir, m.ID), append(data, '\n'), 0o600,
		atomicfile.WriteOptions{SkipDirSync: true, SkipFileSync: true})
}

// readMeta reads one header. A header that is missing, unreadable, of another
// version, or about another session is simply absent: the caller decodes the
// transcript instead.
func readMeta(dir, id string) (Meta, bool) {
	data, err := os.ReadFile(metaPath(dir, id))
	if err != nil {
		return Meta{}, false
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, false
	}
	if m.Version != metaVersion || m.ID != id || validateSessionID(m.ID) != nil {
		return Meta{}, false
	}
	return m, true
}

// headerFile is the subset of a transcript a header needs, for a session
// written before headers existed.
//
// Messages is []struct{} on purpose: encoding/json walks the transcript to
// count it and allocates none of it, so the migration path costs a read and a
// parse but never the megabytes of strings a full decode allocates.
type headerFile struct {
	ID        string            `json:"id"`
	Model     string            `json:"model"`
	Title     string            `json:"title"`
	CWD       string            `json:"cwd,omitempty"`
	Effort    string            `json:"effort,omitempty"`
	Connector string            `json:"connector,omitempty"`
	Pause     *continuity.Pause `json:"pause,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
	Messages  []struct{}        `json:"messages"`
}

// metaFromTranscript is the migration path: the header a session written
// before headers existed would have had.
func metaFromTranscript(dir, id string) (Meta, bool) {
	path := filepath.Join(dir, id+".json")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Meta{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, false
	}
	var file headerFile
	if err := json.Unmarshal(body, &file); err != nil {
		// One unreadable file must not cost the whole listing. A session
		// mid-write looks exactly like a corrupt one for a few milliseconds,
		// and a listing that blanks when that happens is worse than one that
		// shows a session late.
		return Meta{}, false
	}
	if validateSessionID(file.ID) != nil || file.ID != id {
		return Meta{}, false
	}
	return Meta{
		Version:      metaVersion,
		ID:           file.ID,
		Title:        file.Title,
		Model:        file.Model,
		CWD:          file.CWD,
		Effort:       file.Effort,
		Connector:    file.Connector,
		Pause:        file.Pause,
		UpdatedAt:    file.UpdatedAt,
		MessageCount: len(file.Messages),
	}, true
}

// List returns a header for every session in dir, newest first.
//
// The header is read when there is one and derived from the transcript when
// there is not, so a directory written by an older kolk lists exactly as it
// did — one full decode per session, once, until each is saved again.
func List(dir string) ([]Meta, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// `<id>.meta.json`, `<id>.cooldowns.json` and the pre-compaction
		// archives all end in .json; the transcript is the one file whose name
		// is the session id.
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if validateSessionID(id) != nil {
			continue
		}
		m, ok := readMeta(dir, id)
		if !ok {
			m, ok = metaFromTranscript(dir, id)
		}
		if !ok {
			continue // skip corrupt files rather than failing the whole list
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// RepairMeta rewrites a session's header from its transcript, and reports
// whether it had to. This is `kolk doctor`'s half of the bargain: the header
// is derived state, so something must be able to prove it still matches and
// put it back when it does not.
func RepairMeta(dir, id string) (bool, error) {
	fresh, ok := metaFromTranscript(dir, id)
	if !ok {
		return false, os.ErrNotExist
	}
	if stored, ok := readMeta(dir, id); ok && stored.matches(fresh) {
		return false, nil
	}
	writeMeta(dir, fresh)
	if _, ok := readMeta(dir, id); !ok {
		return false, os.ErrPermission
	}
	return true, nil
}

// matches compares two headers by what a listing shows. The transcript's own
// timestamp is compared through UpdatedAt, so a header left behind by a crash
// between the two writes is seen as the stale thing it is.
func (m Meta) matches(other Meta) bool {
	return m.ID == other.ID && m.Title == other.Title && m.Model == other.Model &&
		m.CWD == other.CWD && m.Effort == other.Effort && m.Connector == other.Connector &&
		m.MessageCount == other.MessageCount && m.UpdatedAt.Equal(other.UpdatedAt) &&
		samePause(m.Pause, other.Pause)
}

func samePause(a, b *continuity.Pause) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}
