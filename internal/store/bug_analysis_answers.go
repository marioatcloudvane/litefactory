package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ConsumeBugAnalysisAnswers retains an authenticated question reply and consumes
// its published wait atomically. Each selected question receives the unchanged
// owner reply as an observation, never an inferred answer. The parent decides
// whether another substantive investigation is allowed; this operation spends no
// pass, so answers to an eighth-pass question can still be retained.
func (d *DB) ConsumeBugAnalysisAnswers(ctx context.Context, session int64, in GraphOwnerReply, questionIDs []string, expectedRevision int64) error {
	if session <= 0 || in.InstanceID <= 0 || in.OwnerID <= 0 || in.AuthorID != in.OwnerID || in.CommentID <= in.QuestionCommentID || in.QuestionCommentID <= 0 || in.Mode != "" || in.TaskRef == "" || in.SourceRef != in.TaskRef || in.DeliveryKey == "" || in.At.IsZero() || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || len(questionIDs) == 0 || len(questionIDs) > 100 || expectedRevision <= 0 {
		return errors.New("store: invalid Bug question reply")
	}
	questionIDs = slices.Clone(questionIDs)
	slices.Sort(questionIDs)
	seen := make(map[string]bool, len(questionIDs))
	for _, id := range questionIDs {
		if id == "" || seen[id] {
			return errors.New("store: invalid or duplicate Bug answer question")
		}
		seen[id] = true
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		identity, err := bugIdentityTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if identity.OwnerID != in.OwnerID || in.TaskRef != fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber) {
			return errors.New("store: Bug answer task/owner mismatch")
		}
		wait, err := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if err != nil {
			return err
		}
		replay := wait.State == "allocated" && wait.ReplyID == in.CommentID && wait.ReplyAuthorID == in.AuthorID && wait.ReplyBody == in.Body
		if wait.State != "waiting" && !replay {
			return ErrGraphOwnerWait
		}
		var actualSession, comment, source int64
		var workflow, status, kind, body string
		var candidate []byte
		var published bool
		err = tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,i.status,c.kind,c.candidate,c.source_attempt_id,p.comment_id,p.completed,p.body FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_comment_intents c ON c.instance_id=i.id JOIN report_deliveries p ON p.delivery_key=c.delivery_key WHERE i.id=? AND c.delivery_key=?`, in.InstanceID, in.DeliveryKey).Scan(&actualSession, &workflow, &status, &kind, &candidate, &source, &comment, &published, &body)
		if err != nil {
			return err
		}
		if actualSession != session || workflow != "bug" || kind != "question" || source != wait.SourceAttemptID || !published || comment != in.QuestionCommentID || body == "" || (!replay && status != GraphRunning) {
			return errors.New("store: Bug answer requires its confirmed published question")
		}
		var displayed BugAnalysisRevision
		if err = json.Unmarshal(candidate, &displayed); err != nil {
			return err
		}
		if displayed.Response != "ask" || displayed.Revision <= 0 {
			return errors.New("store: invalid Bug question projection")
		}
		current, err := bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if !replay && (current.Revision != expectedRevision || current.CurrentAnalysis != displayed.Revision) {
			return ErrBugRevisionConflict
		}
		revision, err := bugRevisionTx(ctx, tx, session, displayed.Revision)
		if err != nil {
			return err
		}
		if revision.Digest != displayed.Digest || revision.Invocation != displayed.Invocation {
			return errors.New("store: Bug question projection differs from retained analysis")
		}
		pass, err := bugPassTx(ctx, tx, session, revision.Invocation)
		if err != nil {
			return err
		}
		if pass.GraphAttemptID != source || pass.State != "succeeded" {
			return errors.New("store: Bug question source differs from successful analysis")
		}
		for _, id := range questionIDs {
			found := false
			for _, q := range revision.Analysis.Questions {
				if q.ID == id {
					found = true
					break
				}
			}
			if !found {
				return errors.New("store: answer names a question outside the displayed batch")
			}
		}
		// One event stores the complete selected question set, so changed replay cannot
		// add an answer after the reply has already been consumed.
		selected, err := json.Marshal(questionIDs)
		if err != nil {
			return err
		}
		operation := fmt.Sprintf("question-reply:%s:%d", in.DeliveryKey, in.CommentID)
		if replay {
			var recorded string
			if err = tx.QueryRowContext(ctx, `SELECT reason FROM bug_analysis_events WHERE session_id=? AND kind='question_reply' AND reference=?`, session, operation).Scan(&recorded); err != nil {
				return err
			}
			if recorded != string(selected) {
				return errors.New("store: Bug answer replay changed its question batch")
			}
			return nil
		}
		for _, id := range questionIDs {
			observation := fmt.Sprintf("%s:%s", operation, id)
			if err = recordBugAnalysisAnswerTx(ctx, tx, session, BugAnalysisAnswer{Observation: observation, QuestionID: id, CommentID: in.CommentID, AuthorID: in.AuthorID, Body: in.Body, ObservedAt: in.At}); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), in.DeliveryKey); err != nil {
			return err
		}
		return bugEventTx(ctx, tx, session, "question_reply", operation, string(selected))
	})
}
