// Package xid makes the identifiers kolk puts on sessions, turns and events.
//
// Three properties, each of which is load-bearing somewhere specific:
//
//   - Sortable as text. `ls` on the sessions directory comes out in the order
//     the conversations happened, a database index on the id is also a time
//     index, and merging two event logs is a sort.
//   - Monotonic within a millisecond. A turn emits many events in the same
//     millisecond, and a tie in a supposedly ordered log is a rendering bug on
//     a tablet reconnecting mid-stream.
//   - Prefixed by kind. `s_`, `t_`, `e_` — so an id in a log line, a URL or a
//     bug report says what it identifies without needing its context.
//
// The encoding is ULID: a 48-bit millisecond timestamp followed by 80 bits of
// randomness, in Crockford base32. Chosen because it is an existing spec other
// languages already have libraries for, which matters when the protocol grows
// Swift and Kotlin clients that have to parse these.
package xid

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Kind prefixes an id so it is self-describing.
type Kind string

const (
	Session Kind = "s"
	Turn    Kind = "t"
	Event   Kind = "e"
	Call    Kind = "c" // a tool call
	Task    Kind = "k" // a subagent task
)

// crockford is Crockford base32: no I, L, O or U, so an id read aloud or
// retyped from a screenshot cannot be ambiguous.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// encodedLen is the ULID text length: 10 characters of time, 16 of randomness.
const encodedLen = 26

var gen = &generator{}

// New returns a new id of the given kind, e.g. "s_01JQZK9XW8000000000000000".
// An unknown kind is a programmer error: emitting an id that Valid rejects
// would move that error into a filename, log, or protocol frame.
func New(k Kind) string {
	if !knownKind(k) {
		panic(fmt.Sprintf("xid: unknown kind %q", k))
	}
	return string(k) + "_" + gen.next(time.Now())
}

// generator serialises id creation so that two ids made in the same
// millisecond are still strictly ordered.
type generator struct {
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
}

func (g *generator) next(now time.Time) string {
	ms := uint64(now.UnixMilli())

	g.mu.Lock()
	defer g.mu.Unlock()

	switch {
	case ms > g.lastMS:
		g.lastMS = ms
		fill(&g.lastRand)
	default:
		// The same millisecond increments the random part. A backward clock —
		// NTP correction, a suspended laptop, a container resuming — does the
		// same against the last logical millisecond. In both cases ordering is
		// more important than repeating wall time.
		ms = g.lastMS
		if increment(&g.lastRand) {
			// ULID's 80-bit counter is exhausted. Move the logical clock forward
			// instead of wrapping the id backward, then start a fresh counter.
			g.lastMS++
			ms = g.lastMS
			fill(&g.lastRand)
		}
	}

	return encode(ms, g.lastRand)
}

func fill(b *[10]byte) {
	// crypto/rand.Read never fails on any platform Go supports; it panics
	// internally on a broken entropy source rather than returning an error.
	_, _ = rand.Read(b[:])
}

// increment adds one to the 80-bit random field, big-endian, and reports
// whether it wrapped back to zero.
func increment(b *[10]byte) bool {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return false
		}
	}
	return true
}

// encode renders the 48-bit timestamp and 80-bit payload as 26 Crockford
// base32 characters.
func encode(ms uint64, entropy [10]byte) string {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[:8], ms<<16)
	copy(raw[6:], entropy[:])

	var out [encodedLen]byte
	// 26 characters × 5 bits = 130 bits, of which the top 2 are padding, so the
	// first character only ever carries 3 bits — which is why a ULID's first
	// character is never above '7'.
	for i := 0; i < encodedLen; i++ {
		bitPos := i*5 - 2
		out[i] = crockford[extract(raw, bitPos)]
	}
	return string(out[:])
}

// extract reads the 5 bits starting at bitPos, treating bits before 0 as zero.
func extract(raw [16]byte, bitPos int) byte {
	var v byte
	for i := 0; i < 5; i++ {
		p := bitPos + i
		v <<= 1
		if p < 0 {
			continue
		}
		if raw[p/8]&(1<<(7-uint(p%8))) != 0 {
			v |= 1
		}
	}
	return v
}

// Time recovers the creation time from a typed kolk id. It is how a session
// listing can show when a conversation started without opening its file.
func Time(id string) (time.Time, error) {
	_, body, err := split(id)
	if err != nil {
		return time.Time{}, err
	}

	var ms uint64
	// The first 10 characters carry the 48-bit timestamp.
	for i := 0; i < 10; i++ {
		ms = ms<<5 | uint64(strings.IndexByte(crockford, upper(body[i])))
	}
	// Drop the 2 padding bits the encoding introduces at the front.
	ms &= (1 << 48) - 1
	return time.UnixMilli(int64(ms)).UTC(), nil
}

func split(id string) (Kind, string, error) {
	prefix, body, ok := strings.Cut(id, "_")
	kind := Kind(prefix)
	if !ok || !knownKind(kind) {
		return "", "", fmt.Errorf("%q is not a kolk id", id)
	}
	if len(body) != encodedLen {
		return "", "", fmt.Errorf("%q is not a kolk id", id)
	}

	// The whole body must be in the alphabet, not just the timestamp half: an
	// id that is only half-checked is one that round-trips through a URL or a
	// filename and then fails somewhere less convenient.
	for i := 0; i < len(body); i++ {
		if strings.IndexByte(crockford, upper(body[i])) < 0 {
			return "", "", fmt.Errorf("%q is not a kolk id", id)
		}
	}
	// 26 characters × 5 bits = 130, and a ULID carries 128, so the first
	// character only ever holds 3 bits — anything above '7' is a timestamp
	// beyond the year 10889 and therefore not something kolk produced.
	if body[0] > '7' {
		return "", "", fmt.Errorf("%q is not a kolk id", id)
	}
	return kind, body, nil
}

func knownKind(kind Kind) bool {
	switch kind {
	case Session, Turn, Event, Call, Task:
		return true
	default:
		return false
	}
}

// KindOf reports what a valid id identifies, or "" for malformed or untyped
// input.
func KindOf(id string) Kind {
	kind, _, err := split(id)
	if err != nil {
		return ""
	}
	return kind
}

// Valid reports whether id is well-formed.
func Valid(id string) bool {
	_, err := Time(id)
	return err == nil
}

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}
