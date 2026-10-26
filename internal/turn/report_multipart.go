package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func (r *Reporter) deliverRunParts(ctx context.Context, intent store.ReportDelivery, rep RunReport, renderer interface {
	RenderRunParts(context.Context, report.Run) (redact.Body, []redact.Body, error)
}) (Posted, error) {
	saved, err := r.db.GetReportDelivery(ctx, intent.Key)
	if err == nil {
		return r.dispatchDelivery(ctx, saved)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	if rep.Run.SourceStoryIssue > 0 {
		ref := rep.Run.Ref
		rep.Run.SourceParentURL, rep.Run.SourceReplyURL, err = reportSourceLinks(ctx, r.forge, ref, rep.Run.SourceStoryIssue, rep.Run.SourceCommentID)
		if err != nil {
			return Posted{}, err
		}
	}
	if err := r.attachRecoveryReport(ctx, &rep); err != nil {
		return Posted{}, err
	}
	summary, parts, err := renderer.RenderRunParts(ctx, rep.Run)
	if err != nil {
		return Posted{}, &DeliveryError{Key: intent.Key, State: "pending", Cause: err}
	}
	if len(parts) == 0 {
		return r.deliver(ctx, intent, func() (redact.Body, error) { return summary, nil })
	}
	legacy, err := r.db.IsLegacyReportRun(ctx, intent.RunID)
	if err != nil {
		return Posted{}, err
	}
	if legacy {
		return Posted{}, errors.New("complete multipart reporting needs a journaled run; legacy report remains pending")
	}
	return r.retainMultipartParts(ctx, intent, summary, parts)
}

func (r *Reporter) retainMultipartParts(ctx context.Context, intent store.ReportDelivery, summary redact.Body, parts []redact.Body) (Posted, error) {
	task, err := naming.ParseRef(intent.Task)
	if err != nil {
		return Posted{}, err
	}
	destination, err := naming.ParseRef(intent.Destination)
	if err != nil {
		return Posted{}, err
	}
	intent.BotUserID, err = retainedTicketPublisher(ctx, r.db, intent.SessionID, intent.BotUserID)
	if err != nil {
		return Posted{}, err
	}
	manifest := store.ReportMultipart{Summary: summary.String()}
	for i, part := range parts {
		child := r.deliveryIntent(task, destination, intent.SessionID, intent.TriggerID, intent.RunID, fmt.Sprintf("run-report-part:%s:%d/%d", intent.Key, i+1, len(parts)))
		child.TriggerNamespace = "report-part"
		child.BotUserID = intent.BotUserID
		body, e := r.renderer.RenderReportDelivery(ctx, part.String(), child.Marker)
		if e != nil {
			return Posted{}, e
		}
		child.Body = body.String()
		manifest.Parts = append(manifest.Parts, child)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Posted{}, err
	}
	intent.TriggerNamespace = "report-multipart"
	intent.Body = string(encoded)
	saved, err := r.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		return Posted{}, err
	}
	return r.dispatchDelivery(ctx, saved)
}

func (r *Reporter) dispatchMultipartReport(ctx context.Context, delivery store.ReportDelivery) (Posted, error) {
	if delivery.Completed {
		return r.completedDelivery(ctx, delivery, true)
	}
	var manifest store.ReportMultipart
	if err := json.Unmarshal([]byte(delivery.Body), &manifest); err != nil {
		return Posted{}, err
	}
	if manifest.FinalBody != "" {
		delivery.Body = manifest.FinalBody
		return r.dispatchDeliveryBody(ctx, delivery)
	}
	if len(manifest.Parts) == 0 || len(manifest.Parts) > 32768 {
		return Posted{}, errors.New("invalid complete report manifest")
	}
	var links strings.Builder
	links.WriteString("\n\nComplete retained content (all parts confirmed):\n\n")
	destination, err := naming.ParseRef(delivery.Destination)
	if err != nil {
		return Posted{}, err
	}
	for i, part := range manifest.Parts {
		if part.TriggerNamespace != "report-part" || part.SessionID != delivery.SessionID || part.RunID != delivery.RunID || part.TriggerID != delivery.TriggerID || part.Task != delivery.Task || part.Destination != delivery.Destination || part.BotUserID != delivery.BotUserID {
			return Posted{}, errors.New("multipart report identity drift")
		}
		saved, e := r.db.EnsureReportDelivery(ctx, part)
		if e != nil {
			return Posted{}, e
		}
		if _, e = r.dispatchDelivery(ctx, saved); e != nil {
			return Posted{}, &DeliveryError{Key: delivery.Key, State: "pending", Cause: e}
		}
		saved, e = r.db.GetReportDelivery(ctx, part.Key)
		if e != nil {
			return Posted{}, e
		}
		if !saved.Completed || saved.CommentID <= 0 {
			return Posted{}, errors.New("multipart report remains pending")
		}
		_, link, e := reportSourceLinks(ctx, r.forge, destination, destination.Issue, saved.CommentID)
		if e != nil {
			return Posted{}, e
		}
		fmt.Fprintf(&links, "- [Part %d/%d](%s)\n", i+1, len(manifest.Parts), link)
	}
	body, err := r.renderer.RenderReportDelivery(ctx, manifest.Summary+links.String(), delivery.Marker)
	if err != nil {
		return Posted{}, err
	}
	saved, err := r.db.FinalizeMultipartReport(ctx, delivery.Key, body.String())
	if err != nil {
		return Posted{}, err
	}
	if err := json.Unmarshal([]byte(saved.Body), &manifest); err != nil {
		return Posted{}, err
	}
	saved.Body = manifest.FinalBody
	return r.dispatchDeliveryBody(ctx, saved)
}

// deliverFullText is called only by the existing explicit graph comment
// activities. It presents inert review content without accepting it as a graph
// result and retains full redacted output before any remote effect.
func (r *Reporter) deliverFullText(ctx context.Context, intent store.ReportDelivery, text string) (Posted, error) {
	saved, err := r.db.GetReportDelivery(ctx, intent.Key)
	if err == nil {
		return r.dispatchDelivery(ctx, saved)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	renderer, ok := r.renderer.(interface {
		RenderTextParts(context.Context, string) (redact.Body, []redact.Body, error)
	})
	if !ok {
		return r.deliver(ctx, intent, func() (redact.Body, error) { return r.renderer.RenderReportDelivery(ctx, text, "") })
	}
	summary, parts, err := renderer.RenderTextParts(ctx, text)
	if err != nil {
		return Posted{}, &DeliveryError{Key: intent.Key, State: "pending", Cause: err}
	}
	if len(parts) == 0 {
		return r.deliver(ctx, intent, func() (redact.Body, error) { return summary, nil })
	}
	return r.retainMultipartParts(ctx, intent, summary, parts)
}
