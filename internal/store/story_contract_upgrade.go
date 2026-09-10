package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ContractUpgradeItem retains exact observed and intended bytes for one existing
// unfinished issue. It never carries authority to create a new issue.
type ContractUpgradeItem struct {
	Story          string          `json:"story"`
	IssueID        int64           `json:"issue_id"`
	IssueNumber    int             `json:"issue_number"`
	Title          string          `json:"title"`
	SourceBody     string          `json:"source_body"`
	SourceRevision string          `json:"source_revision"`
	Body           string          `json:"body"`
	Revision       string          `json:"revision"`
	Content        json.RawMessage `json:"content"`
	Criteria       []string        `json:"criteria"`
	Dependencies   []string        `json:"dependencies"`
}

// ContractUpgrade is durable owner-approved publication intent. Its immutable
// cohort survives interrupted applies; confirmations are separate from intent.
type ContractUpgrade struct {
	Parent           string                `json:"parent"`
	SourceGeneration int                   `json:"source_generation"`
	TargetGeneration int                   `json:"target_generation"`
	ScopeHash        string                `json:"scope_hash"`
	OwnerID          int64                 `json:"owner_id"`
	State            string                `json:"state"`
	Items            []ContractUpgradeItem `json:"items"`
}

// GetContractUpgrade reads retained intent without advancing publication.
func (db *DB) GetContractUpgrade(ctx context.Context, parent string) (ContractUpgrade, error) {
	var installed int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='story_contract_upgrades'`).Scan(&installed); err != nil {
		return ContractUpgrade{}, err
	}
	if installed == 0 {
		return ContractUpgrade{}, ErrNotFound
	}
	var data []byte
	var state string
	err := db.sql.QueryRowContext(ctx, `SELECT payload,state FROM story_contract_upgrades WHERE parent_task=?`, parent).Scan(&data, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ContractUpgrade{}, ErrNotFound
	}
	if err != nil {
		return ContractUpgrade{}, err
	}
	var out ContractUpgrade
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	out.State = state
	return out, nil
}

// ContractUpgradeSafety explains conditions that forbid an upgrade. This is a
// read-only preview check; BeginContractUpgrade repeats it inside its transaction.
func (db *DB) ContractUpgradeSafety(ctx context.Context, parent string, generation int) error {
	return contractUpgradeSafety(ctx, db.sql, parent, generation)
}

type upgradeQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func contractUpgradeSafety(ctx context.Context, q upgradeQuery, parent string, generation int) error {
	var state string
	var current, content, submission, active, pending int
	err := q.QueryRowContext(ctx, `SELECT state,generation,content_version,submission_version,
 (SELECT count(*) FROM sessions s WHERE s.task_ref=f.parent_task AND (s.state='running' OR EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status='running')))+(SELECT count(*) FROM story_progress_runs p WHERE p.parent_task=f.parent_task AND p.active=1),
 (SELECT count(*) FROM story_progress_effects e WHERE e.parent_task=f.parent_task AND e.delivered=0 AND e.superseded=0)
 FROM managed_features f WHERE parent_task=?`, parent).Scan(&state, &current, &content, &submission, &active, &pending)
	if err != nil {
		return fmt.Errorf("upgrade feature unavailable: %w", err)
	}
	if current != generation {
		return fmt.Errorf("upgrade source generation changed")
	}
	if state != "planning" {
		return fmt.Errorf("upgrade requires a planning scope; approved or frozen requirements remain unchanged")
	}
	if content != 1 || submission != 1 {
		return fmt.Errorf("feature is not a legacy content/submission contract")
	}
	if active != 0 {
		return fmt.Errorf("upgrade refused while a parent run or story attempt is active")
	}
	if pending != 0 {
		return fmt.Errorf("upgrade waits for pending legacy effects to settle")
	}
	return nil
}

// BeginContractUpgrade records one exact cohort atomically and invalidates
// readiness. Existing pending intent is immutable, including on retry.
func (db *DB) BeginContractUpgrade(ctx context.Context, u ContractUpgrade) error {
	if u.Parent == "" || u.OwnerID <= 0 || u.TargetGeneration != u.SourceGeneration+1 || len(u.Items) == 0 || len(u.Items) > 100 {
		return fmt.Errorf("invalid upgrade intent")
	}
	u.State = "pending"
	canonical := u
	canonical.ScopeHash = ""
	data, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	if submissionDigest(data) != u.ScopeHash {
		return fmt.Errorf("upgrade scope digest differs")
	}
	payload, err := json.Marshal(u)
	if err != nil {
		return err
	}
	if len(payload) > 8<<20 {
		return fmt.Errorf("upgrade intent exceeds transport budget")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT scope_hash FROM story_contract_upgrades WHERE parent_task=?`, u.Parent).Scan(&existing)
		if err == nil {
			if existing != u.ScopeHash {
				return fmt.Errorf("another revision-bound upgrade already exists; resume its retained scope")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := contractUpgradeSafety(ctx, tx, u.Parent, u.SourceGeneration); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM managed_stories WHERE parent_task=? AND generation=? AND state!='completed'`, u.Parent, u.SourceGeneration).Scan(&count); err != nil {
			return err
		}
		if count != len(u.Items) {
			return fmt.Errorf("upgrade must include the whole unfinished cohort")
		}
		seen := map[string]bool{}
		for _, item := range u.Items {
			if seen[item.Story] || item.SourceRevision != submissionDigest([]byte(item.Title+"\x00"+item.SourceBody)) || item.Revision != submissionDigest([]byte(item.Title+"\x00"+item.Body)) || !json.Valid(item.Content) {
				return fmt.Errorf("invalid upgrade item revision")
			}
			seen[item.Story] = true
			var id int64
			var number int
			var state string
			if err := tx.QueryRowContext(ctx, `SELECT issue_id,issue_number,state FROM managed_stories WHERE parent_task=? AND generation=? AND story_key=?`, u.Parent, u.SourceGeneration, item.Story).Scan(&id, &number, &state); err != nil {
				return err
			}
			if id != item.IssueID || number != item.IssueNumber || state != "planning" {
				return fmt.Errorf("upgrade story identity or active scope changed")
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_features SET ready=0 WHERE parent_task=?`, u.Parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO story_contract_upgrades VALUES(?,?,?,?,?,'pending',?)`, u.Parent, u.SourceGeneration, u.TargetGeneration, u.ScopeHash, u.OwnerID, payload); err != nil {
			return err
		}
		for _, item := range u.Items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO story_contract_upgrade_items(parent_task,story_key) VALUES(?,?)`, u.Parent, item.Story); err != nil {
				return err
			}
		}
		return nil
	})
}

