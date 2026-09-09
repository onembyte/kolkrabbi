// Package bus owns Kolkrabbi's per-session ordered event journal.
//
// The journal is the hinge between engine producers and terminal, stdio, HTTP,
// and durable consumers.
package bus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/redact"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

const (
	// DefaultMaxEvents is the maximum number of complete envelopes retained in
	// memory for one session before the oldest envelope leaves the window.
	DefaultMaxEvents = 10_000
	// DefaultMaxBytes bounds retained envelopes by their exact LF-terminated
	// NDJSON representation, which is also the later spill-file representation.
	DefaultMaxBytes = 8 << 20
	// DefaultSubscriberBuffer bounds each live subscriber independently.
	DefaultSubscriberBuffer = 256
	// DefaultMaxSpillBytes bounds the spill file. Past it the file is rewritten
	// from the retained window, which is the whole of what replay promises.
	DefaultMaxSpillBytes = 64 << 20
	// spillQueueDepth bounds the frames a publisher may run ahead of the disk.
	// A full queue blocks the publisher, which is the old behaviour and never
	// silently drops a frame; at one kilobyte a frame this is a megabyte.
	spillQueueDepth = 1024
)

// SyncPolicy says when the spill file is flushed to the platter. Publishing one
// event per streamed token made a per-event fsync the dominant cost of a turn,
// and nothing a person can observe depends on it: the transcript is in the
// session file, and replay promises the retained window, not the last delta
// before a power cut.
type SyncPolicy int

const (
	// SyncOnTurnBoundary is the default and the zero value: fsync at the end of
	// a turn and around every permission event, the two places a client must
	// not have to see an event twice.
	SyncOnTurnBoundary SyncPolicy = iota
	// SyncNever leaves durability entirely to the operating system. The file is
	// still flushed by Close.
	SyncNever
	// SyncEvery is the pre-O1 behaviour, kept as the one-line rollback.
	SyncEvery
)

// String names the policy for error messages and dossiers.
func (p SyncPolicy) String() string {
	switch p {
	case SyncOnTurnBoundary:
		return "turn-boundary"
	case SyncNever:
		return "never"
	case SyncEvery:
		return "every"
	default:
		return fmt.Sprintf("SyncPolicy(%d)", int(p))
	}
}

var (
	// ErrInvalidSession reports a bus constructed for a non-canonical session.
	ErrInvalidSession = errors.New("bus: invalid session id")
	// ErrInvalidOptions reports a negative or otherwise impossible bus limit.
	ErrInvalidOptions = errors.New("bus: invalid options")
	// ErrInvalidClock reports a zero or backward timestamp from the bus clock.
	ErrInvalidClock = errors.New("bus: clock must be nonzero and nondecreasing")
	// ErrEventTooLarge reports one event that cannot fit the configured replay
	// window or the protocol's maximum stream frame.
	ErrEventTooLarge = errors.New("bus: event exceeds the single-event limit")
	// ErrCursorExpired reports a cursor older than the retained replay window.
	ErrCursorExpired = errors.New("bus: cursor expired")
	// ErrCursorAhead reports a cursor beyond the latest published sequence.
	ErrCursorAhead = errors.New("bus: cursor is ahead of the journal")
	// ErrSlowSubscriber reports a live subscriber disconnected after its bounded
	// channel filled. Its last consumed sequence remains a replay cursor.
	ErrSlowSubscriber = errors.New("bus: subscriber fell behind")
	// ErrSequenceExhausted is a defensive terminal boundary for uint64 sequence
	// allocation. No wrapping sequence can be published.
	ErrSequenceExhausted = errors.New("bus: sequence exhausted")
)

// Options configures one session journal. Zero limits select the
// documented defaults; negative limits are invalid. Clock defaults to time.Now.
// SpillPath enables persistent NDJSON disk logging when non-empty; MaxSpillBytes
// bounds that file and SyncPolicy says when it reaches the platter.
type Options struct {
	MaxEvents        int
	MaxBytes         int
	SubscriberBuffer int
	Clock            func() time.Time
	SpillPath        string
	SyncPolicy       SyncPolicy
	MaxSpillBytes    int64
}

// Event is the unsequenced input to Publish. The bus supplies the session,
// sequence, and timestamp and validates the resulting protocol envelope.
type Event struct {
	Turn string
	Type protocol.EventType
	Data json.RawMessage
}

