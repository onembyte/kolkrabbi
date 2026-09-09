package bus

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/protocol"
)

const (
	testSession = "s_01JQZK9XW80000000000000000"
	testTurn    = "t_01JQZK9XW80000000000000001"
)

func TestPublishAssignsContiguousOrderedEnvelopesConcurrently(t *testing.T) {
	const count = 64
	now := time.Date(2026, 8, 24, 3, 0, 0, 0, time.FixedZone("test", -3*60*60))
	b := mustBus(t, Options{
		SubscriberBuffer: count,
		Clock:            func() time.Time { return now },
	})
	sub := mustSubscribe(t, b, 0)

	var (
		wg        sync.WaitGroup
		published = make(chan protocol.Envelope, count)
	)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			envelope, err := b.Publish(delta(strings.Repeat("x", i+1)))
			if err != nil {
				t.Errorf("Publish(%d): %v", i, err)
				return
			}
			published <- envelope
		}(i)
	}
	wg.Wait()
	close(published)

	var publishedSeq []uint64
	for envelope := range published {
		publishedSeq = append(publishedSeq, envelope.Seq)
		if envelope.Timestamp.Location() != time.UTC {
			t.Errorf("Publish timestamp location = %v, want UTC", envelope.Timestamp.Location())
		}
	}
	sort.Slice(publishedSeq, func(i, j int) bool { return publishedSeq[i] < publishedSeq[j] })

	for i := 0; i < count; i++ {
		want := uint64(i + 1)
		if publishedSeq[i] != want {
			t.Fatalf("published seq[%d] = %d, want %d", i, publishedSeq[i], want)
		}
		envelope := <-sub.Events()
		if envelope.Seq != want {
			t.Fatalf("subscriber seq[%d] = %d, want %d", i, envelope.Seq, want)
		}
		if !envelope.Timestamp.Equal(now) {
			t.Errorf("subscriber timestamp = %v, want %v", envelope.Timestamp, now.UTC())
		}
	}
	if err := sub.Err(); err != nil {
		t.Fatalf("subscriber error: %v", err)
	}
}

func TestRetentionCountAndCursorSemantics(t *testing.T) {
	b := mustBus(t, Options{MaxEvents: 2, Clock: fixedClock()})
	publishText(t, b, "one")
	publishText(t, b, "two")
	publishText(t, b, "three")

	if _, err := b.Subscribe(0); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("Subscribe(expired) error = %v, want ErrCursorExpired", err)
	}
	sub := mustSubscribe(t, b, 1)
	assertSequences(t, sub.Replay(), 2, 3)

	latest := mustSubscribe(t, b, 3)
	if got := latest.Replay(); len(got) != 0 {
		t.Fatalf("Subscribe(latest) replay = %#v, want empty", got)
	}
	if _, err := b.Subscribe(4); !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("Subscribe(ahead) error = %v, want ErrCursorAhead", err)
	}
}

func TestRetentionUsesExactNDJSONBytesAndRejectsOversizedEvent(t *testing.T) {
	now := time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	probe := mustBus(t, Options{Clock: func() time.Time { return now }})
	first := publishText(t, probe, "one")
	second := publishText(t, probe, "two")
	firstFrame, err := protocol.EncodeNDJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	secondFrame, err := protocol.EncodeNDJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	limit := max(len(firstFrame), len(secondFrame))

	b := mustBus(t, Options{MaxBytes: limit, Clock: func() time.Time { return now }})
	publishText(t, b, "one")
	publishText(t, b, "two")
	if _, err := b.Subscribe(0); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("Subscribe(evicted byte cursor) error = %v, want ErrCursorExpired", err)
	}
	assertSequences(t, mustSubscribe(t, b, 1).Replay(), 2)

	large := delta(strings.Repeat("x", limit))
	if _, err := b.Publish(large); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("Publish(oversized) error = %v, want ErrEventTooLarge", err)
	}
	if got := publishText(t, b, "ok").Seq; got != 3 {
		t.Fatalf("sequence after rejected oversized event = %d, want 3", got)
	}
}

func TestSubscribeAtomicallySeparatesReplayFromLiveEvents(t *testing.T) {
	b := mustBus(t, Options{SubscriberBuffer: 2, Clock: fixedClock()})
	publishText(t, b, "before one")
	publishText(t, b, "before two")

	sub := mustSubscribe(t, b, 0)
	publishText(t, b, "after three")
	publishText(t, b, "after four")

	assertSequences(t, sub.Replay(), 1, 2)
	assertSequences(t, []protocol.Envelope{<-sub.Events(), <-sub.Events()}, 3, 4)
	if err := sub.Err(); err != nil {
		t.Fatalf("subscriber error: %v", err)
	}
}

