package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// StandardsConversationNoticeDelivery retains immutable notice bytes and guarded transport progress.
type StandardsConversationNoticeDelivery struct {
	NoticeID          int64
	Transport         string
	OwnerID           string
	ChannelID         string
	Payload           string
	Status            string
	PartsAcknowledged int
	Attempts          int
	Fence             int64
	LeaseUntil        time.Time
	NextAttemptAt     time.Time
	RemoteID          string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func readStandardsConversationNoticeDelivery(ctx context.Context, tx *sql.Tx, id int64, transport, owner, channel string) (StandardsConversationNoticeDelivery, error) {
	var d StandardsConversationNoticeDelivery
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT record_json FROM standards_conversation_notice_deliveries WHERE notice_id=? AND transport=? AND owner_id=? AND channel_id=?`, id, transport, owner, channel).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(raw), &d)
	return d, err
}
func writeStandardsConversationNoticeDelivery(ctx context.Context, tx *sql.Tx, d StandardsConversationNoticeDelivery) error {
	raw, e := json.Marshal(d)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE standards_conversation_notice_deliveries SET status=?,next_attempt=?,record_json=? WHERE notice_id=? AND transport=? AND owner_id=? AND channel_id=?`, d.Status, nullTime(d.NextAttemptAt), string(raw), d.NoticeID, d.Transport, d.OwnerID, d.ChannelID)
	return e
}

// SaveStandardsConversationNoticeDelivery projects a retained source notice once per owner destination.
func (db *DB) SaveStandardsConversationNoticeDelivery(ctx context.Context, d StandardsConversationNoticeDelivery) (StandardsConversationNoticeDelivery, error) {
	if d.NoticeID <= 0 || d.Transport == "" || d.OwnerID == "" || d.ChannelID == "" || d.Payload == "" {
		return d, fmt.Errorf("invalid standards notice destination or payload")
	}
	d.Status = "pending"
	d.Attempts = 0
	d.PartsAcknowledged = 0
	d.Fence = 0
	d.LeaseUntil = time.Time{}
	d.NextAttemptAt = time.Time{}
	d.RemoteID = ""
	d.LastError = ""
	d.CreatedAt = time.Now().UTC()
	d.UpdatedAt = d.CreatedAt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		raw, e := json.Marshal(d)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO standards_conversation_notice_deliveries(notice_id,transport,owner_id,channel_id,status,record_json) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, d.NoticeID, d.Transport, d.OwnerID, d.ChannelID, d.Status, string(raw))
		if e != nil {
			return e
		}
		old, e := readStandardsConversationNoticeDelivery(ctx, tx, d.NoticeID, d.Transport, d.OwnerID, d.ChannelID)
		if e != nil {
			return e
		}
		if old.Payload != d.Payload {
			return ErrStandardsConflict
		}
		d = old
		return nil
	})
	return d, err
}

// GetStandardsConversationNoticeDelivery reads retained delivery progress for one destination.
func (db *DB) GetStandardsConversationNoticeDelivery(ctx context.Context, id int64, transport, owner, channel string) (StandardsConversationNoticeDelivery, error) {
	var d StandardsConversationNoticeDelivery
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		d, e = readStandardsConversationNoticeDelivery(ctx, tx, id, transport, owner, channel)
		return e
	})
	return d, err
}

