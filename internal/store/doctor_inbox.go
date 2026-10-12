package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// DoctorScan holds a fixed discovery window independently of ordinary polling.
type DoctorScan struct {
	RepoID                              int64
	ActivatedAt, CompletedAt, WindowEnd time.Time
	Page                                int
}

// DoctorScanWindow initializes activation or resumes the fixed unfinished window.
func (db *DB) DoctorScanWindow(ctx context.Context, repoID int64, at time.Time) (DoctorScan, error) {
	var s DoctorScan
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		stamp := FormatTime(at)
		if _, err := tx.ExecContext(ctx, `INSERT INTO doctor_scans(repo_id,activated_at,completed_at,window_end) VALUES(?,?,?,?) ON CONFLICT(repo_id) DO NOTHING`, repoID, stamp, stamp, stamp); err != nil {
			return err
		}
		var activated, completed, end string
		if err := tx.QueryRowContext(ctx, `SELECT repo_id,activated_at,completed_at,window_end,page FROM doctor_scans WHERE repo_id=?`, repoID).Scan(&s.RepoID, &activated, &completed, &end, &s.Page); err != nil {
			return err
		}
		var err error
		s.ActivatedAt, err = ParseTime(activated)
		if err != nil {
			return err
		}
		s.CompletedAt, err = ParseTime(completed)
		if err != nil {
			return err
		}
		s.WindowEnd, err = ParseTime(end)
		if err != nil {
			return err
		}
		if s.WindowEnd.Equal(s.CompletedAt) && at.After(s.CompletedAt) {
			s.WindowEnd = at
			s.Page = 1
			_, err = tx.ExecContext(ctx, `UPDATE doctor_scans SET window_end=?,page=1 WHERE repo_id=?`, stamp, repoID)
		}
		return err
	})
	return s, err
}

// AdvanceDoctorScan happens after every eligible input on the page is durable.
func (db *DB) AdvanceDoctorScan(ctx context.Context, s DoctorScan, next int) error {
	if next < 0 || (next > 0 && next <= s.Page) {
		return errors.New("invalid doctor discovery page")
	}
	var result sql.Result
	var err error
	if next == 0 {
		result, err = db.sql.ExecContext(ctx, `UPDATE doctor_scans SET completed_at=window_end,page=1 WHERE repo_id=? AND window_end=? AND page=?`, s.RepoID, FormatTime(s.WindowEnd), s.Page)
	} else {
		result, err = db.sql.ExecContext(ctx, `UPDATE doctor_scans SET page=? WHERE repo_id=? AND window_end=? AND page=?`, next, s.RepoID, FormatTime(s.WindowEnd), s.Page)
	}
	return doctorAffected(result, err)
}

// DoctorCommand is an immutable authenticated source and durable dispatch state.
type DoctorCommand struct {
	Source                                  *DoctorCommandSource
	RepairApprovalID                        string // factory-only admission input; empty leaves apply unavailable
	ID, RepoID, IssueID, CommentID, ActorID int64
	TaskRef                                 string
	IssueNumber                             int
	CreatedAt                               time.Time
	Operation, Instructions, State          string
	Attempts                                int
	ResultBody                              string
	ConversationID, ProposalID              int64
}

const doctorCommandColumns = `id,repo_id,task_ref,issue_id,issue_number,comment_id,actor_id,created_at,operation,instructions,state,attempts,result_body,COALESCE(conversation_id,0),COALESCE(proposal_id,0)`

