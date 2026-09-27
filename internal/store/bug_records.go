package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/naming"
)

// BugIdentity is factory attribution. OwnerID is captured from operator
// configuration, never a field accepted from an analyst's result.
type BugIdentity struct {
	SessionID                     int64
	Repository                    string
	IssueID, IssueNumber, OwnerID int64
}

// BugExecution exposes analysis-owned state. Downstream domains own separate
// typed sections, so an analysis operation cannot replace their state.
type BugExecution struct {
	Version                   int
	Revision, CurrentAnalysis int64
	AnalysisUsed              int
}

// BugAnalysisRevision is an immutable analysis and its retained result provenance.
type BugAnalysisRevision struct {
	Revision                 int64
	Digest                   string
	Analysis                 buganalysis.BugAnalysis
	ReviewReady              bool
	Response, Reason         string
	Evidence                 []string
	Invocation, Operation    string
	ResultID, ResultRevision int64
}

// BugAnalysisAnswer is an append-only authenticated owner observation.
type BugAnalysisAnswer struct {
	Observation, QuestionID string
	CommentID, AuthorID     int64
	Body                    string
	ObservedAt              time.Time
}

// BugAnalysisApproval authorizes one exact published analysis revision.
type BugAnalysisApproval struct {
	Review           string
	AnalysisRevision int64
	AnalysisDigest   string
	ReplyID, OwnerID int64
	ApprovedAt       time.Time
}

