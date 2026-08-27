package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/protocol"
)

// blockedTailBytes is how much of a journal is read to answer "is this session
// waiting for me?".
//
// The number comes from the constraint, not from taste. I27.2 made this listing
// cheap on purpose — header-only session reads, no lock stealing — and there
// are hundreds of journals on a working machine. Decoding each one in full
// would be orders of magnitude worse for a view somebody polls, and a listing
// that is expensive is a listing that gets called less often than it should.
//
// 64 KiB holds far more than the handful of events between a prompt being
// raised and it being answered. If a request scrolls out of that window it was
// followed by megabytes of other work, which means the session was not waiting
// on it.
const blockedTailBytes = 64 * 1024

// permissionEventMarker is the cheap test that keeps this read cheap: both
// event names share it, and nothing else in the journal does.
const permissionEventMarker = `"type":"permission.`

// Blocked is what a session is waiting for.
type Blocked struct {
	ID     string // the permission request's id
	Tool   string
	Detail string
}

// BlockedOn reports whether a session is stopped at an unanswered permission
// prompt.
//
// "Blocked" is the decisive field on a card: a session waiting on a prompt is
// not slow, it has *stopped*, and it will stay stopped until a person answers.
// That is the one thing worth seeing when scanning a list of sessions.
//
// The rule is the doc's: the last `permission.requested` with no matching
// `permission.resolved`. Requests are correlated by id rather than by position,
// because answering one prompt does not unblock a later one.
func BlockedOn(dir, id string) (Blocked, bool) {
	tail, err := readTail(filepath.Join(dir, id+".events.ndjson"), blockedTailBytes)
	if err != nil {
		return Blocked{}, false
	}

	// The common case is a session with no permission event in its tail at all.
	// One scan of the whole window answers that without splitting it into
	// lines, which is most of the remaining cost.
	if !strings.Contains(string(tail), permissionEventMarker) {
		return Blocked{}, false
	}

	resolvedIDs := map[string]bool{}
	var open []Blocked
	for _, line := range strings.Split(string(tail), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Decoding every line costs more than the whole feature is worth:
		// measured over 559 journals it was 2.6 ms each, 1.4 s for a listing.
		// A permission event is a rare line in a journal full of message
		// deltas, and a substring scan rejects the rest for almost nothing.
		if !strings.Contains(line, permissionEventMarker) {
			continue
		}
		var envelope protocol.Envelope
		// A journal is appended to by a live process, so the first line of a
		// tail read is usually a fragment and the last one can be half-written.
		// A line that does not decode costs itself and nothing else.
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case protocol.EventPermissionRequested:
			var data protocol.PermissionRequestedData
			if json.Unmarshal(envelope.Data, &data) == nil && data.ID != "" {
				open = append(open, Blocked{ID: data.ID, Tool: data.Tool, Detail: data.Detail})
			}
		case protocol.EventPermissionResolved:
			var data protocol.PermissionResolvedData
			if json.Unmarshal(envelope.Data, &data) == nil {
				resolvedIDs[data.ID] = true
			}
		}
	}

	// Latest first: the newest unanswered request is the one being waited on.
	for i := len(open) - 1; i >= 0; i-- {
		if !resolvedIDs[open[i].ID] {
			return open[i], true
		}
	}
	return Blocked{}, false
}

// readTail returns at most limit bytes from the end of a file.
func readTail(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size > limit {
		if _, err := file.Seek(size-limit, 0); err != nil {
			return nil, err
		}
	}
	buffer := make([]byte, min64(size, limit))
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return nil, err
	}
	return buffer[:n], nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
