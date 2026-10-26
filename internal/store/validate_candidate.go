package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrRepairCandidateConflict refuses changed immutable evidence or a conflicting
// owner decision. Replays of the same fact are allowed.
var ErrRepairCandidateConflict = errors.New("store: repair candidate conflict")

// ErrRepairCandidateIneligible means a complete, eligible review is required
// before authorization can be recorded.
var ErrRepairCandidateIneligible = errors.New("store: repair candidate is not eligible for acceptance")

// RepairCandidate retains the entire snapshot and evidence, without the repair
// round's display cap. Fields following CreatedAt are read-only projections of
// independent delivery, resolution and application facts.
type RepairCandidate struct {
	ID                                                          string
	TaskID                                                      string
	InstanceID, RoundID, NodeAttemptID                          int64
	BaseCommit, Tree, Commit, Ref                               string
	Patch, PatchDigest                                          string
	Manifest, Evidence                                          json.RawMessage
	ScopeRevision, ReportRevision, ReviewDigest                 string
	Eligible                                                    bool
	CreatedAt                                                   time.Time
	ReviewComplete                                              bool   `json:"-"`
	Resolution, Principal, SourceID, ResultCommit, JournalState string `json:"-"`
}

// RepairCandidateDecision is authorization, not proof of successful application.
// Principal and SourceID must be authenticated by the caller before this method.
type RepairCandidateDecision struct {
	Action, ReviewDigest, ReportRevision, Principal, SourceID string
	At                                                        time.Time
}

// RepairCandidateJournal records one immutable boundary of a Git operation.
// ExpectedResult identifies the intended commit before the branch can change.
type RepairCandidateJournal struct {
	ID                                                               int64
	CandidateID                                                      string
	InstanceID, RoundID                                              int64
	Phase, State, ExpectedBase, ExpectedResult, ResultCommit, Reason string
	At                                                               time.Time
}

// The patch is encoded as bytes because a valid Git diff can contain invalid
// UTF-8. JSON strings would silently replace those bytes during persistence.
type storedRepairCandidate struct {
	Candidate RepairCandidate
	Patch     []byte
}

func candidateBytes(in RepairCandidate) ([]byte, error) {
	patch := []byte(in.Patch)
	in.Patch = ""
	return json.Marshal(storedRepairCandidate{Candidate: in, Patch: patch})
}

// PutRepairCandidate retains an immutable candidate. Only a byte-identical
// replay succeeds; the same round cannot acquire another identity.
func (db *DB) PutRepairCandidate(ctx context.Context, in RepairCandidate) error {
	if in.ID == "" || in.TaskID == "" || in.InstanceID < 1 || in.RoundID < 1 || in.CreatedAt.IsZero() || !candidateObject(in.BaseCommit) || !candidateObject(in.Tree) || !candidateObject(in.Commit) || !strings.HasPrefix(in.Ref, "refs/litefactory/") || in.ReviewDigest == "" || in.ScopeRevision == "" || !json.Valid(in.Manifest) || !json.Valid(in.Evidence) {
		return errors.New("store: invalid repair candidate")
	}
	sum := sha256.Sum256([]byte(in.Patch))
	if hex.EncodeToString(sum[:]) != in.PatchDigest {
		return errors.New("store: repair patch digest mismatch")
	}
	raw, err := candidateBytes(in)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT content FROM validate_repair_candidates WHERE id=? OR round_id=?`, in.ID, in.RoundID).Scan(&existing)
		if err == nil {
			if bytes.Equal(raw, existing) {
				return nil
			}
			return ErrRepairCandidateConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var instanceID int64
		if err := tx.QueryRowContext(ctx, `SELECT instance_id FROM validate_repair_rounds WHERE id=?`, in.RoundID).Scan(&instanceID); err != nil {
			return err
		}
		if instanceID != in.InstanceID {
			return ErrRepairCandidateConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO validate_repair_candidates(id,instance_id,round_id,content,created_at) VALUES(?,?,?,?,?)`, in.ID, in.InstanceID, in.RoundID, raw, FormatTime(in.CreatedAt))
		return err
	})
}

