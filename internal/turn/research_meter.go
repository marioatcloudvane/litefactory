package turn

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// RunResearchMetered invokes exactly one provider process using the existing
// meter collector, ledger and pinned pricing. A refused-resume cold fallback
// must call this function again so that it receives a distinct meter attempt.
// It never retries the agent or changes graph/result/publication state.
func RunResearchMetered(ctx context.Context, db *store.DB, kind agent.Kind, req agent.Request, runner agent.Runner) (*agent.RunResult, error) {
	if db == nil || runner == nil || req.Invocation != agent.InvocationResearchNode || req.Graph == nil || req.Usage != nil {
		return nil, errors.New("research meter requires a bound graph request without a second collector")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	g := req.Graph
	a, err := db.StartGraphMeterAttempt(ctx, g.RunID, string(kind), store.MeterGraphLink{InstanceID: g.InstanceID, AttemptID: g.AttemptID, Node: g.Node})
	if err != nil {
		return nil, err
	}
	var sinkFailed atomic.Bool
	req.Usage = &agent.UsageCapture{AttemptID: a.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		err := persistMeterEvent(c, db, a.ID, string(kind), event)
		if err != nil {
			sinkFailed.Store(true)
		}
		return err
	}}
	result, runErr := runner.Run(ctx, req)
	outcome, status, version, format := "failure", "unknown", "", ""
	codes := []string{"missing_evidence"}
	if result != nil {
		if runErr == nil && result.Succeeded() {
			outcome = "success"
		}
		if result.Usage != nil {
			status, version, format = string(result.Usage.Status), result.Usage.Version, result.Usage.SourceFormat
			codes = append([]string(nil), result.Usage.Diagnostics...)
		}
		if !result.Succeeded() {
			codes = append(codes, "process_"+result.Outcome.String())
		}
	}
	if sinkFailed.Load() {
		status = "partial"
		codes = append(codes, "persistence_failure")
		if result != nil {
			result = result.Clone()
			result.Usage = &agent.UsageCoverage{Status: agent.CollectionPartial, Version: version, SourceFormat: format, Diagnostics: codes}
		}
	}
	if ctx.Err() != nil {
		outcome = "cancelled"
	}
	refusal := result != nil && result.ProviderReportedZeroCostRefusal && outcome == "failure"
	if err := db.FinishMeterAttemptWithRefusal(context.WithoutCancel(ctx), a.ID, outcome, status, version, format, codes, refusal); err != nil {
		return result, errors.Join(runErr, fmt.Errorf("finalize research meter attempt %s: %w", a.ID, err))
	}
	return result, runErr
}