// ListStandardsConversationNoticeDeliveries is bounded and includes paused failures for inspection.
func (db *DB) ListStandardsConversationNoticeDeliveries(ctx context.Context, transport, owner, channel string, limit int) ([]StandardsConversationNoticeDelivery, error) {
	if limit <= 0 || limit > 25 {
		limit = 25
	}
	var out []StandardsConversationNoticeDelivery
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM standards_conversation_notice_deliveries WHERE transport=? AND owner_id=? AND channel_id=? ORDER BY (status='delivered'),notice_id LIMIT ?`, transport, owner, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var d StandardsConversationNoticeDelivery
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClaimStandardsConversationNoticeDelivery admits one due attempt and fences late workers.
func (db *DB) ClaimStandardsConversationNoticeDelivery(ctx context.Context, id int64, transport, owner, channel string, now time.Time, lease time.Duration) (StandardsConversationNoticeDelivery, error) {
	var d StandardsConversationNoticeDelivery
	if now.IsZero() || lease <= 0 {
		return d, fmt.Errorf("invalid standards notice lease")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		d, e = readStandardsConversationNoticeDelivery(ctx, tx, id, transport, owner, channel)
		if e != nil {
			return e
		}
		if d.Status == "delivered" || d.Status == "paused" || now.Before(d.NextAttemptAt) || now.Before(d.LeaseUntil) {
			return ErrStandardsConflict
		}
		d.Fence++
		d.Attempts++
		d.Status = "sending"
		d.LeaseUntil = now.Add(lease)
		d.UpdatedAt = time.Now().UTC()
		return writeStandardsConversationNoticeDelivery(ctx, tx, d)
	})
	return d, err
}

// FinishStandardsConversationNoticeDelivery saves delivery, retry timing, or permanent pause.
func (db *DB) FinishStandardsConversationNoticeDelivery(ctx context.Context, d StandardsConversationNoticeDelivery, now time.Time) (StandardsConversationNoticeDelivery, error) {
	if d.Status != "delivered" && d.Status != "pending" && d.Status != "paused" {
		return d, fmt.Errorf("invalid standards notice outcome")
	}
	if d.Status == "delivered" && d.RemoteID == "" {
		return d, fmt.Errorf("missing standards notice acknowledgment")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, e := readStandardsConversationNoticeDelivery(ctx, tx, d.NoticeID, d.Transport, d.OwnerID, d.ChannelID)
		if e != nil {
			return e
		}
		if old.Fence != d.Fence || old.Status != "sending" || !now.Before(old.LeaseUntil) {
			return ErrStandardsConflict
		}
		if d.PartsAcknowledged < old.PartsAcknowledged || d.PartsAcknowledged > old.PartsAcknowledged+1 {
			return ErrStandardsConflict
		}
		old.PartsAcknowledged = d.PartsAcknowledged
		old.Status = d.Status
		old.RemoteID = d.RemoteID
		old.LastError = d.LastError
		old.NextAttemptAt = d.NextAttemptAt
		old.LeaseUntil = time.Time{}
		old.UpdatedAt = time.Now().UTC()
		d = old
		return writeStandardsConversationNoticeDelivery(ctx, tx, d)
	})
	return d, err
}

// CountStandardsConversationNoticeBacklog reports retained failures and pending work.
func (db *DB) CountStandardsConversationNoticeBacklog(ctx context.Context, transport, owner, channel string) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM standards_conversation_notice_deliveries WHERE transport=? AND owner_id=? AND channel_id=? AND status!='delivered'`, transport, owner, channel).Scan(&n)
	return n, err
}

// ListDueStandardsConversationNoticeDeliveries skips completed history and returns at most 25 retained retries.
func (db *DB) ListDueStandardsConversationNoticeDeliveries(ctx context.Context, transport, owner, channel string, now time.Time) ([]StandardsConversationNoticeDelivery, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM standards_conversation_notice_deliveries WHERE transport=? AND owner_id=? AND channel_id=? AND status IN ('pending','sending') AND (next_attempt IS NULL OR next_attempt<=?) ORDER BY notice_id LIMIT 25`, transport, owner, channel, FormatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StandardsConversationNoticeDelivery
	for rows.Next() {
		var raw string
		var d StandardsConversationNoticeDelivery
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, err
		}
		if !now.Before(d.LeaseUntil) {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// LastStandardsConversationNoticeID returns the durable projection cursor for one destination.
func (db *DB) LastStandardsConversationNoticeID(ctx context.Context, transport, owner, channel string) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(notice_id),0) FROM standards_conversation_notice_deliveries WHERE transport=? AND owner_id=? AND channel_id=?`, transport, owner, channel).Scan(&id)
	return id, err
}
