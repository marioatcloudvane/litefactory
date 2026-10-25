package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// StandardsAuthoringSession holds an immutable host-owned authoring state.
type StandardsAuthoringSession struct {
	ID        string          `json:"id"`
	Mode      string          `json:"mode"`
	TargetID  string          `json:"target_id"`
	Version   int64           `json:"version"`
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// StandardsAuthoringAttempt records a finite fenced response-only invocation.
type StandardsAuthoringAttempt struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	InputVersion   int64     `json:"input_version"`
	Token          string    `json:"token"`
	LeaseUntil     time.Time `json:"lease_until"`
	InputDigest    string    `json:"input_digest"`
	RoleVersion    string    `json:"role_version"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	UsageReference string    `json:"usage_reference"`
	Outcome        string    `json:"outcome"`
	Diagnostic     string    `json:"diagnostic"`
	ResultVersion  int64     `json:"result_version"`
	CreatedAt      time.Time `json:"created_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}

func readAuthoring(ctx context.Context, tx *sql.Tx, id string, version int64) (StandardsAuthoringSession, error) {
	var s StandardsAuthoringSession
	var raw, created, updated string
	err := tx.QueryRowContext(ctx, `SELECT s.id,s.mode,s.target_id,v.version,CASE WHEN v.version=s.current_version THEN s.status ELSE v.status END,v.data_json,s.created_at,v.created_at FROM standards_authoring_sessions s JOIN standards_authoring_versions v ON v.session_id=s.id AND v.version=CASE WHEN ?=0 THEN s.current_version ELSE ? END WHERE s.id=?`, version, version, id).Scan(&s.ID, &s.Mode, &s.TargetID, &s.Version, &s.Status, &raw, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if !json.Valid([]byte(raw)) {
		return s, ErrStandardsCorrupt
	}
	s.Data = json.RawMessage(raw)
	s.CreatedAt, err = ParseTime(created)
	if err != nil {
		return s, ErrStandardsCorrupt
	}
	s.UpdatedAt, err = ParseTime(updated)
	if err != nil {
		return s, ErrStandardsCorrupt
	}
	return s, nil
}

// GetStandardsAuthoring reads an immutable version; zero selects current.
func (db *DB) GetStandardsAuthoring(ctx context.Context, id string, version int64) (StandardsAuthoringSession, error) {
	var s StandardsAuthoringSession
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; s, e = readAuthoring(ctx, tx, id, version); return e })
	return s, err
}

func saveAuthoring(ctx context.Context, tx *sql.Tx, s *StandardsAuthoringSession, expected int64) error {
	// The retained base, original target and draft can each approach the 16 MiB
	// snapshot ceiling; allow their saved copies plus authoring metadata.
	if len(s.Data) > 64<<20 {
		return errors.New("standards: saved authoring state exceeds 64 MiB; shorten draft or authoring input")
	}
	if s.ID == "" || s.TargetID == "" || (s.Status != "draft" && s.Status != "needs-context") || (s.Mode != "create" && s.Mode != "edit") || expected < 0 || !json.Valid(s.Data) {
		return errors.New("standards: invalid authoring state")
	}
	stamp := nowUTC().Truncate(time.Millisecond)
	s.Version = expected + 1
	s.UpdatedAt = stamp
	if expected == 0 {
		s.CreatedAt = stamp
		res, e := tx.ExecContext(ctx, `INSERT INTO standards_authoring_sessions(id,mode,target_id,current_version,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, s.ID, s.Mode, s.TargetID, s.Version, s.Status, FormatTime(stamp), FormatTime(stamp))
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrStandardsConflict
		}
	} else {
		var active sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT active_attempt FROM standards_authoring_sessions WHERE id=? AND current_version=?`, s.ID, expected).Scan(&active); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrStandardsConflict
			}
			return err
		}
		if active.Valid {
			old, err := readAuthoringAttempt(ctx, tx, active.String)
			if err != nil {
				return err
			}
			old.Outcome = "interrupted"
			old.Diagnostic = "saved input changed; previous continuation fenced"
			old.FinishedAt = stamp
			if err = writeAuthoringAttempt(ctx, tx, old); err != nil {
				return err
			}
		}
		res, e := tx.ExecContext(ctx, `UPDATE standards_authoring_sessions SET current_version=?,status=?,updated_at=?,active_attempt=NULL WHERE id=? AND current_version=? AND mode=? AND target_id=? AND status!='completed'`, s.Version, s.Status, FormatTime(stamp), s.ID, expected, s.Mode, s.TargetID)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrStandardsConflict
		}
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO standards_authoring_versions(session_id,version,status,data_json,created_at) VALUES(?,?,?,?,?)`, s.ID, s.Version, s.Status, string(s.Data), FormatTime(stamp))
	return e
}

// SaveStandardsAuthoring appends with a compare-and-swap and fences prior output.
func (db *DB) SaveStandardsAuthoring(ctx context.Context, s StandardsAuthoringSession, expected int64) (StandardsAuthoringSession, error) {
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "author", ID: s.ID, Version: s.Version}
	}), func(tx *sql.Tx) error { return saveAuthoring(ctx, tx, &s, expected) })
	return s, err
}

// ListStandardsAuthoring returns a bounded current-session list.
func (db *DB) ListStandardsAuthoring(ctx context.Context, limit int) ([]StandardsAuthoringSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	out := []StandardsAuthoringSession{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM standards_authoring_sessions ORDER BY updated_at DESC,id LIMIT ?`, limit)
		if e != nil {
			return e
		}
		var ids []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			s, e := readAuthoring(ctx, tx, id, 0)
			if e != nil {
				return e
			}
			out = append(out, s)
		}
		return nil
	})
	return out, err
}

