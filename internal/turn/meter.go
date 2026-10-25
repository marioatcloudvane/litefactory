package turn

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// runMetered wraps each actual runner call, so resume refusal and cold fallback
// have distinct durable attempts even when their raw log path is shared.
func runMetered(ctx context.Context, prep *preparation, logger *slog.Logger) (*agent.RunResult, error) {
	return runMeteredWith(ctx, prep, logger, prep.request, func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		return runSubmissionBound(ctx, prep, req)
	})
}

// runMeteredWith meters one actual invocation of a request the caller has
// already bound to its result route.
func runMeteredWith(ctx context.Context, prep *preparation, logger *slog.Logger, req agent.Request, invoke func(context.Context, agent.Request) (*agent.RunResult, error)) (*agent.RunResult, error) {
	if prep.meterDB == nil {
		return invoke(ctx, req)
	}
	attempt, err := prep.meterDB.StartMeterAttempt(ctx, prep.run.ID, prep.segment.Agent)
	if err != nil {
		return nil, err
	}
	var sinkFailed atomic.Bool
	req.Usage = &agent.UsageCapture{AttemptID: attempt.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		err := persistMeterEvent(c, prep.meterDB, attempt.ID, prep.segment.Agent, event)
		if err != nil {
			sinkFailed.Store(true)
		}
		return err
	}}
	result, runErr := invoke(ctx, req)
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
	finish, cancel := agent.EvidenceContext(ctx)
	defer cancel()
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
	return db.RecordMeterEvent(ctx, attemptID, codingAgent, e)
}
