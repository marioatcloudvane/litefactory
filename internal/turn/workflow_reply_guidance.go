package turn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func workflowDecisionReply(kind report.ReplyKind) bool {
	switch kind {
	case report.ReplyImplementCommentNotApplied, report.ReplyBugCommentNotApplied, report.ReplyGraphReplyBetweenQuestions, report.ReplyGraphOwnerWaitRefused, report.ReplyValidateCommentNotApplied, report.ReplyImplementFinishedValidated:
		return true
	}
	return false
}

// workflowDecisionURL reads retained bindings only. It never selects an action
// from a historical display or applies an owner's refused input to a new wait.
func (r *Reporter) workflowDecisionURL(ctx context.Context, ref naming.Ref, sessionID int64, kind report.ReplyKind) (string, error) {
	var instance store.GraphInstance
	var err error
	switch kind {
	case report.ReplyValidateCommentNotApplied, report.ReplyImplementFinishedValidated:
		instance, err = r.db.LatestWorkflowInstance(ctx, sessionID, "validate")
	case report.ReplyImplementCommentNotApplied:
		instance, err = r.db.LatestWorkflowInstance(ctx, sessionID, "implement")
	case report.ReplyBugCommentNotApplied:
		instance, err = r.db.LatestStandaloneBugInstance(ctx, sessionID)
	default:
		instance, err = r.db.LatestSessionGraphInstance(ctx, sessionID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	definition, err := r.db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return "", err
	}
	if definition.Mode == "validate" {
		saved, err := r.db.GetValidateReport(ctx, instance.ID)
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if saved.CommentID == 0 || saved.Digest == "" {
			return "", nil
		}
		// Restore the exact retained graph needed to compute the current revision.
		attempts, err := r.db.ListGraphNodeAttempts(ctx, instance.ID)
		if err != nil {
			return "", err
		}
		if len(attempts) == 0 {
			return "", nil
		}
		capture, err := r.db.WorkflowCaptureForAttempt(ctx, attempts[len(attempts)-1].ID)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrGraphInvariant) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if capture.InstanceID != instance.ID || capture.SessionID != sessionID {
			return "", nil
		}
		var files map[string][]byte
		if err := json.Unmarshal(capture.Files, &files); err != nil {
			return "", err
		}
		graphFile := capture.Provenance.GraphFile
		if graphFile == "" {
			graphFile, err = retainedGraphFile(ctx, r.db, instance.ID)
			if err != nil {
				return "", err
			}
		}
		profile, err := graphexec.ParseProfile(files[graphFile])
		if err != nil {
			return "", err
		}
		st, err := LoadValidateState(ctx, Options{DB: r.db}, profile, instance.ID)
		if err != nil {
			return "", err
		}
		return r.currentValidateReportURL(ctx, ref, sessionID, saved, st)
	}
	if instance.Status != store.GraphRunning {
		return "", nil
	}
	wait, err := r.db.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if wait.State != "waiting" {
		return "", nil
	}
	delivery, err := r.db.GetReportDelivery(ctx, wait.DeliveryKey)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !delivery.Completed || delivery.CommentID == 0 || delivery.SessionID != sessionID || delivery.Task != ref.String() || delivery.Destination != ref.String() {
		return "", nil
	}
	return r.observedDecisionURL(ctx, ref, delivery.CommentID)
}

func (r *Reporter) observedDecisionURL(ctx context.Context, ref naming.Ref, id int64) (string, error) {
	if _, ok := r.forge.(interface {
		SourceLinks(context.Context, naming.Ref, int, int64) (string, string, error)
	}); ok {
		_, link, err := reportSourceLinks(ctx, r.forge, ref, ref.Issue, id)
		return link, err
	}
	if ref.Forge != naming.ForgeGitHub {
		return "", nil
	}
	comments, err := r.deliveryComments(ctx, ref)
	if err != nil {
		return "", err
	}
	for _, c := range comments {
		if c.ID == id && strings.TrimSpace(c.HTMLURL) != "" {
			return c.HTMLURL, nil
		}
	}
	return "", nil
}

// currentValidateReportURL checks the report's current evidence and displayed
// head. Completed results have no pending decision display to authorize.
func (r *Reporter) currentValidateReportURL(ctx context.Context, ref naming.Ref, sessionID int64, saved store.ValidateReport, st ValidateState) (string, error) {
	if saved.InstanceID != st.InstanceID || saved.Revision != st.Revision || saved.CommentID == 0 || saved.Digest == "" || r.validateHead == nil {
		return "", nil
	}
	session, err := r.db.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	head, err := r.validateHead(ctx, session)
	if err != nil {
		return "", err
	}
	if effectDone(st.Effects.Close) || st.Reimplementing {
		expected := st.deliveryCommit()
		if effectDone(st.Effects.Close) {
			expected = st.Effects.Close.Commit
		}
		if head == "" || head != expected {
			return "", nil
		}
		return r.observedDecisionURL(ctx, ref, saved.CommentID)
	}
	display, err := r.db.LatestOwnerDecisionDisplay(ctx, ref.String(), ValidateShipCommand)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if display.Revision != saved.Revision || display.InstanceID != st.InstanceID || display.PublicationID != saved.CommentID || display.TestedCommit != st.Commit || display.DeliveryCommit != head {
		return "", nil
	}
	return r.observedDecisionURL(ctx, ref, saved.CommentID)
}
