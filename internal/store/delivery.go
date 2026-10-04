package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DeliveryProposal binds reviewable content and explicit authority to one head.
// Each remote intent is durable independently; confirmed integration is a fact.
type DeliveryProposal struct {
	Key, Parent, Branch, Head, Target, Readiness, Title, Body                                               string
	Generation, PRNumber                                                                                    int
	ParentID, PRID, ApprovalComment, ActorID                                                                int64
	CreateAttempted, Proposed, MergeAttempted, MergeConfirmed, CloseAttempted, Closed, Reported, Superseded bool
	MergeSHA, Diagnostic                                                                                    string
}

const deliveryColumns = `proposal_key,parent_task,generation,parent_id,branch,head,target,readiness,title,body,pr_id,pr_number,create_attempted,proposed,approval_comment,actor_id,merge_attempted,merge_confirmed,merge_sha,close_attempted,closed,reported,superseded,diagnostic`

func scanDelivery(s interface{ Scan(...any) error }) (p DeliveryProposal, err error) {
	err = s.Scan(&p.Key, &p.Parent, &p.Generation, &p.ParentID, &p.Branch, &p.Head, &p.Target, &p.Readiness, &p.Title, &p.Body, &p.PRID, &p.PRNumber, &p.CreateAttempted, &p.Proposed, &p.ApprovalComment, &p.ActorID, &p.MergeAttempted, &p.MergeConfirmed, &p.MergeSHA, &p.CloseAttempted, &p.Closed, &p.Reported, &p.Superseded, &p.Diagnostic)
	return
}

// GetDelivery loads immutable proposal content and its current effect debt.
func (db *DB) GetDelivery(ctx context.Context, key string) (DeliveryProposal, error) {
	p, err := scanDelivery(db.sql.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM delivery_proposals WHERE proposal_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

// RetainDelivery persists complete proposal content before requesting approval.
func (db *DB) RetainDelivery(ctx context.Context, p DeliveryProposal) error {
	if p.Key == "" || p.Parent == "" || p.Generation < 1 || p.ParentID < 1 || p.Branch == "" || p.Head == "" || p.Target != "main" || p.Readiness == "" || p.Title == "" || p.Body == "" {
		return fmt.Errorf("incomplete delivery proposal")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO delivery_proposals(proposal_key,parent_task,generation,parent_id,branch,head,target,readiness,title,body) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(proposal_key) DO NOTHING`, p.Key, p.Parent, p.Generation, p.ParentID, p.Branch, p.Head, p.Target, p.Readiness, p.Title, p.Body)
	if err != nil {
		return err
	}
	old, err := db.GetDelivery(ctx, p.Key)
	if err != nil {
		return err
	}
	if old.Parent != p.Parent || old.Generation != p.Generation || old.ParentID != p.ParentID || old.Branch != p.Branch || old.Head != p.Head || old.Target != p.Target || old.Readiness != p.Readiness || old.Title != p.Title || old.Body != p.Body {
		return fmt.Errorf("immutable delivery content conflict")
	}
	return nil
}

// ApproveDelivery records authenticated approval exactly once for a concrete proposal.
func (db *DB) ApproveDelivery(ctx context.Context, key string, comment, actor int64) error {
	if comment < 1 || actor < 1 {
		return fmt.Errorf("invalid delivery approval identity")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM delivery_proposals WHERE proposal_key=?`, key))
		if err != nil {
			return err
		}
		if !p.Proposed || p.Superseded || p.PRNumber < 1 || p.PRID < 1 {
			return fmt.Errorf("delivery is not an active concrete proposal")
		}
		if p.ApprovalComment > 0 {
			if p.ActorID != actor {
				return fmt.Errorf("delivery approval owner changed")
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE delivery_proposals SET approval_comment=?,actor_id=? WHERE proposal_key=?`, comment, actor, key)
		return err
	})
}

// SaveDeliveryEffects changes only external effect debt, never content or approval.
func (db *DB) SaveDeliveryEffects(ctx context.Context, p DeliveryProposal) error {
	if p.Closed && (!p.MergeConfirmed || p.MergeSHA == "") {
		return fmt.Errorf("closed delivery lacks confirmed integration")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE delivery_proposals SET pr_id=?,pr_number=?,create_attempted=?,proposed=?,merge_attempted=?,merge_confirmed=?,merge_sha=?,close_attempted=?,closed=?,reported=?,superseded=?,diagnostic=? WHERE proposal_key=?`, p.PRID, p.PRNumber, p.CreateAttempted, p.Proposed, p.MergeAttempted, p.MergeConfirmed, p.MergeSHA, p.CloseAttempted, p.Closed, p.Reported, p.Superseded, p.Diagnostic, p.Key)
		if err != nil {
			return err
		}
		if p.Closed {
			_, err = tx.ExecContext(ctx, `UPDATE managed_features SET state='completed' WHERE parent_task=? AND generation=? AND workflow_version=2 AND EXISTS(SELECT 1 FROM delivery_proposals WHERE proposal_key=? AND closed=1 AND merge_confirmed=1 AND approval_comment>0 AND actor_id>0)`, p.Parent, p.Generation, p.Key)
		}
		return err
	})
}

// ListDeliveries lists retained proposals for one parent in creation order.
func (db *DB) ListDeliveries(ctx context.Context, parent string) ([]DeliveryProposal, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM delivery_proposals WHERE parent_task=? ORDER BY rowid`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryProposal
	for rows.Next() {
		p, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListDeliveryParents discovers ready features and unfinished remote delivery effects.
// A ready feature whose parent was observed closed is left out; its unfinished
// delivery effects are still returned by the second branch.
func (db *DB) ListDeliveryParents(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT parent_task FROM managed_features WHERE workflow_version=2 AND engineering_succeeded=1 AND NOT EXISTS(SELECT 1 FROM delivery_proposals d WHERE d.parent_task=managed_features.parent_task AND d.closed=1 AND d.reported=1) AND parent_task NOT IN (SELECT parent_task FROM managed_parent_closures) UNION SELECT parent_task FROM delivery_proposals WHERE superseded=0 AND reported=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return nil, err
		}
		out = append(out, parent)
	}
	return out, rows.Err()
}

// RetainDeliveryReadiness stores full exact blockers/exceptions independently of
// the bounded public summary, including readiness that cannot yet produce a PR.
func (db *DB) RetainDeliveryReadiness(ctx context.Context, key, parent string, generation int, full string) error {
	if key == "" || parent == "" || generation < 1 || full == "" {
		return fmt.Errorf("invalid readiness record")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO delivery_readiness(record_key,parent_task,generation,full_report) VALUES(?,?,?,?) ON CONFLICT(record_key) DO NOTHING`, key, parent, generation, full)
	if err != nil {
		return err
	}
	var oldParent, oldFull string
	var oldGen int
	err = db.sql.QueryRowContext(ctx, `SELECT parent_task,generation,full_report FROM delivery_readiness WHERE record_key=?`, key).Scan(&oldParent, &oldGen, &oldFull)
	if err != nil {
		return err
	}
	if oldParent != parent || oldGen != generation || oldFull != full {
		return fmt.Errorf("immutable readiness record conflict")
	}
	return nil
}

// GetDeliveryReadiness retrieves the full retained factual readiness report.
func (db *DB) GetDeliveryReadiness(ctx context.Context, key string) (string, error) {
	var full string
	err := db.sql.QueryRowContext(ctx, `SELECT full_report FROM delivery_readiness WHERE record_key=?`, key).Scan(&full)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return full, err
}
