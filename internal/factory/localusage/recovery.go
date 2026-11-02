// Package localusage owns local metering administration and historical recovery.
package localusage

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
	"math/big"
	"os"
)

// RecoveryRequest selects one finalized attempt in its registered project/task.
type RecoveryRequest struct {
	Project string
	Issue   int64
	Attempt string
	Apply   bool
}

// RecoveryResult preserves the historical recovery command's factual output.
type RecoveryResult struct {
	Attempt           string   `json:"attempt"`
	Applied           bool     `json:"applied"`
	AddedObservations int      `json:"added_observations"`
	Additional        string   `json:"additional_observed_usd"`
	Subtotal          string   `json:"resulting_observed_subtotal_usd"`
	Estimate          *string  `json:"estimated_usd"`
	Unresolved        []string `json:"unresolved"`
	Notice            string   `json:"notice"`
}

// Recovery keeps historical evidence and mutation capabilities private.
type Recovery struct{ databasePath string }

// NewRecovery composes local historical recovery against existing state.
func NewRecovery(databasePath string) *Recovery { return &Recovery{databasePath: databasePath} }

// Recover previews by default and applies only after source revalidation.
func (s *Recovery) Recover(ctx context.Context, request RecoveryRequest) (output RecoveryResult, err error) {
	db, err := store.OpenMeterReader(ctx, s.databasePath)
	if err != nil {
		return output, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	repoID, err := meterview.ResolveProject(ctx, db, request.Project)
	if err != nil {
		return output, err
	}
	if err := db.MatchMeterRecoveryAttempt(ctx, request.Attempt, repoID, request.Issue); err != nil {
		return output, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return output, err
	}
	prepared, err := prepareMeterRecovery(ctx, db, home, request.Attempt)
	if err != nil {
		return output, err
	}
	result, err := db.RecoverMeterUsage(ctx, prepared.batch, false, reconcileRecovery)
	if err != nil {
		return output, err
	}
	if request.Apply && len(prepared.unresolved) == 0 {
		if err := prepared.evidence.Revalidate(ctx); err != nil {
			return output, err
		}
		writer, err := store.Open(ctx, s.databasePath)
		if err != nil {
			return output, err
		}
		result, err = writer.RecoverMeterUsage(ctx, prepared.batch, true, reconcileRecovery)
		if err = errors.Join(err, writer.Close()); err != nil {
			return output, err
		}
	}
	additional := new(big.Rat)
	for _, valuation := range result.Valuations {
		if valuation.PricedSubtotalUSD != "" {
			amount, ok := new(big.Rat).SetString(valuation.PricedSubtotalUSD)
			if !ok {
				return output, errors.New("invalid stored valuation")
			}
			additional.Add(additional, amount)
		}
	}
	total := new(big.Rat).Set(additional)
	for _, amount := range prepared.batch.Scope.ExistingSubtotals {
		value, ok := new(big.Rat).SetString(amount)
		if !ok {
			return output, errors.New("invalid stored subtotal")
		}
		total.Add(total, value)
	}
	return RecoveryResult{Attempt: request.Attempt, Applied: result.Applied, AddedObservations: result.AddedObservations, Additional: additional.FloatString(12), Subtotal: total.FloatString(12), Unresolved: prepared.unresolved, Notice: "Observed lower bound at original recorded rates; total unknown. Preview is the default."}, nil
}

type preparedMeterRecovery struct {
	batch      store.MeterRecoveryBatch
	evidence   codex.HistoricalUsage
	unresolved []string
}

func prepareMeterRecovery(ctx context.Context, db *store.DB, home, attempt string) (p preparedMeterRecovery, err error) {
	p.batch.Scope, err = db.MeterRecoveryScope(ctx, attempt)
	if err != nil {
		return p, err
	}
	s := p.batch.Scope
	p.evidence, err = codex.CollectHistoricalUsage(ctx, home, attempt, s.RootID, codex.HistoricalWindow{From: s.From, To: s.To})
	if err != nil {
		return p, err
	}
	for _, code := range p.evidence.Diagnostics {
		switch code {
		case "capability_evidence_missing", "inherited_usage_excluded":
		default:
			p.unresolved = append(p.unresolved, code)
		}
	}
	if len(p.unresolved) > 0 {
		return p, nil
	}
	nodes := map[string]agent.UsageInvocation{}
	for _, v := range p.evidence.Invocations {
		nodes[v.ID] = v
	}
	required := map[string]bool{}
	for _, o := range p.evidence.Observations {
		v := o.InvocationID
		for depth := 0; v != s.RootID; depth++ {
			if depth >= 128 {
				return p, errors.New("historical ancestry cycle")
			}
			node, ok := nodes[v]
			if !ok {
				return p, fmt.Errorf("historical parent unavailable")
			}
			required[v] = true
			v = node.ParentID
		}
		observation, e := recoveryObservation(o)
		if e != nil {
			return p, e
		}
		p.batch.Observations = append(p.batch.Observations, observation)
	}
	for _, node := range p.evidence.Invocations {
		if !required[node.ID] {
			continue
		}
		parent := node.ParentID
		if parent == s.RootID {
			parent = s.RootInvocationID
		}
		p.batch.Invocations = append(p.batch.Invocations, store.MeterInvocation{AttemptID: attempt, ID: node.ID, ParentID: parent, SourceID: node.SourceID, SourceInvocationID: node.SourceInvocationID, Status: "partial", Diagnostics: []string{"historical_reconstruction", "capability_evidence_missing"}})
	}
	return p, nil
}
