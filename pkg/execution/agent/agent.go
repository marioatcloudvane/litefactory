// Package agent defines prepared execution values. Callers own policy, prompts,
// durable state and decisions about retrying a session.
package agent

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// Request is a prepared invocation. Provider-specific controls belong to the
// concrete runner's options, rather than a universal permission vocabulary.
type Request struct {
	Prompt           string
	WorkingDirectory string
	Session          string
	Timeout          time.Duration
	BeforeLaunch     func(context.Context) error
	OnSession        func(context.Context, string) error
	Events           io.Writer
	Usage            *UsageCapture
	Output           OutputPolicy
}

// Result retains provider evidence even when a started invocation fails.
type Result struct {
	Text       string
	Session    string
	Stderr     string
	Events     []json.RawMessage
	Usage      []json.RawMessage
	Coverage   *UsageCoverage
	ExitCode   int
	Started    bool
	Terminal   bool
	IsError    bool
	StartedAt  time.Time
	FinishedAt time.Time
}

// Failure identifies the boundary that failed and preserves the native cause.
type Failure struct {
	Phase   string
	Started bool
	Cause   error
}

func (e *Failure) Error() string { return e.Phase + ": " + e.Cause.Error() }
func (e *Failure) Unwrap() error { return e.Cause }
