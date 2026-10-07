package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/protocol"
)

// streamTurn subscribes before admitting work and drains the final journal
// before closing it, including recovery failures emitted as the turn unwinds.
func streamTurn(ctx context.Context, ag *engine.Agent, prompt string, out io.Writer) error {
	if ag.Bus == nil {
		return fmt.Errorf("stream-json requires an event journal")
	}
	sub, err := ag.Bus.Subscribe(0)
	if err != nil {
		return fmt.Errorf("start stream-json: %w", err)
	}
	previous := ag.Out
	ag.Out = io.Discard
	defer func() { ag.Out = previous }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		err := drainStream(ag.Bus, sub, finished, out)
		if err != nil {
			cancel()
		}
		done <- err
	}()
	turnErr := ag.RunTurn(ctx, prompt)
	close(finished)
	streamErr := <-done
	return errors.Join(turnErr, streamErr, ag.Bus.Close())
}

// A slow subscriber is disconnected by the bounded bus. Replay from the last
// frame actually written, rather than silently dropping the turn's tail. If
// that cursor has expired, report an incomplete stream instead of success.
func drainStream(journal *bus.Bus, sub *bus.Subscription, finished <-chan struct{}, out io.Writer) error {
	defer func() { sub.Close() }()
	var cursor uint64
	emit := func(env protocol.Envelope) error {
		frame, err := protocol.EncodeNDJSON(env)
		if err != nil {
			return err
		}
		n, err := out.Write(frame)
		if err == nil && n != len(frame) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return err
		}
		cursor = env.Seq
		return nil
	}
	replay := func() error {
		for _, env := range sub.Replay() {
			if err := emit(env); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		if err := replay(); err != nil {
			return err
		}
		for {
			select {
			case <-finished:
				sub.Close()
				tail, err := journal.Subscribe(cursor)
				if err != nil {
					return fmt.Errorf("stream-json incomplete: %w", err)
				}
				sub = tail
				return replay()
			case env, ok := <-sub.Events():
				if ok {
					if err := emit(env); err != nil {
						return err
					}
					continue
				}
				if !errors.Is(sub.Err(), bus.ErrSlowSubscriber) {
					return fmt.Errorf("stream-json subscription ended before the turn finished: %w", io.ErrUnexpectedEOF)
				}
				next, err := journal.Subscribe(cursor)
				if err != nil {
					return fmt.Errorf("stream-json incomplete: %w", err)
				}
				sub = next
			}
			break
		}
	}
}
