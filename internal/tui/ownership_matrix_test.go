package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// terminalOwner names every operation that can make the runtime the sole
// consumer of keyboard input. The matrix below is deliberately directed:
// refusing a question while a picker is open does not prove that the picker
// refuses while the question is open.
type terminalOwner uint8

const (
	ownerQuestion terminalOwner = iota
	ownerApproval
	ownerModelPicker
	ownerConfigPicker
	ownerAttached
)

func (o terminalOwner) String() string {
	switch o {
	case ownerQuestion:
		return "question"
	case ownerApproval:
		return "approval"
	case ownerModelPicker:
		return "model-picker"
	case ownerConfigPicker:
		return "config-picker"
	case ownerAttached:
		return "attached"
	default:
		return "unknown-owner"
	}
}

// TestEveryTerminalOwnerRefusesEveryOtherOwner is the complete H7 contract:
// every one of the five entry points rejects each of the other four. The
// candidate runs separately because a missing guard would block it; the
// timeout then turns that hang into one failing subtest rather than a suite
// that never reports its result.
func TestEveryTerminalOwnerRefusesEveryOtherOwner(t *testing.T) {
	owners := []terminalOwner{
		ownerQuestion,
		ownerApproval,
		ownerModelPicker,
		ownerConfigPicker,
		ownerAttached,
	}
	for _, active := range owners {
		for _, candidate := range owners {
			if active == candidate {
				continue
			}
			t.Run(fmt.Sprintf("%s_refuses_%s", candidate, active), func(t *testing.T) {
				r := NewRuntime(RuntimeOptions{})
				activeSession := openTerminalOwner(t, r, active)
				defer activeSession.stop()

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan terminalAttempt, 1)
				go func() {
					result <- attemptTerminalOwner(ctx, r, candidate)
				}()

				select {
				case attempt := <-result:
					if attempt.accepted {
						t.Fatalf("%s was accepted while %s already owned keyboard input (err: %v)", candidate, active, attempt.err)
					}
				case <-time.After(2 * time.Second):
					// A broken guard may have opened a second overlay or attached a
					// child. Cancellation must still release that waiter so this
					// test can fail cleanly and the next matrix case can run.
					cancel()
					select {
					case attempt := <-result:
						if attempt.accepted {
							t.Fatalf("%s claimed keyboard input behind %s and returned only after cancellation (err: %v)", candidate, active, attempt.err)
						}
						t.Fatalf("%s blocked while %s already owned keyboard input", candidate, active)
					case <-time.After(2 * time.Second):
						t.Fatalf("%s blocked and did not respond to cancellation while %s owned keyboard input", candidate, active)
					}
				}
			})
		}
	}
}

type terminalOwnerSession struct {
	stop func()
}

// openTerminalOwner creates exactly one active owner and returns a bounded
// cleanup function. Context cancellation closes overlay waiters; attach has a
// separate release because RunAttached deliberately leaves callback lifetime
// to its caller.
func openTerminalOwner(t *testing.T, r *Runtime, owner terminalOwner) terminalOwnerSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var release chan struct{}
	var once sync.Once

	switch owner {
	case ownerQuestion:
		go func() {
			defer close(done)
			_, _ = r.Ask(ctx, Question{Prompt: "Which?", Options: []string{"one", "two"}})
		}()
		waitFor(t, 2*time.Second, "the question to open", func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.controller.Question() != nil
		})
	case ownerApproval:
		go func() {
			defer close(done)
			_ = r.Decide(ctx, Approval{Action: "run"})
		}()
		waitFor(t, 2*time.Second, "the approval to open", func() bool {
			return r.Approval() != nil
		})
	case ownerModelPicker:
		go func() {
			defer close(done)
			_, _ = r.AskModel(ctx, []ModelPickEntry{{ID: "vendor/model"}})
		}()
		waitFor(t, 2*time.Second, "the model picker to open", func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.controller.ModelPicker() != nil
		})
	case ownerConfigPicker:
		go func() {
			defer close(done)
			_, _ = r.AskConfig(ctx, []SettingSpec{{Key: "effort"}})
		}()
		waitFor(t, 2*time.Second, "the config picker to open", func() bool {
			return r.ConfigPicker() != nil
		})
	case ownerAttached:
		release = make(chan struct{})
		started := make(chan struct{})
		go func() {
			defer close(done)
			_ = r.RunAttached(ctx, func(io.Reader, io.Writer, int, int) error {
				close(started)
				<-release
				return nil
			})
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("the attached owner did not take the terminal")
		}
	default:
		t.Fatalf("unsupported terminal owner %v", owner)
	}

	return terminalOwnerSession{stop: func() {
		once.Do(func() {
			if release != nil {
				close(release)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("%s owner did not clean up after cancellation", owner)
			}
		})
	}}
}

type terminalAttempt struct {
	accepted bool
	err      error
}

func attemptTerminalOwner(ctx context.Context, r *Runtime, owner terminalOwner) terminalAttempt {
	switch owner {
	case ownerQuestion:
		_, ok := r.Ask(ctx, Question{Prompt: "Other?", Options: []string{"one", "two"}})
		return terminalAttempt{accepted: ok}
	case ownerApproval:
		decision := r.Decide(ctx, Approval{Action: "run"})
		return terminalAttempt{accepted: decision != DecisionDeny}
	case ownerModelPicker:
		_, ok := r.AskModel(ctx, []ModelPickEntry{{ID: "vendor/model"}})
		return terminalAttempt{accepted: ok}
	case ownerConfigPicker:
		_, ok := r.AskConfig(ctx, []SettingSpec{{Key: "effort"}})
		return terminalAttempt{accepted: ok}
	case ownerAttached:
		err := r.RunAttached(ctx, func(io.Reader, io.Writer, int, int) error {
			<-ctx.Done()
			return ctx.Err()
		})
		return terminalAttempt{accepted: !errors.Is(err, ErrAlreadyAttached), err: err}
	default:
		return terminalAttempt{accepted: true, err: fmt.Errorf("unsupported terminal owner %v", owner)}
	}
}