// EnsureBugRecord creates the canonical identity once after sticky admission.
// Conflicting repository/issue/owner identities never overwrite that record.
func (d *DB) EnsureBugRecord(ctx context.Context, in BugIdentity) error {
	if in.SessionID <= 0 || in.IssueID <= 0 || in.IssueNumber <= 0 || in.OwnerID <= 0 {
		return errors.New("store: invalid Bug identity")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		a, err := readTaskWorkflowAdmission(ctx, tx, in.SessionID)
		if err != nil {
			return err
		}
		if a.Workflow != "bug" || a.Family != "standalone" {
			return errors.New("store: Bug record requires standalone admission")
		}
		var task string
		if err = tx.QueryRowContext(ctx, `SELECT task_ref FROM sessions WHERE id=?`, in.SessionID).Scan(&task); err != nil {
			return err
		}
		ref, err := naming.ParseRef(task)
		if err != nil {
			return err
		}
		if ref.String() != fmt.Sprintf("github:%s#%d", in.Repository, in.IssueNumber) {
			return errors.New("store: Bug issue differs from admitted task")
		}
		old, err := bugIdentityTx(ctx, tx, in.SessionID)
		if err == nil {
			if old != in {
				return errors.New("store: immutable Bug identity conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bug_records(session_id,version,repository,issue_id,issue_number,owner_id,workflow,workflow_version) VALUES(?,1,?,?,?,?,'bug',1)`, in.SessionID, in.Repository, in.IssueID, in.IssueNumber, in.OwnerID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bug_execution(session_id,version,revision) VALUES(?,1,1)`, in.SessionID)
		return err
	})
}

func bugIdentityTx(ctx context.Context, tx *sql.Tx, session int64) (BugIdentity, error) {
	var out BugIdentity
	err := tx.QueryRowContext(ctx, `SELECT session_id,repository,issue_id,issue_number,owner_id FROM bug_records WHERE session_id=?`, session).Scan(&out.SessionID, &out.Repository, &out.IssueID, &out.IssueNumber, &out.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

func bugExecutionTx(ctx context.Context, tx *sql.Tx, session int64) (BugExecution, error) {
	var out BugExecution
	err := tx.QueryRowContext(ctx, `SELECT version,revision,current_analysis,analysis_used FROM bug_execution WHERE session_id=?`, session).Scan(&out.Version, &out.Revision, &out.CurrentAnalysis, &out.AnalysisUsed)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return out, err
}

func bugRevisionTx(ctx context.Context, tx *sql.Tx, session, revision int64) (BugAnalysisRevision, error) {
	var out BugAnalysisRevision
	var content, evidence []byte
	err := tx.QueryRowContext(ctx, `SELECT revision,digest,content,review_ready,invocation,operation,result_id,result_revision,response,reason,evidence FROM bug_analysis_revisions WHERE session_id=? AND revision=?`, session, revision).Scan(&out.Revision, &out.Digest, &content, &out.ReviewReady, &out.Invocation, &out.Operation, &out.ResultID, &out.ResultRevision, &out.Response, &out.Reason, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(evidence, &out.Evidence); err != nil {
		return out, err
	}
	if err = json.Unmarshal(content, &out.Analysis); err != nil {
		return out, fmt.Errorf("store: decode Bug analysis: %w", err)
	}
	digest, err := out.Analysis.Digest()
	if err != nil {
		return out, err
	}
	if digest != out.Digest {
		return out, errors.New("store: retained Bug analysis digest mismatch")
	}
	return out, nil
}

// CurrentBugAnalysis returns only approval applicable to the current revision.
// Historical approvals remain retained when a new revision supersedes them.
func (d *DB) CurrentBugAnalysis(ctx context.Context, session int64) (BugExecution, BugAnalysisRevision, *BugAnalysisApproval, error) {
	var state BugExecution
	var revision BugAnalysisRevision
	var approval *BugAnalysisApproval
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		state, err = bugExecutionTx(ctx, tx, session)
		if err != nil {
			return err
		}
		if state.CurrentAnalysis == 0 {
			return nil
		}
		revision, err = bugRevisionTx(ctx, tx, session, state.CurrentAnalysis)
		if err != nil {
			return err
		}
		var a BugAnalysisApproval
		var at string
		err = tx.QueryRowContext(ctx, `SELECT a.review,a.analysis_revision,a.analysis_digest,a.reply_id,a.owner_id,a.approved_at FROM bug_analysis_approvals a JOIN bug_analysis_reviews r ON r.session_id=a.session_id AND r.review=a.review WHERE a.session_id=? AND a.analysis_revision=? AND a.analysis_digest=? AND r.active=1`, session, revision.Revision, revision.Digest).Scan(&a.Review, &a.AnalysisRevision, &a.AnalysisDigest, &a.ReplyID, &a.OwnerID, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		a.ApprovedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return err
		}
		approval = &a
		return nil
	})
	return state, revision, approval, err
}

// GetBugAnalysisRevision reads immutable history without granting approval.
func (d *DB) GetBugAnalysisRevision(ctx context.Context, session, revision int64) (BugAnalysisRevision, error) {
	var out BugAnalysisRevision
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = bugRevisionTx(ctx, tx, session, revision)
		return err
	})
	return out, err
}

func bugEventTx(ctx context.Context, tx *sql.Tx, session int64, kind, reference, reason string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO bug_analysis_events(session_id,kind,reference,reason,recorded_at) VALUES(?,?,?,?,?)`, session, kind, reference, reason, FormatTime(time.Now()))
	return err
}

// RecordBugAnalysisAnswer appends an authenticated observation; an edit uses a
// new observation identity and preserves the original answer.
func (d *DB) RecordBugAnalysisAnswer(ctx context.Context, session int64, in BugAnalysisAnswer) error {
	return d.withTx(ctx, func(tx *sql.Tx) error { return recordBugAnalysisAnswerTx(ctx, tx, session, in) })
}

func recordBugAnalysisAnswerTx(ctx context.Context, tx *sql.Tx, session int64, in BugAnalysisAnswer) error {

	if in.Observation == "" || in.QuestionID == "" || in.CommentID <= 0 || in.AuthorID <= 0 || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || in.ObservedAt.IsZero() {
		return errors.New("store: invalid Bug answer")
	}

	identity, err := bugIdentityTx(ctx, tx, session)
	if err != nil {
		return err
	}
	if identity.OwnerID != in.AuthorID {
		return errors.New("store: Bug answer author is not the numeric owner")
	}
	var old BugAnalysisAnswer
	var at string
	err = tx.QueryRowContext(ctx, `SELECT observation,question_id,comment_id,author_id,body,observed_at FROM bug_analysis_answers WHERE session_id=? AND observation=?`, session, in.Observation).Scan(&old.Observation, &old.QuestionID, &old.CommentID, &old.AuthorID, &old.Body, &at)
	if err == nil {
		old.ObservedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return err
		}
		if old.Observation != in.Observation || old.QuestionID != in.QuestionID || old.CommentID != in.CommentID || old.AuthorID != in.AuthorID || old.Body != in.Body || !old.ObservedAt.Equal(in.ObservedAt) {
			return errors.New("store: Bug answer replay conflict")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	state, err := bugExecutionTx(ctx, tx, session)
	if err != nil {
		return err
	}
	revision, err := bugRevisionTx(ctx, tx, session, state.CurrentAnalysis)
	if err != nil {
		return err
	}
	found := false
	for _, q := range revision.Analysis.Questions {
		if q.ID == in.QuestionID {
			found = true
		}
	}
	if !found {
		return errors.New("store: Bug answer references no current question")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bug_analysis_answers(session_id,observation,question_id,comment_id,author_id,body,observed_at) VALUES(?,?,?,?,?,?,?)`, session, in.Observation, in.QuestionID, in.CommentID, in.AuthorID, in.Body, in.ObservedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, session); err != nil {
		return err
	}
	return bugEventTx(ctx, tx, session, "answer", in.Observation, "")
}

// BugAnalysisAnswers supplies the same append-only context for warm and cold
// preparation. Bounded pages prevent unbounded owner discussion allocation.
func (d *DB) BugAnalysisAnswers(ctx context.Context, session int64, after string, limit int) ([]BugAnalysisAnswer, error) {
	if session <= 0 || limit < 1 || limit > 100 {
		return nil, errors.New("store: invalid Bug answer page")
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT observation,question_id,comment_id,author_id,body,observed_at FROM bug_analysis_answers WHERE session_id=? AND observation>? ORDER BY observation LIMIT ?`, session, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugAnalysisAnswer
	for rows.Next() {
		var a BugAnalysisAnswer
		var at string
		if err = rows.Scan(&a.Observation, &a.QuestionID, &a.CommentID, &a.AuthorID, &a.Body, &at); err != nil {
			return nil, err
		}
		a.ObservedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
