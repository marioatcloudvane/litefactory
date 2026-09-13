package daemon

import (
	"context"
	"log/slog"
)

// LogRetirement emits value-only evidence at default visibility. Never include
// response bodies, file contents, URLs with credentials or raw command errors.
func LogRetirement(ctx context.Context, log *slog.Logger, sweep, phase string, preview bool, e RetirementEvaluation) {
	if log == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("sweep", sweep), slog.String("task", e.Owner.Session.TaskRef),
		slog.String("repository", e.Owner.Repo.Owner+"/"+e.Owner.Repo.Name), slog.Int("issue", e.Owner.Session.IssueNumber),
		slog.Time("observed_at", e.ObservedAt), slog.String("source", "github_get_issue"), slog.String("phase", phase), slog.Bool("preview", preview),
		slog.String("issue_state", e.IssueState), slog.String("issue_reason", e.IssueReason), slog.String("issue_error", e.IssueError),
		slog.String("retention_source", e.RetentionSource), slog.Time("retention_at", e.RetentionAt), slog.Time("expires_at", e.ExpiresAt),
		slog.String("base_sha", e.Git.BaseSHA), slog.String("local_sha", e.Git.LocalSHA), slog.String("remote_sha", e.Git.RemoteSHA),
	}
	level := slog.LevelInfo
	if e.Error != nil || e.IssueError != "" {
		level = slog.LevelWarn
	}
	log.LogAttrs(ctx, level, "retirement observation", attrs...)
	for _, decision := range e.Decisions {
		decisionLevel := level
		if decision.Action == "unknown" {
			decisionLevel = slog.LevelWarn
		}
		if decision.Action == "failed" || decision.Reason == "outcome_write_failed" || decision.Reason == "mutation_failed_or_uncertain" || decision.Reason == "confirmation_failed" {
			decisionLevel = slog.LevelError
		}
		fields := append(append([]slog.Attr(nil), attrs...), slog.String("resource", decision.Resource), slog.String("kind", decision.Kind), slog.String("expected_sha", decision.ExpectedSHA), slog.String("action", decision.Action), slog.String("reason", decision.Reason), slog.String("outcome", decision.Action))
		log.LogAttrs(ctx, decisionLevel, "retirement resource", fields...)
	}
}
