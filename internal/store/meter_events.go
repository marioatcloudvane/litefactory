package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
)

// RecordMeterEvent persists provider observations in the shared ownership ledger.
// Both workflow and doctor attempts use the same deduplication and valuation.
func (db *DB) RecordMeterEvent(ctx context.Context, attemptID, codingAgent string, e agent.UsageEvent) error {
	if e.Invocation != nil {
		v := e.Invocation
		return db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: attemptID, ID: v.ID, ParentID: v.ParentID, SourceID: v.SourceID, SourceInvocationID: v.SourceInvocationID, Status: string(v.Coverage.Status), Diagnostics: v.Coverage.Diagnostics})
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
	source := MeterObservation{AttemptID: attemptID, SourceID: o.SourceID, EventKey: o.EventKey, InvocationID: o.InvocationID, Semantics: string(o.Mode) + "/" + string(o.Scope), CounterEpoch: o.CounterEpoch, CounterKey: meter.CounterIdentity(o), ModelKey: string(key), ModelProvenance: o.ModelProvenance, Digest: hex.EncodeToString(digest[:]), Status: string(o.Coverage.Status), ActualModel: o.ActualModel, Tokens: MeterTokens(o.Tokens), Diagnostics: o.Coverage.Diagnostics}
	return db.ApplyMeterObservation(ctx, source, func(state MeterLedgerState) (MeterMutation, error) {
		var prior *meter.CounterSnapshot
		if state.Checkpoint != nil {
			prior = &meter.CounterSnapshot{Identity: meter.CounterIdentity(o), Tokens: agent.TokenUsage(*state.Checkpoint)}
		}
		transition, err := meter.Reconcile(o, prior, state.AcceptedDigest)
		if err != nil {
			var invalid *meter.UsageError
			if errors.As(err, &invalid) {
				return MeterMutation{Rejection: invalid.Code}, nil
			}
			return MeterMutation{}, err
		}
		next := MeterMutation{Duplicate: transition.Duplicate}
		if transition.Next != nil {
			tokens := MeterTokens(transition.Next.Tokens)
			next.Checkpoint = &tokens
		}
		if transition.Additive {
			tokens := transition.Delta
			if state.Measurement != nil {
				tokens, err = meter.Add(agent.TokenUsage(*state.Measurement), tokens)
				if err != nil {
					return MeterMutation{Rejection: "invalid_counter_sum"}, nil //nolint:nilerr // Persist diagnostic evidence while rejecting the counter update.
				}
			}
			value := MeterTokens(tokens)
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
				return MeterMutation{}, err
			}
			frozen := MeterValuation{EstimatedUSD: valuation.EstimatedUSD, PricedSubtotalUSD: valuation.PricedSubtotalUSD, Status: valuation.Status}
			for _, d := range valuation.Dimensions {
				frozen.Dimensions = append(frozen.Dimensions, MeterValuationDimension{Dimension: d.Dimension, Quantity: d.Quantity, USDPerMillion: d.USDPerMillion, AmountUSD: d.AmountUSD, Status: d.Status})
			}
			next.Valuation = &frozen

		}
		return next, nil
	})
}
