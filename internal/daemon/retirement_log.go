package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// retirementErrorAttrs names the failing Git step of a retirement error using
// only structured CommandError fields. Raw error text and stderr are never
// included because either can carry credentials or file contents.
func retirementErrorAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}
	var ce *git.CommandError
	if !errors.As(err, &ce) {
		return []slog.Attr{slog.String("git_step", "non_git"), slog.String("error_class", retirementErrorClass(err))}
	}
	step := ""
	if len(ce.Args) > 0 {
		step = ce.Args[0]
		if step == "worktree" && len(ce.Args) > 1 {
			step += " " + ce.Args[1]
		}
	}
	return []slog.Attr{slog.String("git_step", step), slog.Int("git_exit_code", ce.ExitCode), slog.Bool("git_timed_out", ce.TimedOut)}
}

// retirementErrorClass maps a non-Git failure to a bounded class from known
// sentinels; everything else is "other". Raw error text is never used.
func retirementErrorClass(err error) string {
	var githubRate *github.RateLimitError
	var ticketRate *ticket.RateLimitError
	switch {
	case errors.Is(err, store.ErrTicketIdentityConflict):
		return "ticket_identity_conflict"
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrDiscoveryDeferred):
		return "deferred"
	case errors.As(err, &githubRate), errors.As(err, &ticketRate), errors.Is(err, github.ErrRateLimited):
		return "rate_limited"
	default:
		return "other"
	}
}

// retirementSource names the issue reader actually used for the task.
func retirementSource(taskRef string) string {
	if ref, err := naming.ParseRef(taskRef); err == nil && ref.Forge == string(ticket.Linear) {
		return "linear_ticket"
	}
	return "github_get_issue"
}

// LogRetirement emits value-only evidence at default visibility. Never include
// response bodies, file contents, URLs with credentials or raw command errors.
func LogRetirement(ctx context.Context, log *slog.Logger, sweep, phase string, preview bool, e RetirementEvaluation) {
	if log == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("sweep", sweep), slog.String("task", e.Owner.Session.TaskRef),
		slog.String("repository", e.Owner.Repo.Owner+"/"+e.Owner.Repo.Name), slog.Int("issue", e.Owner.Session.IssueNumber),
		slog.Time("observed_at", e.ObservedAt), slog.String("source", retirementSource(e.Owner.Session.TaskRef)), slog.String("phase", phase), slog.Bool("preview", preview),
		slog.String("issue_state", e.IssueState), slog.String("issue_reason", e.IssueReason), slog.String("issue_error", e.IssueError),
		slog.String("retention_source", e.RetentionSource), slog.Time("retention_at", e.RetentionAt), slog.Time("expires_at", e.ExpiresAt),
		slog.String("base_sha", e.Git.BaseSHA), slog.String("local_sha", e.Git.LocalSHA), slog.String("remote_sha", e.Git.RemoteSHA),
	}
	level := slog.LevelInfo
	if e.Error != nil || e.IssueError != "" {
		level = slog.LevelWarn
	}
	// Both line types carry the bounded error attributes so a resource line
	// alone shows why its lookup failed.
	attrs = append(attrs, retirementErrorAttrs(e.Error)...)
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
