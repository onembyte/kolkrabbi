package tui

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
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
	Settings []SettingSpec
	Files    []string
	Turn     func(context.Context, string) error
	// CyclePermission advances to the next permission tier and returns the
	// one now in effect. Nil leaves Shift+Tab inert.
	CyclePermission func() string
	// Meter supplies fresh context-window and session-cost labels, read on the
	// spinner's tick while work is running. Without it the CLI could rebuild the
	// footer only between turns, so both numbers froze for exactly as long as a
	// turn ran — which is when the context number is the interesting one.
	Meter func() (context string, cost string)
}

// synchronizedWriter serializes renderer frames with raw bytes from an
// attached provider CLI. A terminal file is safe for concurrent writes on the
// usual platforms, but Runtime also accepts bytes.Buffer and strings.Builder
// in tests and embedding callers; more importantly, two writes to a terminal
// can interleave halfway through an escape sequence. Both output owners must
// therefore share one write boundary.
type synchronizedWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
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
	spinClock  spinnerClock
	turn       func(context.Context, string) error
	cyclePerm  func() string
	meter      func() (string, string)
	// Frame pacing. Streaming floods Write with a token apiece; repainting on
	// every token re-renders the whole transcript per byte and makes the frame
	// chase the model instead of the reader. Frames coalesce to ~30/s, which no
	// one can tell from instant and which a flood costs one repaint each.
	frameWindow bool
	frameTimer  *time.Timer
	frameDirty  bool

	baseContext context.Context
	activeID    uint64
	activeStop  context.CancelFunc
	activityID  uint64
	// activities are every piece of work currently in flight, oldest first.
	// Agent mode runs several subagents at once, so this cannot be one slot:
	// the first of them to finish used to blank the row while the others were
	// still working, which read as "it froze".
	activities []activityEntry
	frame      int
	animating  bool
	animDone   chan struct{}
	// animIdle wakes the animator the moment the last activity ends, so a stop
	// never has to wait out a frame interval to be told the row is empty.
	animIdle chan struct{}
	turns    sync.WaitGroup
	approval chan Decision
	// question is the reply channel of the model worker waiting on a picker.
	question chan questionReply
	// output is the terminal itself, kept so a child process can be given the
	// screen directly for as long as it owns it.
	output io.Writer
	// attached, while set, is where raw keyboard bytes go instead of to the
	// decoder. It is how a vendor CLI runs inside the session: the ONE reader
	// on the terminal stays the read goroutine, and it forwards. A second
	// reader on the same descriptor is the bug this design exists to avoid.
	attached io.Writer
	// modelPick is the reply channel of the surface waiting on the /model
	// overlay. Empty answer means dismissed.
	modelPick chan string
	// configPick is modelPick's counterpart for the /config overlay.
	configPick chan string
	quit       chan struct{}
	quitOnce   sync.Once
	resize     <-chan struct{}
	// closing is set once the read loop has exited. A queued request must not
	// start after that: turns.Wait has begun, so Add would race it, and a
	// session that is ending should not send one more message.
	closing bool
	// closed is set once the renderer has been shut down. A pacing timer firing
	// after Run has returned must not paint into a terminal the process no
	// longer owns; with a closed flag the timer's work collapses to flag care.
	closed    bool
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
	output := &synchronizedWriter{out: options.Output}
	controller := NewController(options.Status, defaultDraftSize)
	controller.SetCommands(options.Commands, 8)
	controller.SetModels(options.Models)
	controller.SetFiles(options.Files)
	controller.SetPlans(options.Plans)
	controller.SetSettings(options.Settings)
	return &Runtime{
		input: options.Input, controller: controller,
		renderer: NewRenderer(output), decoder: NewDecoder(),
		output: output,
		width:  options.Width, height: options.Height, resize: options.Resize, turn: options.Turn,
		cyclePerm: options.CyclePermission, meter: options.Meter,
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
			r.mu.Lock()
			sink := r.attached
			r.mu.Unlock()
			if sink != nil {
				// The child owns the keyboard. Nothing is decoded, so nothing
				// is interpreted: kolk is a wire here, which is what keeps it
				// from seeing a credential typed at a vendor's prompt.
				if len(result.data) > 0 {
					_, _ = sink.Write(result.data)
				}
				if result.err != nil {
					break readLoop
				}
				continue
			}
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
	// The last frame first: a deferred one pending in the pacing window has to
	// reach the screen while the renderer can still draw it, not after Close.
	r.flushFrameLocked()
	r.closed = true
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
	if (effect.Choice > 0 || effect.ChoiceDismissed) && r.question != nil {
		r.question <- r.controller.chosen(effect)
		r.question = nil
	}
	if (effect.PickModel != "" || effect.PickDismissed) && r.modelPick != nil {
		r.modelPick <- effect.PickModel
		r.modelPick = nil
	}
	if (effect.PickConfig != "" || effect.PickDismissed) && r.configPick != nil {
		r.configPick <- effect.PickConfig
		r.configPick = nil
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

// activityEntry is one in-flight piece of work. The phase is kept so that when
// the newest finishes the row can fall back to what is still running instead of
// going blank.
type activityEntry struct {
	id    uint64
	phase string
}

func (r *Runtime) startActivity(ctx context.Context, phase string) func() {
	activityContext, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.spinClock == nil {
		r.spinClock = realSpinnerClock{}
	}
	r.activityID++
	id := r.activityID
	r.activities = append(r.activities, activityEntry{id: id, phase: phase})
	r.showActivityLocked()
	// One animator serves every activity. Starting it here, under the same lock
	// that appended, is what makes the handoff safe: the animator only retires
	// while holding this lock and seeing an empty list, so it can never quit
	// between the append and this check.
	spawn := !r.animating
	if spawn {
		r.animating = true
		r.animDone = make(chan struct{})
		// Each animator gets a fresh idle channel, so a wake-up meant for a
		// retired one cannot reach its successor.
		r.animIdle = make(chan struct{}, 1)
	}
	animDone, animIdle := r.animDone, r.animIdle
	r.mu.Unlock()

	if spawn {
		go r.animateActivities(animDone, animIdle)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-activityContext.Done()
		r.finishActivity(id)
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
			// The engine is promised that presentation is gone before output
			// continues. That only needs the animator joined when this was the
			// last activity; otherwise the row legitimately still belongs to
			// work that has not finished.
			r.mu.Lock()
			last := len(r.activities) == 0
			r.mu.Unlock()
			if last {
				<-animDone
			}
		})
	}
}