func candidateObject(v string) bool {
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

const candidateSelect = `SELECT c.content,COALESCE(d.action,'pending'),COALESCE(d.principal,''),COALESCE(d.source_id,''),EXISTS(SELECT 1 FROM validate_repair_candidate_reviews r WHERE r.candidate_id=c.id),COALESCE((SELECT j.result_commit FROM validate_repair_candidate_journal j WHERE j.candidate_id=c.id AND j.phase='apply' AND j.state='complete'),''),COALESCE((SELECT j.phase||':'||j.state FROM validate_repair_candidate_journal j WHERE j.candidate_id=c.id ORDER BY j.id DESC LIMIT 1),'') FROM validate_repair_candidates c LEFT JOIN validate_repair_candidate_decisions d ON d.candidate_id=c.id `

func scanRepairCandidate(row scanner) (RepairCandidate, error) {
	var out RepairCandidate
	var raw []byte
	var resolution, principal, source, result, state string
	var complete bool
	if err := row.Scan(&raw, &resolution, &principal, &source, &complete, &result, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, err
	}
	var stored storedRepairCandidate
	if err := json.Unmarshal(raw, &stored); err != nil {
		return out, fmt.Errorf("store: decode repair candidate: %w", err)
	}
	out = stored.Candidate
	out.Patch = string(stored.Patch)
	out.Resolution, out.Principal, out.SourceID, out.ResultCommit, out.JournalState, out.ReviewComplete = resolution, principal, source, result, state, complete
	return out, nil
}

// GetRepairCandidate returns retained evidence and current lifecycle facts.
func (db *DB) GetRepairCandidate(ctx context.Context, id string) (RepairCandidate, error) {
	return scanRepairCandidate(db.sql.QueryRowContext(ctx, candidateSelect+`WHERE c.id=?`, id))
}

// ListRepairCandidates returns candidates in repair-round order.
func (db *DB) ListRepairCandidates(ctx context.Context, instanceID int64) ([]RepairCandidate, error) {
	rows, err := db.sql.QueryContext(ctx, candidateSelect+`WHERE c.instance_id=? ORDER BY c.round_id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepairCandidate
	for rows.Next() {
		c, err := scanRepairCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetRepairCandidateReviewComplete records confirmed delivery of every review
// part. The caller must verify receipts and refuse redacted or partial evidence.
func (db *DB) SetRepairCandidateReviewComplete(ctx context.Context, id, digest string, at time.Time) error {
	if at.IsZero() {
		return errors.New("store: missing review completion time")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		c, err := scanRepairCandidate(tx.QueryRowContext(ctx, candidateSelect+`WHERE c.id=?`, id))
		if err != nil {
			return err
		}
		if digest != c.ReviewDigest {
			return ErrRepairCandidateConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO validate_repair_candidate_reviews(candidate_id,review_digest,completed_at) VALUES(?,?,?)`, id, digest, FormatTime(at))
		return err
	})
}

// ResolveRepairCandidate performs the pending-to-terminal compare and set in a
// transaction. Reusing a source for another candidate or changing any decision
// identity is refused. No file is changed here.
func (db *DB) ResolveRepairCandidate(ctx context.Context, id string, in RepairCandidateDecision) (RepairCandidate, error) {
	if (in.Action != "accept" && in.Action != "discard") || in.Principal == "" || in.SourceID == "" || in.ReportRevision == "" || in.At.IsZero() {
		return RepairCandidate{}, errors.New("store: invalid repair candidate decision")
	}
	var out RepairCandidate
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		c, err := scanRepairCandidate(tx.QueryRowContext(ctx, candidateSelect+`WHERE c.id=?`, id))
		if err != nil {
			return err
		}
		if c.ReviewDigest != in.ReviewDigest {
			return ErrRepairCandidateConflict
		}
		if c.Resolution != "pending" {
			var report string
			if err := tx.QueryRowContext(ctx, `SELECT report_revision FROM validate_repair_candidate_decisions WHERE candidate_id=?`, id).Scan(&report); err != nil {
				return err
			}
			if report != in.ReportRevision {
				return ErrRepairCandidateConflict
			}
			if c.Resolution != in.Action || c.Principal != in.Principal || c.SourceID != in.SourceID {
				return ErrRepairCandidateConflict
			}
			out = c
			return nil
		}
		if in.Action == "accept" && (!c.Eligible || !c.ReviewComplete) {
			return ErrRepairCandidateIneligible
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT candidate_id FROM validate_repair_candidate_decisions WHERE source_id=?`, in.SourceID).Scan(&previous)
		if err == nil {
			return ErrRepairCandidateConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO validate_repair_candidate_decisions(candidate_id,action,review_digest,report_revision,principal,source_id,decided_at) VALUES(?,?,?,?,?,?,?)`, id, in.Action, in.ReviewDigest, in.ReportRevision, in.Principal, in.SourceID, FormatTime(in.At))
		if err != nil {
			return err
		}
		c.Resolution, c.Principal, c.SourceID = in.Action, in.Principal, in.SourceID
		out = c
		return nil
	})
	return out, err
}