// ConfirmContractUpgradeItem records a verified read of the exact desired bytes.
func (db *DB) ConfirmContractUpgradeItem(ctx context.Context, parent, scopeHash, story string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_contract_upgrade_items SET confirmed=1 WHERE parent_task=? AND story_key=? AND EXISTS(SELECT 1 FROM story_contract_upgrades u WHERE u.parent_task=? AND u.scope_hash=? AND u.state='pending')`, parent, story, parent, scopeHash)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("upgrade intent changed")
	}
	return nil
}

// FinishContractUpgrade selects a new, unapproved typed generation only after
// all issue bytes are confirmed. Historical generations and workflow stay intact.
func (db *DB) FinishContractUpgrade(ctx context.Context, parent, scopeHash string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var payload []byte
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT payload,state FROM story_contract_upgrades WHERE parent_task=? AND scope_hash=?`, parent, scopeHash).Scan(&payload, &state); err != nil {
			return err
		}
		if state == "completed" {
			return nil
		}
		var u ContractUpgrade
		if err := json.Unmarshal(payload, &u); err != nil {
			return err
		}
		var missing int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM story_contract_upgrade_items WHERE parent_task=? AND confirmed=0`, parent).Scan(&missing); err != nil {
			return err
		}
		if missing != 0 {
			return fmt.Errorf("upgrade publication is still pending")
		}
		if err := contractUpgradeSafety(ctx, tx, parent, u.SourceGeneration); err != nil {
			return err
		}
		// Mark settled inside this transaction before updating guarded rows. Other
		// connections see either pending intent or the fully selected generation.
		if _, err := tx.ExecContext(ctx, `UPDATE story_contract_upgrades SET state='completed' WHERE parent_task=?`, parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO submission_generations VALUES(?,?,1,1)`, parent, u.SourceGeneration); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO submission_generations VALUES(?,?,2,2)`, parent, u.TargetGeneration); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_features SET generation=?,state='planning',ready=0,engineering_succeeded=0,content_version=2,submission_version=2 WHERE parent_task=?`, u.TargetGeneration, parent); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_stories(parent_task,generation,story_key,issue_number,issue_id,body_hash,dependencies,state) SELECT parent_task,?,story_key,issue_number,issue_id,body_hash,dependencies,state FROM managed_stories WHERE parent_task=? AND generation=?`, u.TargetGeneration, parent, u.SourceGeneration); err != nil {
			return err
		}
		for _, item := range u.Items {
			deps, err := json.Marshal(item.Dependencies)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE managed_stories SET body_hash=?,dependencies=?,engineering_complete=0 WHERE parent_task=? AND generation=? AND story_key=?`, item.Revision, deps, parent, u.TargetGeneration, item.Story); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO submission_projections VALUES(?,?,?,?,?,2,?,?,?)`, parent, u.TargetGeneration, item.Story, item.IssueID, item.Revision, item.Title, item.Body, []byte(item.Content)); err != nil {
				return err
			}
			for _, id := range item.Criteria {
				if !submissionCriterionID.MatchString(id) {
					return fmt.Errorf("invalid upgraded criterion")
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO submission_criterion_history(parent_task,story_key,criterion_id,first_revision) VALUES(?,?,?,?)`, parent, item.Story, id, item.Revision); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
