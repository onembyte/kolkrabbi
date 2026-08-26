package serve

import (
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/bus"
)

// StreamEventsToNDJSON streams events from a subscription to an output writer as compact NDJSON lines.
func StreamEventsToNDJSON(ctx context.Context, sub *bus.Subscription, out io.Writer) error {
	var mu sync.Mutex
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-sub.Events():
			if !ok {
				return nil
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			mu.Lock()
			if _, err := out.Write(append(b, '\n')); err != nil {
				mu.Unlock()
				return err
			}
			mu.Unlock()
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
