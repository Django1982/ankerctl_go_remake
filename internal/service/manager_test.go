package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockService struct {
	name string

	mu       sync.Mutex
	state    RunState
	starts   int
	stops    int
	events   []string
	videoOn  bool
	handlers []func(any)
	orderMu  *sync.Mutex
	order    *[]string
}

func newMockService(name string) *mockService {
	return &mockService{name: name, state: StateStopped}
}

func (s *mockService) WorkerInit() {}
func (s *mockService) WorkerStart() error {
	return nil
}
func (s *mockService) WorkerRun(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
func (s *mockService) WorkerStop() {}
func (s *mockService) Name() string {
	return s.name
}
func (s *mockService) State() RunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}
func (s *mockService) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	s.state = StateRunning
	s.events = append(s.events, "start:"+s.name)
	if s.order != nil && s.orderMu != nil {
		s.orderMu.Lock()
		*s.order = append(*s.order, "start:"+s.name)
		s.orderMu.Unlock()
	}
}
func (s *mockService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	s.state = StateStopped
	s.events = append(s.events, "stop:"+s.name)
	if s.order != nil && s.orderMu != nil {
		s.orderMu.Lock()
		*s.order = append(*s.order, "stop:"+s.name)
		s.orderMu.Unlock()
	}
}
func (s *mockService) Restart()  {}
func (s *mockService) Shutdown() {}
func (s *mockService) Notify(data any) {
	s.mu.Lock()
	handlers := append([]func(any){}, s.handlers...)
	s.mu.Unlock()
	for _, h := range handlers {
		h(data)
	}
}
func (s *mockService) Tap(handler func(any)) func() {
	s.mu.Lock()
	s.handlers = append(s.handlers, handler)
	idx := len(s.handlers) - 1
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if idx >= 0 && idx < len(s.handlers) {
			s.handlers[idx] = nil
		}
	}
}
func (s *mockService) VideoEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.videoOn
}

func TestManagerBorrowReturnRefCount(t *testing.T) {
	mgr := NewServiceManager()
	svc := newMockService("mqttqueue")
	mgr.Register(svc)

	_, err := mgr.Borrow("mqttqueue")
	if err != nil {
		t.Fatalf("borrow failed: %v", err)
	}
	_, err = mgr.Borrow("mqttqueue")
	if err != nil {
		t.Fatalf("second borrow failed: %v", err)
	}

	mgr.Return("mqttqueue")
	mgr.Return("mqttqueue")

	if svc.starts != 1 {
		t.Fatalf("expected 1 start, got %d", svc.starts)
	}
	if svc.stops != 1 {
		t.Fatalf("expected 1 stop, got %d", svc.stops)
	}
}

func TestManagerBorrowUnknown(t *testing.T) {
	mgr := NewServiceManager()
	if _, err := mgr.Borrow("missing"); err == nil {
		t.Fatal("expected unknown service error")
	}
}

func TestManagerVideoQueueException(t *testing.T) {
	mgr := NewServiceManager()
	video := newMockService("videoqueue")
	video.videoOn = true
	mgr.Register(video)

	if _, err := mgr.Borrow("videoqueue"); err != nil {
		t.Fatalf("borrow failed: %v", err)
	}
	mgr.Return("videoqueue")

	if video.stops != 0 {
		t.Fatalf("expected videoqueue to stay running, got %d stops", video.stops)
	}
}

func TestManagerShutdownReverseOrder(t *testing.T) {
	mgr := NewServiceManager()
	order := make([]string, 0, 8)
	var orderMu sync.Mutex
	a := newMockService("a")
	b := newMockService("b")
	c := newMockService("c")
	a.order, a.orderMu = &order, &orderMu
	b.order, b.orderMu = &order, &orderMu
	c.order, c.orderMu = &order, &orderMu

	mgr.Register(a)
	mgr.Register(b)
	mgr.Register(c)

	if _, err := mgr.Borrow("a"); err != nil {
		t.Fatalf("borrow a: %v", err)
	}
	if _, err := mgr.Borrow("b"); err != nil {
		t.Fatalf("borrow b: %v", err)
	}
	if _, err := mgr.Borrow("c"); err != nil {
		t.Fatalf("borrow c: %v", err)
	}

	mgr.Shutdown()

	wantSuffix := []string{"stop:c", "stop:b", "stop:a"}
	orderMu.Lock()
	got := append([]string(nil), order...)
	orderMu.Unlock()

	if len(got) < len(wantSuffix) {
		t.Fatalf("event sequence too short: %v", got)
	}
	for i := 0; i < len(wantSuffix); i++ {
		if got[len(got)-len(wantSuffix)+i] != wantSuffix[i] {
			t.Fatalf("expected shutdown suffix %v, got %v", wantSuffix, got)
		}
	}
}

// TestManagerShutdownBlocks verifies that ServiceManager.Shutdown() blocks until
// all worker goroutines have fully exited (no goroutine leaks).
func TestManagerShutdownBlocks(t *testing.T) {
	mgr := NewServiceManager()

	// Use real BaseWorker-backed workers to exercise goroutine lifecycle.
	w1 := newTestWorker("svc1")
	w1.restartOn = -1 // never restart
	w2 := newTestWorker("svc2")
	w2.restartOn = -1

	mgr.Register(w1)
	mgr.Register(w2)

	if _, err := mgr.Borrow("svc1"); err != nil {
		t.Fatalf("borrow svc1: %v", err)
	}
	if _, err := mgr.Borrow("svc2"); err != nil {
		t.Fatalf("borrow svc2: %v", err)
	}

	// Wait until both workers reach Running state.
	waitForState(t, w1, StateRunning, 2*time.Second)
	waitForState(t, w2, StateRunning, 2*time.Second)

	// Shutdown must block until goroutines exit.
	done := make(chan struct{})
	go func() {
		mgr.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// good — Shutdown returned
	case <-time.After(5 * time.Second):
		t.Fatal("ServiceManager.Shutdown() did not complete within 5 seconds (goroutine leak)")
	}

	// After Shutdown, both workers must be in Stopped state.
	if s := w1.State(); s != StateStopped {
		t.Errorf("svc1 not stopped after Shutdown: %v", s)
	}
	if s := w2.State(); s != StateStopped {
		t.Errorf("svc2 not stopped after Shutdown: %v", s)
	}
}

func lastEvent(s *mockService) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return ""
	}
	return s.events[len(s.events)-1]
}