func TestSlowSubscriberIsIsolatedAndCanResumeWithoutAGap(t *testing.T) {
	b := mustBus(t, Options{SubscriberBuffer: 1, Clock: fixedClock()})
	slow := mustSubscribe(t, b, 0)
	healthy := mustSubscribe(t, b, 0)

	publishText(t, b, "one")
	if got := <-healthy.Events(); got.Seq != 1 {
		t.Fatalf("healthy first seq = %d, want 1", got.Seq)
	}
	publishText(t, b, "two")

	if got := <-slow.Events(); got.Seq != 1 {
		t.Fatalf("slow buffered seq = %d, want 1", got.Seq)
	}
	if _, ok := <-slow.Events(); ok {
		t.Fatal("slow subscriber channel remained open")
	}
	if !errors.Is(slow.Err(), ErrSlowSubscriber) {
		t.Fatalf("slow subscriber error = %v, want ErrSlowSubscriber", slow.Err())
	}
	if got := <-healthy.Events(); got.Seq != 2 {
		t.Fatalf("healthy second seq = %d, want 2", got.Seq)
	}
	if err := healthy.Err(); err != nil {
		t.Fatalf("healthy subscriber error: %v", err)
	}

	resumed := mustSubscribe(t, b, 1)
	assertSequences(t, resumed.Replay(), 2)
	publishText(t, b, "three")
	if got := <-resumed.Events(); got.Seq != 3 {
		t.Fatalf("resumed live seq = %d, want 3", got.Seq)
	}
}

func TestPayloadOwnershipIsIsolatedAtEveryBoundary(t *testing.T) {
	b := mustBus(t, Options{SubscriberBuffer: 2, Clock: fixedClock()})
	one := mustSubscribe(t, b, 0)
	two := mustSubscribe(t, b, 0)
	raw := json.RawMessage(`{"text":"original"}`)

	published, err := b.Publish(Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	raw[9] = 'X'
	published.Data[9] = 'Y'

	first := <-one.Events()
	second := <-two.Events()
	first.Data[9] = 'Z'
	if string(second.Data) != `{"text":"original"}` {
		t.Fatalf("second subscriber data = %s, want original", second.Data)
	}

	replay := mustSubscribe(t, b, 0)
	firstSnapshot := replay.Replay()
	if string(firstSnapshot[0].Data) != `{"text":"original"}` {
		t.Fatalf("retained data = %s, want original", firstSnapshot[0].Data)
	}
	firstSnapshot[0].Data[9] = 'Q'
	if got := string(replay.Replay()[0].Data); got != `{"text":"original"}` {
		t.Fatalf("second Replay data = %s, want original", got)
	}
}

func TestInvalidInputAndClockDoNotConsumeASequenceOrNotify(t *testing.T) {
	if _, err := New("legacy-session", Options{}); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("New(invalid session) error = %v, want ErrInvalidSession", err)
	}
	for _, options := range []Options{
		{MaxEvents: -1},
		{MaxBytes: -1},
		{SubscriberBuffer: -1},
	} {
		if _, err := New(testSession, options); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("New(%+v) error = %v, want ErrInvalidOptions", options, err)
		}
	}

	times := []time.Time{
		time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC),
		{},
		time.Date(2026, 8, 24, 5, 59, 59, 0, time.UTC),
		time.Date(2026, 8, 24, 6, 0, 1, 0, time.UTC),
	}
	var clockMu sync.Mutex
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		now := times[0]
		times = times[1:]
		return now
	}
	b := mustBus(t, Options{SubscriberBuffer: 1, Clock: clock})
	sub := mustSubscribe(t, b, 0)

	if _, err := b.Publish(Event{Turn: "not-a-turn", Type: protocol.EventMessageDelta, Data: json.RawMessage(`{"text":"x"}`)}); err == nil {
		t.Fatal("Publish accepted an invalid turn")
	}
	if _, err := b.Publish(Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("Publish accepted an invalid payload")
	}
	first := publishText(t, b, "first")
	if first.Seq != 1 {
		t.Fatalf("first valid sequence = %d, want 1", first.Seq)
	}
	if _, err := b.Publish(delta("zero clock")); !errors.Is(err, ErrInvalidClock) {
		t.Fatalf("Publish(zero clock) error = %v, want ErrInvalidClock", err)
	}
	if _, err := b.Publish(delta("backward clock")); !errors.Is(err, ErrInvalidClock) {
		t.Fatalf("Publish(backward clock) error = %v, want ErrInvalidClock", err)
	}
	last := publishText(t, b, "last")
	if last.Seq != 2 {
		t.Fatalf("sequence after rejected input = %d, want 2", last.Seq)
	}
	assertSequences(t, []protocol.Envelope{<-sub.Events()}, 1)
	if _, ok := <-sub.Events(); ok {
		t.Fatal("subscriber should be closed after its one-event buffer filled")
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	b := mustBus(t, Options{Clock: fixedClock()})
	sub := mustSubscribe(t, b, 0)
	sub.Close()
	sub.Close()
	if _, ok := <-sub.Events(); ok {
		t.Fatal("closed subscription channel remained open")
	}
	if err := sub.Err(); err != nil {
		t.Fatalf("explicit Close reported an error: %v", err)
	}
	publishText(t, b, "after close")
}

