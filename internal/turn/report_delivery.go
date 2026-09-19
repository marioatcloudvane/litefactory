package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// DeliveryError identifies durable pending work without exposing retained body
// text. The daemon owns retry timing; a turn makes only one delivery attempt.
type DeliveryError struct {
	Key   string
	State string
	Cause error
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("report delivery %s remains %s: %v", e.Key, e.State, e.Cause)
}
func (e *DeliveryError) Unwrap() error { return e.Cause }

func (r *Reporter) deliveryIntent(task, destination naming.Ref, sessionID, triggerID, runID int64, purpose string) store.ReportDelivery {
	namespace := "comment"
	switch {
	case triggerID == 0:
		namespace = "issue-body"
	case store.IsAutomaticTrigger(triggerID):
		namespace = "automatic"
	case store.IsScheduleTrigger(triggerID):
		namespace = "schedule"
	}
	identity := struct {
		Version                      int
		Task, Destination, Namespace string
		Trigger, Run                 int64
		Purpose                      string
	}{1, task.String(), destination.String(), namespace, triggerID, runID, purpose}
	encoded, _ := json.Marshal(identity) // This fixed scalar shape cannot fail JSON encoding.
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	return store.ReportDelivery{Version: 1, Key: key, Task: task.String(), Destination: destination.String(), SessionID: sessionID, TriggerID: triggerID, TriggerNamespace: namespace, RunID: runID, Purpose: purpose, BotUserID: r.botUserID, Marker: "<!-- lf:delivery v=1 key=" + key + " -->"}
}

// ReportPending resumes the exact retained delivery before command/readiness
// reevaluation. A refused original request can never become a fresh invocation.
func (r *Reporter) ReportPending(ctx context.Context, sessionID, triggerID int64) (Posted, bool, error) {
	delivery, found, err := r.db.FindPendingReportDelivery(ctx, sessionID, triggerID)
	if err != nil || !found {
		return Posted{}, found, err
	}
	posted, err := r.dispatchDelivery(ctx, delivery)
	return posted, true, err
}

func (r *Reporter) deliver(ctx context.Context, intent store.ReportDelivery, render func() (redact.Body, error)) (posted Posted, err error) {
	defer func() {
		var pending *DeliveryError
		if err != nil && !errors.As(err, &pending) {
			err = &DeliveryError{Key: intent.Key, State: "pending", Cause: err}
		}
	}()
	saved, err := r.db.GetReportDelivery(ctx, intent.Key)
	if err == nil {
		return r.dispatchDelivery(ctx, saved)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	body, err := render()
	if err != nil {
		return Posted{}, err
	}
	if intent.RunID != 0 {
		_, graphErr := r.db.GetGraphComment(ctx, intent.Key)
		if graphErr != nil && !errors.Is(graphErr, store.ErrNotFound) {
			return Posted{}, graphErr
		}
		if errors.Is(graphErr, store.ErrNotFound) {
			intent.Legacy, err = r.db.IsLegacyReportRun(ctx, intent.RunID)
			if err != nil {
				return Posted{}, err
			}
		}
	}
	if intent.Legacy {
		// Older allocated reports have only their status marker. Stored run/task/
		// trigger provenance plus an exact reconstructed body must all agree.
		ref, parseErr := naming.ParseRef(intent.Task)
		if parseErr != nil {
			return Posted{}, parseErr
		}
		run, readErr := r.db.GetRun(ctx, intent.RunID)
		if readErr != nil {
			return Posted{}, readErr
		}
		intent.Marker = report.StatusMarker(ref, run.Ordinal)
		intent.Body = body.String()
	} else {
		body, err = r.renderer.RenderReportDelivery(ctx, body.String(), intent.Marker)
		if err != nil {
			return Posted{}, err
		}
		intent.Body = body.String()
	}
	saved, err = r.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		return Posted{}, err
	}
	return r.dispatchDelivery(ctx, saved)
}

