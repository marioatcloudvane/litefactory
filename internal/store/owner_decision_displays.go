package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OwnerDecisionDisplay is a published exact target. Historical rows are retained
// so a delayed short reply can never silently approve a replacement.
type OwnerDecisionDisplay struct {
	ID, InstanceID, PublicationID                                              int64
	TaskRef, Action, Reference, Target, Revision, TestedCommit, DeliveryCommit string
	CreatedAt                                                                  time.Time
	Resolved, RequiresReference                                                bool
}

// OwnerDecisionReference is deterministic across retries of the same display.
func OwnerDecisionReference(task, action, target, revision string) string {
	return fmt.Sprintf("d-%x", sha256.Sum256([]byte(task+"\x00"+action+"\x00"+target+"\x00"+revision)))[:12]
}

// PutOwnerDecisionDisplay retains only confirmed publications, without replacing history.
func (db *DB) PutOwnerDecisionDisplay(ctx context.Context, d OwnerDecisionDisplay) error {
	if d.TaskRef == "" || d.Action == "" || d.Target == "" || d.Revision == "" || d.PublicationID < 1 || d.CreatedAt.IsZero() {
		return graphInvariant("invalid published owner decision")
	}
	if d.Reference == "" {
		d.Reference = OwnerDecisionReference(d.TaskRef, d.Action, d.Target, d.Revision)
	}
	prefix := "%"
	if d.Action == "/approve-plan" {
		parts := strings.Fields(d.Target)
		if len(parts) > 1 {
			prefix = "/approve-plan " + parts[1] + " %"
		}
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO owner_decision_displays(task_ref,action,reference,target,revision,instance_id,publication_id,tested_commit,delivery_commit,created_at,requires_reference) VALUES(?,?,?,?,?,?,?,?,?,?,EXISTS(SELECT 1 FROM owner_decision_displays WHERE task_ref=? AND action=? AND target LIKE ? AND ((resolved=0 AND (target<>? OR revision<>?)) OR (target=? AND revision=? AND requires_reference=1)))) ON CONFLICT DO NOTHING`, d.TaskRef, d.Action, d.Reference, d.Target, d.Revision, d.InstanceID, d.PublicationID, d.TestedCommit, d.DeliveryCommit, FormatTime(d.CreatedAt), d.TaskRef, d.Action, prefix, d.Target, d.Revision, d.Target, d.Revision)
	return err
}

const decisionColumns = `id,task_ref,action,reference,target,revision,instance_id,publication_id,tested_commit,delivery_commit,created_at,resolved,requires_reference`

func scanDecision(row scanner) (d OwnerDecisionDisplay, err error) {
	var at string
	err = row.Scan(&d.ID, &d.TaskRef, &d.Action, &d.Reference, &d.Target, &d.Revision, &d.InstanceID, &d.PublicationID, &d.TestedCommit, &d.DeliveryCommit, &at, &d.Resolved, &d.RequiresReference)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		d.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
	}
	return
}

// ResolveOwnerDecisionDisplay gives explicit references priority, and rejects
// replacement ambiguity for bare shorthand. The caller still checks current
// target/evidence freshness and authenticates the actor.
func (db *DB) ResolveOwnerDecisionDisplay(ctx context.Context, task, action, reference string, replyTo int64) (OwnerDecisionDisplay, error) {
	return db.resolveOwnerDecisionDisplay(ctx, task, action, reference, replyTo, "")
}

// ResolveOwnerStoryDecisionDisplay scopes bare child approvals to that story.
func (db *DB) ResolveOwnerStoryDecisionDisplay(ctx context.Context, task, action, reference string, replyTo int64, key string) (OwnerDecisionDisplay, error) {
	return db.resolveOwnerDecisionDisplay(ctx, task, action, reference, replyTo, "/approve-plan "+key+" ")
}
func (db *DB) resolveOwnerDecisionDisplay(ctx context.Context, task, action, reference string, replyTo int64, prefix string) (OwnerDecisionDisplay, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+decisionColumns+` FROM owner_decision_displays WHERE task_ref=? AND action=? ORDER BY id`, task, action)
	if err != nil {
		return OwnerDecisionDisplay{}, err
	}
	defer rows.Close()
	var all []OwnerDecisionDisplay
	for rows.Next() {
		d, e := scanDecision(rows)
		if e != nil {
			return d, e
		}
		if prefix == "" || strings.HasPrefix(d.Target, prefix) {
			all = append(all, d)
		}
	}
	if err = rows.Err(); err != nil {
		return OwnerDecisionDisplay{}, err
	}
	if len(all) == 0 {
		return OwnerDecisionDisplay{}, ErrNotFound
	}
	latest := all[len(all)-1]
	if reference != "" {
		var match *OwnerDecisionDisplay
		for i := range all {
			d := &all[i]
			if d.Reference != reference {
				continue
			}
			if match != nil && (match.Target != d.Target || match.Revision != d.Revision) {
				return *d, errors.New("ambiguous decision reference; request a fresh display")
			}
			match = d
		}
	}
	if reference != "" || replyTo > 0 {
		for i := len(all) - 1; i >= 0; i-- {
			d := all[i]
			if (reference != "" && d.Reference == reference) || (reference == "" && d.PublicationID == replyTo) {
				current := latest
				if action == "/approve-plan" {
					parts := strings.Fields(d.Target)
					if len(parts) > 1 {
						storyPrefix := "/approve-plan " + parts[1] + " "
						for j := len(all) - 1; j >= 0; j-- {
							if strings.HasPrefix(all[j].Target, storyPrefix) {
								current = all[j]
								break
							}
						}
					}
				}
				if reference == "" {
					for _, other := range all {
						if other.PublicationID == replyTo && (other.Target != d.Target || other.Revision != d.Revision) {
							return d, errors.New("the replied-to display was replaced; use the displayed short reference")
						}
					}
				}
				if d.Target != current.Target || d.Revision != current.Revision {
					return d, errors.New("that decision display was replaced; use the displayed short reference")
				}
				return d, nil
			}
		}
		return OwnerDecisionDisplay{}, errors.New("unknown decision reference")
	}
	if latest.Resolved {
		return latest, errors.New("that decision is already resolved")
	}
	for _, d := range all {
		if d.Resolved {
			continue
		}
		if d.RequiresReference || d.Target != latest.Target || d.Revision != latest.Revision {
			return latest, errors.New("several decision targets were displayed; use the displayed short reference")
		}
	}
	return latest, nil
}

// OwnerDecisionBinding preserves original owner bytes alongside the normalized
// exact command. Replay reads this binding rather than resolving a newer target.
type OwnerDecisionBinding struct {
	TaskRef             string
	CommentID, AuthorID int64
	Body, Normalized    string
	Display             OwnerDecisionDisplay
	CreatedAt           time.Time
}

// GetOwnerDecisionBinding reads the immutable source and exact normalized decision.
func (db *DB) GetOwnerDecisionBinding(ctx context.Context, task string, comment int64) (b OwnerDecisionBinding, err error) {
	var display int64
	var at string
	err = db.sql.QueryRowContext(ctx, `SELECT task_ref,comment_id,author_id,body,display_id,normalized,created_at FROM owner_decision_bindings WHERE task_ref=? AND comment_id=?`, task, comment).Scan(&b.TaskRef, &b.CommentID, &b.AuthorID, &b.Body, &display, &b.Normalized, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	b.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return b, err
	}
	b.Display, err = scanDecision(db.sql.QueryRowContext(ctx, `SELECT `+decisionColumns+` FROM owner_decision_displays WHERE id=?`, display))
	return
}

// PutOwnerDecisionBinding saves one source binding and rejects conflicting replay.
func (db *DB) PutOwnerDecisionBinding(ctx context.Context, b OwnerDecisionBinding) error {
	if b.TaskRef == "" || b.CommentID < 1 || b.AuthorID < 1 || b.Body == "" || b.Normalized == "" || b.Display.ID < 1 || b.CreatedAt.IsZero() {
		return graphInvariant("invalid owner decision binding")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO owner_decision_bindings(task_ref,comment_id,author_id,body,display_id,normalized,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, b.TaskRef, b.CommentID, b.AuthorID, b.Body, b.Display.ID, b.Normalized, FormatTime(b.CreatedAt))
	if err != nil {
		return err
	}
	saved, err := db.GetOwnerDecisionBinding(ctx, b.TaskRef, b.CommentID)
	if err != nil {
		return err
	}
	if saved.Body != b.Body || saved.AuthorID != b.AuthorID || saved.Normalized != b.Normalized || saved.Display.ID != b.Display.ID {
		return graphInvariant("owner decision binding replay changed")
	}
	return nil
}

// LatestOwnerDecisionDisplay supports the existing explicit revision grammar.
func (db *DB) LatestOwnerDecisionDisplay(ctx context.Context, task, action string) (OwnerDecisionDisplay, error) {
	return scanDecision(db.sql.QueryRowContext(ctx, `SELECT `+decisionColumns+` FROM owner_decision_displays WHERE task_ref=? AND action=? ORDER BY id DESC LIMIT 1`, task, action))
}

// ExplicitOwnerDecisionDisplay resolves an explicit revision without silently
// retargeting it when that report revision was displayed at two delivery heads.
func (db *DB) ExplicitOwnerDecisionDisplay(ctx context.Context, task, action, revision string) (OwnerDecisionDisplay, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+decisionColumns+` FROM owner_decision_displays WHERE task_ref=? AND action=? AND revision=? ORDER BY id`, task, action, revision)
	if err != nil {
		return OwnerDecisionDisplay{}, err
	}
	defer rows.Close()
	var latest OwnerDecisionDisplay
	for rows.Next() {
		d, e := scanDecision(rows)
		if e != nil {
			return d, e
		}
		if latest.ID != 0 && d.Target != latest.Target {
			return d, errors.New("this report revision was displayed for replacement work; use the displayed short reference")
		}
		latest = d
	}
	if err = rows.Err(); err != nil {
		return latest, err
	}
	if latest.ID == 0 {
		return latest, ErrNotFound
	}
	return latest, nil
}

// ResolveOwnerDecisionBinding marks only an accepted binding's displayed target
// resolved. It never deletes history or changes the saved owner decision.
func (db *DB) ResolveOwnerDecisionBinding(ctx context.Context, task string, comment int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_decision_displays SET resolved=1 WHERE task_ref=? AND (action,target,revision) IN (SELECT d.action,d.target,d.revision FROM owner_decision_bindings b JOIN owner_decision_displays d ON d.id=b.display_id WHERE b.task_ref=? AND b.comment_id=?)`, task, task, comment)
	return err
}

// ResolveOwnerDecisionTarget handles existing explicit Plan commands too.
func (db *DB) ResolveOwnerDecisionTarget(ctx context.Context, task, action, target string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_decision_displays SET resolved=1 WHERE task_ref=? AND action=? AND target=?`, task, action, target)
	return err
}

// ResolveOwnerDecisionInstance retires published choices after confirmed delivery
// or a retained handoff. Historical targets and admitted bindings remain intact.
func (db *DB) ResolveOwnerDecisionInstance(ctx context.Context, instanceID int64) error {
	if instanceID < 1 {
		return graphInvariant("invalid completed decision instance")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_decision_displays SET resolved=1 WHERE instance_id=?`, instanceID)
	return err
}
