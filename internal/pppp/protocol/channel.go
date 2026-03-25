package protocol

import (
	"context"
	"sort"
	"sync"
	"time"
)

const (
	DefaultMaxInFlight = 64
	DefaultMaxAgeWarn  = uint16(128)
	DefaultRetransmit  = 500 * time.Millisecond
)

type txItem struct {
	deadline time.Time
	index    CyclicU16
	payload  []byte
}

// Wire is an in-memory byte stream used by logical PPPP channels.
// Readers wait via a channel-based notification mechanism that supports
// context cancellation, avoiding the mutex contention of sync.Cond+timer.
type Wire struct {
	mu       sync.Mutex
	notifyCh chan struct{}
	buf      []byte
}

// NewWire allocates a new wire.
func NewWire() *Wire {
	return &Wire{
		notifyCh: make(chan struct{}, 1),
	}
}

// Write appends bytes and notifies waiting readers.
func (w *Wire) Write(data []byte) {
	w.mu.Lock()
	w.buf = append(w.buf, data...)
	w.mu.Unlock()
	// Non-blocking send to wake one waiter.
	select {
	case w.notifyCh <- struct{}{}:
	default:
	}
}

// PeekContext returns the next size bytes without consuming them.
// It blocks until enough data is available, the context is cancelled,
// or the timeout elapses.
func (w *Wire) PeekContext(ctx context.Context, size int, timeout time.Duration) ([]byte, error) {
	w.mu.Lock()
	if len(w.buf) >= size {
		out := make([]byte, size)
		copy(out, w.buf[:size])
		w.mu.Unlock()
		return out, nil
	}
	w.mu.Unlock()

	if timeout == 0 {
		return nil, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-w.notifyCh:
			w.mu.Lock()
			if len(w.buf) >= size {
				out := make([]byte, size)
				copy(out, w.buf[:size])
				w.mu.Unlock()
				return out, nil
			}
			w.mu.Unlock()
		}
	}
}

// ReadContext returns and consumes the next size bytes.
// It blocks until enough data is available, the context is cancelled,
// or the timeout elapses.
func (w *Wire) ReadContext(ctx context.Context, size int, timeout time.Duration) ([]byte, error) {
	w.mu.Lock()
	if len(w.buf) >= size {
		out := make([]byte, size)
		copy(out, w.buf[:size])
		w.buf = w.buf[size:]
		w.mu.Unlock()
		return out, nil
	}
	w.mu.Unlock()

	if timeout == 0 {
		return nil, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-w.notifyCh:
			w.mu.Lock()
			if len(w.buf) >= size {
				out := make([]byte, size)
				copy(out, w.buf[:size])
				w.buf = w.buf[size:]
				w.mu.Unlock()
				return out, nil
			}
			w.mu.Unlock()
		}
	}
}

// Peek returns the next size bytes without consuming them.
// It delegates to PeekContext with context.Background() for backward compatibility.
func (w *Wire) Peek(size int, timeout time.Duration) []byte {
	out, _ := w.PeekContext(context.Background(), size, timeout)
	return out
}

// Read returns and consumes the next size bytes.
// It delegates to ReadContext with context.Background() for backward compatibility.
func (w *Wire) Read(size int, timeout time.Duration) []byte {
	out, _ := w.ReadContext(context.Background(), size, timeout)
	return out
}

// Channel models one of the 8 logical PPPP channels.
type Channel struct {
	Index       uint8
	maxInFlight int
	maxAgeWarn  uint16
	timeout     time.Duration

	mu      sync.Mutex
	rxQueue map[CyclicU16][]byte
	txQueue []txItem
	backlog []txItem

	rxCtr CyclicU16
	txCtr CyclicU16
	txAck CyclicU16

	acks map[CyclicU16]struct{}

	RX *Wire
	TX *Wire

	eventCh chan struct{}
}

// NewChannel creates a channel with protocol defaults.
func NewChannel(index uint8) *Channel {
	return &Channel{
		Index:       index,
		maxInFlight: DefaultMaxInFlight,
		maxAgeWarn:  DefaultMaxAgeWarn,
		timeout:     DefaultRetransmit,
		rxQueue:     make(map[CyclicU16][]byte),
		acks:        make(map[CyclicU16]struct{}),
		RX:          NewWire(),
		TX:          NewWire(),
		eventCh:     make(chan struct{}, 1),
	}
}

func (c *Channel) signal() {
	select {
	case c.eventCh <- struct{}{}:
	default:
	}
}

