// Package stats is the 100% local usage/rating store behind `kolk stats`.
// Every model call is appended as one JSON line to stats.jsonl in the data
// directory — no database, no telemetry, no network; grep it, jq it, back it
// up, or delete it. Ratings (`/rate 1-5`) are appended as their own lines and
// joined to turns at read time.
//
// The directory is passed in, never computed here: internal/paths owns that
// answer, and it lives in Data rather than Config because a usage log is state
// a person would be annoyed to lose, not a setting they would edit.
package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

const fileName = "stats.jsonl"

// Store implements engine.Recorder by writing to stats.jsonl in dir.
type Store struct {
	dir string
}

// NewStore constructs a Store writing to dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// RecordCall records a completed model attempt.
func (s *Store) RecordCall(r engine.CallRecord) error {
	return Append(s.dir, Record{
		Kind:                "call",
		Session:             r.Session,
		Turn:                r.Turn,
		Mode:                r.Mode,
		Effort:              r.Effort,
		Role:                r.Role,
		Model:               r.Model,
		PromptTokens:        r.PromptTokens,
		CompletionTokens:    r.CompletionTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		Cost:                r.Cost,
		Ms:                  r.Ms,
		ToolCalls:           r.ToolCalls,
	})
}

// RecordRating records a user rating for a turn.
func (s *Store) RecordRating(session, turn string, rating int) error {
	return Append(s.dir, Record{
		Kind:    "rating",
		Session: session,
		Turn:    turn,
		Rating:  rating,
	})
}

