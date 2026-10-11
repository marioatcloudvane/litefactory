package ask

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// ChatRunner is a provider-owned response-only adapter behind the doctor gate.
type ChatRunner interface {
	DoctorIsolation(context.Context) (string, error)
	RunChat(context.Context, agent.ChatRequest) (agent.ChatResult, error)
}

// MeterWriter grants only the existing usage ledger, no project writes.
type MeterWriter interface {
	StartChatMeterAttempt(context.Context, int64, string) (store.MeterAttempt, error)
	RecordMeterEvent(context.Context, string, string, agent.UsageEvent) error
	FinishMeterAttempt(context.Context, string, string, string, string, string, []string) error
}

// NativeModel reuses configured provider order and doctor isolation, choosing
// the balanced tier. Each call, including correction, has its own meter attempt.
type NativeModel struct {
	Config    *config.Config
	Paths     config.Paths
	Providers map[string]ChatRunner
	Meter     MeterWriter
}

// Answer never falls back to a workflow runner or an unverified provider.
func (m *NativeModel) Answer(ctx context.Context, repoID int64, prompt string) (_ []byte, err error) {
	if m.Config == nil || m.Meter == nil {
		return nil, ErrUnavailable
	}
	var provider, model string
	var runner ChatRunner
	for _, kind := range m.Config.AgentOrder() {
		r := m.Providers[string(kind)]
		configured, ok := m.Config.Tiers.ResolveModel(kind, config.TierBalanced)
		if r == nil || !ok || configured == "" {
			continue
		}
		proof, e := r.DoctorIsolation(ctx)
		if e == nil && proof != "" {
			provider, model, runner = string(kind), configured, r
			break
		}
	}
	if runner == nil {
		return nil, ErrUnavailable
	}
	if !filepath.IsAbs(m.Paths.DoctorScratchDir()) {
		return nil, errors.New("ask: absolute private scratch root is required")
	}
	if err = os.MkdirAll(m.Paths.DoctorScratchDir(), 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(m.Paths.DoctorScratchDir(), "chat-answer-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	attempt, err := m.Meter.StartChatMeterAttempt(ctx, repoID, provider)
	if err != nil {
		return nil, err
	}
	var failed atomic.Bool
	capture := &agent.UsageCapture{AttemptID: attempt.ID, Sink: func(ctx context.Context, event agent.UsageEvent) error {
		err := m.Meter.RecordMeterEvent(ctx, attempt.ID, provider, event)
		if err != nil {
			failed.Store(true)
		}
		return err
	}}
	result, runErr := runner.RunChat(ctx, agent.ChatRequest{Prompt: prompt, Model: model, WorkingDirectory: dir, Usage: capture})
	coverage := agent.UsageCoverage{Status: agent.CollectionUnknown, Diagnostics: []string{"chat_provider_usage_not_observed"}}
	if result.Usage != nil {
		coverage = result.Usage.Clone()
	}
	if failed.Load() {
		coverage.Status = agent.CollectionUnknown
		coverage.Diagnostics = append(coverage.Diagnostics, "chat_meter_sink_failed")
	}
	outcome := "success"
	if runErr != nil {
		outcome = "failure"
	}
	if ctx.Err() != nil {
		outcome = "cancelled"
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	meterErr := m.Meter.FinishMeterAttempt(cleanup, attempt.ID, outcome, string(coverage.Status), coverage.Version, coverage.SourceFormat, coverage.Diagnostics)
	return result.Answer, errors.Join(runErr, meterErr)
}
