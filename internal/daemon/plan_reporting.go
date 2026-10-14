package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type planProgressReporter interface {
	ReportPlanNotices(context.Context, naming.Ref, int64) (int, error)
	ReportPlanReply(context.Context, naming.Ref, int64, string, int, int64, string) (turn.Posted, error)
}

// planSummaryReporter keeps a Plan parent's one progress summary comment current.
type planSummaryReporter interface {
	ReportPlanSummary(context.Context, naming.Ref, int64) error
}

func (d *Daemon) reportPlanProgress(ctx context.Context, t *task) error {
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if route != "graph" {
		return nil
	}
	reporter, ok := d.reporter.(planProgressReporter)
	if !ok {
		return nil
	}
	pending, err := reporter.ReportPlanNotices(ctx, t.ref, t.session.ID)
	if pending > 0 && t.log != nil {
		t.log.InfoContext(ctx, "Plan notices retain uncertain receipts", slog.Int("pending", pending))
	}
	if err != nil {
		return err
	}
	if summary, ok := d.reporter.(planSummaryReporter); ok {
		return summary.ReportPlanSummary(ctx, t.ref, t.session.ID)
	}
	return nil
}

func (d *Daemon) explainPlanReply(ctx context.Context, t *task, draft store.PlanStoryDraft, commentID int64, reason string) (bool, error) {
	reporter, ok := d.reporter.(planProgressReporter)
	if !ok {
		return false, errors.New("plan reply explanations require durable reporting")
	}
	_, err := reporter.ReportPlanReply(ctx, t.ref, t.session.ID, draft.Key, draft.IssueNumber, commentID, reason)
	if errors.Is(err, turn.ErrReportReceiptUncertain) {
		if t.log != nil {
			t.log.InfoContext(ctx, "Plan child reply awaits confirmed explanation", slog.Int("child", draft.IssueNumber), slog.Int64("comment", commentID))
		}
		return false, nil
	}
	return err == nil, err
}

// Plan reply deferrals are reported on every attempt, including after dispatch:
// the general commentFate transition cache intentionally suppresses those repeats.
func (d *Daemon) planReplyDeferred(ctx context.Context, t *task, commentID int64, reason string) {
	t.recordWorkerStatus(workerDeferred)
	t.log.InfoContext(ctx, "owner Plan reply deferred",
		slog.Int64("session_id", t.session.ID), slog.Int64("comment_id", commentID),
		slog.Int("issue", t.issue.Number), slog.String("fate", fateDeferred), slog.String("reason", reason))
}
