// Package session persists conversations to disk so they survive restarts
// and can be resumed. Each session is a single JSON file under the sessions
// directory; file-change checkpoints for a session live in a sibling
// "<id>.ckpt" directory.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/xid"
)

// FunctionCall is the serialized function invocation.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is the serialized tool call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// Message is the frozen persisted session message shape on disk.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Session struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	// CWD is the directory the session was started in. Sessions written before
	// this field existed have none, which is why every match here is explicit
	// rather than a comparison against the empty string.
	CWD string `json:"cwd,omitempty"`
	// Effort is the level the session's dial ran at, persisted so a resume
	// lands at the same width of effort instead of the configured default.
	// Written before this field existed are sessions with none.
	Effort string `json:"effort,omitempty"`
	// Mode is the mode the session was left in — chat, code or agent —
	// persisted so a resume lands in it instead of the default. Plan 06 §3
	// promised this from the start ("written on switch and on save; resume
	// restores it") and nothing had built it: the F7.2 transcript re-issued
	// /mode agent on every wake. Sessions written before this field have none.
	Mode string `json:"mode,omitempty"`
	// Connector records the subscription connector the session ran on, when
	// it ran on one. A plan model re-derives its connector from its name, so
	// this is display state, never routing state.
	Connector string `json:"connector,omitempty"`
	// ProviderState is opaque provider-side state worth resuming: for Claude,
	// the vendor conversation handle. Kolk mints the handle itself, so a later
	// Kolkrabbi process can --resume the same vendor conversation without the
	// child having ever reported anything secret. Names a conversation, never
	// a credential.
	ProviderState string `json:"provider_state,omitempty"`
	// TitleAuto marks a title Kolkrabbi derived rather than one the user chose.
	TitleAuto bool      `json:"title_auto,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  []Message `json:"messages"`

	// messagesMu guards Messages. The TUI samples context and cost from a
	// drawing goroutine while a turn appends, so the one field two goroutines
	// actually share is serialized instead of left to schedule luck.
	messagesMu sync.Mutex
	// dir is where this session is stored; not serialized

	dir string // where this session is stored; not serialized
}

func toProvider(m Message) provider.Message {
	pm := provider.Message{
		Role:       m.Role,
		Content:    m.Content,
		Reasoning:  m.Reasoning,
		ToolCallID: m.ToolCallID,
	}
	if len(m.ToolCalls) > 0 {
		pm.ToolCalls = make([]provider.ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			pm.ToolCalls[i] = provider.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: provider.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}
	return pm
}

func fromProvider(pm provider.Message) Message {
	m := Message{
		Role:       pm.Role,
		Content:    pm.Content,
		Reasoning:  pm.Reasoning,
		ToolCallID: pm.ToolCallID,
	}
	if len(pm.ToolCalls) > 0 {
		m.ToolCalls = make([]ToolCall, len(pm.ToolCalls))
		for i, tc := range pm.ToolCalls {
			m.ToolCalls[i] = ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}
	return m
}

// New creates a fresh, not-yet-saved session in dir.
func New(dir, model string) *Session {
	now := time.Now()
	// A session belongs to the project it was started in. Failing to read the
	// working directory leaves it empty, which simply means "no project",
	// rather than tying the session to the wrong one.
	cwd, _ := os.Getwd()
	return &Session{
		ID:        xid.New(xid.Session),
		Model:     model,
		CreatedAt: now,
		CWD:       cwd,
		dir:       dir,
	}
}

// LatestForDir resumes this project's most recent session, falling back to the
// most recent overall.
//
// Standing in a directory and asking to resume means the work done here, not
// whatever happened to be typed last in another window. A session with no
// recorded directory belongs to no project and is only ever reachable through
// the fallback: matching it against every directory would make one old session
// hijack resume everywhere.
func LatestForDir(dir, cwd string) (*Session, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	if cwd != "" {
		for _, candidate := range all {
			if candidate.CWD != "" && sameDir(candidate.CWD, cwd) {
				return candidate, nil
			}
		}
	}
	return all[0], nil
}

// sameDir compares directories through symlinks, so /tmp and /private/tmp are
// one project rather than two.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func validateSessionID(id string) error {
	if xid.KindOf(id) != xid.Session {
		return fmt.Errorf("invalid session id %q: expected xid.Session", id)
	}
	return nil
}

func (s *Session) path() string {
	if err := validateSessionID(s.ID); err != nil {
		panic(err)
	}
	return filepath.Join(s.dir, s.ID+".json")
}

// CkptDir is where this session's file checkpoints are stored.
func (s *Session) CkptDir() string {
	if err := validateSessionID(s.ID); err != nil {
		panic(err)
	}
	return filepath.Join(s.dir, s.ID+".ckpt")
}

// Save writes the session atomically and durably.
//
// A transcript is the one thing here a person cannot reconstruct, so this goes
// through internal/atomicfile rather than a hand-rolled tmp-and-rename: that
// buys an fsync (a rename is atomic against other processes but not against
// power loss) and a unique temp name (a REPL in one terminal and `kolk -p` in
// another used to write the same "x.json.tmp" and shred each other).
func (s *Session) Save() error {
	if err := validateSessionID(s.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	// The snapshot is taken under the same lock the mutations hold, so what
	// reaches disk is a state the session was actually in -- never a slice
	// half-appended by the turn that is running while the autosave fires
	// (V34.2b). The write itself happens outside the lock.
	s.messagesMu.Lock()
	b, err := json.MarshalIndent(s, "", " ")
	s.messagesMu.Unlock()
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path(), b, 0o600)
}

// SetTitleFromInput sets a human-readable title from the first user message.
// SetTitleFromInput derives a first title from what the user typed. The title
// is marked automatic so Kolkrabbi may later improve on its own guess without
// ever overwriting a name a person chose.
func (s *Session) SetTitleFromInput(input string) {
	if s.Title != "" {
		return
	}
	s.Title = trimTitle(strings.Join(strings.Fields(input), " "))
	s.TitleAuto = s.Title != ""
}

// SetTitle records a title as chosen rather than derived.
func (s *Session) SetTitle(title string) {
	s.Title = trimTitle(strings.Join(strings.Fields(title), " "))
	s.TitleAuto = false
}

// SetAutoTitle replaces a derived title with a better derived one, and does
// nothing to a title the user chose.
func (s *Session) SetAutoTitle(title string) bool {
	if !s.TitleAuto {
		return false
	}
	trimmed := trimTitle(strings.Join(strings.Fields(title), " "))
	if trimmed == "" {
		return false
	}
	s.Title = trimmed
	// One improvement, then stable: a title that keeps changing under the user
	// is worse than a mediocre one that stays put.
	s.TitleAuto = false
	return true
}

// TitleIsAuto reports whether the current title was derived rather than chosen.
func (s *Session) TitleIsAuto() bool { return s.TitleAuto }

const maxTitleBytes = 60

// trimTitle caps a title without splitting a rune. The cut is at a byte count,
// so any prompt not written in English can land mid-character.
func trimTitle(t string) string {
	if len(t) <= maxTitleBytes {
		return t
	}
	cut := t[:maxTitleBytes]
	for len(cut) > 0 {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

func (s *Session) SessionID() string             { return s.ID }
func (s *Session) SessionTitle() string          { return s.Title }
func (s *Session) ModelName() string             { return s.Model }
func (s *Session) SetModelName(m string)         { s.Model = m }
func (s *Session) SessionEffort() string         { return s.Effort }
func (s *Session) SetEffort(level string)        { s.Effort = level }
func (s *Session) SessionMode() string           { return s.Mode }
func (s *Session) SetMode(mode string)           { s.Mode = mode }
func (s *Session) ConnectorName() string         { return s.Connector }
func (s *Session) SetConnector(n string)         { s.Connector = n }
func (s *Session) ProviderStateName() string     { return s.ProviderState }
func (s *Session) SetProviderStateName(v string) { s.ProviderState = v }
func (s *Session) GetMessages() []provider.Message {
	s.messagesMu.Lock()
	defer s.messagesMu.Unlock()
	out := make([]provider.Message, len(s.Messages))
	for i, m := range s.Messages {
		out[i] = toProvider(m)
	}
	return out
}
func (s *Session) SetMessages(msgs []provider.Message) {
	s.messagesMu.Lock()
	defer s.messagesMu.Unlock()
	s.Messages = make([]Message, len(msgs))
	for i, m := range msgs {
		s.Messages[i] = fromProvider(m)
	}
}
func (s *Session) AppendMessage(msg provider.Message) {
	s.messagesMu.Lock()
	s.Messages = append(s.Messages, fromProvider(msg))
	s.messagesMu.Unlock()
}

func Load(dir, id string) (*Session, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("session file %s is not a regular file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if err := validateSessionID(s.ID); err != nil {
		return nil, fmt.Errorf("invalid session id in %s.json: %w", id, err)
	}
	if s.ID != id {
		return nil, fmt.Errorf("session id %q does not match filename %q", s.ID, id)
	}
	s.dir = dir
	return &s, nil
}

// List returns all sessions in dir, newest first.
func List(dir string) ([]*Session, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		s, err := Load(dir, strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue // skip corrupt files rather than failing the whole list
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Latest returns the most recently updated session, or nil if none exist.
func Latest(dir string) (*Session, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[0], nil
}

// CompactionArchives are the pre-compaction conversations kept for one session.
func CompactionArchives(dir, id string) ([]string, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	return filepath.Glob(filepath.Join(dir, id+".pre-compact-*.json"))
}

// Delete removes a session file, its checkpoint directory, and every
// pre-compaction archive belonging to it.
//
// The archives hold the conversation a compaction replaced, so leaving them
// behind would mean deleting a session that is still readable on disk.
func Delete(dir, id string) error {
	if err := validateSessionID(id); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	archives, err := CompactionArchives(dir, id)
	if err != nil {
		return err
	}
	for _, archive := range archives {
		if err := os.Remove(archive); err != nil {
			return fmt.Errorf("removing compaction archive %s: %w", archive, err)
		}
	}
	// RemoveAll is nil for a path that does not exist, so this only reports a
	// checkpoint directory that really could not be removed — which matters,
	// because a stale .ckpt outlives the session it belonged to.
	return os.RemoveAll(filepath.Join(dir, id+".ckpt"))
}

// Clear removes all sessions and checkpoints in dir.
func Clear(dir string) error {
	all, err := List(dir)
	if err != nil {
		return err
	}
	for _, s := range all {
		if err := Delete(dir, s.ID); err != nil {
			return fmt.Errorf("deleting %s: %w", s.ID, err)
		}
	}
	return nil
}