func scanDoctorCommand(row scanner) (DoctorCommand, error) {
	var c DoctorCommand
	var created string
	err := row.Scan(&c.ID, &c.RepoID, &c.TaskRef, &c.IssueID, &c.IssueNumber, &c.CommentID, &c.ActorID, &created, &c.Operation, &c.Instructions, &c.State, &c.Attempts, &c.ResultBody, &c.ConversationID, &c.ProposalID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.CreatedAt, err = ParseTime(created)
	return c, err
}

// SourceID binds one invocation attempt to its immutable admitted command.
func (c DoctorCommand) SourceID() string {
	return "github-doctor:" + strconv.FormatInt(c.ID, 10) + ":" + strconv.Itoa(c.Attempts)
}

// AdmitDoctorCommand deduplicates before dispatch. Stop atomically fences the
// conversation and queued/claimed earlier input, including delayed discovery.
func (db *DB) AdmitDoctorCommand(ctx context.Context, c DoctorCommand) (DoctorCommand, error) {
	ref, err := naming.ParseRef(c.TaskRef)
	if err != nil {
		return c, err
	}
	if c.RepoID <= 0 || c.IssueID <= 0 || c.CommentID <= 0 || c.ActorID <= 0 || c.CreatedAt.IsZero() || c.IssueNumber != ref.Issue || len(c.Instructions) > 32768 {
		return c, errors.New("invalid doctor command")
	}
	switch c.Operation {
	case "diagnose", "stop", "apply", "usage":
	default:
		return c, errors.New("invalid doctor operation")
	}
	var out DoctorCommand
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanDoctorCommand(tx.QueryRowContext(ctx, `SELECT `+doctorCommandColumns+` FROM doctor_inbox WHERE repo_id=? AND comment_id=?`, c.RepoID, c.CommentID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var owner, name, forge string
		if err := tx.QueryRowContext(ctx, `SELECT owner,name,forge FROM repos WHERE id=?`, c.RepoID).Scan(&owner, &name, &forge); err != nil {
			return err
		}
		matches := strings.EqualFold(owner, ref.Owner) && strings.EqualFold(name, ref.Repo) && forge == ref.Forge
		tickets, connectionErr := currentConnection(ctx, tx, c.RepoID, TicketConnection)
		if connectionErr == nil {
			matches = string(tickets.Provider) == ref.Forge && strings.EqualFold(tickets.Destination, ref.Owner+"/"+ref.Repo)
		} else if !errors.Is(connectionErr, ErrNotFound) {
			return connectionErr
		}
		if !matches {
			var retained int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions s JOIN session_ticket_identities t ON t.session_id=s.id WHERE s.repo_id=? AND t.provider=? AND lower(t.container)=lower(?) AND t.locator_key=?`, c.RepoID, ref.Forge, ref.Owner+"/"+ref.Repo, strconv.Itoa(ref.Issue)).Scan(&retained); err != nil {
				return err
			}
			matches = retained > 0
		}
		if !matches {
			return errors.New("foreign doctor ticket project")
		}

		state := "queued"
		var laterStop int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_inbox WHERE task_ref=? AND operation='stop' AND comment_id>?`, c.TaskRef, c.CommentID).Scan(&laterStop); err != nil {
			return err
		}
		if laterStop > 0 {
			state = "stopped"
		}
		out, err = scanDoctorCommand(tx.QueryRowContext(ctx, `INSERT INTO doctor_inbox(repo_id,task_ref,issue_id,issue_number,comment_id,actor_id,created_at,operation,instructions,state) VALUES(?,?,?,?,?,?,?,?,?,?) RETURNING `+doctorCommandColumns, c.RepoID, c.TaskRef, c.IssueID, c.IssueNumber, c.CommentID, c.ActorID, FormatTime(c.CreatedAt), c.Operation, c.Instructions, state))
		if err != nil {
			return err
		}
		if c.Source != nil {
			if err = retainDoctorSource(ctx, tx, out, *c.Source); err != nil {
				return err
			}
		}
		if c.Operation == "apply" && state != "stopped" && c.RepairApprovalID != "" {
			// A still-active repair holds the task's unique slot; admit without
			// reserving so the control path answers this apply once.
			var active int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repairs WHERE task_ref=? AND state NOT IN ('recovered','stopped','completed','failed')`, c.TaskRef).Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return nil
			}
			var repair DoctorRepair
			approval := DoctorApproval{ID: c.RepairApprovalID, SourceID: "github-doctor-apply:" + strconv.FormatInt(out.ID, 10), Principal: "github-owner:" + strconv.FormatInt(c.ActorID, 10), TaskRef: c.TaskRef, InboxID: out.ID}
			if err := reserveDoctorRepairTx(ctx, tx, approval, &repair); err != nil {
				return err
			}
		}

		if c.Operation == "stop" && state != "stopped" {
			stop, err := requestOwnerStopTx(ctx, tx, c.TaskRef, out.ID)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(stop)
			if err != nil {
				return err
			}
			out.ResultBody = string(raw)
			if _, err := tx.ExecContext(ctx, `UPDATE doctor_inbox SET result_body=? WHERE id=?`, out.ResultBody, out.ID); err != nil {
				return err
			}

			if _, err := tx.ExecContext(ctx, `UPDATE doctor_inbox SET state='stopped' WHERE task_ref=? AND id<? AND state IN ('queued','claimed')`, c.TaskRef, out.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE doctor_attempts SET state='interrupted',failure='Stopped by owner',finished_at=? WHERE state='running' AND conversation_id IN (SELECT id FROM doctor_conversations WHERE task_ref=? AND state='active')`, FormatTime(nowUTC()), c.TaskRef); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE doctor_conversations SET state='stopped',revision=revision+1,updated_at=? WHERE task_ref=? AND state='active'`, FormatTime(nowUTC()), c.TaskRef); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// validateDoctorSourceTx prevents a claimed-but-delayed worker reopening a stop.
func validateDoctorSourceTx(ctx context.Context, tx *sql.Tx, source, ref string) error {
	if !strings.HasPrefix(source, "github-doctor:") {
		return nil
	}
	bits := strings.Split(source, ":")
	if len(bits) != 3 {
		return ErrDoctorFenced
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_inbox WHERE id=? AND attempts=? AND task_ref=? AND state='claimed'`, bits[1], bits[2], ref).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrDoctorFenced
	}
	return nil
}

// ClaimDoctorCommand is one bounded fair selection. Per-issue admission order is
// preserved; the last task rotates behind all other queued issues.
func (db *DB) ClaimDoctorCommand(ctx context.Context, lastTask string, control bool, instance string) (DoctorCommand, error) {
	var out DoctorCommand
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		op := "(operation='diagnose' OR (operation='apply' AND EXISTS(SELECT 1 FROM doctor_repairs r WHERE r.source_id='github-doctor-apply:'||d.id)))"
		if control {
			op = "operation<>'diagnose' AND NOT (operation='apply' AND EXISTS(SELECT 1 FROM doctor_repairs r WHERE r.source_id='github-doctor-apply:'||d.id))"
		}
		c, err := scanDoctorCommand(tx.QueryRowContext(ctx, `SELECT `+doctorCommandColumns+` FROM doctor_inbox d WHERE state='queued' AND `+op+` AND (operation='stop' OR NOT EXISTS(SELECT 1 FROM doctor_inbox p WHERE p.task_ref=d.task_ref AND p.id<d.id AND p.state IN ('queued','claimed'))) ORDER BY task_ref=?,id LIMIT 1`, lastTask))
		if err != nil {
			return err
		}
		if c.Attempts >= 2 {
			return errors.New("doctor recovery budget exhausted")
		}
		out, err = scanDoctorCommand(tx.QueryRowContext(ctx, `UPDATE doctor_inbox SET state='claimed',attempts=attempts+1,claim_owner=? WHERE id=? AND state='queued' RETURNING `+doctorCommandColumns, instance, c.ID))
		return err
	})
	return out, err
}

