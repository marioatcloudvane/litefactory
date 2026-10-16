package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type researchReminderReporter interface {
	ReportResearchReminder(context.Context, string, time.Time) (turn.Posted, error)
}

// researchReminders uses the tick clock and session exclusion. Its three bounded
// queues cover due notices, old waits needing verified age, and receipt recovery.
func (d *Daemon) researchReminders(ctx context.Context, at time.Time) {
	reporter, ok := d.reporter.(researchReminderReporter)
	if !ok {
		return
	}
	waits, err := d.db.ListResearchReminderWaits(ctx, at, 16)
	if err != nil {
		d.log.ErrorContext(ctx, "Research reminder selection failed", slog.Any("error", err))
		return
	}
	for _, wait := range waits {
		ctx := sessionContext(ctx, wait.SessionID)
		if _, held := d.connectionHold(ctx, at); held {
			continue
		}
		if !d.enterDoctorGuarded(ctx, wait.SessionID) {
			continue
		}
		err = d.researchReminder(ctx, reporter, wait, at)
		d.inflight.leave(wait.SessionID)
		if err != nil {
			d.rateLimited(ctx, d.log, at, err)
			d.log.ErrorContext(ctx, "Research reminder remains pending; owner wait unchanged", slog.String("wait", wait.WaitKey), slog.Any("error", err))
		}
	}
}

func (d *Daemon) researchReminder(ctx context.Context, reporter researchReminderReporter, wait store.ResearchReminder, at time.Time) (retErr error) {
	ctx = sessionContext(ctx, wait.SessionID)
	locked, err := d.db.AcquireSessionLock(ctx, wait.SessionID, d.instanceID)
	if err != nil || !locked {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), wait.SessionID, d.instanceID))
	}()
	// Possibly-sent notices recover independently of current issue/wait eligibility.
	if wait.DeliveryKey != "" {
		delivery, e := d.db.GetReportDelivery(ctx, wait.DeliveryKey)
		if e != nil {
			return e
		}
		if delivery.PossiblySent {
			_, e = reporter.ReportResearchReminder(ctx, wait.WaitKey, at)
			return e
		}
	}
	ref, err := naming.ParseRef(wait.Task)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	issue, err := d.readIssue(ctx, repo, ref.Issue)
	if err != nil {
		return err
	}
	comments, err := d.readComments(ctx, repo, ref.Issue, 0)
	if err != nil {
		return err
	}
	// Verify the exact original receipt, not a copied marker or an edit timestamp.
	// GitHub receipts carry their GitHub URL and exact body. Linear stores a
	// normalized body under a Linear URL, so it is matched by identity, author
	// and Linear-equivalent content.
	onGitHub := ref.Forge == naming.ForgeGitHub
	expectedURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", ref.Owner, ref.Repo, ref.Issue, wait.CommentID)
	var published time.Time
	for _, comment := range comments {
		if comment.ID != wait.CommentID || comment.Author.ID != wait.BotUserID {
			continue
		}
		if onGitHub && (comment.Body != wait.Body || comment.HTMLURL != expectedURL) {
			continue
		}
		if !onGitHub && !linear.ContentEquivalent(wait.Body, comment.Body) {
			continue
		}
		published = comment.CreatedAt
		break
	}
	if published.IsZero() {
		// Retrying re-reads the whole issue history every pass and can never
		// succeed (it drained the cockpit Linear allowance for a day). A
		// reminder is optional; stop it instead of looping.
		d.log.WarnContext(ctx, "Research reminder stopped: original publication time cannot be verified", slog.String("wait", wait.WaitKey))
		return d.db.SuppressResearchReminder(ctx, wait.WaitKey)
	}
	if err = d.db.VerifyResearchWaitPublication(ctx, wait.WaitKey, published); err != nil {
		return err
	}
	mode, present, err := resolveModeLabel(issue.Labels)
	if err != nil {
		return err
	}
	if issue.State != github.StateOpen || (!present || mode != agent.ModeResearch) {
		return d.db.SuppressResearchReminder(ctx, wait.WaitKey)
	}
	wm, _, err := d.db.GetWatermark(ctx, wait.SessionID)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if comment.Author.ID != d.ticketIdentities(ctx).OwnerUserID || comment.ID <= wait.CommentID || comment.ID <= wm.LastCommentID || strings.TrimSpace(comment.Body) == "" {
			continue
		}
		// Normal polling owns command precedence and reply admission. Defer rather
		// than turn a mode command into an answer or consume input from this path.
		if err = d.db.SetSessionNeedsPoll(ctx, wait.SessionID); err != nil {
			return err
		}
		if command := turn.ParseCommand(comment.Body); command.Kind == turn.KindFreeText {
			return d.db.SuppressResearchReminder(ctx, wait.WaitKey)
		}
		return nil
	}
	if at.Before(published.Add(store.ResearchReminderDelay)) {
		return nil
	}
	_, err = reporter.ReportResearchReminder(ctx, wait.WaitKey, at)
	return err
}
