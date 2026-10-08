package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DoctorRepairAction is an exact replacement, never an executable SQL or shell string.
type DoctorRepairAction struct {
	Kind   string `json:"kind"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// DoctorApproval binds a trusted caller to a previously presented immutable proposal.
type DoctorApproval struct {
	ID, SourceID, Principal, TaskRef, PresentationID string
	InboxID                                          int64
}

// DoctorRepair is durable status; backup bytes are deliberately excluded.
type DoctorRepair struct {
	NewlyReserved                                      bool
	ID, SourceID, TaskRef, State, Failure, ExclusionID string
	ConversationID, Generation, ProposalID, Revision   int64
	Fenced                                             bool
}

// DoctorRepairStep is an approved immutable step and its factual journal.
type DoctorRepairStep struct {
	Number                                                                                           int
	Kind, Target, Before, After, State, BackupReference, BackupDigest, ObservedAfter, SourceIdentity string
}

// PresentDoctorProposal records a local presentation only after output succeeds.
func (db *DB) PresentDoctorProposal(ctx context.Context, ref, id string, proposalID int64) error {
	if id == "" || proposalID <= 0 {
		return errors.New("invalid doctor presentation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var payload []byte
		var generation int64
		if err := tx.QueryRowContext(ctx, `SELECT p.payload,c.generation FROM doctor_conversations c JOIN doctor_proposals p ON p.id=c.current_proposal_id WHERE c.task_ref=? AND c.state='active' AND p.id=?`, ref, proposalID).Scan(&payload, &generation); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO doctor_local_presentations(id,proposal_id,task_ref,generation,digest,presented_at) VALUES(?,?,?,?,?,?)`, id, proposalID, ref, generation, doctorDigest(payload), FormatTime(nowUTC()))
		return err
	})
}
func doctorDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// ReserveDoctorRepair atomically freezes approval, exact steps and the issue hold.
// All capability preflight must happen before invoking this mutating operation.
func (db *DB) ReserveDoctorRepair(ctx context.Context, a DoctorApproval) (DoctorRepair, error) {
	var out DoctorRepair
	if a.ID == "" || a.SourceID == "" || a.TaskRef == "" || a.Principal == "" {
		return out, errors.New("invalid doctor approval")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error { return reserveDoctorRepairTx(ctx, tx, a, &out) })
	return out, err
}

