package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

func publishN(t *testing.T, b *bus.Bus, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := b.Publish(bus.Event{Turn: "t_01ARYZ6S41TSV4RRFFQ69G5FAW", Type: protocol.EventMessageDelta, Data: json.RawMessage(`{"text":"x"}`)}); err != nil {
			t.Fatal(err)
		}
	}
}

// sseSeqs connects to the event stream with the given Last-Event-ID (empty for
// a fresh client), publishes one live event once connected, and returns the
// sequence numbers seen in order until the live one arrives.
func sseSeqs(t *testing.T, b *bus.Bus, lastEventID string, liveSeq uint64) []uint64 {
	t.Helper()
	handler, err := Mux(Options{Bus: b, Token: "test-token", Addr: "127.0.0.1:8080", PingInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go func() { time.Sleep(50 * time.Millisecond); publishN(t, b, 1) }()
	var seqs []uint64
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		var seq uint64
		if _, err := fmtSscan(strings.TrimPrefix(line, "id: "), &seq); err != nil {
			t.Fatalf("bad id line %q: %v", line, err)
		}
		seqs = append(seqs, seq)
		if seq == liveSeq {
			break
		}
	}
	return seqs
}

// A fresh SSE client gets the retained events, then the live ones, in order.
func TestSSEDeliversRetainedReplayBeforeLiveEvents(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	publishN(t, b, 2)
	got := sseSeqs(t, b, "", 3)
	if want := []uint64{1, 2, 3}; !equalSeqs(got, want) {
		t.Fatalf("SSE sequences = %v, want %v (retained replay lost)", got, want)
	}
}

// A reconnecting client with Last-Event-ID gets what it missed and nothing it
// already had.
func TestSSEResumesAfterLastEventIDWithoutDuplication(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	publishN(t, b, 2)
	got := sseSeqs(t, b, "1", 3)
	if want := []uint64{2, 3}; !equalSeqs(got, want) {
		t.Fatalf("SSE sequences after Last-Event-ID 1 = %v, want %v", got, want)
	}
}

// The stdio stream attaches at zero: everything retained, then live.
func TestStdioDeliversRetainedReplayBeforeLiveEvents(t *testing.T) {
	b, err := bus.New(xid.New(xid.Session), bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	publishN(t, b, 2)
	var out lockedBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeStdio(ctx, strings.NewReader(""), &out, b) }()
	time.Sleep(50 * time.Millisecond)
	publishN(t, b, 1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && strings.Count(out.String(), "\n") < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	var seqs []uint64
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var env protocol.Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", line, err)
		}
		seqs = append(seqs, env.Seq)
	}
	if want := []uint64{1, 2, 3}; !equalSeqs(seqs, want) {
		t.Fatalf("stdio sequences = %v, want %v (retained replay lost)", seqs, want)
	}
}

func equalSeqs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buf.String() }

func fmtSscan(s string, seq *uint64) (int, error) { return fmt.Sscan(s, seq) }
