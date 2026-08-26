// Package bus owns Kolkrabbi's per-session ordered event journal.
//
// The journal is the hinge between engine producers and terminal, stdio, HTTP,
// and durable consumers.
package bus

import (
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
)

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
// SpillPath enables persistent append-only NDJSON disk logging when non-empty.
type Options struct {
	MaxEvents        int
	MaxBytes         int
	SubscriberBuffer int
	Clock            func() time.Time
	SpillPath        string
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
// concurrent use. It owns no goroutine: live fan-out happens non-blockingly on
// the publisher's goroutine.
type Bus struct {
	mu sync.Mutex

	session          string
	maxEvents        int
	maxBytes         int
	subscriberBuffer int
	clock            func() time.Time
	spillPath        string
	spillFile        *os.File

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
	if options.MaxEvents < 0 || options.MaxBytes < 0 || options.SubscriberBuffer < 0 {
		return nil, ErrInvalidOptions
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
	if options.Clock == nil {
		options.Clock = time.Now
	}

	b := &Bus{
		session:          session,
		maxEvents:        options.MaxEvents,
		maxBytes:         options.MaxBytes,
		subscriberBuffer: options.SubscriberBuffer,
		clock:            options.Clock,
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
		b.spillPath = spillPath
		b.spillFile = sf
	}

	return b, nil
}

// Publish validates, sequences, retains, and fans out one event atomically.
// Any returned error leaves the sequence, replay window, and subscribers
// unchanged.
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

	if b.spillFile != nil {
		if _, err := b.spillFile.Write(frame); err != nil {
			return protocol.Envelope{}, fmt.Errorf("bus: write spill: %w", err)
		}
		_ = b.spillFile.Sync()
	}

	b.latest = envelope.Seq
	b.lastTime = now
	b.retained = append(b.retained, retainedEvent{
		envelope: cloneEnvelope(envelope),
		bytes:    len(frame),
	})
	b.retainedSize += len(frame)
	b.trim()

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
	return envelopes, nil
}

// Close closes the journal, all active subscriptions, and the spill file if open.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subscribers {
		delete(b.subscribers, sub)
		sub.finish(nil)
	}
	if b.spillFile != nil {
		err := b.spillFile.Close()
		b.spillFile = nil
		return err
	}
	return nil
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