// Wait blocks until channel state changes.
func (c *Channel) Wait(timeout time.Duration) bool {
	if timeout <= 0 {
		<-c.eventCh
		return true
	}
	select {
	case <-c.eventCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

// RXAck applies received ACKs to queued transmissions.
func (c *Channel) RXAck(acks []uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ackSet := make(map[CyclicU16]struct{}, len(acks))
	for _, ack := range acks {
		ackSet[CyclicU16(ack)] = struct{}{}
	}

	filtered := c.txQueue[:0]
	for _, tx := range c.txQueue {
		if _, ok := ackSet[tx.index]; ok {
			continue
		}
		filtered = append(filtered, tx)
	}
	c.txQueue = filtered

	for ack := range ackSet {
		if IsAfterOrEqual(c.txAck, ack) {
			c.acks[ack] = struct{}{}
		}
	}

	for {
		if _, ok := c.acks[c.txAck]; !ok {
			break
		}
		delete(c.acks, c.txAck)
		c.txAck = c.txAck.Add(1)
	}

	c.signal()
}

// RXDrw queues and reorders incoming DRW payloads.
func (c *Channel) RXDrw(index uint16, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	idx := CyclicU16(index)
	if c.rxCtr.Greater(idx) {
		if c.maxAgeWarn > 0 && uint16(c.rxCtr.Sub(uint16(idx))) > c.maxAgeWarn {
			return
		}
		return
	}

	if _, exists := c.rxQueue[idx]; !exists {
		copyData := make([]byte, len(data))
		copy(copyData, data)
		c.rxQueue[idx] = copyData
	}

	for {
		chunk, ok := c.rxQueue[c.rxCtr]
		if !ok {
			break
		}
		delete(c.rxQueue, c.rxCtr)
		c.rxCtr = c.rxCtr.Add(1)
		c.RX.Write(chunk)
	}

	c.signal()
}

// Poll returns DRW packets due for (re)transmission.
func (c *Channel) Poll(now time.Time) []Drw {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.signal()

	if len(c.backlog) > 0 && len(c.txQueue) < c.maxInFlight {
		for len(c.backlog) > 0 && len(c.txQueue) < c.maxInFlight {
			c.txQueue = append(c.txQueue, c.backlog[0])
			c.backlog = c.backlog[1:]
		}
		sort.Slice(c.txQueue, func(i, j int) bool {
			return c.txQueue[i].deadline.Before(c.txQueue[j].deadline)
		})
	}

	var out []Drw
	for len(c.txQueue) > 0 && !c.txQueue[0].deadline.After(now) {
		next := c.txQueue[0]
		c.txQueue = c.txQueue[1:]
		out = append(out, Drw{
			Chan:  c.Index,
			Index: uint16(next.index),
			Data:  append([]byte(nil), next.payload...),
		})
		next.deadline = next.deadline.Add(c.timeout)
		c.txQueue = append(c.txQueue, next)
	}

	return out
}

// Peek reads from reassembled inbound stream.
func (c *Channel) Peek(nbytes int, timeout time.Duration) []byte {
	return c.RX.Peek(nbytes, timeout)
}

// Read reads from reassembled inbound stream.
func (c *Channel) Read(nbytes int, timeout time.Duration) []byte {
	return c.RX.Read(nbytes, timeout)
}

// Write schedules outbound payload split into 1024-byte DRW chunks.
// For cancellable blocking writes, use WriteContext instead.
func (c *Channel) Write(payload []byte, block bool) (CyclicU16, CyclicU16, error) {
	return c.WriteContext(context.Background(), payload, block)
}

// WriteContext schedules outbound payload split into 1024-byte DRW chunks.
// When block is true, it waits for all chunks to be ACKed. The wait loop
// respects ctx cancellation so that hung uploads can be aborted.
func (c *Channel) WriteContext(ctx context.Context, payload []byte, block bool) (CyclicU16, CyclicU16, error) {
	c.mu.Lock()
	start := c.txCtr
	deadline := time.Now()
	remaining := payload
	for len(remaining) > 0 {
		n := 1024
		if len(remaining) < n {
			n = len(remaining)
		}
		chunk := make([]byte, n)
		copy(chunk, remaining[:n])
		c.backlog = append(c.backlog, txItem{deadline: deadline, index: c.txCtr, payload: chunk})
		c.txCtr = c.txCtr.Add(1)
		remaining = remaining[n:]
	}
	done := c.txCtr
	c.mu.Unlock()

	c.signal()

	if !block {
		return start, done, nil
	}

	for {
		c.mu.Lock()
		acked := IsAfterOrEqual(done, c.txAck)
		c.mu.Unlock()
		if acked {
			break
		}
		select {
		case <-ctx.Done():
			return start, done, ctx.Err()
		case <-c.eventCh:
			// state changed, re-check
		case <-time.After(250 * time.Millisecond):
			// poll timeout, re-check
		}
	}

	return start, done, nil
}

// ResetTx clears all pending TX state. Used after a DRW timeout to allow
// retransmission of data from the application layer. Mirrors Python's
// reset_tx() method from the PPPP upload hardening.
func (c *Channel) ResetTx() {
	c.mu.Lock()
	c.txQueue = c.txQueue[:0]
	c.backlog = c.backlog[:0]
	c.acks = make(map[CyclicU16]struct{})
	c.txAck = c.txCtr
	c.mu.Unlock()
	c.signal()
}