// showActivityLocked draws the newest activity, or clears the row when nothing
// is running. The newest wins because it is the most specific description of
// what the session is doing right now.
func (r *Runtime) showActivityLocked() {
	if len(r.activities) == 0 {
		r.controller.SetActivity("")
		r.renderLocked()
		return
	}
	r.controller.SetActivity(activityLine(r.frame, r.activities[len(r.activities)-1].phase))
	r.renderLocked()
}

func (r *Runtime) animateActivities(done, idle chan struct{}) {
	defer close(done)
	for {
		timer := r.spinClock.NewTimer(spinnerInterval)
		select {
		case <-r.quit:
			timer.Stop()
			r.mu.Lock()
			r.animating = false
			r.mu.Unlock()
			return
		case <-idle:
			// The list may have emptied. Checking here rather than waiting for
			// the next tick is what keeps a stop prompt; without it a stop that
			// joins the animator waits a whole frame, and against an injected
			// clock that never ticks again it waits forever.
			timer.Stop()
			if r.retireIfIdle() {
				return
			}
			continue
		case <-timer.C():
			timer.Stop()
		}

		if r.retireIfIdle() {
			return
		}
		// Labels are read before the screen lock: the meter reaches into the
		// engine, and the engine must be able to need the screen while answering
		// without the screen waiting on the engine.
		var contextLabel, costLabel string
		if r.meter != nil {
			contextLabel, costLabel = r.meter()
		}
		r.mu.Lock()
		r.frame = (r.frame + 1) % len(wheelFrames)
		if r.meter != nil {
			r.controller.SetUsage(contextLabel, costLabel)
		}
		r.showActivityLocked()
		r.mu.Unlock()
	}
}

