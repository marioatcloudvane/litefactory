package factory

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResponseMeter owns response invocation admission and usage recording.
type ResponseMeter struct{ db *store.DB }

// NewResponseMeter binds response-only recording to the existing usage ledger.
func NewResponseMeter(db *store.DB) *ResponseMeter { return &ResponseMeter{db: db} }

// StartChatMeterAttempt opens one recorded chat invocation.
func (m *ResponseMeter) StartChatMeterAttempt(ctx context.Context, repoID int64, provider string) (contracts.ResponseMeterAttempt, error) {
	attempt, err := m.db.StartChatMeterAttempt(ctx, repoID, provider)
	return contracts.ResponseMeterAttempt{ID: attempt.ID}, err
}

// StartAuthoringMeterAttempt retains the existing authoring admission fence.
func (m *ResponseMeter) StartAuthoringMeterAttempt(ctx context.Context, session, provider, model, inputDigest, attemptID, token string) (contracts.ResponseMeterAttempt, error) {
	attempt, err := m.db.StartAuthoringMeterAttempt(ctx, session, provider, model, inputDigest, attemptID, token)
	return contracts.ResponseMeterAttempt{ID: attempt.ID}, err
}

// RecordMeterEvent retains the observed provider measurement.
func (m *ResponseMeter) RecordMeterEvent(ctx context.Context, attempt, provider string, event agent.UsageEvent) error {
	return m.db.RecordMeterEvent(ctx, attempt, provider, event)
}

// FinishMeterAttempt closes the invocation without inventing missing usage.
func (m *ResponseMeter) FinishMeterAttempt(ctx context.Context, attempt, outcome, status, version, format string, diagnostics []string) error {
	return m.db.FinishMeterAttempt(ctx, attempt, outcome, status, version, format, diagnostics)
}
