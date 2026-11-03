package factory

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// PresentationUsage is an opaque receipt for one metered conversation invocation.
// Its identity and unknown-usage classification remain factory-owned.
type PresentationUsage struct {
	db             *store.DB
	id, diagnostic string
}

// BeginPresentation records one existing conversation provider invocation.
func (m *Messaging) BeginPresentation(ctx context.Context, turn, provider, kind string, ordinal int) (PresentationUsage, error) {
	var prefix, diagnostic string
	switch kind {
	case "provider":
		prefix, diagnostic = "conversation", "conversation_provider_usage_not_observed"
	case "reasoner":
		prefix, diagnostic = "conversation-reasoner", "conversation_reasoner_usage_not_observed"
	case "inbox-organizer":
		prefix, diagnostic = "conversation-inbox-organizer", "conversation_inbox_organizer_usage_not_observed"
	default:
		return PresentationUsage{}, errors.New("unknown conversation presentation kind")
	}
	attempt, err := m.db.StartPresentationMeterAttempt(ctx, 0, fmt.Sprintf("%s-%s-%d", prefix, turn, ordinal), "generation", provider)
	if err != nil {
		return PresentationUsage{}, err
	}
	return PresentationUsage{db: m.db, id: attempt.ID, diagnostic: diagnostic}, nil
}

// Complete records outcome without converting unobserved usage into zero usage.
func (u PresentationUsage) Complete(ctx context.Context, failed, cancelled bool) error {
	outcome := "success"
	if failed {
		outcome = "failure"
	}
	if cancelled {
		outcome = "cancelled"
	}
	return u.db.FinishMeterAttempt(ctx, u.id, outcome, "unknown", "", "", []string{u.diagnostic})
}

// RunUsage reads a completed run's usage for an owner notice. The snapshot never
// escapes the factory and the absence of recorded usage remains unknown.
func (m *Messaging) RunUsage(ctx context.Context, runID int64) (summary *report.MeterSummary, err error) {
	snapshot, err := m.db.NewMeterSnapshot(ctx, store.MeterFilter{RunID: runID}, store.MeterSnapshotOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	for {
		rows, e := snapshot.Next(ctx, 256)
		if e != nil {
			return nil, e
		}
		if len(rows) == 0 {
			break
		}
		if summary == nil {
			summary = &report.MeterSummary{}
		}
		for _, row := range rows {
			if e = summary.Add(report.MeterValues{Tokens: agent.TokenUsage(row.Tokens), EstimatedUSD: row.EstimatedUSD, PricedSubtotalUSD: row.PricedSubtotalUSD, PriceRevisionID: row.PriceRevisionID, CollectionStatus: row.CollectionStatus, PricingStatus: row.PricingStatus, Diagnostics: row.Diagnostics}); e != nil {
				return nil, e
			}
		}
	}
	return summary, nil
}
