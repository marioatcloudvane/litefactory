package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// DiscussionPublisher uses the report journal's conservative summary policy.
type DiscussionPublisher struct{ Reporter *Reporter }

func (p DiscussionPublisher) intent(ctx context.Context, c store.DiscussionConclusion) (store.ReportDelivery, error) {
	if p.Reporter == nil {
		return store.ReportDelivery{}, errors.New("discussion publisher unavailable")
	}
	session, err := p.Reporter.db.GetSessionByTaskRef(ctx, c.TaskRef)
	if err != nil {
		return store.ReportDelivery{}, err
	}
	marker := "<!-- lf:discussion-summary v=1 key=" + c.ID + " -->"
	return store.ReportDelivery{Key: "discussion-summary:" + c.ID, Version: 1, SessionID: session.ID, Task: c.TaskRef, Destination: c.TaskRef, TriggerNamespace: "discussion-summary", Purpose: "discussion-summary", BotUserID: p.Reporter.botUserID, Marker: marker}, nil
}

// Publish dispatches frozen summary bytes through the report journal.
func (p DiscussionPublisher) Publish(ctx context.Context, c store.DiscussionConclusion) (string, error) {
	intent, err := p.intent(ctx, c)
	if err != nil {
		return "", err
	}
	_, err = p.Reporter.deliver(ctx, intent, func() (redact.Body, error) { return p.Reporter.renderer.RenderReportDelivery(ctx, c.Rendered, "") })
	if err != nil {
		var rate *github.RateLimitError
		if errors.As(err, &rate) {
			return "", &taskdiscussion.PublicationDeferred{RetryAfter: rate.RetryAfter}
		}
		return "", err
	}
	d, err := p.Reporter.db.GetReportDelivery(ctx, intent.Key)
	return d.ConfirmedURL, err
}

// Recover reconciles trusted receipts without issuing another POST.
func (p DiscussionPublisher) Recover(ctx context.Context, c store.DiscussionConclusion) (string, bool, error) {
	intent, err := p.intent(ctx, c)
	if err != nil {
		return "", false, err
	}
	d, err := p.Reporter.db.GetReportDelivery(ctx, intent.Key)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if d.CommentID != 0 {
		return d.ConfirmedURL, true, nil
	}
	if !d.PossiblySent {
		return "", false, nil
	}
	destination, err := naming.ParseRef(c.TaskRef)
	if err != nil {
		return "", false, err
	}
	receipt, err := p.Reporter.recoverDeliveryReceipt(ctx, d, destination)
	if err != nil {
		var rate *github.RateLimitError
		if errors.As(err, &rate) {
			return "", false, &taskdiscussion.PublicationDeferred{RetryAfter: rate.RetryAfter}
		}
		return "", false, err
	}
	d, err = p.Reporter.db.ConfirmReportDelivery(ctx, d.Key, receipt.ID, receipt.HTMLURL)
	if err != nil {
		return "", false, err
	}
	_, err = p.Reporter.finishDelivery(ctx, d)
	return d.ConfirmedURL, err == nil, err
}