type retainedEvent struct {
	envelope protocol.Envelope
	bytes    int
}

// Bus is one per-session ordered journal. Publish and Subscribe are safe for
// concurrent use. Live fan-out happens non-blockingly on the publisher's
// goroutine; a configured spill file adds exactly one goroutine, the writer,
// which Close drains and stops.
type Bus struct {
	mu sync.Mutex

	session          string
	maxEvents        int
	maxBytes         int
	subscriberBuffer int
	clock            func() time.Time
	spillPath        string
	syncPolicy       SyncPolicy
	maxSpillBytes    int64
	spillBytes       int64
	spill            *spillWriter

	latest       uint64
	lastTime     time.Time
	retained     []retainedEvent
	retainedSize int
	subscribers  map[*Subscription]struct{}
}

// Subscription atomically combines a retained replay snapshot with a bounded
// channel of events published after that snapshot. Replay and live envelopes
// never overlap.
type Subscription struct {
	bus    *Bus
	replay []protocol.Envelope
	events chan protocol.Envelope

	mu     sync.Mutex
	closed bool
	err    error
}

// New constructs a journal for one canonical protocol session ID.
func New(session string, options Options) (*Bus, error) {
	if !canonicalSessionID(session) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidSession, session)
	}
	if options.MaxEvents < 0 || options.MaxBytes < 0 || options.SubscriberBuffer < 0 || options.MaxSpillBytes < 0 {
		return nil, ErrInvalidOptions
	}
	if options.SyncPolicy < SyncOnTurnBoundary || options.SyncPolicy > SyncEvery {
		return nil, fmt.Errorf("%w: sync policy %d", ErrInvalidOptions, int(options.SyncPolicy))
	}
	if options.MaxEvents == 0 {
		options.MaxEvents = DefaultMaxEvents
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultMaxBytes
	}
	if options.SubscriberBuffer == 0 {
		options.SubscriberBuffer = DefaultSubscriberBuffer
	}
	if options.MaxSpillBytes == 0 {
		options.MaxSpillBytes = DefaultMaxSpillBytes
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}

	b := &Bus{
		session:          session,
		maxEvents:        options.MaxEvents,
		maxBytes:         options.MaxBytes,
		subscriberBuffer: options.SubscriberBuffer,
		clock:            options.Clock,
		syncPolicy:       options.SyncPolicy,
		maxSpillBytes:    options.MaxSpillBytes,
		subscribers:      make(map[*Subscription]struct{}),
	}

	if options.SpillPath != "" {
		spillPath := filepath.Clean(options.SpillPath)
		if err := os.MkdirAll(filepath.Dir(spillPath), 0700); err != nil {
			return nil, err
		}

		if info, err := os.Stat(spillPath); err == nil && !info.IsDir() {
			f, err := os.Open(spillPath)
			if err != nil {
				return nil, err
			}
			defer func() { _ = f.Close() }()

			err = protocol.DecodeStream(f, protocol.StreamNDJSON, func(env protocol.Envelope) error {
				if env.Session != session {
					return fmt.Errorf("bus: spill session mismatch: %q != %q", env.Session, session)
				}
				b.latest = env.Seq
				b.lastTime = env.Timestamp
				frame, err := protocol.EncodeNDJSON(env)
				if err != nil {
					return err
				}
				b.retained = append(b.retained, retainedEvent{
					envelope: cloneEnvelope(env),
					bytes:    len(frame),
				})
				b.retainedSize += len(frame)
				b.trim()
				return nil
			})
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("bus: recover spill file: %w", err)
			}
		}

		sf, err := os.OpenFile(spillPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
		info, err := sf.Stat()
		if err != nil {
			_ = sf.Close()
			return nil, err
		}
		b.spillPath = spillPath
		b.spillBytes = info.Size()
		b.spill = &spillWriter{
			path: spillPath,
			file: sf,
			ops:  make(chan spillOp, spillQueueDepth),
			done: make(chan struct{}),
		}
		go b.spill.run()
	}

	return b, nil
}

