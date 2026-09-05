package serve

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/protocol"
)

// StreamEventsToNDJSON streams events from a subscription to an output writer as compact NDJSON lines.
func StreamEventsToNDJSON(ctx context.Context, sub *bus.Subscription, out io.Writer) error {
	var mu sync.Mutex
	write := func(env protocol.Envelope) error {
		b, err := json.Marshal(env)
		if err != nil {
			return nil // an envelope that cannot be encoded is skipped, as before
		}
		mu.Lock()
		defer mu.Unlock()
		_, err = out.Write(append(b, '\n'))
		return err
	}
	// The retained replay first, then the live channel (V34.2d).
	for _, env := range sub.Replay() {
		if err := write(env); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-sub.Events():
			if !ok {
				return nil
			}
			if err := write(env); err != nil {
				return err
			}
		}
	}
}

// ServeStdio streams event bus envelopes over stdio in NDJSON format.
func ServeStdio(ctx context.Context, in io.Reader, out io.Writer, b *bus.Bus) error {
	sub, err := b.Subscribe(0)
	if err != nil {
		return err
	}
	defer sub.Close()

	return StreamEventsToNDJSON(ctx, sub, out)
}
