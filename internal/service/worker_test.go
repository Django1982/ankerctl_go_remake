package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testWorker struct {
	BaseWorker

	mu         sync.Mutex
	startTimes []time.Time
	runCount   int
	restartOn  int
}

func newTestWorker(name string) *testWorker {
	tw := &testWorker{
		BaseWorker: NewBaseWorker(name),
		restartOn:  1,
	}
	tw.BindHooks(tw)
	return tw
}

func (w *testWorker) WorkerInit() {}

func (w *testWorker) WorkerStart() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.startTimes = append(w.startTimes, time.Now())
	return nil
}

func (w *testWorker) WorkerRun(ctx context.Context) error {
	w.mu.Lock()
	w.runCount++
	runCount := w.runCount
	restartOn := w.restartOn
	w.mu.Unlock()

	if runCount == restartOn {
		return ErrServiceRestartSignal
	}

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func (w *testWorker) WorkerStop() {}

func TestWorkerTapNotify(t *testing.T) {
	w := newTestWorker("tap-worker")

	got := make([]int, 0, 4)
	var mu sync.Mutex

	unsubA := w.Tap(func(v any) {
		mu.Lock()
		got = append(got, v.(int))
		mu.Unlock()
	})
	unsubB := w.Tap(func(v any) {
		mu.Lock()
		got = append(got, v.(int)+100)
		mu.Unlock()
	})

	w.Notify(1)
	unsubA()
	w.Notify(2)
	unsubB()
	w.Notify(3)

	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 notifications, got %d (%v)", len(got), got)
	}
}

func TestWorkerRestartHoldoff(t *testing.T) {
	w := newTestWorker("restart-worker")
	defer w.Shutdown()

	w.Start(context.Background())

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		count := len(w.startTimes)
		var delta time.Duration
		if count >= 2 {
			delta = w.startTimes[1].Sub(w.startTimes[0])
		}
		w.mu.Unlock()

		if count >= 2 {
			if delta < 900*time.Millisecond {
				t.Fatalf("expected restart holdoff near 1s, got %v", delta)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("expected worker to restart at least once")
}
