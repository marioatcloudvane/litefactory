package agent

import "sync"

// StderrTailBytes bounds retained subprocess diagnostics before allocation.
const StderrTailBytes = 64 << 10

// TailBuffer retains the final bytes of a stream without blocking its producer.
// Reads and writes are safe concurrently; callers should join the writer before
// reading when they require a complete tail.
type TailBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

// NewTailBuffer constructs a tail with a byte limit. A nonpositive limit retains
// no bytes and still drains the producer.
func NewTailBuffer(limit int) *TailBuffer {
	return &TailBuffer{limit: max(0, limit)}
}

// Write implements io.Writer, accepting all bytes even when earlier bytes drop.
func (w *TailBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if n >= w.limit {
		w.truncated = w.truncated || len(w.buf)+n > w.limit
		w.buf = append(w.buf[:0], p[n-w.limit:]...)
		return n, nil
	}
	if excess := len(w.buf) + n - w.limit; excess > 0 {
		copy(w.buf, w.buf[excess:])
		w.buf = w.buf[:len(w.buf)-excess]
		w.truncated = true
	}
	w.buf = append(w.buf, p...)
	return n, nil
}

// String marks a truncated stream so its tail is never mistaken for full output.
func (w *TailBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.truncated {
		return "[earlier stderr dropped]\n" + string(w.buf)
	}
	return string(w.buf)
}