func mustBus(t *testing.T, options Options) *Bus {
	t.Helper()
	b, err := New(testSession, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func mustSubscribe(t *testing.T, b *Bus, after uint64) *Subscription {
	t.Helper()
	sub, err := b.Subscribe(after)
	if err != nil {
		t.Fatalf("Subscribe(%d): %v", after, err)
	}
	return sub
}

func publishText(t *testing.T, b *Bus, text string) protocol.Envelope {
	t.Helper()
	envelope, err := b.Publish(delta(text))
	if err != nil {
		t.Fatalf("Publish(%q): %v", text, err)
	}
	return envelope
}

func delta(text string) Event {
	data, err := json.Marshal(protocol.MessageDeltaData{Text: text})
	if err != nil {
		panic(err)
	}
	return Event{Turn: testTurn, Type: protocol.EventMessageDelta, Data: data}
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC)
	}
}

func assertSequences(t *testing.T, envelopes []protocol.Envelope, want ...uint64) {
	t.Helper()
	if len(envelopes) != len(want) {
		t.Fatalf("envelope count = %d, want %d", len(envelopes), len(want))
	}
	for i := range want {
		if envelopes[i].Seq != want[i] {
			t.Fatalf("sequence[%d] = %d, want %d", i, envelopes[i].Seq, want[i])
		}
	}
}

func TestPublishScrubsCanaryCredentialsBeforeRetentionAndFanOut(t *testing.T) {
	apiKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	b := mustBus(t, Options{Clock: fixedClock()})
	sub := mustSubscribe(t, b, 0)

	rawEvent := Event{
		Turn: testTurn,
		Type: protocol.EventMessageDelta,
		Data: []byte(`{"text":"here is my key: ` + apiKey + `"}`),
	}

	published, err := b.Publish(rawEvent)
	if err != nil {
		t.Fatalf("Publish with canary failed: %v", err)
	}

	// 1. Returned envelope must not contain the raw secret
	if strings.Contains(string(published.Data), apiKey) {
		t.Fatalf("published envelope leaked raw secret: %s", string(published.Data))
	}
	if !strings.Contains(string(published.Data), "[redacted ") {
		t.Fatalf("published envelope missing redaction sentinel: %s", string(published.Data))
	}

	// 2. Live subscriber receives the scrubbed envelope
	live := <-sub.Events()
	if strings.Contains(string(live.Data), apiKey) {
		t.Fatalf("live subscriber leaked raw secret: %s", string(live.Data))
	}

	// 3. Replay subscriber receives the scrubbed envelope
	replaySub := mustSubscribe(t, b, 0)
	replayed := replaySub.Replay()
	if len(replayed) != 1 {
		t.Fatalf("replay count = %d, want 1", len(replayed))
	}
	if strings.Contains(string(replayed[0].Data), apiKey) {
		t.Fatalf("replayed envelope leaked raw secret: %s", string(replayed[0].Data))
	}
}

func TestPublishRejectsInvalidOrNonObjectData(t *testing.T) {
	b := mustBus(t, Options{Clock: fixedClock()})
	sub := mustSubscribe(t, b, 0)

	invalidInputs := [][]byte{
		[]byte(""),
		[]byte("   "),
		[]byte("42"),
		[]byte("true"),
		[]byte(`"string"`),
		[]byte(`["array"]`),
		[]byte(`{unclosed`),
	}

	for _, invalid := range invalidInputs {
		_, err := b.Publish(Event{
			Turn: testTurn,
			Type: protocol.EventMessageDelta,
			Data: invalid,
		})
		if err == nil {
			t.Fatalf("Publish should fail on invalid data %q, but succeeded", string(invalid))
		}
	}

	// Sequence must still be 0 and subscriber received nothing
	if b.latest != 0 {
		t.Fatalf("b.latest = %d, want 0 after rejected publishes", b.latest)
	}
	if len(sub.Events()) != 0 {
		t.Fatal("subscriber should have received 0 events")
	}
}