// JournalRepairCandidate appends a durable operation boundary. Capture intent
// is allowed before candidate storage; reset requires verified capture, and
// application requires exact owner authorization. Completion never substitutes
// for intent and every replay must retain its original identities.
func (db *DB) JournalRepairCandidate(ctx context.Context, id string, in RepairCandidateJournal) error {
	if id == "" || in.InstanceID < 1 || in.RoundID < 1 || in.At.IsZero() || !candidateObject(in.ExpectedBase) || (in.Phase != "capture" && in.Phase != "reset" && in.Phase != "apply") || (in.State != "intent" && in.State != "complete" && in.State != "blocked") {
		return errors.New("store: invalid repair candidate journal")
	}
	if in.Phase == "apply" && (!candidateObject(in.ExpectedResult) || (in.State == "complete" && in.ResultCommit != in.ExpectedResult)) {
		return errors.New("store: invalid repair application identity")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var instanceID int64
		var base string
		if err := tx.QueryRowContext(ctx, `SELECT instance_id,pre_commit FROM validate_repair_rounds WHERE id=?`, in.RoundID).Scan(&instanceID, &base); err != nil {
			return err
		}
		if instanceID != in.InstanceID || base != in.ExpectedBase {
			return ErrRepairCandidateConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,candidate_id,instance_id,round_id,phase,state,expected_base,expected_result,result_commit,reason,recorded_at FROM validate_repair_candidate_journal WHERE candidate_id=? ORDER BY id`, id)
		if err != nil {
			return err
		}
		var prior []RepairCandidateJournal
		for rows.Next() {
			j, err := scanRepairCandidateJournal(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			prior = append(prior, j)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		complete := map[string]bool{}
		var intent *RepairCandidateJournal
		for i := range prior {
			p := &prior[i]
			if p.InstanceID != in.InstanceID || p.RoundID != in.RoundID || p.ExpectedBase != in.ExpectedBase {
				return ErrRepairCandidateConflict
			}
			if p.State == "complete" {
				complete[p.Phase] = true
			}
			if p.Phase == in.Phase && p.State == "intent" {
				intent = p
			}
			if p.Phase == in.Phase && p.State == in.State {
				if p.ExpectedResult == in.ExpectedResult && p.ResultCommit == in.ResultCommit && p.Reason == in.Reason {
					return nil
				}
				return ErrRepairCandidateConflict
			}
		}
		if complete[in.Phase] {
			return ErrRepairCandidateConflict
		}
		if in.State != "intent" && (intent == nil || intent.ExpectedResult != in.ExpectedResult) {
			return ErrRepairCandidateConflict
		}
		if in.Phase == "reset" && !complete["capture"] {
			return ErrRepairCandidateConflict
		}
		if in.Phase != "capture" || in.State == "complete" {
			c, err := scanRepairCandidate(tx.QueryRowContext(ctx, candidateSelect+`WHERE c.id=?`, id))
			if err != nil {
				return err
			}
			if c.InstanceID != in.InstanceID || c.RoundID != in.RoundID || c.BaseCommit != in.ExpectedBase {
				return ErrRepairCandidateConflict
			}
			if in.Phase == "apply" && (!complete["reset"] || c.Resolution != "accept" || !c.ReviewComplete || !c.Eligible) {
				return ErrRepairCandidateIneligible
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO validate_repair_candidate_journal(candidate_id,instance_id,round_id,phase,state,expected_base,expected_result,result_commit,reason,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, in.InstanceID, in.RoundID, in.Phase, in.State, in.ExpectedBase, in.ExpectedResult, in.ResultCommit, in.Reason, FormatTime(in.At))
		return err
	})
}

func scanRepairCandidateJournal(row scanner) (RepairCandidateJournal, error) {
	var out RepairCandidateJournal
	var at string
	if err := row.Scan(&out.ID, &out.CandidateID, &out.InstanceID, &out.RoundID, &out.Phase, &out.State, &out.ExpectedBase, &out.ExpectedResult, &out.ResultCommit, &out.Reason, &at); err != nil {
		return out, err
	}
	var err error
	out.At, err = ParseTime(at)
	return out, err
}

// ListRepairCandidateJournals includes unfinished capture intents that do not
// yet have a candidate row. Recovery must reconcile these before any worker.
func (db *DB) ListRepairCandidateJournals(ctx context.Context, instanceID int64) ([]RepairCandidateJournal, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,candidate_id,instance_id,round_id,phase,state,expected_base,expected_result,result_commit,reason,recorded_at FROM validate_repair_candidate_journal WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepairCandidateJournal
	for rows.Next() {
		j, err := scanRepairCandidateJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// LatestAdoptedRepairCommit returns only a verified result of owner-authorized
// exact application. Pending, discarded and apply-intent rows never qualify.
func (db *DB) LatestAdoptedRepairCommit(ctx context.Context, instanceID int64) (string, error) {
	var commit string
	err := db.sql.QueryRowContext(ctx, `SELECT j.result_commit FROM validate_repair_candidate_journal j JOIN validate_repair_candidate_decisions d ON d.candidate_id=j.candidate_id JOIN validate_repair_candidates c ON c.id=j.candidate_id WHERE j.instance_id=? AND j.phase='apply' AND j.state='complete' AND d.action='accept' ORDER BY c.round_id DESC LIMIT 1`, instanceID).Scan(&commit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return commit, err
}

// ListRepairCandidateJournal returns one candidate's recorded operation steps.
func (db *DB) ListRepairCandidateJournal(ctx context.Context, id string) ([]RepairCandidateJournal, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,candidate_id,instance_id,round_id,phase,state,expected_base,expected_result,result_commit,reason,recorded_at FROM validate_repair_candidate_journal WHERE candidate_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepairCandidateJournal
	for rows.Next() {
		j, err := scanRepairCandidateJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