// retireIfIdle stands the animator down when nothing is left to animate. The
// check and the flag move together under one lock, which is what lets a start
// racing this decision either be seen here or spawn a replacement, never
// neither.
func (r *Runtime) retireIfIdle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.activities) > 0 {
		return false
	}
	r.animating = false
	return true
}

func (r *Runtime) finishActivity(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, entry := range r.activities {
		if entry.id == id {
			r.activities = append(r.activities[:index], r.activities[index+1:]...)
			break
		}
	}
	r.showActivityLocked()
	if len(r.activities) == 0 && r.animIdle != nil {
		// Never block while holding the lock: one pending wake-up is enough,
		// and the animator re-reads the list rather than trusting the signal.
		select {
		case r.animIdle <- struct{}{}:
		default:
		}
	}
}

// questionReply carries a picked option back to the waiting model worker.
// Dismissed is kept apart from the option because closing a question is not
// choosing anything, and the engine must be able to tell the difference.
type questionReply struct {
	option    string
	dismissed bool
}

// Ask puts a fixed-option question on screen and blocks the calling model
// worker until it is answered or dismissed. Only the worker blocks: the read
// loop keeps running, so the picker responds to keys and the session can still
// be interrupted.
//
// ok is false when the question was dismissed. A second question arriving while
// one is open is refused rather than queued -- the model should be working on
// the first answer, not stacking questions in front of the person.
func (r *Runtime) Ask(ctx context.Context, question Question) (string, bool) {
	if len(question.Options) == 0 {
		return "", false
	}
	reply := make(chan questionReply, 1)
	r.mu.Lock()
	if r.question != nil || r.approval != nil || r.modelPick != nil || r.configPick != nil || r.attached != nil {
		r.mu.Unlock()
		return "", false
	}
	r.question = reply
	r.controller.RequestQuestion(question)
	r.renderLocked()
	r.mu.Unlock()

	select {
	case answer := <-reply:
		if answer.dismissed {
			return "", false
		}
		return answer.option, true
	case <-ctx.Done():
		// The turn was interrupted. Take the picker down rather than leave it
		// on screen waiting for an answer nobody is listening for any more.
		r.mu.Lock()
		if r.question == reply {
			r.question = nil
			_ = r.controller.HandleKey(Key{Kind: KeyInterrupt})
			r.renderLocked()
		}
		r.mu.Unlock()
		return "", false
	}
}

// AskModel opens the /model picker and returns the command line to run, or
// false when the picker was dismissed or refused (another overlay is already
// open, or no rows were given). Like Ask, it blocks only the calling worker.
func (r *Runtime) AskModel(ctx context.Context, entries []ModelPickEntry) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	reply := make(chan string, 1)
	r.mu.Lock()
	if r.question != nil || r.approval != nil || r.modelPick != nil || r.configPick != nil || r.attached != nil {
		r.mu.Unlock()
		return "", false
	}
	r.modelPick = reply
	r.controller.RequestModelPicker(entries)
	r.renderLocked()
	r.mu.Unlock()

	select {
	case answer := <-reply:
		if answer == "" {
			return "", false
		}
		return answer, true
	case <-ctx.Done():
		// The turn was interrupted. Take the picker down rather than leave it
		// on screen waiting for an answer nobody is listening for any more.
		r.mu.Lock()
		if r.modelPick == reply {
			r.modelPick = nil
			_ = r.controller.HandleKey(Key{Kind: KeyInterrupt})
			r.renderLocked()
		}
		r.mu.Unlock()
		return "", false
	}
}

