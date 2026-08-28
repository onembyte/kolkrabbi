package tui

import (
	"context"
	"errors"
	"io"
	"sync"
)

// ErrExit lets a CLI-owned slash dispatcher request a clean runtime exit
// without teaching the presentation package what /exit means.
var ErrExit = errors.New("exit terminal runtime")

const (
	defaultWidth     = 80
	defaultHeight    = 24
	defaultDraftSize = 64 * 1024
)

// RuntimeOptions supplies the terminal-independent seams used by the
// interactive event loop. The CLI owns command dispatch and model work; the
// runtime owns only input decoding, screen repainting, and concurrency.
type RuntimeOptions struct {
	Input  io.Reader
	Output io.Writer
	Width  func() int
	Height func() int
	// Resize fires when the terminal changes size. The runtime probes Width and
	// Height again and repaints; a nil channel means the size never changes.
	Resize   <-chan struct{}
	Status   Status
	Commands []CommandSpec
	Models   []ModelSpec
	Plans    []PlanSpec
	Files    []string
	Turn     func(context.Context, string) error
	// CyclePermission advances to the next permission tier and returns the
	// one now in effect. Nil leaves Shift+Tab inert.
	CyclePermission func() string
}

// Runtime serializes all terminal presentation through one controller. Model
// output may stream from a worker while the main goroutine keeps accepting a
// separate type-ahead draft.
type Runtime struct {
	mu         sync.Mutex
	input      io.Reader
	controller *Controller
	renderer   *Renderer
	decoder    *Decoder
	width      func() int
	height     func() int
	turn       func(context.Context, string) error
	cyclePerm  func() string
	spinClock  spinnerClock

	baseContext context.Context
	activeID    uint64
	activeStop  context.CancelFunc
	activityID  uint64
	turns       sync.WaitGroup
	approval    chan Decision
	quit        chan struct{}
	quitOnce    sync.Once
	resize      <-chan struct{}
	// closing is set once the read loop has exited. A queued request must not
	// start after that: turns.Wait has begun, so Add would race it, and a
	// session that is ending should not send one more message.
	closing   bool
	renderErr error
}