// Record is one model call (a turn may contain several: tool loops,
// orchestrator plan/subagents/synthesis).
type Record struct {
	Kind             string    `json:"kind"` // "call" or "rating"
	Time             time.Time `json:"time"`
	Session          string    `json:"session,omitempty"`
	Turn             string    `json:"turn,omitempty"` // ratings join on this
	Mode             string    `json:"mode,omitempty"` // chat | code | agent
	Effort           string    `json:"effort,omitempty"`
	Role             string    `json:"role,omitempty"` // main | planner | subagent | synthesis
	Model            string    `json:"model,omitempty"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	// Named for the OTel GenAI convention the dashboard will map onto, and
	// omitted entirely when a provider does not report cache usage.
	CacheReadTokens     int     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int     `json:"cache_creation_tokens,omitempty"`
	Cost                float64 `json:"cost,omitempty"`
	Ms                  int64   `json:"ms,omitempty"`
	ToolCalls           int     `json:"tool_calls,omitempty"`
	Rating              int     `json:"rating,omitempty"` // 1-5, kind=rating
}

func path(dir string) string { return filepath.Join(dir, fileName) }

// Append writes one record. Failures are returned but callers should treat
// them as non-fatal: stats must never break a working session.
func Append(dir string, r Record) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// Not a deferred Close: this is the write path for the file every cost
	// number is later read from, and a Close error is how a short write on a
	// full or networked disk reports itself.
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// Load reads all records; corrupt lines are skipped, a missing file is empty.
func Load(dir string) ([]Record, error) {
	records, _, err := LoadCounted(dir)
	return records, err
}

// maxStatsLine bounds one record. Anything longer is a partial write or a
// record that should never have been produced, and either way it is one line,
// not a reason to lose a history.
const maxStatsLine = 4 * 1024 * 1024

// LoadCounted reads the usage log, reporting how many lines it could not
// understand.
//
// An unreadable line is skipped rather than fatal — a single bad append, or a
// write interrupted by a power cut, must not cost a user every record they
// have. But it is counted and surfaced, because stats that quietly omit half a
// history are worse than stats that admit they are incomplete.
func LoadCounted(dir string) ([]Record, int, error) {
	f, err := os.Open(path(dir))
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }() // read path: nothing to lose on close

	var out []Record
	skipped := 0
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := readStatsLine(reader)
		if len(line) > 0 || err == nil {
			switch {
			case len(bytes.TrimSpace(line)) == 0:
				// Blank lines are formatting, not lost data.
			default:
				var r Record
				if json.Unmarshal(line, &r) == nil && r.Kind != "" {
					out = append(out, r)
				} else {
					skipped++
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, skipped, nil
			}
			if errors.Is(err, errLineTooLong) {
				skipped++
				continue
			}
			return out, skipped, err
		}
	}
}

var errLineTooLong = errors.New("stats line exceeds the maximum record size")

// readStatsLine returns one line, discarding the remainder of any line too long
// to be a record so that reading can continue with the next one.
func readStatsLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > maxStatsLine {
			// Drain to the end of this line, then report it as one skip.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, errLineTooLong
		}
		line = append(line, chunk...)
		if err == nil {
			return bytes.TrimRight(line, "\r\n"), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimRight(line, "\r\n"), err
	}
}

// ModelRow is one line of the dashboard: everything known about one model.
type ModelRow struct {
	Model     string
	Calls     int
	Tokens    int // prompt + completion
	Cost      float64
	AvgMs     int64
	Ratings   int
	AvgRating float64
	ByMode    map[string]int // calls per mode
	LastUsed  time.Time
	ToolCalls int
}

// Aggregate joins calls with their turn ratings and folds them per model,
// sorted by spend (highest first).
func Aggregate(recs []Record) []ModelRow {
	// a rating applies to every call of the rated turn (they share the work)
	turnRating := map[string]int{}
	for _, r := range recs {
		if r.Kind == "rating" && r.Turn != "" && r.Rating >= 1 && r.Rating <= 5 {
			turnRating[r.Turn] = r.Rating // last rating for a turn wins
		}
	}

	rows := map[string]*ModelRow{}
	var totalMs = map[string]int64{}
	var ratingSum = map[string]int{}
	for _, r := range recs {
		if r.Kind != "call" || r.Model == "" {
			continue
		}
		row, ok := rows[r.Model]
		if !ok {
			row = &ModelRow{Model: r.Model, ByMode: map[string]int{}}
			rows[r.Model] = row
		}
		row.Calls++
		row.Tokens += r.PromptTokens + r.CompletionTokens
		row.Cost += r.Cost
		row.ToolCalls += r.ToolCalls
		totalMs[r.Model] += r.Ms
		if r.Mode != "" {
			row.ByMode[r.Mode]++
		}
		if r.Time.After(row.LastUsed) {
			row.LastUsed = r.Time
		}
		if rating, ok := turnRating[r.Turn]; ok {
			row.Ratings++
			ratingSum[r.Model] += rating
		}
	}

	out := make([]ModelRow, 0, len(rows))
	for _, row := range rows {
		if row.Calls > 0 {
			row.AvgMs = totalMs[row.Model] / int64(row.Calls)
		}
		if row.Ratings > 0 {
			row.AvgRating = float64(ratingSum[row.Model]) / float64(row.Ratings)
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Calls > out[j].Calls
	})
	return out
}

// Render draws the dashboard table for the terminal.
func Render(rows []ModelRow) string {
	if len(rows) == 0 {
		return "no usage recorded yet — stats are collected locally as you use kolk.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-42s %6s %10s %9s %7s %7s  %s\n",
		"MODEL", "CALLS", "TOKENS", "COST", "AVG", "RATING", "MODES")
	var totCost float64
	var totCalls, totTokens int
	for _, r := range rows {
		rating := "  —"
		if r.Ratings > 0 {
			rating = fmt.Sprintf("%.1f★", r.AvgRating)
		}
		fmt.Fprintf(&b, "%-42s %6d %10d %9s %6dms %7s  %s\n",
			trunc(r.Model, 42), r.Calls, r.Tokens, money(r.Cost), r.AvgMs, rating, modes(r.ByMode))
		totCost += r.Cost
		totCalls += r.Calls
		totTokens += r.Tokens
	}
	fmt.Fprintf(&b, "%-42s %6d %10d %9s\n", "TOTAL", totCalls, totTokens, money(totCost))
	return b.String()
}

func money(v float64) string {
	if v == 0 {
		return "—"
	}
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func modes(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
