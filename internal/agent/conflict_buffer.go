package agent

import (
	"bytes"
	"sync"
)

// ConflictOutputBuffer bounds subprocess output before allocation. It intentionally
// does not embed bytes.Buffer: io.Copy must not bypass Write through ReadFrom.
type ConflictOutputBuffer struct {
	buffer   bytes.Buffer
	budget   *conflictOutputBudget
	Exceeded bool
}
type conflictOutputBudget struct {
	mu   sync.Mutex
	used int
}

// ConflictOutputBuffers share the one-MiB budget across stdout and stderr.
func ConflictOutputBuffers() (*ConflictOutputBuffer, *ConflictOutputBuffer) {
	budget := &conflictOutputBudget{}
	return &ConflictOutputBuffer{budget: budget}, &ConflictOutputBuffer{budget: budget}
}
func (b *ConflictOutputBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := MaxConflictResponseBytes - b.buffer.Len()
	if b.budget != nil {
		b.budget.mu.Lock()
		defer b.budget.mu.Unlock()
		remaining = MaxConflictResponseBytes - b.budget.used
	}
	if len(p) > remaining {
		b.Exceeded = true
		p = p[:remaining]
	}
	if b.budget != nil {
		b.budget.used += len(p)
	}
	_, err := b.buffer.Write(p)
	return n, err
}

// Bytes returns the bounded captured stream after the subprocess finishes.
func (b *ConflictOutputBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *ConflictOutputBuffer) String() string { return b.buffer.String() }

// Len reports the number of captured bytes.
func (b *ConflictOutputBuffer) Len() int { return b.buffer.Len() }