func (r *Reporter) dispatchDelivery(ctx context.Context, delivery store.ReportDelivery) (posted Posted, err error) {
	defer func() {
		if err != nil {
			err = &DeliveryError{Key: delivery.Key, State: deliveryState(delivery), Cause: err}
		}
	}()
	if delivery.Completed {
		return r.completedDelivery(ctx, delivery, true)
	}
	if delivery.CommentID != 0 {
		return r.finishDelivery(ctx, delivery)
	}
	destination, err := naming.ParseRef(delivery.Destination)
	if err != nil {
		return Posted{}, err
	}
	if delivery.PossiblySent {
		id, err := r.recoverDelivery(ctx, delivery, destination)
		if err != nil {
			return Posted{}, err
		}
		confirmed, confirmErr := r.db.ConfirmReportDelivery(context.WithoutCancel(ctx), delivery.Key, id)
		err = confirmErr
		if err != nil {
			return Posted{CommentID: id}, err
		}
		delivery = confirmed
		return r.finishDelivery(ctx, delivery)
	}
	// Legacy task-only replies cannot establish which unconsumed request they
	// answered. Keep the new intent durable while investigating ambiguity.
	if delivery.RunID == 0 {
		ambiguous, err := r.legacyRunlessConflict(ctx, delivery, destination)
		if err != nil {
			return Posted{}, err
		}
		if ambiguous {
			if _, err := r.db.MarkReportDeliverySent(ctx, delivery.Key); err != nil {
				return Posted{}, err
			}
			return Posted{}, errors.New("legacy runless report provenance is ambiguous; no new comment sent")
		}
	}
	body, err := r.renderer.RenderReportDelivery(ctx, delivery.Body, "")
	if err != nil {
		return Posted{}, err
	}
	if body.String() != delivery.Body {
		return Posted{}, errors.New("retained report no longer passes unchanged outbound redaction")
	}
	claimed, err := r.db.MarkReportDeliverySent(ctx, delivery.Key)
	if err != nil {
		return Posted{}, err
	}
	if !claimed {
		saved, err := r.db.GetReportDelivery(ctx, delivery.Key)
		if err != nil {
			return Posted{}, err
		}
		return r.dispatchDelivery(ctx, saved)
	}
	delivery.PossiblySent = true
	comment, err := r.post(ctx, destination, body, r.log)
	if err != nil {
		return Posted{}, err
	}
	posted.CommentID = comment.ID
	confirmed, confirmErr := r.db.ConfirmReportDelivery(context.WithoutCancel(ctx), delivery.Key, comment.ID)
	err = confirmErr
	if err != nil {
		return posted, err
	}
	delivery = confirmed
	return r.finishDelivery(ctx, delivery)
}

func deliveryState(d store.ReportDelivery) string {
	switch {
	case d.Completed:
		return "completed"
	case d.CommentID != 0:
		return "confirmed"
	case d.PossiblySent:
		return "uncertain"
	default:
		return "pending"
	}
}

func (r *Reporter) finishDelivery(ctx context.Context, delivery store.ReportDelivery) (Posted, error) {
	completed, err := r.db.CompleteReportDelivery(context.WithoutCancel(ctx), delivery.Key)
	if err != nil {
		return Posted{CommentID: delivery.CommentID}, err
	}
	return r.completedDelivery(ctx, completed, false)
}
func (r *Reporter) completedDelivery(ctx context.Context, delivery store.ReportDelivery, skipped bool) (Posted, error) {
	posted := Posted{CommentID: delivery.CommentID, Skipped: skipped}
	if delivery.RunID != 0 {
		run, err := r.db.GetRun(ctx, delivery.RunID)
		if err != nil {
			return posted, err
		}
		posted.Run = run
	}
	return posted, nil
}

func (r *Reporter) deliveryComments(ctx context.Context, destination naming.Ref) ([]github.Comment, error) {
	reader, ok := r.forge.(interface {
		ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	})
	if !ok {
		return nil, errors.New("report recovery requires complete comment lookup")
	}
	return reader.ListCommentsAfter(ctx, github.RepoFromRef(destination), destination.Issue, 0)
}
func commentAtDestination(comment github.Comment, destination naming.Ref) bool {
	parsed, err := url.Parse(comment.HTMLURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.Path == "/"+destination.Owner+"/"+destination.Repo+"/issues/"+strconv.Itoa(destination.Issue) && parsed.Fragment == "issuecomment-"+strconv.FormatInt(comment.ID, 10)
}
func (r *Reporter) recoverDelivery(ctx context.Context, delivery store.ReportDelivery, destination naming.Ref) (int64, error) {
	comments, err := r.deliveryComments(ctx, destination)
	if err != nil {
		return 0, err
	}
	var lowest int64
	conflict := false
	for _, comment := range comments {
		if comment.Author.ID != delivery.BotUserID || comment.ID <= 0 || !commentAtDestination(comment, destination) {
			continue
		}
		if !strings.Contains(comment.Body, delivery.Marker) {
			continue
		}
		if comment.Body != delivery.Body {
			conflict = true
			continue
		}
		if lowest == 0 || comment.ID < lowest {
			lowest = comment.ID
		}
	}
	if conflict {
		return 0, errors.New("matching delivery identity has conflicting remote content")
	}
	if lowest == 0 {
		return 0, errors.New("no trustworthy receipt found; empty lookup does not prove non-delivery")
	}
	return lowest, nil
}
func (r *Reporter) legacyRunlessConflict(ctx context.Context, delivery store.ReportDelivery, destination naming.Ref) (bool, error) {
	comments, err := r.deliveryComments(ctx, destination)
	if err != nil {
		return false, err
	}
	ref, err := naming.ParseRef(delivery.Task)
	if err != nil {
		return false, err
	}
	for _, comment := range comments {
		if comment.Author.ID != delivery.BotUserID || !commentAtDestination(comment, destination) || strings.Contains(comment.Body, "<!-- lf:delivery v=1 key=") {
			continue
		}
		if strings.Contains(comment.Body, report.ReplyMarker(ref)) || strings.Contains(comment.Body, "<!-- lf:status task="+ref.String()+" ") {
			return true, nil
		}
	}
	return false, nil
}
