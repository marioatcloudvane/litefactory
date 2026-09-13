package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
	"github.com/cloudvane/litefactory/internal/store"
)

// runMetered wraps each actual runner call, so resume refusal and cold fallback
// have distinct durable attempts even when their raw log path is shared.
func runMetered(ctx context.Context, prep *preparation, logger *slog.Logger) (*agent.RunResult, error) {
	if prep.meterDB == nil {
		return runSubmissionBound(ctx, prep, prep.request)
	}
	attempt, err := prep.meterDB.StartMeterAttempt(ctx, prep.run.ID, prep.segment.Agent)
	if err != nil {
		return nil, err
	}
	var sinkFailed atomic.Bool
	req := prep.request
	req.Usage = &agent.UsageCapture{AttemptID: attempt.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		err := persistMeterEvent(c, prep.meterDB, attempt.ID, prep.segment.Agent, event)
		if err != nil {
			sinkFailed.Store(true)
		}
		return err
	}}
	result, runErr := runSubmissionBound(ctx, prep, req)
	outcome, status, version, format := "failure", "unknown", "", ""
	codes := []string{"missing_evidence"}
	if result != nil {
		if runErr == nil && result.Succeeded() {
			outcome = "success"
		}
		if result.Usage != nil {
			status = string(result.Usage.Status)
			version = result.Usage.Version
			format = result.Usage.SourceFormat
			codes = result.Usage.Diagnostics
		}
	}
	if sinkFailed.Load() {
		status = "partial"
		codes = append(append([]string(nil), codes...), "persistence_failure")
		if result != nil {
			result = result.Clone()
			result.Usage = &agent.UsageCoverage{Status: agent.CollectionPartial, Version: version, SourceFormat: format, Diagnostics: codes}
		}
	}
	if ctx.Err() != nil {
		outcome = "cancelled"
	}
	zeroCostRefusal := result != nil && result.ProviderReportedZeroCostRefusal && outcome == "failure"
	finish := context.WithoutCancel(ctx)
	if err := prep.meterDB.FinishMeterAttemptWithRefusal(finish, attempt.ID, outcome, status, version, format, codes, zeroCostRefusal); err != nil {
		prep.stoppedRepairs = append(prep.stoppedRepairs, func(c context.Context) error {
			return prep.meterDB.FinishMeterAttemptWithRefusal(c, attempt.ID, outcome, status, version, format, codes, zeroCostRefusal)
		})
		logger.Error("meter attempt finalization failed", slog.String("attempt_id", attempt.ID), slog.Any("error", err))
		if result != nil {
			result = result.Clone()
			result.Usage = &agent.UsageCoverage{Status: agent.CollectionPartial, Diagnostics: []string{"persistence_failure"}}
		}
	}
	return result, runErr
}
func persistMeterEvent(ctx context.Context, db *store.DB, attemptID, codingAgent string, e agent.UsageEvent) error {
	if e.Invocation != nil {
		v := e.Invocation
		return db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attemptID, ID: v.ID, ParentID: v.ParentID, SourceID: v.SourceID, SourceInvocationID: v.SourceInvocationID, Status: string(v.Coverage.Status), Diagnostics: v.Coverage.Diagnostics})
	}
	if e.Coverage != nil {
		return db.UpdateMeterCoverage(ctx, attemptID, string(e.Coverage.Status), e.Coverage.Version, e.Coverage.SourceFormat, e.Coverage.Diagnostics)
	}
	if e.Observation == nil {
		return errors.New("missing meter event")
	}
	o := e.Observation.Clone()
	encoded, err := json.Marshal(o)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	key, _ := json.Marshal([]*string{o.ActualModel})
	source := store.MeterObservation{AttemptID: attemptID, SourceID: o.SourceID, EventKey: o.EventKey, InvocationID: o.InvocationID, Semantics: string(o.Mode) + "/" + string(o.Scope), CounterEpoch: o.CounterEpoch, CounterKey: meter.CounterIdentity(o), ModelKey: string(key), ModelProvenance: o.ModelProvenance, Digest: hex.EncodeToString(digest[:]), Status: string(o.Coverage.Status), ActualModel: o.ActualModel, Tokens: store.MeterTokens(o.Tokens), Diagnostics: o.Coverage.Diagnostics}
	return db.ApplyMeterObservation(ctx, source, func(state store.MeterLedgerState) (store.MeterMutation, error) {
		var prior *meter.CounterSnapshot
		if state.Checkpoint != nil {
			prior = &meter.CounterSnapshot{Identity: meter.CounterIdentity(o), Tokens: agent.TokenUsage(*state.Checkpoint)}
		}
		transition, err := meter.Reconcile(o, prior, state.AcceptedDigest)
		if err != nil {
			var invalid *meter.UsageError
			if errors.As(err, &invalid) {
				return store.MeterMutation{Rejection: invalid.Code}, nil
			}
			return store.MeterMutation{}, err
		}
		next := store.MeterMutation{Duplicate: transition.Duplicate}
		if transition.Next != nil {
			tokens := store.MeterTokens(transition.Next.Tokens)
			next.Checkpoint = &tokens
		}
		if transition.Additive {
			tokens := transition.Delta
			if state.Measurement != nil {
				tokens, err = meter.Add(agent.TokenUsage(*state.Measurement), tokens)
				if err != nil {
					return store.MeterMutation{Rejection: "invalid_counter_sum"}, nil //nolint:nilerr // Persist diagnostic evidence while rejecting the counter update.
				}
			}
			value := store.MeterTokens(tokens)
			next.Measurement = &value
			rates := []meter.PriceRate{}
			if o.ActualModel != nil {
				for _, rate := range state.Catalog.Rates {
					if rate.CodingAgent == codingAgent && rate.Model == *o.ActualModel {
						rates = append(rates, meter.PriceRate{Dimension: rate.Dimension, USDPerMillion: rate.USDPerMillion})
					}
				}
			}
			valuation, err := meter.Value(tokens, rates)
			if err != nil {
				return store.MeterMutation{}, err
			}
			frozen := store.MeterValuation{EstimatedUSD: valuation.EstimatedUSD, PricedSubtotalUSD: valuation.PricedSubtotalUSD, Status: valuation.Status}
			for _, d := range valuation.Dimensions {
				frozen.Dimensions = append(frozen.Dimensions, store.MeterValuationDimension{Dimension: d.Dimension, Quantity: d.Quantity, USDPerMillion: d.USDPerMillion, AmountUSD: d.AmountUSD, Status: d.Status})
			}
			next.Valuation = &frozen

		}
		return next, nil
	})
}