// Publish validates, sequences, retains, and fans out one event atomically.
// Any returned error leaves the sequence, replay window, and subscribers
// unchanged.
//
// The spill file is written by a separate goroutine, so a disk error is not the
// error of the Publish whose frame failed: the first one is remembered and
// returned by every later Publish and by Close. It is deliberately sticky —
// once one frame is missing, the file no longer satisfies the replay contract,
// and appending over the hole would be worse than failing loudly.
func (b *Bus) Publish(event Event) (protocol.Envelope, error) {
	scrubbed, err := redact.ScrubJSON(event.Data)
	if err != nil {
		return protocol.Envelope{}, fmt.Errorf("bus: scrub payload: %w", err)
	}
	event.Data = scrubbed
	if err := b.validateEvent(event); err != nil {
		return protocol.Envelope{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.spill != nil {
		if err := b.spill.failure(); err != nil {
			return protocol.Envelope{}, err
		}
	}
	if b.latest == math.MaxUint64 {
		return protocol.Envelope{}, ErrSequenceExhausted
	}
	now := b.clock().UTC()
	if now.IsZero() || (!b.lastTime.IsZero() && now.Before(b.lastTime)) {
		return protocol.Envelope{}, ErrInvalidClock
	}
	envelope := protocol.Envelope{
		Seq:       b.latest + 1,
		Timestamp: now,
		Session:   b.session,
		Turn:      event.Turn,
		Type:      event.Type,
		Data:      bytes.Clone(event.Data),
	}
	frame, err := protocol.EncodeNDJSON(envelope)
	if err != nil {
		return protocol.Envelope{}, fmt.Errorf("bus: validate envelope: %w", err)
	}
	if len(frame)-1 > protocol.MaxStreamFrameBytes || len(frame) > b.maxBytes {
		return protocol.Envelope{}, fmt.Errorf("%w: %d bytes", ErrEventTooLarge, len(frame))
	}

	b.latest = envelope.Seq
	b.lastTime = now
	b.retained = append(b.retained, retainedEvent{
		envelope: cloneEnvelope(envelope),
		bytes:    len(frame),
	})
	b.retainedSize += len(frame)
	b.trim()

	if b.spill != nil {
		b.enqueueSpill(frame, envelope.Type)
	}

	for subscriber := range b.subscribers {
		select {
		case subscriber.events <- cloneEnvelope(envelope):
		default:
			delete(b.subscribers, subscriber)
			subscriber.finish(ErrSlowSubscriber)
		}
	}
	return cloneEnvelope(envelope), nil
}

// enqueueSpill hands one frame to the writer goroutine, deciding here — under
// b.mu, never in the writer — whether the file has grown past its cap and must
// be rebuilt from the retained window instead. The writer must not reach for
// b.mu: a publisher blocked on a full queue holds it, and a writer waiting for
// it would deadlock the process.
//
// Rewriting is skipped unless it would actually shrink the file, which is what
// bounds the file at max(MaxSpillBytes, MaxBytes) plus one frame however small
// the cap is set, and stops a tiny cap from rewriting on every publish.
//
// The window handed over shares its payload bytes with the retained events. No
// code path mutates a payload after Publish clones it, and trim only zeroes the
// bus's own slice element, not this copy of it.
func (b *Bus) enqueueSpill(frame []byte, eventType protocol.EventType) {
	op := spillOp{frame: frame, sync: b.syncsOn(eventType)}
	if b.spillBytes+int64(len(frame)) > b.maxSpillBytes && int64(b.retainedSize) < b.spillBytes+int64(len(frame)) {
		op.frame = nil
		op.rewrite = append([]retainedEvent(nil), b.retained...)
		b.spillBytes = int64(b.retainedSize)
	} else {
		b.spillBytes += int64(len(frame))
	}
	b.spill.ops <- op
}

// syncsOn answers the one question the policy exists to answer.
func (b *Bus) syncsOn(eventType protocol.EventType) bool {
	switch b.syncPolicy {
	case SyncEvery:
		return true
	case SyncNever:
		return false
	default:
		// A turn boundary is where a client stops and takes stock, and a
		// permission event is the one thing it must never be asked twice.
		return eventType == protocol.EventTurnFinished ||
			eventType == protocol.EventTurnCancelled ||
			strings.HasPrefix(string(eventType), "permission.")
	}
}

// flushSpill waits for every frame already queued to reach the file. Reading the
// file back for a replay needs it, because after O1 the tail of the journal can
// still be in the writer's queue. Subscribe reaches it holding b.mu, which is
// safe in the one way that matters: the writer never takes that lock, so this
// waits on the disk and on nothing that could be waiting on the caller.
func (b *Bus) flushSpill() error {
	if b.spill == nil {
		return nil
	}
	done := make(chan struct{})
	b.spill.ops <- spillOp{done: done}
	<-done
	return b.spill.failure()
}

// Subscribe atomically snapshots retained events strictly after afterSeq and
// registers for later live events. afterSeq has Last-Event-ID semantics: it is
// the last envelope the caller already consumed, not the first one requested.
func (b *Bus) Subscribe(afterSeq uint64) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if afterSeq > b.latest {
		return nil, fmt.Errorf("%w: cursor %d, latest %d", ErrCursorAhead, afterSeq, b.latest)
	}

	var replay []protocol.Envelope

	if len(b.retained) > 0 {
		oldest := b.retained[0].envelope.Seq
		if afterSeq >= oldest-1 {
			// In-memory replay is sufficient
			for _, retained := range b.retained {
				if retained.envelope.Seq > afterSeq {
					replay = append(replay, cloneEnvelope(retained.envelope))
				}
			}
		} else if b.spillPath != "" {
			// Evicted from in-memory window; replay from on-disk spill log
			var err error
			replay, err = b.readSpillAfter(afterSeq)
			if err != nil {
				return nil, fmt.Errorf("bus: read spill replay: %w", err)
			}
		} else {
			return nil, fmt.Errorf("%w: cursor %d, oldest %d", ErrCursorExpired, afterSeq, oldest)
		}
	} else if afterSeq < b.latest {
		if b.spillPath != "" {
			var err error
			replay, err = b.readSpillAfter(afterSeq)
			if err != nil {
				return nil, fmt.Errorf("bus: read spill replay: %w", err)
			}
		} else {
			return nil, fmt.Errorf("%w: cursor %d", ErrCursorExpired, afterSeq)
		}
	}

	subscription := &Subscription{
		bus:    b,
		replay: replay,
		events: make(chan protocol.Envelope, b.subscriberBuffer),
	}
	b.subscribers[subscription] = struct{}{}
	return subscription, nil
}

func (b *Bus) readSpillAfter(afterSeq uint64) ([]protocol.Envelope, error) {
	if err := b.flushSpill(); err != nil {
		return nil, err
	}
	f, err := os.Open(b.spillPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var envelopes []protocol.Envelope
	err = protocol.DecodeStream(f, protocol.StreamNDJSON, func(env protocol.Envelope) error {
		if env.Seq > afterSeq {
			envelopes = append(envelopes, cloneEnvelope(env))
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	// A rewritten file starts at the oldest event of the retained window, not at
	// the first event of the session. A cursor older than that cannot be served
	// from anywhere, and saying so is the contract: handing back a replay that
	// silently skips the missing events would be the one failure a Last-Event-ID
	// client cannot detect.
	if len(envelopes) > 0 && envelopes[0].Seq > afterSeq+1 {
		return nil, fmt.Errorf("%w: cursor %d, oldest on disk %d", ErrCursorExpired, afterSeq, envelopes[0].Seq)
	}
	return envelopes, nil
}

// Close closes the journal and all active subscriptions, then drains every
// frame still queued for the spill file, flushes it whatever the sync policy
// says, and closes it. It is safe to call more than once. The error is the
// first the writer met, including one from a Publish that had already returned.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		delete(b.subscribers, sub)
		sub.finish(nil)
	}
	if b.spill == nil {
		return nil
	}
	spill := b.spill
	b.spill = nil
	close(spill.ops)
	<-spill.done
	return spill.failure()
}

// Replay returns a defensive copy of the retained snapshot captured by
// Subscribe. It is safe for the caller to mutate or retain indefinitely.
func (s *Subscription) Replay() []protocol.Envelope {
	replay := make([]protocol.Envelope, len(s.replay))
	for i := range s.replay {
		replay[i] = cloneEnvelope(s.replay[i])
	}
	return replay
}

// Events returns events published after the replay snapshot. The channel is
// closed by Close or when the subscriber falls behind.
func (s *Subscription) Events() <-chan protocol.Envelope { return s.events }

// Err reports why the live channel closed. Explicit Close has no error.
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close detaches the subscriber. It is safe to call repeatedly.
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if _, ok := s.bus.subscribers[s]; ok {
		delete(s.bus.subscribers, s)
		s.finish(nil)
	}
}

func (s *Subscription) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	close(s.events)
}

// spillOp is one unit of work for the spill writer. A frame is appended, a
// rewrite replaces the file with the retained window, a sync flushes what has
// been written, and done is the barrier a reader waits on. An op may carry any
// combination, and the empty op with only done set is the barrier alone.
type spillOp struct {
	frame   []byte
	rewrite []retainedEvent
	sync    bool
	done    chan struct{}
}

// spillWriter owns the spill file. Exactly one goroutine runs it, so the handle
// needs no lock; only the error it has to hand back to publishers does.
type spillWriter struct {
	path string
	file *os.File
	ops  chan spillOp
	done chan struct{}

	mu  sync.Mutex
	err error
}

func (w *spillWriter) run() {
	defer close(w.done)
	dirty := false
	for op := range w.ops {
		if op.rewrite != nil {
			if err := w.rewriteFrom(op.rewrite); err != nil {
				w.fail(fmt.Errorf("bus: rewrite spill: %w", err))
			} else {
				// rewriteFrom flushes the file it renames into place.
				dirty = false
			}
		}
		if len(op.frame) > 0 {
			if _, err := w.file.Write(op.frame); err != nil {
				w.fail(fmt.Errorf("bus: write spill: %w", err))
			} else {
				dirty = true
			}
		}
		if op.sync && dirty {
			if err := w.file.Sync(); err != nil {
				w.fail(fmt.Errorf("bus: sync spill: %w", err))
			}
			dirty = false
		}
		if op.done != nil {
			close(op.done)
		}
	}
	// The channel is closed by Close, which is the last durability point there
	// is: whatever the policy, the journal is on the platter when Close returns.
	if dirty {
		if err := w.file.Sync(); err != nil {
			w.fail(fmt.Errorf("bus: sync spill: %w", err))
		}
	}
	if err := w.file.Close(); err != nil {
		w.fail(fmt.Errorf("bus: close spill: %w", err))
	}
}

// rewriteFrom rebuilds the file from the retained window and swaps it in with a
// rename, so a reader either sees the whole old file or the whole new one. The
// temporary file becomes the spill file, so its handle becomes the writer's.
func (w *spillWriter) rewriteFrom(window []retainedEvent) error {
	temp := w.path + ".rewrite"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	abandon := func(err error) error {
		_ = f.Close()
		_ = os.Remove(temp)
		return err
	}

	buffered := bufio.NewWriter(f)
	for _, retained := range window {
		frame, err := protocol.EncodeNDJSON(retained.envelope)
		if err != nil {
			return abandon(err)
		}
		if _, err := buffered.Write(frame); err != nil {
			return abandon(err)
		}
	}
	if err := buffered.Flush(); err != nil {
		return abandon(err)
	}
	if err := f.Sync(); err != nil {
		return abandon(err)
	}
	if err := os.Rename(temp, w.path); err != nil {
		return abandon(err)
	}
	_ = w.file.Close()
	w.file = f
	return nil
}

// fail remembers the first error only. The ones after it are consequences.
func (w *spillWriter) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

func (w *spillWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (b *Bus) validateEvent(event Event) error {
	// Use fixed valid sequencing fields so invalid caller-controlled identity,
	// type, or payload fails before consulting the clock or taking a sequence.
	_, err := protocol.Encode(protocol.Envelope{
		Seq:       1,
		Timestamp: time.Unix(0, 0).UTC(),
		Session:   b.session,
		Turn:      event.Turn,
		Type:      event.Type,
		Data:      event.Data,
	})
	if err != nil {
		return fmt.Errorf("bus: invalid event: %w", err)
	}
	return nil
}

func (b *Bus) trim() {
	for len(b.retained) > b.maxEvents || b.retainedSize > b.maxBytes {
		b.retainedSize -= b.retained[0].bytes
		b.retained[0] = retainedEvent{}
		b.retained = b.retained[1:]
	}
}

func canonicalSessionID(id string) bool {
	return strings.HasPrefix(id, "s_") &&
		len(id) == 28 &&
		id[2:] == strings.ToUpper(id[2:]) &&
		xid.KindOf(id) == xid.Session
}

func cloneEnvelope(envelope protocol.Envelope) protocol.Envelope {
	envelope.Data = bytes.Clone(envelope.Data)
	return envelope
}