func reserveDoctorRepairTx(ctx context.Context, tx *sql.Tx, a DoctorApproval, out *DoctorRepair) error {
	prior, err := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE source_id=?`, a.SourceID))
	if err == nil {
		if prior.TaskRef != a.TaskRef {
			return errors.New("doctor approval source target mismatch")
		}
		*out = prior
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var cid, generation, pid int64
	var payload []byte
	if err := tx.QueryRowContext(ctx, `SELECT c.id,c.generation,p.id,p.payload FROM doctor_conversations c JOIN doctor_proposals p ON p.id=c.current_proposal_id WHERE c.task_ref=? AND c.state='active'`, a.TaskRef).Scan(&cid, &generation, &pid, &payload); err != nil {
		return err
	}
	digest := doctorDigest(payload)
	var captured string
	if err = tx.QueryRowContext(ctx, `SELECT a.evidence FROM doctor_attempts a JOIN doctor_proposals p ON p.attempt_id=a.id WHERE p.id=?`, pid).Scan(&captured); err != nil {
		return err
	}
	var evidence DoctorEvidence
	if err = json.Unmarshal([]byte(captured), &evidence); err != nil {
		return err
	}

	presentation := a.PresentationID
	if a.InboxID > 0 {
		var comment, actor int64
		var target, operation string
		if err := tx.QueryRowContext(ctx, `SELECT task_ref,operation,comment_id,actor_id FROM doctor_inbox WHERE id=? AND actor_id>0`, a.InboxID).Scan(&target, &operation, &comment, &actor); err != nil {
			return err
		}
		if target != a.TaskRef || operation != "apply" || a.SourceID != fmt.Sprintf("github-doctor-apply:%d", a.InboxID) || a.Principal != fmt.Sprintf("github-owner:%d", actor) {
			return errors.New("doctor GitHub approval provenance mismatch")
		}
		var count, total int
		err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(MAX(p.total),0) FROM doctor_presentations p JOIN report_deliveries d ON d.delivery_key=p.delivery_key JOIN doctor_inbox i ON i.id=p.command_id WHERE p.proposal_id=? AND i.task_ref=? AND d.completed=1 AND d.comment_id<? AND d.destination=?`, pid, a.TaskRef, comment, a.TaskRef).Scan(&count, &total)
		if err != nil {
			return err
		}
		if total == 0 || count != total {
			return errors.New("doctor proposal not completely presented before approval")
		}
		presentation = fmt.Sprintf("github:%d", a.InboxID)
	} else {
		var n int
		if a.Principal != "local-owner" {
			return errors.New("doctor local approval principal refused")
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_local_presentations WHERE id=? AND proposal_id=? AND task_ref=? AND generation=? AND digest=?`, a.PresentationID, pid, a.TaskRef, generation, digest).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("doctor current proposal has no matching local presentation")
		}
	}
	var proposed []struct {
		Number int                 `json:"number"`
		Target string              `json:"target"`
		Action *DoctorRepairAction `json:"action"`
	}
	if err := json.Unmarshal(payload, &proposed); err != nil {
		return err
	}
	if len(proposed) == 0 || len(proposed) > 30 {
		return errors.New("doctor repair requires bounded exact steps")
	}
	now := FormatTime(nowUTC())
	_, err = tx.ExecContext(ctx, `INSERT INTO doctor_repairs(id,source_id,principal,task_ref,conversation_id,generation,proposal_id,proposal_digest,presentation_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'reserved',?,?)`, a.ID, a.SourceID, a.Principal, a.TaskRef, cid, generation, pid, digest, presentation, now, now)
	if err != nil {
		return err
	}
	for index, step := range proposed {
		if step.Number != index+1 || step.Action == nil || len(step.Action.After) > 65536 {
			return errors.New("doctor proposal lacks exact repair action")
		}
		kind := step.Action.Kind
		sourceIdentity := ""
		switch kind {
		case "session_file", "worktree_file":
			if len(step.Action.After) > 32768 {
				return errors.New("doctor file replacement exceeds bound")
			}
			for _, record := range evidence.Records {
				if kind+":"+record.ID == step.Target && record.Kind == kind && record.State == step.Action.Before {
					sourceIdentity = record.Identity
				}
			}
			if sourceIdentity == "" {
				return errors.New("doctor file target lacks captured identity")
			}
			if !strings.HasPrefix(step.Target, kind+":") || (len(step.Action.Before) != 64 && step.Action.Before != "absent") {
				return errors.New("invalid doctor file target")
			}
		default:
			if !DoctorRowRepairTarget(kind, step.Target, step.Action.Before, step.Action.After) && !DoctorTypedRowRepairTarget(kind, step.Target, step.Action.Before, step.Action.After) {
				return errors.New("unsupported or unsafe doctor row repair")
			}
			state, _, e := doctorRowSnapshot(ctx, tx, a.TaskRef, kind, step.Target)
			if e != nil {
				return e
			}
			if state != step.Action.Before {
				return errors.New("doctor row precondition already changed")
			}

		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO doctor_repair_steps(repair_id,number,kind,target,before_value,after_value,source_identity) VALUES(?,?,?,?,?,?,?)`, a.ID, step.Number, kind, step.Target, step.Action.Before, step.Action.After, sourceIdentity); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO doctor_repair_holds(task_ref,repair_id,generation) VALUES(?,?,?) ON CONFLICT(task_ref) DO UPDATE SET repair_id=excluded.repair_id,generation=excluded.generation,active=1 WHERE doctor_repair_holds.active=0`, a.TaskRef, a.ID, generation)
	if err != nil {
		return err
	}
	*out, err = readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, a.ID))
	out.NewlyReserved = err == nil
	return err
}

const doctorRepairColumns = `id,source_id,task_ref,state,failure,exclusion_id,conversation_id,generation,proposal_id,revision,fenced`

func readDoctorRepair(row scanner) (DoctorRepair, error) {
	var r DoctorRepair
	err := row.Scan(&r.ID, &r.SourceID, &r.TaskRef, &r.State, &r.Failure, &r.ExclusionID, &r.ConversationID, &r.Generation, &r.ProposalID, &r.Revision, &r.Fenced)
	return r, err
}

// ReadDoctorRepair returns the latest retained operation for the selected issue.
func (db *DB) ReadDoctorRepair(ctx context.Context, ref string) (DoctorRepair, error) {
	r, err := readDoctorRepair(db.sql.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE task_ref=? ORDER BY rowid DESC LIMIT 1`, ref))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// DoctorRepairSteps exposes the exact manifest and journal without private backups.
