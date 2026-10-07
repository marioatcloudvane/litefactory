package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Remote effect kinds of the owner side of Validate.
const (
	EffectIssue = "issue"
	EffectPush  = "push"
	EffectPR    = "pr"
	EffectMerge = "merge"
	EffectClose = "close"
)

// Remote effect states. Intent is written before the remote call; succeeded is
// final. A failed or uncertain effect is retried only by the owner repeating the
// command, which puts it back to intent.
const (
	EffectIntent    = "intent"
	EffectSucceeded = "succeeded"
	EffectFailed    = "failed"
	EffectUncertain = "uncertain"
)

// Owner verdicts and finding choices.
const (
	VerdictPass  = "pass"
	VerdictFail  = "fail"
	ChoiceRepair = "repair"
	ChoiceIssue  = "issue"
	ChoiceDefer  = "defer"
)

// ErrEffectSucceeded refuses a change to an effect that already succeeded.
var ErrEffectSucceeded = errors.New("store: a succeeded effect cannot change")

func validCommit(c string) bool { return len(c) == 40 || len(c) == 64 }

// ValidateReport is the single live report comment of one Validate run.
type ValidateReport struct {
	InstanceID int64
	CommentID  int64
	Marker     string
	// Revision is the report revision the owner's commands must name.
	Revision string
	// Digest is the digest of the last rendered content.
	Digest string
	// Notice is the last refused command and the reason, shown to the owner.
	Notice    string
	UpdatedAt time.Time
}

const reportColumns = `instance_id,comment_id,marker,revision,digest,notice,updated_at`

// GetValidateReport returns the report row of a run, or ErrNotFound.
func (db *DB) GetValidateReport(ctx context.Context, instanceID int64) (ValidateReport, error) {
	var r ValidateReport
	var at string
	err := db.sql.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM validate_reports WHERE instance_id=?`, instanceID).
		Scan(&r.InstanceID, &r.CommentID, &r.Marker, &r.Revision, &r.Digest, &r.Notice, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return ValidateReport{}, ErrNotFound
	}
	if err != nil {
		return ValidateReport{}, fmt.Errorf("store: read validate report: %w", err)
	}
	if r.UpdatedAt, err = ParseTime(at); err != nil {
		return ValidateReport{}, err
	}
	return r, nil
}

// PutValidateReport saves the report row, replacing the earlier one. A comment
// id once saved is never replaced by another or erased.
func (db *DB) PutValidateReport(ctx context.Context, in ValidateReport) error {
	if in.InstanceID < 1 || in.UpdatedAt.IsZero() || in.CommentID < 0 {
		return errors.New("store: invalid validate report")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var comment int64
		err := tx.QueryRowContext(ctx, `SELECT comment_id FROM validate_reports WHERE instance_id=?`, in.InstanceID).Scan(&comment)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if comment != 0 && in.CommentID != comment {
			return errors.New("store: the report comment of a run never changes")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO validate_reports(`+reportColumns+`) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(instance_id) DO UPDATE SET comment_id=excluded.comment_id,marker=excluded.marker,revision=excluded.revision,digest=excluded.digest,notice=excluded.notice,updated_at=excluded.updated_at`,
			in.InstanceID, in.CommentID, in.Marker, in.Revision, in.Digest, in.Notice, FormatTime(in.UpdatedAt))
		return err
	})
}

// ValidateVerdict is the owner's pass or fail for one criterion.
type ValidateVerdict struct {
	ID         int64
	InstanceID int64
	Commit     string
	Criterion  string
	Verdict    string
	Evidence   string
	CommentID  int64
	AuthorID   int64
	Current    bool
	CreatedAt  time.Time
}

const verdictColumns = `id,instance_id,commit_sha,criterion,verdict,evidence,comment_id,author_id,current,created_at`

