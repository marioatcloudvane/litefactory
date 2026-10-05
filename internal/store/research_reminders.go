package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ResearchReminderNamespace isolates notices from graph effects and owner input.
const ResearchReminderNamespace = "research-reminder"

// ResearchReminderDelay is seven elapsed days after verified forge publication.
const ResearchReminderDelay = 168 * time.Hour

// ResearchReminder identifies the original published wait and its independent notice.
type ResearchReminder struct {
	WaitKey, DeliveryKey                        string
	InstanceID, SessionID, CommentID, BotUserID int64
	Task, Destination, Body                     string
	PublishedAt                                 time.Time
	Suppressed                                  bool
}

const reminderColumns = `w.delivery_key,COALESCE(m.reminder_delivery_key,''),w.instance_id,i.session_id,r.comment_id,r.bot_user_id,r.task,r.destination,r.body,m.published_at,COALESCE(m.suppressed,0)`
const reminderJoins = ` FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions g ON g.id=i.definition_id JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key JOIN report_deliveries r ON r.delivery_key=w.delivery_key LEFT JOIN research_wait_reminders m ON m.wait_delivery_key=w.delivery_key`

func scanResearchReminder(row scanner) (v ResearchReminder, err error) {
	var at sql.NullString
	err = row.Scan(&v.WaitKey, &v.DeliveryKey, &v.InstanceID, &v.SessionID, &v.CommentID, &v.BotUserID, &v.Task, &v.Destination, &v.Body, &at, &v.Suppressed)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		v.PublishedAt, err = scanTime(at)
	}
	return v, err
}

// GetResearchReminder reads reminder history bound to an original published wait.
func (db *DB) GetResearchReminder(ctx context.Context, key string) (ResearchReminder, error) {
	return scanResearchReminder(db.sql.QueryRowContext(ctx, `SELECT `+reminderColumns+reminderJoins+` WHERE w.delivery_key=? AND COALESCE(NULLIF(g.workflow,''),g.mode)='research' AND c.kind IN ('question','review') AND r.completed=1`, key))
}