func (db *DB) DoctorRepairSteps(ctx context.Context, id string) ([]DoctorRepairStep, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT number,kind,target,before_value,after_value,state,backup_reference,backup_digest,observed_after,source_identity FROM doctor_repair_steps WHERE repair_id=? ORDER BY number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DoctorRepairStep
	for rows.Next() {
		var s DoctorRepairStep
		if err := rows.Scan(&s.Number, &s.Kind, &s.Target, &s.Before, &s.After, &s.State, &s.BackupReference, &s.BackupDigest, &s.ObservedAfter, &s.SourceIdentity); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DoctorRepairHeld is the normal writer/admission guard, independent of doctor locks.
func (db *DB) DoctorRepairHeld(ctx context.Context, ref string) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_holds WHERE task_ref=? AND active=1`, ref).Scan(&n)
	return n > 0, err
}

// DoctorProposalReference returns identity and bytes together for exact presentation binding.
func (db *DB) DoctorProposalReference(ctx context.Context, cid int64) (int64, []byte, error) {
	var id int64
	var data []byte
	err := db.sql.QueryRowContext(ctx, `SELECT p.id,p.payload FROM doctor_conversations c JOIN doctor_proposals p ON p.id=c.current_proposal_id WHERE c.id=?`, cid).Scan(&id, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	return id, data, err
}

// DoctorRepairHeldSession includes parent writers whose aggregate can touch a held child.
func (db *DB) DoctorRepairHeldSession(ctx context.Context, id int64) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_held_session_owners WHERE session_id=?`, id).Scan(&n)
	return n > 0, err
}

// DoctorRepairWriterSessions returns only local writer identities, not sibling evidence.
func (db *DB) DoctorRepairWriterSessions(ctx context.Context, ref string) ([]int64, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT v.session_id FROM doctor_held_session_owners v JOIN doctor_repair_holds h ON h.repair_id=v.repair_id WHERE h.task_ref=? AND h.active=1 ORDER BY v.session_id`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClaimDoctorRepairDispatch is a one-time writing-agent claim. A crash after
// claiming requires observation/review, never automatic agent replay.
func (db *DB) ClaimDoctorRepairDispatch(ctx context.Context, id, source string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := doctorRepairAuthority(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.SourceID != source || r.State != "reserved" {
			return errors.New("doctor repair already dispatched or source mismatched")
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='quiescing',revision=revision+1 WHERE id=? AND state='reserved'`, id)
		return err
	})
}

// CurrentDoctorLocalPresentation selects only a presentation of the exact active
// proposal; it does not present a proposal or authorize a different revision.
func (db *DB) CurrentDoctorLocalPresentation(ctx context.Context, ref string) (string, error) {
	var id string
	err := db.sql.QueryRowContext(ctx, `SELECT p.id FROM doctor_local_presentations p JOIN doctor_conversations c ON c.task_ref=p.task_ref AND c.current_proposal_id=p.proposal_id AND c.generation=p.generation WHERE c.task_ref=? AND c.state='active' ORDER BY p.rowid DESC LIMIT 1`, ref).Scan(&id)
	return id, err
}
