package service

import (
	"context"
	"sync"
	"time"
)

const restartHoldoff = time.Second

// BaseWorker provides thread-safe default lifecycle and Tap/Notify behavior.
// Concrete services should embed BaseWorker and implement worker hooks.
type BaseWorker struct {
	name string

	mu          sync.Mutex
	state       RunState
	wanted      bool
	initialized bool

	loopCtx    context.Context
	loopCancel context.CancelFunc
	loopDone   chan struct{}

	runCtx    context.Context
	runCancel context.CancelFunc

	holdoffUntil time.Time

	handlersMu     sync.RWMutex
	handlers       map[uint64]func(any)
	nextHandlerID  uint64
	stopSignalChan chan struct{}
	hooks          workerHooks
}

type workerHooks interface {
	WorkerInit()
	WorkerStart() error
	// WorkerRun must block until ctx is cancelled. Returning nil immediately
	// causes the run loop to re-invoke WorkerRun without delay (spin loop).
	WorkerRun(ctx context.Context) error
	WorkerStop()
}

// WorkerInit is a default no-op hook.
func (w *BaseWorker) WorkerInit() {}

// WorkerStart is a default no-op hook.
func (w *BaseWorker) WorkerStart() error { return nil }

// WorkerRun is a default no-op hook.
func (w *BaseWorker) WorkerRun(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// WorkerStop is a default no-op hook.
func (w *BaseWorker) WorkerStop() {}

// NewBaseWorker creates a BaseWorker with the given service name.
func NewBaseWorker(name string) BaseWorker {
	return BaseWorker{
		name:           name,
		state:          StateStopped,
		handlers:       make(map[uint64]func(any)),
		stopSignalChan: make(chan struct{}, 1),
	}
}

// BindHooks sets lifecycle hooks used by the internal run loop.
// Concrete services that embed BaseWorker MUST call BindHooks(self) in their
// constructor, before Start() is called. Failure to do so means WorkerInit,
// WorkerStart, WorkerRun, and WorkerStop dispatch to the BaseWorker no-ops
// rather than the embedding type's implementations.
func (w *BaseWorker) BindHooks(hooks workerHooks) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if hooks == nil {
		w.hooks = nil
		return
	}
	w.hooks = hooks
}

func (w *BaseWorker) hooksLocked() workerHooks {
	if w.hooks != nil {
		return w.hooks
	}
	return w
}

func (w *BaseWorker) currentHooks() workerHooks {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hooksLocked()
}

// Name returns the logical service name.
func (w *BaseWorker) Name() string {
	return w.name
}

// State returns the current lifecycle state.
func (w *BaseWorker) State() RunState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// Start requests the worker to start and remain running until Stop is called.
func (w *BaseWorker) Start(ctx context.Context) {
	w.mu.Lock()
	w.wanted = true
	if w.loopDone == nil {
		w.loopCtx, w.loopCancel = context.WithCancel(ctx)
		w.loopDone = make(chan struct{})
		go w.runLoop()
	}
	w.signalLoopLocked()
	w.mu.Unlock()
}

// Stop requests the worker to stop.
func (w *BaseWorker) Stop() {
	w.mu.Lock()
	w.wanted = false
	if w.runCancel != nil {
		w.runCancel()
	}
	w.signalLoopLocked()
	w.mu.Unlock()
}

// Restart requests stop + delayed start (1 second holdoff).
func (w *BaseWorker) Restart() {
	w.mu.Lock()
	if w.runCancel != nil {
		w.runCancel()
	}
	w.wanted = true
	w.holdoffUntil = time.Now().Add(restartHoldoff)
	w.setStateLocked(StateStopping)
	w.signalLoopLocked()
	w.mu.Unlock()
}

// Notify fans out data to all currently registered handlers.
func (w *BaseWorker) Notify(data any) {
	w.handlersMu.RLock()
	handlers := make([]func(any), 0, len(w.handlers))
	for _, h := range w.handlers {
		handlers = append(handlers, h)
	}
	w.handlersMu.RUnlock()

	for _, h := range handlers {
		h(data)
	}
}