// NewRuntime creates a normal-screen runtime. Terminal raw mode remains a CLI
// responsibility so tests and non-TTY fallbacks never change process state.
func NewRuntime(options RuntimeOptions) *Runtime {
	if options.Input == nil {
		options.Input = emptyReader{}
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Width == nil {
		options.Width = func() int { return defaultWidth }
	}
	if options.Height == nil {
		options.Height = func() int { return defaultHeight }
	}
	controller := NewController(options.Status, defaultDraftSize)
	controller.SetCommands(options.Commands, 8)
	controller.SetModels(options.Models)
	controller.SetFiles(options.Files)
	controller.SetPlans(options.Plans)
	return &Runtime{
		input: options.Input, controller: controller,
		renderer: NewRenderer(options.Output), decoder: NewDecoder(),
		width: options.Width, height: options.Height, resize: options.Resize, turn: options.Turn,
		cyclePerm: options.CyclePermission,
		spinClock: realSpinnerClock{},
		quit:      make(chan struct{}),
	}
}

// Run owns terminal rendering until EOF, Ctrl+D, or parent cancellation.
func (r *Runtime) Run(ctx context.Context) error {
	r.mu.Lock()
	r.baseContext = ctx
	if err := r.renderer.Start(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.renderLocked()
	r.mu.Unlock()

	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := r.input.Read(buffer)
			result := readResult{err: err}
			if n > 0 {
				result.data = append([]byte(nil), buffer[:n]...)
			}
			select {
			case reads <- result:
			case <-ctx.Done():
				return
			case <-r.quit:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var readErr error
readLoop:
	for {
		select {
		case <-ctx.Done():
			readErr = ctx.Err()
			break readLoop
		case <-r.quit:
			break readLoop
		case <-r.resize:
			r.Resize()
		case result := <-reads:
			for _, key := range r.decoder.Feed(result.data) {
				if r.HandleKey(key).Exit {
					break readLoop
				}
			}
			if result.err != nil {
				if !errors.Is(result.err, io.EOF) {
					readErr = result.err
				}
				break readLoop
			}
		}
	}

	r.mu.Lock()
	r.closing = true
	if r.activeStop != nil {
		r.activeStop()
	}
	r.mu.Unlock()
	r.turns.Wait()

	r.mu.Lock()
	closeErr := r.renderer.Close()
	renderErr := r.renderErr
	r.mu.Unlock()
	return errors.Join(readErr, renderErr, closeErr)
}

// Resize repaints for the terminal's current size. The renderer is told first,
// so the rows it erases are the rows the re-flowed previous frame occupies.
func (r *Runtime) Resize() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.renderErr != nil {
		return
	}
	width := r.width()
	if width <= 0 {
		width = defaultWidth
	}
	r.renderer.Resized(width)
	r.renderLocked()
}

// HandleKey applies one already-decoded input event and performs its runtime
// effects without ever running model work while holding the screen lock.
func (r *Runtime) HandleKey(key Key) Effect {
	r.mu.Lock()
	effect := r.controller.HandleKey(key)
	if effect.Interrupt && r.activeStop != nil {
		r.activeStop()
	}
	if effect.Decision != DecisionNone && r.approval != nil {
		r.approval <- effect.Decision
		r.approval = nil
	}
	if effect.CyclePermission && r.cyclePerm != nil {
		if tier := r.cyclePerm(); tier != "" {
			r.controller.SetApproval(tier)
			r.renderLocked()
		}
	}
	if effect.Submit != "" {
		r.startTurnLocked(effect.Submit)
	}
	r.renderLocked()
	r.mu.Unlock()
	return effect
}

// Write appends streamed assistant/tool bytes to the transcript region. It
// deliberately never touches the activity row or current composer draft.
func (r *Runtime) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controller.AppendTranscript(string(p))
	r.renderLocked()
	if r.renderErr != nil {
		return len(p), r.renderErr
	}
	return len(p), nil
}

// Start implements the engine activity port. Loading is one replaceable
// spinner cell, never transcript output, so repeated phases cannot flood the
// terminal or move the user's draft.
func (r *Runtime) Start(ctx context.Context, phase string) func() {
	return r.startActivity(ctx, phase)
}

// StartWork implements the engine's local-tool activity port. Its argument is
// the tool's own description, which is too specific for the status row; local
// work is reported as work.
func (r *Runtime) StartWork(ctx context.Context, _ string) func() {
	return r.startActivity(ctx, "working")
}

func (r *Runtime) startActivity(ctx context.Context, phase string) func() {
	activityContext, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.spinClock == nil {
		r.spinClock = realSpinnerClock{}
	}
	r.activityID++
	id := r.activityID
	r.controller.SetActivity(activityLine(0, phase))
	r.renderLocked()
	r.mu.Unlock()

	done := make(chan struct{})
	go r.animateActivity(activityContext, id, phase, done)
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func (r *Runtime) animateActivity(ctx context.Context, id uint64, phase string, done chan<- struct{}) {
	defer close(done)
	defer r.clearActivity(id)
	frame := 1
	for {
		timer := r.spinClock.NewTimer(spinnerInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
			timer.Stop()
		}

		r.mu.Lock()
		if r.activityID != id {
			r.mu.Unlock()
			return
		}
		r.controller.SetActivity(activityLine(frame, phase))
		r.renderLocked()
		r.mu.Unlock()
		frame = (frame + 1) % len(wheelFrames)
	}
}

func (r *Runtime) clearActivity(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activityID != id {
		return
	}
	r.controller.SetActivity("")
	r.renderLocked()
}

// Confirm displays a focused approval overlay and blocks only the calling
// model worker. Main-composer storage is never reused for the y/N answer.
func (r *Runtime) Confirm(ctx context.Context, approval Approval) bool {
	decision := r.Decide(ctx, approval)
	return decision == DecisionAllow || decision == DecisionAllowAlways
}

// Decide is Confirm with the answer kept whole, so a caller that can act on
// "always" is not handed a boolean that has already forgotten which yes it was.
func (r *Runtime) Decide(ctx context.Context, approval Approval) Decision {
	reply := make(chan Decision, 1)
	r.mu.Lock()
	if r.approval != nil {
		r.mu.Unlock()
		return DecisionDeny
	}
	r.approval = reply
	r.controller.RequestApproval(approval)
	r.renderLocked()
	r.mu.Unlock()

	select {
	case decision := <-reply:
		return decision
	case <-ctx.Done():
		r.mu.Lock()
		if r.approval == reply {
			r.approval = nil
			_ = r.controller.HandleKey(Key{Kind: KeyInterrupt})
			r.renderLocked()
		}
		r.mu.Unlock()
		return DecisionDeny
	}
}

// Snapshot returns a race-free copy of all independently rendered regions.
func (r *Runtime) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller.Snapshot()
}

// SetStatus updates mode/model/session labels after a slash command without
// disturbing output or a type-ahead draft.
func (r *Runtime) SetStatus(status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controller.SetStatus(status)
	r.renderLocked()
}

// Approval returns a race-free copy of the active permission overlay.
func (r *Runtime) Approval() *Approval {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller.Approval()
}

// Controller exposes the pure controller for setup before Run. Concurrent
// callers should use Runtime methods instead.
func (r *Runtime) Controller() *Controller { return r.controller }

func (r *Runtime) startTurnLocked(prompt string) {
	if r.turn == nil {
		r.controller.FinishTurn("ready")
		return
	}
	base := r.baseContext
	if base == nil {
		r.controller.FinishTurn("failed")
		return
	}
	turnContext, stop := context.WithCancel(base)
	r.activeID++
	id := r.activeID
	r.activeStop = stop
	r.turns.Add(1)
	go func() {
		defer r.turns.Done()
		err := r.turn(turnContext, prompt)
		contextErr := turnContext.Err()
		stop()
		lifecycle := "ready"
		if errors.Is(err, context.Canceled) || errors.Is(contextErr, context.Canceled) {
			lifecycle = "interrupted"
		} else if err != nil {
			lifecycle = "failed"
		}
		r.mu.Lock()
		if r.activeID == id {
			r.activeStop = nil
			if errors.Is(err, ErrExit) {
				lifecycle = "ready"
				r.quitOnce.Do(func() { close(r.quit) })
			}
			r.controller.FinishTurn(lifecycle)
			// A request queued while this turn ran starts now, on the same
			// goroutine's lock, so the queue drains without the user pressing
			// anything again. Not after an exit, and not after an interrupt:
			// Ctrl+C means stop, and sending the queued request then would be
			// the opposite of what was asked.
			if lifecycle == "ready" && !errors.Is(err, ErrExit) && !r.closing {
				if queued := r.controller.TakeQueued(); queued != "" {
					r.controller.BeginTurn()
					r.startTurnLocked(queued)
				}
			}
			r.renderLocked()
		}
		r.mu.Unlock()
	}()
}

func (r *Runtime) renderLocked() {
	if r.renderErr != nil {
		return
	}
	width, height := r.width(), r.height()
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	r.renderErr = r.renderer.Render(r.controller.RenderView(width, height))
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
