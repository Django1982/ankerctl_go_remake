package service

import (
	"context"
	"errors"
)

// RunState is the lifecycle state of a service worker.
type RunState int

const (
	StateStopped RunState = iota
	StateStarting
	StateRunning
	StateStopping
)

// Service defines the lifecycle and event fan-out API used by background services.
type Service interface {
	WorkerInit()
	WorkerStart() error
	// WorkerRun is called repeatedly while the service is in the Running state.
	// Implementations MUST block until ctx is cancelled (e.g. select on ctx.Done()).
	// A WorkerRun that returns immediately will cause the framework to spin at 100% CPU.
	WorkerRun(ctx context.Context) error
	WorkerStop()
	Name() string
	State() RunState
	// Start requests the worker to start. Idempotent if already running.
	Start(ctx context.Context)
	// Stop requests the worker to stop. Returns immediately; does not block.
	Stop()
	Restart()
	// Shutdown cancels the internal loop goroutine and blocks until it exits.
	// Must be called to avoid goroutine leaks after Stop().
	Shutdown()
	Notify(data any)
	Tap(handler func(any)) func()
}

// ServiceRestartSignal is a sentinel error that requests a clean worker restart.
type ServiceRestartSignal struct{}

func (ServiceRestartSignal) Error() string {
	return "service restart requested"
}

// ErrServiceRestartSignal is returned by WorkerRun to request a restart.
var ErrServiceRestartSignal error = ServiceRestartSignal{}

// IsServiceRestartSignal reports whether err asks for a clean restart.
func IsServiceRestartSignal(err error) bool {
	if err == nil {
		return false
	}
	var sig ServiceRestartSignal
	return errors.As(err, &sig)
}
