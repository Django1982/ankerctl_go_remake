package protocol

import (
	"context"
	"testing"
	"time"
)

func TestWire_PeekContext(t *testing.T) {
	tests := []struct {
		name       string
		preload    []byte  // data written before PeekContext call
		asyncWrite []byte  // data written from goroutine after short delay
		peekSize   int
		cancelCtx  bool        // cancel context before call
		timeout    time.Duration
		wantData   string
		wantErr    error
		wantNil    bool // expect (nil, nil)
	}{
		{
			name:     "immediate data available",
			preload:  []byte("hello"),
			peekSize: 5,
			timeout:  time.Second,
			wantData: "hello",
		},
		{
			name:      "context cancelled before timeout",
			peekSize:  10,
			cancelCtx: true,
			timeout:   time.Second,
			wantErr:   context.Canceled,
		},
		{
			name:     "timeout with no data",
			peekSize: 10,
			timeout:  5 * time.Millisecond,
			wantNil:  true,
		},
		{
			name:     "zero timeout returns nil immediately",
			peekSize: 10,
			timeout:  0,
			wantNil:  true,
		},
		{
			name:       "data arrives before timeout",
			asyncWrite: []byte("async-data"),
			peekSize:   10,
			timeout:    time.Second,
			wantData:   "async-data",
		},
		{
			name:     "peek does not consume data",
			preload:  []byte("keep"),
			peekSize: 4,
			timeout:  time.Millisecond,
			wantData: "keep",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWire()
			if tc.preload != nil {
				w.Write(tc.preload)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelCtx {
				cancel()
			}

			if tc.asyncWrite != nil {
				go func() {
					time.Sleep(10 * time.Millisecond)
					w.Write(tc.asyncWrite)
				}()
			}

			got, err := w.PeekContext(ctx, tc.peekSize, tc.timeout)

			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q", got)
				}
				return
			}
			if string(got) != tc.wantData {
				t.Fatalf("expected %q, got %q", tc.wantData, string(got))
			}

			// Verify peek did not consume data.
			if tc.name == "peek does not consume data" {
				w.mu.Lock()
				bufLen := len(w.buf)
				w.mu.Unlock()
				if bufLen != len(tc.preload) {
					t.Fatalf("peek consumed data: buf len %d, want %d", bufLen, len(tc.preload))
				}
			}
		})
	}
}

func TestWire_ReadContext(t *testing.T) {
	tests := []struct {
		name       string
		preload    []byte
		asyncWrite []byte
		readSize   int
		cancelCtx  bool
		timeout    time.Duration
		wantData   string
		wantErr    error
		wantNil    bool
	}{
		{
			name:     "immediate data available",
			preload:  []byte("hello"),
			readSize: 5,
			timeout:  time.Second,
			wantData: "hello",
		},
		{
			name:      "context cancelled before timeout",
			readSize:  10,
			cancelCtx: true,
			timeout:   time.Second,
			wantErr:   context.Canceled,
		},
		{
			name:     "timeout with no data",
			readSize: 10,
			timeout:  5 * time.Millisecond,
			wantNil:  true,
		},
		{
			name:     "zero timeout returns nil immediately",
			readSize: 10,
			timeout:  0,
			wantNil:  true,
		},
		{
			name:       "data arrives before timeout",
			asyncWrite: []byte("asyncmsg!"),
			readSize:   9,
			timeout:    time.Second,
			wantData:   "asyncmsg!",
		},
		{
			name:     "read consumes data",
			preload:  []byte("consume"),
			readSize: 7,
			timeout:  time.Millisecond,
			wantData: "consume",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWire()
			if tc.preload != nil {
				w.Write(tc.preload)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelCtx {
				cancel()
			}

			if tc.asyncWrite != nil {
				go func() {
					time.Sleep(10 * time.Millisecond)
					w.Write(tc.asyncWrite)
				}()
			}

			got, err := w.ReadContext(ctx, tc.readSize, tc.timeout)

			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q", got)
				}
				return
			}
			if string(got) != tc.wantData {
				t.Fatalf("expected %q, got %q", tc.wantData, string(got))
			}

			// Verify read consumed the data.
			if tc.name == "read consumes data" {
				w.mu.Lock()
				bufLen := len(w.buf)
				w.mu.Unlock()
				if bufLen != 0 {
					t.Fatalf("read did not consume data: buf len %d, want 0", bufLen)
				}
			}
		})
	}
}

// TestWire_ReadContext_DeadlineExceeded tests context.WithTimeout integration.
func TestWire_ReadContext_DeadlineExceeded(t *testing.T) {
	w := NewWire()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	got, err := w.ReadContext(ctx, 100, time.Second)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got err=%v data=%q", err, got)
	}
}

// TestWire_PeekContext_DeadlineExceeded tests context.WithTimeout integration.
func TestWire_PeekContext_DeadlineExceeded(t *testing.T) {
	w := NewWire()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	got, err := w.PeekContext(ctx, 100, time.Second)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got err=%v data=%q", err, got)
	}
}

// TestWire_BackwardCompat verifies Peek/Read still work via delegation.
func TestWire_BackwardCompat(t *testing.T) {
	w := NewWire()
	w.Write([]byte("abcdef"))

	got := w.Peek(3, time.Millisecond)
	if string(got) != "abc" {
		t.Fatalf("Peek: expected 'abc', got %q", got)
	}

	got = w.Read(3, time.Millisecond)
	if string(got) != "abc" {
		t.Fatalf("Read: expected 'abc', got %q", got)
	}

	// After Read consumed 3, next Read should get "def".
	got = w.Read(3, time.Millisecond)
	if string(got) != "def" {
		t.Fatalf("Read: expected 'def', got %q", got)
	}

	// Buffer empty, zero timeout returns nil.
	got = w.Read(1, 0)
	if got != nil {
		t.Fatalf("Read: expected nil on empty buffer, got %q", got)
	}
}
