package xid

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewHasTheRightShape(t *testing.T) {
	id := New(Session)
	if !strings.HasPrefix(id, "s_") {
		t.Errorf("New(Session) = %q, want an s_ prefix", id)
	}
	if len(id) != len("s_")+encodedLen {
		t.Errorf("New(Session) = %q, length %d, want %d", id, len(id), len("s_")+encodedLen)
	}
	if !Valid(id) {
		t.Errorf("%q did not validate", id)
	}
	if KindOf(id) != Session {
		t.Errorf("KindOf(%q) = %q", id, KindOf(id))
	}
}

func TestTypedValidationRejectsMissingOrUnknownKinds(t *testing.T) {
	const body = "01ARYZ6S41TSV4RRFFQ69G5FAV"
	for _, id := range []string{
		body,
		"_" + body,
		"unknown_" + body,
	} {
		t.Run(id, func(t *testing.T) {
			if Valid(id) {
				t.Errorf("Valid(%q) = true; a kolk id requires a registered kind", id)
			}
			if _, err := Time(id); err == nil {
				t.Errorf("Time(%q) accepted an untyped id", id)
			}
			if got := KindOf(id); got != "" {
				t.Errorf("KindOf(%q) = %q, want an empty kind", id, got)
			}
		})
	}
}

func TestNewRejectsAnUnknownKind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New accepted an unknown kind")
		}
	}()
	_ = New(Kind("unknown"))
}

// This vector is published by the canonical JavaScript implementation linked
// from the ULID specification. It keeps encode and Time from agreeing with
// each other about a representation no other language understands.
func TestOfficialULIDVector(t *testing.T) {
	const body = "01ARYZ6S41TSV4RRFFQ69G5FAV"
	const millis = uint64(1_469_918_176_385)
	entropy := [10]byte{0xd6, 0x76, 0x4c, 0x61, 0xef, 0xb9, 0x93, 0x02, 0xbd, 0x5b}

	if got := encode(millis, entropy); got != body {
		t.Errorf("encode = %q, want official vector %q", got, body)
	}

	want := time.UnixMilli(int64(millis)).UTC()
	for _, encoded := range []string{body, strings.ToLower(body)} {
		got, err := Time("s_" + encoded)
		if err != nil {
			t.Errorf("Time(%q): %v", encoded, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("Time(%q) = %s, want %s", encoded, got, want)
		}
	}
}

// Sortable as text is the property that makes `ls` on the sessions directory
// come out in the order the conversations happened.
func TestIDsSortIntoCreationOrder(t *testing.T) {
	const n = 500
	ids := make([]string, n)
	for i := range ids {
		ids[i] = New(Event)
	}

	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)

	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("id %d is out of order: created %q, sorted to position of %q", i, ids[i], sorted[i])
		}
	}
}

// A turn emits many events in the same millisecond, and a tie in a supposedly
// ordered log is a rendering bug on a client reconnecting mid-stream.
func TestIDsAreStrictlyIncreasingWithinAMillisecond(t *testing.T) {
	g := &generator{}
	now := time.UnixMilli(1_700_000_000_000)

	prev := ""
	for i := 0; i < 1000; i++ {
		id := g.next(now) // the same instant, every time
		if id <= prev {
			t.Fatalf("id %d did not increase: %q then %q", i, prev, id)
		}
		prev = id
	}
}

// A clock that goes backwards — NTP correction, a suspended laptop, a
// container resuming — must not produce a log that goes back in time.
func TestClockGoingBackwardsStillIncreases(t *testing.T) {
	g := &generator{}
	base := time.UnixMilli(1_700_000_000_000)

	first := g.next(base)
	for i := 1; i <= 50; i++ {
		id := g.next(base.Add(-time.Duration(i) * time.Millisecond))
		if id <= first {
			t.Fatalf("an id from a backwards clock went backwards: %q then %q", first, id)
		}
		first = id
	}
}

func TestRandomnessOverflowAdvancesTheLogicalMillisecond(t *testing.T) {
	base := time.UnixMilli(1_700_000_000_000)
	g := &generator{lastMS: uint64(base.UnixMilli())}
	for i := range g.lastRand {
		g.lastRand[i] = 0xff
	}
	previous := encode(g.lastMS, g.lastRand)

	got := g.next(base)
	if got <= previous {
		t.Fatalf("randomness overflow moved backward: %q then %q", previous, got)
	}
	wantTime := base.Add(time.Millisecond)
	gotTime, err := Time("e_" + got)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(wantTime) {
		t.Errorf("overflow time = %s, want logical time %s", gotTime, wantTime)
	}
}

