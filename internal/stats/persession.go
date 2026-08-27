package stats

import (
	"encoding/json"
	"os"
	"strings"
)

// costBySession totals what each session has spent, by decoding every row.
//
// It is unexported on purpose. CostForSessions is what callers want — it reads
// the same file an order of magnitude faster by rejecting rows before parsing
// them — and this stays as the plain implementation the cheap one is checked
// against, so a bug in the fast path shows up as a disagreement rather than as
// a wrong number in a listing. A reference implementation is not API.
//
// One read of one file answers for every card, which is why this is a map
// rather than a per-session lookup: `stats.jsonl` holds every session's calls
// together, so asking it once per card would re-read the same file once per
// card. I27.3 measured what that class of mistake costs.
//
// Ratings are skipped. A rating costs nothing, and joining it in would inflate
// the number a person reads when deciding whether to stop a session.
//
// A session that spent nothing still appears, with zero. Absent and zero mean
// different things — "no calls recorded" and "ran on a free model" — and a
// listing that collapsed them would report a working free session as unknown.
func costBySession(dir string) (map[string]float64, error) {
	records, err := Load(dir)
	if err != nil {
		return nil, err
	}
	costs := make(map[string]float64, len(records))
	for _, record := range records {
		if record.Kind != "call" || record.Session == "" {
			continue
		}
		costs[record.Session] += record.Cost
	}
	return costs, nil
}

// CostForSessions totals only the sessions a caller is going to show.
//
// costBySession decodes every row, which measured 210 ms over a 4.4 MB log — a
// listing that pays that gets polled less often than it should, which is the
// failure I27.3 already measured once. A listing shows a handful of sessions
// out of hundreds, so this rejects a row on two substring scans before any JSON
// is parsed: the row must be a call, and its session must be one of the wanted
// ones.
func CostForSessions(dir string, wanted map[string]bool) (map[string]float64, error) {
	if len(wanted) == 0 {
		return map[string]float64{}, nil
	}
	body, err := os.ReadFile(path(dir))
	if os.IsNotExist(err) {
		return map[string]float64{}, nil
	}
	if err != nil {
		return nil, err
	}

	costs := make(map[string]float64, len(wanted))
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.Contains(line, callKindMarker) {
			continue
		}
		id := sessionIDOf(line)
		if id == "" || !wanted[id] {
			continue
		}
		var record Record
		if json.Unmarshal([]byte(line), &record) != nil {
			continue // one unreadable line costs one line, not the totals
		}
		costs[record.Session] += record.Cost
	}
	return costs, nil
}

const (
	callKindMarker  = `"kind":"call"`
	sessionKeyStart = `"session":"`
)

// sessionIDOf pulls the session id out of a row without parsing it.
func sessionIDOf(line string) string {
	start := strings.Index(line, sessionKeyStart)
	if start < 0 {
		return ""
	}
	rest := line[start+len(sessionKeyStart):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