// ListResearchReminderWaits bounds each independent queue, so uncertain receipts
// or unverified old comments cannot starve newly due waits.
func (db *DB) ListResearchReminderWaits(ctx context.Context, at time.Time, limit int) ([]ResearchReminder, error) {
	if at.IsZero() || limit < 1 {
		return nil, errors.New("store: reminder selection needs time and positive limit")
	}
	var out []ResearchReminder
	const base = `SELECT ` + reminderColumns + reminderJoins + ` WHERE COALESCE(NULLIF(g.workflow,''),g.mode)='research' AND c.kind IN ('question','review') AND r.completed=1 AND `
	const recoveryQuery = base + `m.reminder_delivery_key IS NOT NULL AND m.suppressed=0 AND EXISTS(SELECT 1 FROM report_deliveries notice WHERE notice.delivery_key=m.reminder_delivery_key AND notice.completed=0) ORDER BY r.comment_id LIMIT ?`
	const dueQuery = base + `w.state='waiting' AND i.status='running' AND m.reminder_delivery_key IS NULL AND m.suppressed=0 AND m.published_at<=? ORDER BY r.comment_id LIMIT ?`
	const unknownQuery = base + `w.state='waiting' AND i.status='running' AND m.wait_delivery_key IS NULL ORDER BY r.comment_id LIMIT ?`
	for _, selection := range []struct {
		query string
		args  []any
	}{
		{recoveryQuery, []any{limit}},
		{dueQuery, []any{FormatTime(at.Add(-ResearchReminderDelay)), limit}},
		{unknownQuery, []any{limit}},
	} {
		rows, err := db.sql.QueryContext(ctx, selection.query, selection.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			v, e := scanResearchReminder(rows)
			if e != nil {
				return nil, errors.Join(e, rows.Close())
			}
			out = append(out, v)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// VerifyResearchWaitPublication retains the forge-created time, never local age.
// The caller verifies exact comment identity, destination, bot identity and body.
func (db *DB) VerifyResearchWaitPublication(ctx context.Context, key string, at time.Time) error {
	if at.IsZero() {
		return errors.New("store: unknown Research publication time")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		v, e := scanResearchReminder(tx.QueryRowContext(ctx, `SELECT `+reminderColumns+reminderJoins+` WHERE w.delivery_key=? AND COALESCE(NULLIF(g.workflow,''),g.mode)='research' AND c.kind IN ('question','review') AND r.completed=1`, key))
		if e != nil {
			return e
		}
		if !v.PublishedAt.IsZero() {
			if FormatTime(v.PublishedAt) != FormatTime(at) {
				return errors.New("store: publication time is immutable")
			}
			return nil
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO research_wait_reminders(wait_delivery_key,published_at) VALUES(?,?)`, key, FormatTime(at))
		return e
	})
}

func researchReminderEligibleTx(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	var eligible bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1`+reminderJoins+` WHERE w.delivery_key=? AND w.state='waiting' AND i.status='running' AND COALESCE(NULLIF(g.workflow,''),g.mode)='research' AND c.kind IN ('question','review') AND r.completed=1 AND m.suppressed=0 AND NOT EXISTS(SELECT 1 FROM graph_owner_waits newer WHERE newer.instance_id=w.instance_id AND newer.rowid>w.rowid) AND NOT EXISTS(SELECT 1 FROM graph_instances newer JOIN graph_definitions ng ON ng.id=newer.definition_id WHERE newer.session_id=i.session_id AND COALESCE(NULLIF(ng.workflow,''),ng.mode)='research' AND newer.id>i.id))`, key).Scan(&eligible)
	return eligible, err
}

// BindResearchReminder fixes the notice identity after its redacted intent exists.
func (db *DB) BindResearchReminder(ctx context.Context, key, delivery string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var published string
		var prior sql.NullString
		e := tx.QueryRowContext(ctx, `SELECT published_at,reminder_delivery_key FROM research_wait_reminders WHERE wait_delivery_key=?`, key).Scan(&published, &prior)
		if e != nil {
			return e
		}
		started, e := ParseTime(published)
		if e != nil {
			return e
		}
		if at.IsZero() || at.Before(started.Add(ResearchReminderDelay)) {
			return errors.New("store: Research reminder is not due")
		}
		if prior.Valid && prior.String != delivery {
			return errors.New("store: reminder identity is immutable")
		}
		var valid bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_deliveries notice JOIN report_deliveries original ON original.delivery_key=? WHERE notice.delivery_key=? AND notice.trigger_namespace=? AND notice.purpose=? AND notice.run_id IS NULL AND notice.trigger_id=0 AND notice.session_id=original.session_id AND notice.task=original.task AND notice.destination=original.destination)`, key, delivery, ResearchReminderNamespace, ResearchReminderNamespace).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return errors.New("store: reminder intent provenance mismatch")
		}
		_, e = tx.ExecContext(ctx, `UPDATE research_wait_reminders SET reminder_delivery_key=? WHERE wait_delivery_key=?`, delivery, key)
		return e
	})
}

// SuppressResearchReminder permanently retires only a notice that cannot have posted.
func (db *DB) SuppressResearchReminder(ctx context.Context, key string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE research_wait_reminders SET suppressed=1 WHERE wait_delivery_key=? AND (reminder_delivery_key IS NULL OR EXISTS(SELECT 1 FROM report_deliveries r WHERE r.delivery_key=reminder_delivery_key AND r.possibly_sent=0 AND r.comment_id IS NULL))`, key)
	return err
}

func researchReminderKeyTx(ctx context.Context, tx *sql.Tx, delivery string) (string, error) {
	var key string
	err := tx.QueryRowContext(ctx, `SELECT wait_delivery_key FROM research_wait_reminders WHERE reminder_delivery_key=?`, delivery).Scan(&key)
	if err != nil {
		return "", fmt.Errorf("store: reminder delivery has no binding: %w", err)
	}
	return key, nil
}

// ResearchReminderForDelivery resolves a journal notice to its original wait.
func (db *DB) ResearchReminderForDelivery(ctx context.Context, delivery string) (ResearchReminder, error) {
	return scanResearchReminder(db.sql.QueryRowContext(ctx, `SELECT `+reminderColumns+reminderJoins+` WHERE m.reminder_delivery_key=?`, delivery))
}