func readAuthoringAttempt(ctx context.Context, tx *sql.Tx, id string) (StandardsAuthoringAttempt, error) {
	var a StandardsAuthoringAttempt
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT attempt_json FROM standards_authoring_attempts WHERE id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if e != nil {
		return a, e
	}
	if e = json.Unmarshal([]byte(raw), &a); e != nil || a.ID != id {
		return a, ErrStandardsCorrupt
	}
	return a, nil
}
func writeAuthoringAttempt(ctx context.Context, tx *sql.Tx, a StandardsAuthoringAttempt) error {
	raw, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE standards_authoring_attempts SET attempt_json=? WHERE id=?`, string(raw), a.ID)
	return e
}

// BeginStandardsAuthoringAttempt acquires ownership without holding a transaction during model work.
func (db *DB) BeginStandardsAuthoringAttempt(ctx context.Context, a StandardsAuthoringAttempt) (StandardsAuthoringAttempt, error) {
	if a.ID == "" || a.Token == "" || a.InputVersion < 1 {
		return a, errors.New("standards: invalid authoring attempt lease")
	}
	a.Outcome = "running"
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		now := nowUTC()
		if !a.LeaseUntil.After(now) || a.LeaseUntil.After(now.Add(20*time.Minute)) {
			return errors.New("standards: invalid authoring attempt lease")
		}
		a.CreatedAt = now
		s, e := readAuthoring(ctx, tx, a.SessionID, 0)
		if e != nil {
			return e
		}
		if s.Version != a.InputVersion || s.Status == "completed" {
			return ErrStandardsConflict
		}
		var active sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT active_attempt FROM standards_authoring_sessions WHERE id=?`, a.SessionID).Scan(&active); e != nil {
			return e
		}
		if active.Valid {
			old, e := readAuthoringAttempt(ctx, tx, active.String)
			if e != nil {
				return e
			}
			if old.LeaseUntil.After(now) {
				return fmt.Errorf("%w: continuation active until %s", ErrStandardsConflict, FormatTime(old.LeaseUntil))
			}
			old.Outcome = "interrupted"
			old.Diagnostic = "lease expired; explicit continuation fenced prior output"
			old.FinishedAt = now
			if e = writeAuthoringAttempt(ctx, tx, old); e != nil {
				return e
			}
		}
		raw, e := json.Marshal(a)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO standards_authoring_attempts(id,session_id,input_version,token,lease_until,attempt_json) VALUES(?,?,?,?,?,?)`, a.ID, a.SessionID, a.InputVersion, a.Token, FormatTime(a.LeaseUntil), string(raw)); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE standards_authoring_sessions SET active_attempt=? WHERE id=?`, a.ID, a.SessionID)
		return e
	})
	return a, err
}