// RequeueDoctorBusy retains admission order without spending the recovery budget.
func (db *DB) RequeueDoctorBusy(ctx context.Context, id int64) error {
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_inbox SET state='queued',attempts=attempts-1 WHERE id=? AND state='claimed'`, id)
	return doctorAffected(r, err)
}

// CompleteDoctorCommand retains the outcome before any posting. A fenced stopped command
// cannot overwrite an active generation or its result.
func (db *DB) CompleteDoctorCommand(ctx context.Context, id int64, body string, conversationID int64) error {
	if body == "" || len(body) > 262144 {
		return errors.New("doctor report exceeds bounds")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_inbox SET state='completed',result_body=?,conversation_id=?,proposal_id=(SELECT p.id FROM doctor_proposals p JOIN doctor_attempts a ON a.id=p.attempt_id WHERE a.source_id=('github-doctor:'||doctor_inbox.id||':'||doctor_inbox.attempts) ORDER BY p.id DESC LIMIT 1) WHERE id=? AND state='claimed'`, body, nullID(conversationID), id)
	return doctorAffected(r, err)
}

// DoctorCommandsNeedingReports lists bounded completed outcomes lacking presentation.
func (db *DB) DoctorCommandsNeedingReports(ctx context.Context) ([]DoctorCommand, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+doctorCommandColumns+` FROM doctor_inbox d WHERE state='completed' AND result_body<>'' AND NOT EXISTS(SELECT 1 FROM doctor_presentations p WHERE p.command_id=d.id) ORDER BY id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DoctorCommand
	for rows.Next() {
		c, err := scanDoctorCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecoverDoctorCommands lists abandoned claims for daemon-owned reconciliation;
// it does not relaunch or alter ordinary workflow attempts.
func (db *DB) RecoverDoctorCommands(ctx context.Context, instance string) ([]DoctorCommand, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+doctorCommandColumns+` FROM doctor_inbox WHERE state='claimed' AND claim_owner<>? ORDER BY id LIMIT 100`, instance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DoctorCommand
	for rows.Next() {
		c, err := scanDoctorCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RetryDoctorCommand permits at most one recovery attempt for an abandoned input.
func (db *DB) RetryDoctorCommand(ctx context.Context, id int64) error {
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_inbox SET state='queued' WHERE id=? AND state='claimed' AND attempts<2`, id)
	return doctorAffected(r, err)
}
