package doctor

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"

	"github.com/cloudvane/litefactory/internal/store"
)

// Invocation is mode-free and contains only prepared context and private scratch.
// Provider credentials are never fields of this value; the prompt contains paths
// for live reads rather than pre-read source contents.
type Invocation struct {
	ID               string
	Provider         string
	ConfiguredModel  string
	Prompt           string
	WorkingDirectory string
	MeterAttempt     store.MeterAttempt
	Usage            *agent.UsageCapture
}

// InvocationOutcome identifies a durable communication result, not terminal text.
type InvocationOutcome struct {
	RequestID       string
	ObservedModel   string
	ProviderVersion string
	Usage           *agent.UsageCoverage
}

// Runner is a factory-installed adapter, never an agent-selected implementation.
// IsolationEvidence must name an authentic versioned observation before launch.
// Production installs no adapter until its exact tool ceiling is verified.
type Runner interface {
	IsolationEvidence(context.Context, string) (string, error)
	Run(context.Context, Invocation, *ResultService) (InvocationOutcome, error)
}