// PutValidateVerdict records a verdict. A newer verdict for the same run, commit
// and criterion replaces the older one, which stays as history. Recording the
// same comment again returns the stored row.
func (db *DB) PutValidateVerdict(ctx context.Context, in ValidateVerdict) (ValidateVerdict, error) {
	in.Evidence = strings.TrimSpace(in.Evidence)
	if in.InstanceID < 1 || !validCommit(in.Commit) || in.Criterion == "" || !slices.Contains([]string{VerdictPass, VerdictFail}, in.Verdict) || in.Evidence == "" || in.CommentID < 1 || in.AuthorID < 1 || in.CreatedAt.IsZero() {
		return ValidateVerdict{}, errors.New("store: invalid validate verdict")
	}
	var out ValidateVerdict
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanVerdict(tx.QueryRowContext(ctx, `SELECT `+verdictColumns+` FROM validate_verdicts WHERE instance_id=? AND comment_id=? AND criterion=?`, in.InstanceID, in.CommentID, in.Criterion))
		if e == nil {
			out = cur
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE validate_verdicts SET current=0 WHERE instance_id=? AND commit_sha=? AND criterion=? AND current=1`, in.InstanceID, in.Commit, in.Criterion); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_verdicts(instance_id,commit_sha,criterion,verdict,evidence,comment_id,author_id,current,created_at) VALUES(?,?,?,?,?,?,?,1,?)`,
			in.InstanceID, in.Commit, in.Criterion, in.Verdict, in.Evidence, in.CommentID, in.AuthorID, FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out = in
		out.ID, out.Current = id, true
		return nil
	})
	if err != nil {
		return ValidateVerdict{}, fmt.Errorf("store: record validate verdict: %w", err)
	}
	return out, nil
}

