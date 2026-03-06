package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// RingBuffer is a thread-safe, fixed-capacity circular buffer of log lines.
// It is designed to be embedded as an slog.Handler so that all structured
// log records are captured in memory and can be served via the debug log viewer.
type RingBuffer struct {
	mu   sync.RWMutex
	buf  []string
	cap  int
	head int // index of the oldest entry (write head wraps here)
	size int // number of valid entries currently stored
}

// NewRingBuffer creates a RingBuffer with the given capacity.
// Capacity must be > 0; if 0 or negative, it is set to 1.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer{
		buf: make([]string, capacity),
		cap: capacity,
	}
}

// Append adds a single line to the buffer, evicting the oldest entry when full.
func (r *RingBuffer) Append(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf[r.head] = line
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// Lines returns all stored lines in chronological order (oldest first).
// The returned slice is a copy; it is safe to modify.
func (r *RingBuffer) Lines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.size == 0 {
		return nil
	}
	out := make([]string, r.size)
	if r.size < r.cap {
		// Buffer not yet full: entries start at index 0.
		copy(out, r.buf[:r.size])
	} else {
		// Buffer is full: oldest entry is at head.
		n := copy(out, r.buf[r.head:])
		copy(out[n:], r.buf[:r.head])
	}
	return out
}

// Tail returns the last n lines in chronological order.
// If n >= the number of stored lines, all lines are returned.
func (r *RingBuffer) Tail(n int) []string {
	all := r.Lines()
	if n <= 0 || len(all) <= n {
		return all
	}
	return all[len(all)-n:]
}

// String returns all stored lines joined by newlines.
func (r *RingBuffer) String() string {
	return strings.Join(r.Lines(), "\n")
}

// --- slog.Handler implementation ---

// RingBufferHandler wraps an inner slog.Handler and also writes formatted
// records to a RingBuffer.  It satisfies the slog.Handler interface.
type RingBufferHandler struct {
	inner slog.Handler
	ring  *RingBuffer
}

// NewRingBufferHandler wraps inner and captures all records to ring.
// If inner is nil, records are only captured (no further output).
func NewRingBufferHandler(inner slog.Handler, ring *RingBuffer) *RingBufferHandler {
	return &RingBufferHandler{inner: inner, ring: ring}
}

// Enabled reports whether both this handler and the inner handler are enabled
// for the given level.
func (h *RingBufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.inner != nil {
		return h.inner.Enabled(ctx, level)
	}
	return true
}

// Handle captures the formatted record to the ring buffer then delegates to inner.
func (h *RingBufferHandler) Handle(ctx context.Context, r slog.Record) error {
	// Format a compact text line: "TIME LEVEL MSG key=val ..."
	var sb strings.Builder
	sb.WriteString(r.Time.Format("2006-01-02T15:04:05.000"))
	sb.WriteByte(' ')
	sb.WriteString(r.Level.String())
	sb.WriteByte(' ')
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(a.Value.String())
		return true
	})
	h.ring.Append(sb.String())

	if h.inner != nil {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

// WithAttrs returns a new handler whose records include the given attrs.
func (h *RingBufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var inner slog.Handler
	if h.inner != nil {
		inner = h.inner.WithAttrs(attrs)
	}
	return &RingBufferHandler{inner: inner, ring: h.ring}
}

// WithGroup returns a new handler that scopes future attrs under name.
func (h *RingBufferHandler) WithGroup(name string) slog.Handler {
	var inner slog.Handler
	if h.inner != nil {
		inner = h.inner.WithGroup(name)
	}
	return &RingBufferHandler{inner: inner, ring: h.ring}
}
