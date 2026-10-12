package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// LegacyApproverEligible fixes legacy membership at migration time, not connection provenance.
func (db *DB) LegacyApproverEligible(ctx context.Context, repoID int64) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_legacy_approver_eligibility WHERE repo_id=?`, repoID).Scan(&n)
	return n == 1, err
}

// SetApproverImportConflict preserves an actionable conflict independently of saved work.
func (db *DB) SetApproverImportConflict(ctx context.Context, repoID int64, reason string) error {
	return db.SetApproverInitialization(ctx, ApproverInitialization{RepoID: repoID, Status: "conflict", Reason: reason})
}

// ApproverImportConflict returns correction guidance for a legacy identity conflict.
func (db *DB) ApproverImportConflict(ctx context.Context, repoID int64) (string, error) {
	s, err := db.GetApproverInitialization(ctx, repoID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if s.Status == "conflict" {
		return s.Reason, nil
	}
	return "", nil
}

// LegacyApproverOwnerIDs reads historical ticket authority without modifying original records.
func (db *DB) LegacyApproverOwnerIDs(ctx context.Context, repoID int64) ([]int64, error) {
	return legacyApproverOwnerIDs(ctx, db.sql, repoID)
}
func legacyApproverOwnerIDs(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, repoID int64) ([]int64, error) {
	queries := []string{
		`SELECT actor_id FROM doctor_inbox WHERE repo_id=? AND operation IN ('diagnose','usage')`,
		`SELECT p.actor_id FROM owner_exceptions p JOIN sessions s ON s.task_ref=p.parent_task WHERE s.repo_id=?`,
		`SELECT p.actor_id FROM followup_proposals p JOIN sessions s ON s.task_ref=p.parent_task WHERE s.repo_id=? AND p.approval_comment>0`,
		`SELECT p.actor_id FROM story_refinements p JOIN sessions s ON s.id=p.session_id WHERE s.repo_id=?`,
		`SELECT COALESCE(c.author_user_id,0) FROM schedules p JOIN sessions s ON s.id=p.session_id LEFT JOIN comments c ON c.session_id=p.session_id AND c.forge_comment_id=p.source_comment_id WHERE s.repo_id=? AND p.source_comment_id>0`,

		`SELECT COALESCE(c.author_user_id,0) FROM comments c JOIN sessions s ON s.id=c.session_id WHERE s.repo_id=? AND c.classification='instruction' AND c.forge_comment_id>0`,
		`SELECT p.owner_id FROM bug_tasks p JOIN sessions s ON s.id=p.session_id WHERE s.repo_id=?`,
		`SELECT p.approval_owner_id FROM bug_analyses p JOIN sessions s ON s.id=p.session_id WHERE s.repo_id=? AND p.approval_owner_id IS NOT NULL`,
		`SELECT i.owner_id FROM mcp_creation_intents i JOIN sessions s ON s.task_ref=i.source_task WHERE s.repo_id=?`,
		`SELECT p.owner_id FROM creation_preparations p JOIN sessions s ON s.id=p.session_id WHERE s.repo_id=?`,
		`SELECT p.owner_id FROM story_contract_upgrades p JOIN sessions s ON s.task_ref=p.parent_task WHERE s.repo_id=?`,
		`SELECT w.reply_author_id FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=? AND w.reply_author_id IS NOT NULL`,
	}
	seen := map[int64]bool{}
	var out []int64
	for _, query := range queries {
		rows, err := q.QueryContext(ctx, query, repoID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	// Typed Plan authority snapshots contain owner IDs; keep their bytes untouched.
	rows, err := q.QueryContext(ctx, `SELECT a.authority_json FROM plan_story_authorities a JOIN sessions s ON s.task_ref=a.parent WHERE s.repo_id=?`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v any
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("store: legacy Plan authority cannot be reconciled: %w", err)
		}
		collectOwnerIDs(v, seen, &out)
	}
	return out, rows.Err()
}
func collectOwnerIDs(v any, seen map[int64]bool, out *[]int64) {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "owner_id" || key == "OwnerID" {
				var id int64
				switch n := child.(type) {
				case float64:
					id = int64(n)
				case string:
					id, _ = strconv.ParseInt(n, 10, 64)
				}
				if !seen[id] {
					seen[id] = true
					*out = append(*out, id)
				}
			} else {
				collectOwnerIDs(child, seen, out)
			}
		}
	case []any:
		for _, child := range value {
			collectOwnerIDs(child, seen, out)
		}
	}
}

// LegacyApproverIdentity returns the authenticated owner saved before init can refresh global identities.
func (db *DB) LegacyApproverIdentity(ctx context.Context, repoID int64) (Identity, error) {
	var id Identity
	var at string
	err := db.sql.QueryRowContext(ctx, `SELECT forge,role,handle,COALESCE(user_id,0),source,derived_at FROM project_legacy_approver_identities WHERE repo_id=?`, repoID).Scan(&id.Forge, &id.Role, &id.Handle, &id.UserID, &id.Source, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return id, ErrNotFound
	}
	if err != nil {
		return id, err
	}
	id.DerivedAt, err = ParseTime(at)
	return id, err
}

// ReconcileLegacyApproverEvidence attaches the frozen initial authority to
// existing owner records, never rewriting their payload, candidate or effect bytes.
// The caller has verified the immutable historical account against GitHub.
func (db *DB) ReconcileLegacyApproverEvidence(ctx context.Context, a ApproverAppointment) error {
	if a.Provenance != "legacy" {
		return nil
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := rawCurrentApprover(ctx, tx, a.RepoID)
		if err != nil {
			return err
		}
		if current.ID != a.ID || !current.Actor.Equal(a.Actor) {
			return ErrApproverConflict
		}
		if e := checkLegacyOutstandingAttribution(ctx, tx, a.RepoID, a.Actor.RemoteID); e != nil {
			return e
		}
		ownerIDs, e := legacyApproverOwnerIDs(ctx, tx, a.RepoID)
		if e != nil {
			return e
		}
		for _, id := range ownerIDs {
			if id > 0 && strconv.FormatInt(id, 10) != a.Actor.RemoteID {
				return fmt.Errorf("%w: historical approval actor conflicts with initial appointment", ErrApproverUnauthorized)
			}
		}
		queries := []struct{ kind, sql string }{
			{"doctor_instruction", `SELECT CAST(id AS TEXT) FROM doctor_inbox WHERE repo_id=? AND operation IN ('diagnose','usage')`},
			{"doctor_effect", `SELECT d.delivery_key FROM report_deliveries d JOIN doctor_inbox i ON i.id=d.doctor_command_id WHERE i.repo_id=?`},
			{"owner_exception", `SELECT CAST(w.id AS TEXT) FROM owner_exceptions w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=?`},
			{"followup_approval", `SELECT w.proposal_key FROM followup_proposals w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=? AND w.approval_comment>0`},
			{"story_refinement", `SELECT CAST(w.comment_id AS TEXT) FROM story_refinements w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=?`},
			{"story_progress_effect", `SELECT CAST(w.id AS TEXT) FROM story_progress_effects w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=?`},
			{"acceptance_decision", `SELECT CAST(w.id AS TEXT) FROM acceptance_decisions w JOIN sessions s ON s.task_ref=w.parent_task WHERE s.repo_id=?`},

			{"session", `SELECT CAST(id AS TEXT) FROM sessions WHERE repo_id=?`},
			{"run", `SELECT CAST(r.id AS TEXT) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.repo_id=?`},
			{"owner_wait", `SELECT w.delivery_key FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=?`},
			{"plan_authority", `SELECT w.parent||':'||w.request_id FROM plan_story_authorities w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=?`},
			{"plan_state", `SELECT w.parent FROM plan_workflow_states w JOIN sessions s ON s.task_ref=w.parent WHERE s.repo_id=?`},
			{"schedule", `SELECT CAST(w.id AS TEXT) FROM schedules w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=?`},
			{"continuation", `SELECT CAST(w.id AS TEXT) FROM automatic_continuations w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=?`},
			{"report_effect", `SELECT w.delivery_key FROM report_deliveries w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=?`},
			{"creation_intent", `SELECT CAST(w.id AS TEXT) FROM mcp_creation_intents w JOIN sessions s ON s.task_ref=w.source_task WHERE s.repo_id=?`},
			{"graph_effect", `SELECT w.operation FROM graph_effects w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE s.repo_id=?`},
			{"bug_analysis_approval", `SELECT w.invocation FROM bug_analyses w JOIN sessions s ON s.id=w.session_id WHERE s.repo_id=? AND w.approval_owner_id IS NOT NULL`},
		}
		for _, q := range queries {
			rows, e := tx.QueryContext(ctx, q.sql, a.RepoID)
			if e != nil {
				return e
			}
			var keys []string
			for rows.Next() {
				var key string
				if e = rows.Scan(&key); e != nil {
					_ = rows.Close()
					return e
				}
				keys = append(keys, key)
			}
			e = rows.Err()
			_ = rows.Close()
			if e != nil {
				return e
			}
			for _, key := range keys {
				actor, e := legacyRecordActor(ctx, tx, q.kind, key)
				if e != nil {
					return e
				}
				if actor <= 0 {
					continue
				}
				if strconv.FormatInt(actor, 10) != a.Actor.RemoteID {
					return fmt.Errorf("%w: original source actor differs for %s %s", ErrApproverUnauthorized, q.kind, key)
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO project_legacy_authority_evidence(repo_id,appointment_id,record_kind,record_key,actor_provider,actor_origin,actor_id,provenance) VALUES(?,?,?,?,?,?,?,'authenticated-legacy-owner') ON CONFLICT(repo_id,record_kind,record_key) DO NOTHING`, a.RepoID, a.ID, q.kind, key, a.Actor.Provider, a.Actor.Origin, a.Actor.RemoteID)
				if e != nil {
					return e
				}
			}
		}
		return nil
	})
}
