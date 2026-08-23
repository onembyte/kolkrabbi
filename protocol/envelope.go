package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Envelope is the one wrapper carried by every Kolkrabbi event transport.
// Field order is wire-significant for conformance fixtures and is therefore
// kept identical to spec/schemas/envelope.json.
type Envelope struct {
	Seq       uint64          `json:"seq"`
	Timestamp time.Time       `json:"ts"`
	Session   string          `json:"session"`
	Turn      string          `json:"turn"`
	Type      EventType       `json:"type"`
	Data      json.RawMessage `json:"data"`
}

func (e Envelope) validate() error {
	if e.Seq == 0 {
		return fmt.Errorf("protocol: seq must be positive")
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("protocol: ts is required")
	}
	if !validID(e.Session, 's') {
		return fmt.Errorf("protocol: session must be a canonical s_ ULID")
	}
	if !validID(e.Turn, 't') {
		return fmt.Errorf("protocol: turn must be a canonical t_ ULID")
	}
	if !validEventType(string(e.Type)) {
		return fmt.Errorf("protocol: type must be lowercase dot-separated words")
	}
	data := bytes.TrimSpace(e.Data)
	if len(data) == 0 || data[0] != '{' || !json.Valid(data) {
		return fmt.Errorf("protocol: data must be a JSON object")
	}
	return validateEventData(e.Type, data)
}

func validID(id string, kind byte) bool {
	if len(id) != 28 || id[0] != kind || id[1] != '_' {
		return false
	}
	body := id[2:]
	if body[0] < '0' || body[0] > '7' {
		return false
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for i := 1; i < len(body); i++ {
		if !strings.ContainsRune(alphabet, rune(body[i])) {
			return false
		}
	}
	return true
}

func validEventType(name string) bool {
	if name == "" {
		return false
	}
	start := true
	for _, r := range name {
		switch {
		case start && r >= 'a' && r <= 'z':
			start = false
		case !start && r >= 'a' && r <= 'z':
		case !start && r >= '0' && r <= '9':
		case !start && r == '.':
			start = true
		default:
			return false
		}
	}
	return !start
}