// FinishStandardsAuthoringAttempt applies only a still-current fenced result. A nil state records failure.
func (db *DB) FinishStandardsAuthoringAttempt(ctx context.Context, a StandardsAuthoringAttempt, s *StandardsAuthoringSession) (StandardsAuthoringAttempt, error) {
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		if s == nil {
			return StandardsConversationEffect{}
		}
		kind := "author"
		if a.Outcome != "accepted" && a.Outcome != "succeeded" {
			kind = "author-interrupted"
		}
		return StandardsConversationEffect{DomainKind: kind, ID: s.ID, Version: s.Version}
	}), func(tx *sql.Tx) error {
		old, e := readAuthoringAttempt(ctx, tx, a.ID)
		if e != nil {
			return e
		}
		var active sql.NullString
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT active_attempt,current_version FROM standards_authoring_sessions WHERE id=?`, old.SessionID).Scan(&active, &version); e != nil {
			return e
		}
		if !active.Valid || active.String != a.ID || old.Token != a.Token || version != old.InputVersion || old.Outcome != "running" || !old.LeaseUntil.After(nowUTC()) {
			return ErrStandardsConflict
		}
		if s != nil {
			if s.ID != old.SessionID {
				return ErrStandardsConflict
			}
			if e = saveAuthoring(ctx, tx, s, old.InputVersion); e != nil {
				return e
			}
			old.ResultVersion = s.Version
		}
		old.Provider = a.Provider
		old.Model = a.Model
		old.UsageReference = a.UsageReference
		old.InputDigest = a.InputDigest
		old.RoleVersion = a.RoleVersion
		old.Diagnostic = a.Diagnostic
		old.Outcome = a.Outcome
		if old.Outcome == "" || old.Outcome == "running" {
			old.Outcome = "succeeded"
		}
		old.FinishedAt = nowUTC()
		if e = writeAuthoringAttempt(ctx, tx, old); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE standards_authoring_sessions SET active_attempt=NULL WHERE id=?`, old.SessionID)
		a = old
		return e
	})
	return a, err
}

// ListStandardsAuthoringAttempts exposes bounded provenance and interruption diagnostics.
func (db *DB) ListStandardsAuthoringAttempts(ctx context.Context, id string) ([]StandardsAuthoringAttempt, error) {
	out := []StandardsAuthoringAttempt{}
	rows, e := db.sql.QueryContext(ctx, `SELECT attempt_json FROM standards_authoring_attempts WHERE session_id=? ORDER BY rowid DESC LIMIT 100`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var a StandardsAuthoringAttempt
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return nil, ErrStandardsCorrupt
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ErrStandardsAuthoringSettled refuses to reopen a session whose proposal the owner already approved or published.
var ErrStandardsAuthoringSettled = errors.New("standards: the proposal is already approved or published; start a new authoring session to change it")

// authoringProposalOpen returns the current version of an authoring proposal
// that is neither approved nor published.
func authoringProposalOpen(ctx context.Context, tx *sql.Tx, proposal string) (int64, error) {
	var current int64
	var published sql.NullInt64
	if e := tx.QueryRowContext(ctx, `SELECT current_version,published_version FROM standards_proposals WHERE id=?`, proposal).Scan(&current, &published); e != nil {
		return 0, e
	}
	var approvals int64
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM standards_approvals WHERE proposal_id=? AND version=?`, proposal, current).Scan(&approvals); e != nil {
		return 0, e
	}
	if published.Valid || approvals > 0 {
		return 0, ErrStandardsAuthoringSettled
	}
	return current, nil
}

// ReopenStandardsAuthoring takes a completed session back to draft as the next
// saved version s, while the proposal it produced is still unapproved and
// unpublished. Completing it again writes the next version of that proposal.
func (db *DB) ReopenStandardsAuthoring(ctx context.Context, s StandardsAuthoringSession, expected int64) (StandardsAuthoringSession, error) {
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "author", ID: s.ID, Version: s.Version}
	}), func(tx *sql.Tx) error {
		var proposal string
		e := tx.QueryRowContext(ctx, `SELECT c.proposal_id FROM standards_authoring_completions c JOIN standards_authoring_sessions s ON s.id=c.session_id WHERE c.session_id=? AND s.status='completed' AND s.current_version=?`, s.ID, expected).Scan(&proposal)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrStandardsConflict
		}
		if e != nil {
			return e
		}
		if _, e = authoringProposalOpen(ctx, tx, proposal); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE standards_authoring_sessions SET status='draft' WHERE id=? AND status='completed'`, s.ID); e != nil {
			return e
		}
		return saveAuthoring(ctx, tx, &s, expected)
	})
	return s, err
}

