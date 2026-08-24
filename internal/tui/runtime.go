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
	Input    io.Reader
	Output   io.Writer
	Width    func() int
	Height   func() int
	Status   Status
	Commands []CommandSpec
	Turn     func(context.Context, string) error
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

	baseContext context.Context
	activeID    uint64
	activeStop  context.CancelFunc
	turns       sync.WaitGroup
	approval    chan Decision
	quit        chan struct{}
	quitOnce    sync.Once
	renderErr   error
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
	return &Runtime{
		input: options.Input, controller: controller,
		renderer: NewRenderer(options.Output), decoder: NewDecoder(),
		width: options.Width, height: options.Height, turn: options.Turn,
		quit: make(chan struct{}),
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
// octopus row, never transcript output, so repeated phases cannot flood the
// terminal or move the user's draft.
func (r *Runtime) Start(_ context.Context, phase string) func() {
	return r.startActivity(phase)
}

// StartWork implements the engine's local-tool activity port.
func (r *Runtime) StartWork(_ context.Context, description string) func() {
	return r.startActivity(description)
}

func (r *Runtime) startActivity(description string) func() {
	r.mu.Lock()
	r.controller.SetActivity("🐙 " + description + "…")
	r.renderLocked()
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.controller.SetActivity("")
			r.renderLocked()
			r.mu.Unlock()
		})
	}
}

// Confirm displays a focused approval overlay and blocks only the calling
// model worker. Main-composer storage is never reused for the y/N answer.
func (r *Runtime) Confirm(ctx context.Context, approval Approval) bool {
	reply := make(chan Decision, 1)
	r.mu.Lock()
	if r.approval != nil {
		r.mu.Unlock()
		return false
	}
	r.approval = reply
	r.controller.RequestApproval(approval)
	r.renderLocked()
	r.mu.Unlock()

	select {
	case decision := <-reply:
		return decision == DecisionAllow
	case <-ctx.Done():
		r.mu.Lock()
		if r.approval == reply {
			r.approval = nil
			_ = r.controller.HandleKey(Key{Kind: KeyInterrupt})
			r.renderLocked()
		}
		r.mu.Unlock()
		return false
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
	r.renderErr = r.renderer.Render(r.controller.View(width, height))
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
