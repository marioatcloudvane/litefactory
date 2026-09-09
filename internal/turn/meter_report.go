package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func (r *Reporter) meterRunSummary(ctx context.Context, runID int64) (summary *report.MeterSummary, err error) {
	if runID <= 0 {
		return nil, nil
	}
	snapshot, err := r.db.NewMeterSnapshot(ctx, store.MeterFilter{RunID: runID}, store.MeterSnapshotOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	for {
		rows, err := snapshot.Next(ctx, 256)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		if summary == nil {
			summary = &report.MeterSummary{}
		}
		for _, row := range rows {
			if err := summary.Add(report.MeterValues{Tokens: agent.TokenUsage(row.Tokens), EstimatedUSD: row.EstimatedUSD, PricedSubtotalUSD: row.PricedSubtotalUSD, PriceRevisionID: row.PriceRevisionID, CollectionStatus: row.CollectionStatus, PricingStatus: row.PricingStatus, Diagnostics: row.Diagnostics}); err != nil {
				return nil, err
			}
		}
	}
	return summary, nil
}