// CompleteStandardsAuthoring atomically creates an unapproved proposal and its unique recovery link.
// A reopened session completes into the next version of its existing proposal.
func (db *DB) CompleteStandardsAuthoring(ctx context.Context, id string, expected int64, p StandardsProposalVersion) (StandardsProposalVersion, error) {
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "proposal", ID: p.ProposalID, Version: p.Version}
	}), func(tx *sql.Tx) error {
		var linked, status string
		var version, sessionVersion int64
		e := tx.QueryRowContext(ctx, `SELECT c.proposal_id,c.proposal_version,c.version,s.status FROM standards_authoring_completions c JOIN standards_authoring_sessions s ON s.id=c.session_id WHERE c.session_id=?`, id).Scan(&linked, &version, &sessionVersion, &status)
		reopened := e == nil
		if reopened && status == "completed" {
			if sessionVersion != expected {
				return ErrStandardsConflict
			}
			p, e = readStandardsProposal(ctx, tx, linked, version)
			return e
		}
		if !reopened && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		s, e := readAuthoring(ctx, tx, id, 0)
		if e != nil {
			return e
		}
		if s.Version != expected || s.Status == "completed" {
			return ErrStandardsConflict
		}
		head, e := standardsHead(ctx, tx)
		if e != nil {
			return e
		}
		if head != p.BaseRevision {
			return ErrStandardsConflict
		}
		if p.ProposalID == "" || p.SchemaVersion != 1 || p.ChangeDigest == "" || p.ResultDigest == "" {
			return errors.New("standards: invalid proposal metadata")
		}
		p.CreatedAt = nowUTC().Truncate(time.Millisecond)
		if reopened {
			// The new draft supersedes the unapproved version it was reopened from.
			if p.ProposalID != linked {
				return ErrStandardsConflict
			}
			current, e := authoringProposalOpen(ctx, tx, linked)
			if e != nil {
				return e
			}
			if p, e = saveStandardsProposalTx(ctx, tx, p, current); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE standards_authoring_completions SET version=?,proposal_version=? WHERE session_id=?`, expected, p.Version, id); e != nil {
				return e
			}
		} else {
			p.Version = 1
			ops, e := json.Marshal(p.Operations)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO standards_proposals(id,current_version) VALUES(?,1)`, p.ProposalID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO standards_proposal_versions(proposal_id,version,base_revision,schema_version,operations_json,change_digest,result_digest,author,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, p.ProposalID, p.Version, nullID(p.BaseRevision), p.SchemaVersion, string(ops), p.ChangeDigest, p.ResultDigest, p.Author, FormatTime(p.CreatedAt)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO standards_authoring_completions(session_id,version,proposal_id,proposal_version) VALUES(?,?,?,?)`, id, expected, p.ProposalID, p.Version); e != nil {
				return e
			}
		}
		var active sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT active_attempt FROM standards_authoring_sessions WHERE id=?`, id).Scan(&active); e != nil {
			return e
		}
		if active.Valid {
			old, err := readAuthoringAttempt(ctx, tx, active.String)
			if err != nil {
				return err
			}
			old.Outcome = "interrupted"
			old.Diagnostic = "session completed; previous continuation fenced"
			old.FinishedAt = p.CreatedAt
			if err = writeAuthoringAttempt(ctx, tx, old); err != nil {
				return err
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE standards_authoring_sessions SET status='completed',active_attempt=NULL,updated_at=? WHERE id=?`, FormatTime(p.CreatedAt), id)
		return e
	})
	return p, err
}

// GetStandardsAuthoringCompletion returns the proposal version the session last completed into, if any.
func (db *DB) GetStandardsAuthoringCompletion(ctx context.Context, id string) (StandardsProposalVersion, error) {
	var p StandardsProposalVersion
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var proposal string
		var version int64
		e := tx.QueryRowContext(ctx, `SELECT proposal_id,proposal_version FROM standards_authoring_completions WHERE session_id=?`, id).Scan(&proposal, &version)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		p, e = readStandardsProposal(ctx, tx, proposal, version)
		return e
	})
	return p, err
}
