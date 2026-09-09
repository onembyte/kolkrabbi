package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/protocol"
)

// sseHandler streams events from the event bus to HTTP clients.
func sseHandler(b *bus.Bus, pingInterval time.Duration) http.HandlerFunc {
	if pingInterval <= 0 {
		pingInterval = 15 * time.Second
	}

	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		afterSeq := uint64(0)
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if seq, err := strconv.ParseUint(lastID, 10, 64); err == nil {
				afterSeq = seq
			}
		} else if afterParam := r.URL.Query().Get("after"); afterParam != "" {
			if seq, err := strconv.ParseUint(afterParam, 10, 64); err == nil {
				afterSeq = seq
			}
		}

		sub, err := b.Subscribe(afterSeq)
		if errors.Is(err, bus.ErrCursorExpired) {
			// The journal is bounded (plan O1): a device that was away long
			// enough can name an event the file no longer holds. That is a
			// state it can act on, not a fault in the server, and the one
			// thing it must not get is a replay with a silent gap.
			http.Error(w, "cursor expired: the journal no longer holds events that old; reconnect without Last-Event-ID to start from what is retained", http.StatusGone)
			return
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("subscribe error: %v", err), http.StatusInternalServerError)
			return
		}
		defer sub.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Reconnection retry hint
		fmt.Fprint(w, "retry: 1000\n\n")
		flusher.Flush()
		writeFrame := func(env protocol.Envelope) {
			data, err := json.Marshal(env)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", env.Seq, env.Type, data)
			flusher.Flush()
		}
		// The retained replay first, then the live channel: the subscription took
		// both atomically, so nothing is missed and nothing arrives twice (V34.2d).
		for _, env := range sub.Replay() {
			writeFrame(env)
		}

		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			case env, ok := <-sub.Events():
				if !ok {
					return
				}
				writeFrame(env)
			}
		}
	}
}