// AskConfig opens the /config picker and returns the setting key chosen, or
// false when the picker was dismissed or refused (another overlay is already
// open, or no rows were given). Unlike AskModel's answer, the key returned is
// never itself a command: resolving it already filled the composer's draft
// with `/config set <key> `, so the caller has nothing further to submit.
func (r *Runtime) AskConfig(ctx context.Context, entries []SettingSpec) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	reply := make(chan string, 1)
	r.mu.Lock()
	if r.question != nil || r.approval != nil || r.modelPick != nil || r.configPick != nil || r.attached != nil {
		r.mu.Unlock()
		return "", false
	}
	r.configPick = reply
	r.controller.RequestConfigPicker(entries)
	r.renderLocked()
	r.mu.Unlock()

	select {
	case answer := <-reply:
		if answer == "" {
			return "", false
		}
		return answer, true
	case <-ctx.Done():
		r.mu.Lock()
		if r.configPick == reply {
			r.configPick = nil
			_ = r.controller.HandleKey(Key{Kind: KeyInterrupt})
			r.renderLocked()
		}
		r.mu.Unlock()
		return "", false
	}
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
	if r.approval != nil || r.question != nil || r.modelPick != nil || r.configPick != nil || r.attached != nil {
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

// ConfigPicker reports the open /config overlay, race-safe for a caller
// outside this package: Controller.ConfigPicker reads state HandleKey and
// AskConfig mutate under this Runtime's own lock, so a caller polling it
// without going through here would be racing them.
func (r *Runtime) ConfigPicker() *ConfigPick {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller.ConfigPicker()
}

// SetStatus updates mode/model/session labels after a slash command without
// disturbing output or a type-ahead draft.
func (r *Runtime) SetStatus(status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controller.SetStatus(status)
	r.renderLocked()
}

// SetAgentStatus applies one concurrent subagent lifecycle update under the
// same lock that owns rendering and snapshots.
func (r *Runtime) SetAgentStatus(status AgentStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controller.SetAgentStatus(status)
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
	// The request joins the transcript before the answer does. Without it the
	// draft vanished on Enter and the scrollback held only replies, so a
	// session read as a monologue and there was no record of what was asked.
	r.controller.AppendTranscript(promptEcho(prompt))
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
			// The stop has to be visible where the output it stopped is, or
			// the first thing a person reads against is whether the key did
			// anything at all. Under the screen lock like every other
			// transcript mutation: the model has none of its own.
			if lifecycle == "interrupted" {
				r.controller.AppendTranscript(interruptedNotice)
			}
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

const renderInterval = 33 * time.Millisecond

// renderLocked repaints or defers. Inside an open pacing window a request only
// marks the frame dirty; the window's timer does the painting, so a burst of
// twenty writes costs one repaint instead of twenty. Input and resize arrive as
// single events and a held frame would lag the keys by at most the window, so
// they take the same path — simplicity here is worth more than a fast path.
func (r *Runtime) renderLocked() {
	if r.renderErr != nil {
		return
	}
	if r.frameWindow {
		r.frameDirty = true
		return
	}
	r.paintLocked()
}

// paintLocked draws a frame and opens the next pacing window. The window always
// closes through closeFrameLocked, which runs on the timer's goroutine; the
// timer is armed under the same lock that set the flag, so a window can neither
// leak open nor be closed twice.
func (r *Runtime) paintLocked() {
	// A frame after close would land on a terminal this process no longer
	// owns; the pacing timer that could outlive Run is answered with nothing.
	if r.closed {
		return
	}
	width, height := r.width(), r.height()
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	// Order matters: what leaves the frame is printed above it in the same
	// write, so a line is never on screen twice and never missing for a frame.
	committed := r.controller.CommitOverflow(width, height)
	r.renderErr = r.renderer.Render(committed, r.controller.RenderView(width, height))
	r.frameWindow = true
	r.frameTimer = time.AfterFunc(renderInterval, r.closeFrameWindow)
}

func (r *Runtime) closeFrameWindow() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frameWindow = false
	r.frameTimer = nil
	if r.frameDirty && r.renderErr == nil {
		r.frameDirty = false
		r.paintLocked()
	}
}

// flushFrameLocked paints any deferred frame now: called on the way out, so a
// session's last state reaches the screen even if it arrived inside a window.
func (r *Runtime) flushFrameLocked() {
	if r.frameTimer != nil {
		r.frameTimer.Stop()
		r.frameTimer = nil
	}
	r.frameWindow = false
	if r.frameDirty && r.renderErr == nil {
		r.frameDirty = false
		r.paintLocked()
	}
}

type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