func TestIDsAreUniqueUnderConcurrency(t *testing.T) {
	const workers, each = 16, 500

	var mu sync.Mutex
	seen := make(map[string]bool, workers*each)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, each)
			for i := range local {
				local[i] = New(Turn)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				if seen[id] {
					t.Errorf("duplicate id %q", id)
				}
				seen[id] = true
			}
		}()
	}
	wg.Wait()

	if len(seen) != workers*each {
		t.Errorf("got %d unique ids, want %d", len(seen), workers*each)
	}
}

// Recovering the time is how a session listing shows when a conversation
// started without opening its file.
func TestTimeRoundTrips(t *testing.T) {
	g := &generator{}
	for _, want := range []time.Time{
		time.UnixMilli(0),
		time.UnixMilli(1_700_000_000_000),
		time.Date(2026, 8, 23, 1, 2, 3, 456_000_000, time.UTC),
	} {
		id := "s_" + g.next(want)
		got, err := Time(id)
		if err != nil {
			t.Errorf("Time(%q): %v", id, err)
			continue
		}
		if !got.Equal(want.UTC().Truncate(time.Millisecond)) {
			t.Errorf("Time(%q) = %s, want %s", id, got, want.UTC())
		}
	}
}

func TestTimeRejectsRubbish(t *testing.T) {
	// "u" is excluded from Crockford base32 precisely because it is
	// confusable; "x" is NOT excluded, so a string of x's is a well-formed id.
	for _, id := range []string{
		"", "s_", "nonsense", "s_tooshort",
		"s_" + strings.Repeat("u", 26),
		"s_" + strings.Repeat("l", 26),
		"s_" + strings.Repeat("z", 26), // first character carries only 3 bits
		"s_" + strings.Repeat("x", 25), // one short
	} {
		if _, err := Time(id); err == nil {
			t.Errorf("Time(%q) accepted a malformed id", id)
		}
		if Valid(id) {
			t.Errorf("Valid(%q) = true", id)
		}
	}
}

// Crockford base32 omits I, L, O and U so an id read aloud or retyped from a
// screenshot cannot be ambiguous.
func TestAlphabetHasNoAmbiguousCharacters(t *testing.T) {
	for _, c := range "ILOU" {
		if strings.ContainsRune(crockford, c) {
			t.Errorf("the alphabet contains %q, which is confusable", c)
		}
	}
	if len(crockford) != 32 {
		t.Errorf("the alphabet has %d characters, want 32", len(crockford))
	}
	for i := 0; i < 2000; i++ {
		id := New(Event)
		for _, c := range strings.TrimPrefix(id, "e_") {
			if !strings.ContainsRune(crockford, c) {
				t.Fatalf("id %q contains %q, which is outside the alphabet", id, c)
			}
		}
	}
}

func TestKindsAreDistinct(t *testing.T) {
	seen := map[Kind]bool{}
	for _, k := range []Kind{Session, Turn, Event, Call, Task} {
		if seen[k] {
			t.Errorf("duplicate kind prefix %q", k)
		}
		seen[k] = true
		if strings.Contains(string(k), "_") {
			t.Errorf("kind %q contains the separator", k)
		}
		id := New(k)
		if !Valid(id) || KindOf(id) != k {
			t.Errorf("New(%q) returned invalid or mistyped id %q", k, id)
		}
	}
}

func FuzzParserConsistency(f *testing.F) {
	for _, seed := range []string{
		"",
		"s_01ARYZ6S41TSV4RRFFQ69G5FAV",
		"unknown_01ARYZ6S41TSV4RRFFQ69G5FAV",
		"e_" + strings.Repeat("0", encodedLen),
		"t_" + strings.Repeat("z", encodedLen),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, id string) {
		_, err := Time(id)
		valid := Valid(id)
		kind := KindOf(id)
		if valid != (err == nil) {
			t.Fatalf("parser disagreement for %q: Valid=%v, Time error=%v", id, valid, err)
		}
		if valid && !knownKind(kind) {
			t.Fatalf("valid id %q has unknown kind %q", id, kind)
		}
		if !valid && kind != "" {
			t.Fatalf("invalid id %q reports kind %q", id, kind)
		}
	})
}
