package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func newMeterRecoverCommand() *Command {
	return &Command{Name: "meter recover", Usage: "meter recover <repository> <task-id> --attempt ID [--apply]", Short: "Preview missing historical Codex descendant usage; apply explicitly", Args: ArgSpec{Names: []string{"repository", "task-id"}, Min: 2}, New: func(fs *flag.FlagSet) Handler {
		var attempt string
		var apply bool
		fs.StringVar(&attempt, "attempt", "", "existing finalized attempt ID")
		fs.BoolVar(&apply, "apply", false, "atomically append evidenced missing descendants")
		return func(ctx context.Context, env *Env, args []string) (err error) {
			filter, project, err := meterFilter(args, meterOptions{}, false)
			if err != nil {
				return err
			}
			if len(args) != 2 || attempt == "" || len(attempt) > 256 || strings.ContainsAny(attempt, "\x00\r\n\t") {
				return Usagef(nil, "recovery requires repository, positive task-id and --attempt")
			}
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			db, err := store.OpenMeterReader(ctx, paths.DBPath())
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, db.Close()) }()
			filter.RepoID, err = resolveMeterProject(ctx, db, project)
			if err != nil {
				return err
			}
			if err = db.MatchMeterRecoveryAttempt(ctx, attempt, filter.RepoID, filter.Issue); err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			prepared, err := prepareMeterRecovery(ctx, db, home, attempt)
			if err != nil {
				return err
			}
			result, err := db.RecoverMeterUsage(ctx, prepared.batch, false, reconcileRecovery)
			if err != nil {
				return err
			}
			if apply && len(prepared.unresolved) == 0 {
				if err = prepared.evidence.Revalidate(ctx); err != nil {
					return err
				}
				writer, e := store.Open(ctx, paths.DBPath())
				if e != nil {
					return e
				}
				result, e = writer.RecoverMeterUsage(ctx, prepared.batch, true, reconcileRecovery)
				err = errors.Join(e, writer.Close())
				if err != nil {
					return err
				}
			}
			additional := new(big.Rat)
			for _, v := range result.Valuations {
				if v.PricedSubtotalUSD != "" {
					n, ok := new(big.Rat).SetString(v.PricedSubtotalUSD)
					if !ok {
						return errors.New("invalid stored valuation")
					}
					additional.Add(additional, n)
				}
			}
			total := new(big.Rat).Set(additional)
			for _, amount := range prepared.batch.Scope.ExistingSubtotals {
				n, ok := new(big.Rat).SetString(amount)
				if !ok {
					return errors.New("invalid stored subtotal")
				}
				total.Add(total, n)
			}
			output := struct {
				Attempt           string   `json:"attempt"`
				Applied           bool     `json:"applied"`
				AddedObservations int      `json:"added_observations"`
				Additional        string   `json:"additional_observed_usd"`
				Subtotal          string   `json:"resulting_observed_subtotal_usd"`
				Estimate          *string  `json:"estimated_usd"`
				Unresolved        []string `json:"unresolved"`
				Notice            string   `json:"notice"`
			}{attempt, result.Applied, result.AddedObservations, additional.FloatString(12), total.FloatString(12), nil, prepared.unresolved, "Observed lower bound at original recorded rates; total unknown. Preview is the default."}
			return json.NewEncoder(env.Stdout).Encode(output)
		}
	}}
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