// TestSyncPolicyPicksExactlyTheEventsAClientMustNotSeeTwice pins the O1
// decision table. Which events reach the platter is a policy, not an
// implementation detail: a fsync per streamed token is what made a 2,000-token
// answer spend seconds in the bus, and a turn boundary or a permission prompt
// is where a reconnecting client would otherwise be asked the same question a
// second time.
func TestSyncPolicyPicksExactlyTheEventsAClientMustNotSeeTwice(t *testing.T) {
	boundaries := map[protocol.EventType]bool{
		protocol.EventTurnFinished:        true,
		protocol.EventTurnCancelled:       true,
		protocol.EventPermissionRequested: true,
		protocol.EventPermissionResolved:  true,
	}

	for _, tc := range []struct {
		name   string
		policy SyncPolicy
		want   func(protocol.EventType) bool
	}{
		{"turn-boundary", SyncOnTurnBoundary, func(e protocol.EventType) bool { return boundaries[e] }},
		{"never", SyncNever, func(protocol.EventType) bool { return false }},
		{"every", SyncEvery, func(protocol.EventType) bool { return true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := mustBus(t, Options{SyncPolicy: tc.policy, Clock: fixedClock()})
			for _, eventType := range protocol.KnownEventTypes() {
				if got := b.syncsOn(eventType); got != tc.want(eventType) {
					t.Errorf("syncsOn(%s) under %s = %v, want %v", eventType, tc.policy, got, !got)
				}
			}
		})
	}

	// The default is the zero value, so a caller that says nothing gets the
	// turn-boundary policy rather than an unflushed journal.
	if got := mustBus(t, Options{Clock: fixedClock()}).syncPolicy; got != SyncOnTurnBoundary {
		t.Fatalf("default policy = %s, want turn-boundary", got)
	}
}

func TestImpossibleSyncPolicyAndSpillCapAreRefused(t *testing.T) {
	for _, options := range []Options{
		{SyncPolicy: SyncPolicy(-1)},
		{SyncPolicy: SyncEvery + 1},
		{MaxSpillBytes: -1},
	} {
		if _, err := New(testSession, options); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("New(%+v) error = %v, want ErrInvalidOptions", options, err)
		}
	}
}

// TestASecretSplitAcrossTwoDeltasIsScrubbedInTheCompletedMessage settles the
// O16 question of OPTIMIZATION_PLAN.md: could message.delta skip the scrubber,
// given that message.completed carries the same text and is scrubbed?
//
// The fallback holds — a key torn in half by the tokenizer is whole again in
// message.completed, and is redacted there. The shortcut is still refused, and
// the third assertion is why: a key that arrives inside one delta is scrubbed
// in that delta today, and every delta is fanned out live to subscribers and
// appended to the spill file on disk. One provider chunk can carry a whole
// message, so "deltas are too small to hold a secret" is not a fact anyone can
// rely on. message.completed is the second line of defence, not the only one.
func TestASecretSplitAcrossTwoDeltasIsScrubbedInTheCompletedMessage(t *testing.T) {
	apiKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"
	head, tail := apiKey[:23], apiKey[23:]

	b := mustBus(t, Options{Clock: fixedClock()})
	sub := mustSubscribe(t, b, 0)

	firstDelta := publishText(t, b, head)
	secondDelta := publishText(t, b, tail)

	// Neither fragment is a credential on its own: the head is short of the
	// shape's minimum suffix and the tail has no prefix at all. The scrubber
	// does not pretend otherwise, which is exactly why the whole text has to be
	// scrubbed again once it exists.
	if !strings.Contains(string(firstDelta.Data), head) || !strings.Contains(string(secondDelta.Data), tail) {
		t.Fatalf("a fragment was redacted, so this test no longer proves the split case: %s / %s",
			firstDelta.Data, secondDelta.Data)
	}

	completedData, err := json.Marshal(protocol.MessageCompletedData{Text: head + tail})
	if err != nil {
		t.Fatalf("marshal completed payload: %v", err)
	}
	completed, err := b.Publish(Event{Turn: testTurn, Type: protocol.EventMessageCompleted, Data: completedData})
	if err != nil {
		t.Fatalf("Publish(message.completed): %v", err)
	}
	if strings.Contains(string(completed.Data), apiKey) {
		t.Fatalf("the rejoined key survived message.completed: %s", completed.Data)
	}
	if !strings.Contains(string(completed.Data), "[redacted ") {
		t.Fatalf("message.completed carries no redaction sentinel: %s", completed.Data)
	}

	// And the reason deltas keep their scrub: a whole key inside one delta is
	// redacted in the delta, before any subscriber or the spill file sees it.
	whole := publishText(t, b, "here it is: "+apiKey)
	if strings.Contains(string(whole.Data), apiKey) {
		t.Fatalf("a whole key inside one delta leaked: %s", whole.Data)
	}

	for i, want := range []uint64{1, 2, 3, 4} {
		live := <-sub.Events()
		if live.Seq != want {
			t.Fatalf("live event %d has sequence %d, want %d", i, live.Seq, want)
		}
		if strings.Contains(string(live.Data), apiKey) {
			t.Fatalf("live subscriber saw the whole key: %s", live.Data)
		}
	}
}