// Tap registers a handler and returns an unsubscribe function.
func (w *BaseWorker) Tap(handler func(any)) func() {
	w.handlersMu.Lock()
	id := w.nextHandlerID
	w.nextHandlerID++
	w.handlers[id] = handler
	w.handlersMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			w.handlersMu.Lock()
			delete(w.handlers, id)
			w.handlersMu.Unlock()
		})
	}
}

// Shutdown terminates the internal worker loop and blocks until it exits.
func (w *BaseWorker) Shutdown() {
	w.mu.Lock()
	if w.loopCancel != nil {
		w.wanted = false
		if w.runCancel != nil {
			w.runCancel()
		}
		w.loopCancel()
		w.signalLoopLocked()
	}
	done := w.loopDone
	w.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (w *BaseWorker) runLoop() {
	defer func() {
		w.mu.Lock()
		done := w.loopDone
		w.state = StateStopped
		w.loopDone = nil
		w.runCancel = nil
		w.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	w.mu.Lock()
	needInit := !w.initialized
	w.mu.Unlock()
	if needInit {
		w.currentHooks().WorkerInit()
		w.mu.Lock()
		w.initialized = true
		w.mu.Unlock()
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		w.mu.Lock()
		loopCtx := w.loopCtx
		state := w.state
		wanted := w.wanted
		holdoffUntil := w.holdoffUntil
		w.mu.Unlock()

		select {
		case <-loopCtx.Done():
			w.stopWorker()
			return
		default:
		}

		switch state {
		case StateStopped:
			if wanted {
				if !holdoffUntil.IsZero() && time.Now().Before(holdoffUntil) {
					w.waitLoopSignal(ticker.C)
					continue
				}
				w.mu.Lock()
				w.holdoffUntil = time.Time{}
				w.setStateLocked(StateStarting)
				w.mu.Unlock()
			} else {
				w.waitLoopSignal(ticker.C)
			}
		case StateStarting:
			if !holdoffUntil.IsZero() && time.Now().Before(holdoffUntil) {
				w.waitLoopSignal(ticker.C)
				continue
			}
			if err := w.currentHooks().WorkerStart(); err != nil {
				w.mu.Lock()
				if w.wanted {
					w.holdoffUntil = time.Now().Add(restartHoldoff)
				}
				w.setStateLocked(StateStopped)
				w.mu.Unlock()
				w.waitLoopSignal(ticker.C)
				continue
			}
			w.mu.Lock()
			w.holdoffUntil = time.Time{}
			w.setStateLocked(StateRunning)
			w.mu.Unlock()
		case StateRunning:
			if !wanted {
				w.mu.Lock()
				w.setStateLocked(StateStopping)
				w.mu.Unlock()
				continue
			}
			runCtx := w.newRunContext()
			err := w.currentHooks().WorkerRun(runCtx)
			if IsServiceRestartSignal(err) {
				w.mu.Lock()
				w.holdoffUntil = time.Now().Add(restartHoldoff)
				w.setStateLocked(StateStopping)
				w.mu.Unlock()
				continue
			}
			if err != nil {
				w.mu.Lock()
				w.holdoffUntil = time.Now()
				w.setStateLocked(StateStopping)
				w.mu.Unlock()
				continue
			}
		case StateStopping:
			w.stopWorker()
			w.mu.Lock()
			if w.wanted {
				if w.holdoffUntil.IsZero() {
					w.holdoffUntil = time.Now().Add(restartHoldoff)
				}
			}
			w.setStateLocked(StateStopped)
			w.mu.Unlock()
		}
	}
}

func (w *BaseWorker) waitLoopSignal(tick <-chan time.Time) {
	select {
	case <-w.stopSignalChan:
	case <-tick:
	}
}

func (w *BaseWorker) signalLoopLocked() {
	select {
	case w.stopSignalChan <- struct{}{}:
	default:
	}
}

func (w *BaseWorker) newRunContext() context.Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.loopCtx == nil {
		return context.Background()
	}
	if w.runCancel != nil {
		w.runCancel()
	}
	w.runCtx, w.runCancel = context.WithCancel(w.loopCtx)
	return w.runCtx
}

func (w *BaseWorker) stopWorker() {
	w.mu.Lock()
	cancel := w.runCancel
	w.runCancel = nil
	w.runCtx = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.currentHooks().WorkerStop()
}

func (w *BaseWorker) setStateLocked(state RunState) {
	w.state = state
}
