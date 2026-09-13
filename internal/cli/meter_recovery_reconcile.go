package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
	"github.com/cloudvane/litefactory/internal/store"
)

func recoveryObservation(o agent.UsageObservation) (store.MeterObservation, error) {
	o.Coverage = agent.UsageCoverage{Status: o.Coverage.Status, Diagnostics: []string{"historical_reconstruction", "capability_evidence_missing"}}

	encoded, err := json.Marshal(o)
	if err != nil {
		return store.MeterObservation{}, err
	}
	digest := sha256.Sum256(encoded)
	key, _ := json.Marshal([]*string{o.ActualModel})
	source := store.MeterObservation{AttemptID: o.AttemptID, SourceID: o.SourceID, EventKey: o.EventKey, InvocationID: o.InvocationID, Semantics: string(o.Mode) + "/" + string(o.Scope), CounterEpoch: o.CounterEpoch, CounterKey: meter.CounterIdentity(o), ModelKey: string(key), ModelProvenance: o.ModelProvenance, Digest: hex.EncodeToString(digest[:]), Status: string(o.Coverage.Status), ActualModel: o.ActualModel, Tokens: store.MeterTokens(o.Tokens), Diagnostics: o.Coverage.Diagnostics}
	return source, nil
}
func reconcileRecovery(source store.MeterObservation, state store.MeterLedgerState) (store.MeterMutation, error) {
	o := agent.UsageObservation{AttemptID: source.AttemptID, SourceID: source.SourceID, EventKey: source.EventKey, InvocationID: source.InvocationID, ActualModel: source.ActualModel, ModelProvenance: source.ModelProvenance, Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage(source.Tokens), Coverage: agent.UsageCoverage{Status: agent.CollectionStatus(source.Status), Diagnostics: source.Diagnostics}}

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
				if rate.CodingAgent == "codex" && rate.Model == *o.ActualModel {
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
}