// ListValidateVerdicts returns the verdicts of a run on one commit, oldest
// first. History includes the replaced ones; otherwise only the current ones.
func (db *DB) ListValidateVerdicts(ctx context.Context, instanceID int64, commit string, history bool) ([]ValidateVerdict, error) {
	q := `SELECT ` + verdictColumns + ` FROM validate_verdicts WHERE instance_id=? AND commit_sha=?`
	if !history {
		q += ` AND current=1`
	}
	rows, err := db.sql.QueryContext(ctx, q+` ORDER BY id`, instanceID, commit)
	if err != nil {
		return nil, fmt.Errorf("store: list validate verdicts: %w", err)
	}
	defer rows.Close()
	var out []ValidateVerdict
	for rows.Next() {
		v, err := scanVerdict(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanVerdict(row scanner) (ValidateVerdict, error) {
	var v ValidateVerdict
	var at string
	if err := row.Scan(&v.ID, &v.InstanceID, &v.Commit, &v.Criterion, &v.Verdict, &v.Evidence, &v.CommentID, &v.AuthorID, &v.Current, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateVerdict{}, ErrNotFound
		}
		return ValidateVerdict{}, fmt.Errorf("store: read validate verdict: %w", err)
	}
	var err error
	v.CreatedAt, err = ParseTime(at)
	return v, err
}

// ValidateChoice is the owner's choice for one finding.
type ValidateChoice struct {
	ID         int64
	InstanceID int64
	Commit     string
	Finding    string
	Choice     string
	IssueKind  string
	CommentID  int64
	AuthorID   int64
	Current    bool
	CreatedAt  time.Time
}

const choiceColumns = `id,instance_id,commit_sha,finding,choice,issue_kind,comment_id,author_id,current,created_at`

// PutValidateChoice records a choice. A newer choice for the same run, commit
// and finding replaces the older one, which stays as history. Recording the
// same comment again returns the stored row. This record only keeps the owner's
// choice; it never decides whether a finding is cleared, and the store does not
// know which findings are blocking.
func (db *DB) PutValidateChoice(ctx context.Context, in ValidateChoice) (ValidateChoice, error) {
	if in.InstanceID < 1 || !validCommit(in.Commit) || in.Finding == "" || !slices.Contains([]string{ChoiceRepair, ChoiceIssue, ChoiceDefer}, in.Choice) ||
		(in.Choice == ChoiceIssue) != (in.IssueKind != "") || in.IssueKind != "" && !slices.Contains([]string{"bug", "story"}, in.IssueKind) ||
		in.CommentID < 1 || in.AuthorID < 1 || in.CreatedAt.IsZero() {
		return ValidateChoice{}, errors.New("store: invalid validate choice")
	}
	var out ValidateChoice
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanChoice(tx.QueryRowContext(ctx, `SELECT `+choiceColumns+` FROM validate_choices WHERE instance_id=? AND comment_id=? AND finding=?`, in.InstanceID, in.CommentID, in.Finding))
		if e == nil {
			out = cur
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE validate_choices SET current=0 WHERE instance_id=? AND commit_sha=? AND finding=? AND current=1`, in.InstanceID, in.Commit, in.Finding); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_choices(instance_id,commit_sha,finding,choice,issue_kind,comment_id,author_id,current,created_at) VALUES(?,?,?,?,?,?,?,1,?)`,
			in.InstanceID, in.Commit, in.Finding, in.Choice, in.IssueKind, in.CommentID, in.AuthorID, FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out = in
		out.ID, out.Current = id, true
		return nil
	})
	if err != nil {
		return ValidateChoice{}, fmt.Errorf("store: record validate choice: %w", err)
	}
	return out, nil
}

// ListValidateChoices returns the choices of a run on one commit, oldest first.
func (db *DB) ListValidateChoices(ctx context.Context, instanceID int64, commit string, history bool) ([]ValidateChoice, error) {
	q := `SELECT ` + choiceColumns + ` FROM validate_choices WHERE instance_id=? AND commit_sha=?`
	if !history {
		q += ` AND current=1`
	}
	rows, err := db.sql.QueryContext(ctx, q+` ORDER BY id`, instanceID, commit)
	if err != nil {
		return nil, fmt.Errorf("store: list validate choices: %w", err)
	}
	defer rows.Close()
	var out []ValidateChoice
	for rows.Next() {
		c, err := scanChoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanChoice(row scanner) (ValidateChoice, error) {
	var c ValidateChoice
	var at string
	if err := row.Scan(&c.ID, &c.InstanceID, &c.Commit, &c.Finding, &c.Choice, &c.IssueKind, &c.CommentID, &c.AuthorID, &c.Current, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateChoice{}, ErrNotFound
		}
		return ValidateChoice{}, fmt.Errorf("store: read validate choice: %w", err)
	}
	var err error
	c.CreatedAt, err = ParseTime(at)
	return c, err
}

// ValidateEffect is one owner-authorized remote action.
type ValidateEffect struct {
	ID               int64
	InstanceID       int64
	Kind             string
	Commit           string
	Target           string
	ConfirmCommentID int64
	State            string
	Result           json.RawMessage
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const effectColumns = `id,instance_id,kind,commit_sha,target,confirm_comment_id,state,result,error,created_at,updated_at`

// BeginValidateEffect records the intent of a remote effect before it acts, or
// returns the row that already exists for the run, kind and target. When the
// existing row is failed or uncertain and the call carries a newer confirming
// comment, the row goes back to intent (the owner repeated the command).
// created reports whether this call made the row or put it back to intent.
func (db *DB) BeginValidateEffect(ctx context.Context, in ValidateEffect) (out ValidateEffect, created bool, err error) {
	if in.InstanceID < 1 || !slices.Contains([]string{EffectIssue, EffectPush, EffectPR, EffectMerge, EffectClose}, in.Kind) || !validCommit(in.Commit) || in.Target == "" || in.ConfirmCommentID < 0 || in.CreatedAt.IsZero() {
		return ValidateEffect{}, false, errors.New("store: invalid validate effect")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		cur, e := scanEffect(tx.QueryRowContext(ctx, `SELECT `+effectColumns+` FROM validate_effects WHERE instance_id=? AND kind=? AND target=?`, in.InstanceID, in.Kind, in.Target))
		if e == nil {
			out = cur
			if (cur.State == EffectFailed || cur.State == EffectUncertain) && in.ConfirmCommentID > cur.ConfirmCommentID {
				if _, e = tx.ExecContext(ctx, `UPDATE validate_effects SET state='intent',confirm_comment_id=?,error='',updated_at=? WHERE id=?`, in.ConfirmCommentID, FormatTime(in.CreatedAt), cur.ID); e != nil {
					return e
				}
				out.State, out.ConfirmCommentID, out.Error, out.UpdatedAt = EffectIntent, in.ConfirmCommentID, "", in.CreatedAt
				created = true
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO validate_effects(instance_id,kind,commit_sha,target,confirm_comment_id,state,created_at,updated_at) VALUES(?,?,?,?,?,'intent',?,?)`,
			in.InstanceID, in.Kind, in.Commit, in.Target, in.ConfirmCommentID, FormatTime(in.CreatedAt), FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out = in
		out.ID, out.State, out.Result, out.UpdatedAt = id, EffectIntent, json.RawMessage("{}"), in.CreatedAt
		created = true
		return nil
	})
	if err != nil {
		return ValidateEffect{}, false, fmt.Errorf("store: begin validate effect: %w", err)
	}
	return out, created, nil
}

// SettleValidateEffect records what the remote check or call found. Settling a
// succeeded effect with the same result succeeds; any other change is
// [ErrEffectSucceeded].
func (db *DB) SettleValidateEffect(ctx context.Context, id int64, state string, result json.RawMessage, errText string, at time.Time) error {
	if id < 1 || !slices.Contains([]string{EffectSucceeded, EffectFailed, EffectUncertain}, state) || at.IsZero() {
		return errors.New("store: invalid validate effect settlement")
	}
	if len(result) == 0 {
		result = json.RawMessage("{}")
	}
	if len(errText) > 4096 {
		errText = strings.ToValidUTF8(errText[:4096], "")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanEffect(tx.QueryRowContext(ctx, `SELECT `+effectColumns+` FROM validate_effects WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State == EffectSucceeded {
			if state == EffectSucceeded && string(cur.Result) == string(result) {
				return nil
			}
			return ErrEffectSucceeded
		}
		_, err = tx.ExecContext(ctx, `UPDATE validate_effects SET state=?,result=?,error=?,updated_at=? WHERE id=?`, state, string(result), errText, FormatTime(at), id)
		return err
	})
}

// ListValidateEffects returns every effect of a run, oldest first.
func (db *DB) ListValidateEffects(ctx context.Context, instanceID int64) ([]ValidateEffect, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+effectColumns+` FROM validate_effects WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("store: list validate effects: %w", err)
	}
	defer rows.Close()
	var out []ValidateEffect
	for rows.Next() {
		e, err := scanEffect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetValidateEffect returns one effect by run, kind and target, or ErrNotFound.
func (db *DB) GetValidateEffect(ctx context.Context, instanceID int64, kind, target string) (ValidateEffect, error) {
	return scanEffect(db.sql.QueryRowContext(ctx, `SELECT `+effectColumns+` FROM validate_effects WHERE instance_id=? AND kind=? AND target=?`, instanceID, kind, target))
}

func scanEffect(row scanner) (ValidateEffect, error) {
	var e ValidateEffect
	var result, created, updated string
	if err := row.Scan(&e.ID, &e.InstanceID, &e.Kind, &e.Commit, &e.Target, &e.ConfirmCommentID, &e.State, &result, &e.Error, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ValidateEffect{}, ErrNotFound
		}
		return ValidateEffect{}, fmt.Errorf("store: read validate effect: %w", err)
	}
	e.Result = json.RawMessage(result)
	var err error
	if e.CreatedAt, err = ParseTime(created); err != nil {
		return ValidateEffect{}, err
	}
	e.UpdatedAt, err = ParseTime(updated)
	return e, err
}
