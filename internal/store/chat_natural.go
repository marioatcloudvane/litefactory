package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// NaturalScan retains a closed interval's progress without admitting partial history.
type NaturalScan struct{ ChannelRef, Cursor, HighWater, Before string }

// NaturalMessage contains only prefiltered owner text from one allowed channel.
type NaturalMessage struct {
	Transport, ChannelRef, SourceID, OwnerID, ReplyTo, Text string
	CreatedAt                                               time.Time
}

// ReadNaturalScan returns the saved channel cursor and unfinished scan progress.
func (db *DB) ReadNaturalScan(ctx context.Context, channel string) (NaturalScan, error) {
	s := NaturalScan{ChannelRef: channel}
	err := db.sql.QueryRowContext(ctx, `SELECT cursor,high_water,before_id FROM chat_natural_scans WHERE channel_ref=?`, channel).Scan(&s.Cursor, &s.HighWater, &s.Before)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return s, err
}

// ActivateNaturalScan establishes the current boundary once, never replaying history.
func (db *DB) ActivateNaturalScan(ctx context.Context, channel, boundary string) error {
	if channel == "" || boundary == "" {
		return errors.New("invalid natural activation")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO chat_natural_scans(channel_ref,cursor) VALUES(?,?) ON CONFLICT DO NOTHING`, channel, boundary)
	return err
}

// StageNaturalScan atomically saves progress and releases a completed interval oldest first.
// expected fences concurrent collectors. At most 1000 released, unprocessed texts remain pending
// per channel; the interval in progress stays staged until complete, bounded by the admission window.
func (db *DB) StageNaturalScan(ctx context.Context, expected, next NaturalScan, messages []NaturalMessage, complete bool) error {
	if expected.ChannelRef != next.ChannelRef || next.HighWater == "" || len(messages) > 400 {
		return errors.New("invalid natural scan")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_natural_messages WHERE channel_ref=? AND state='pending'`, next.ChannelRef).Scan(&count); err != nil {
			return err
		}
		if count+len(messages) > 1000 {
			return errors.New("natural intake backpressure")
		}
		cursor, high, before := next.Cursor, next.HighWater, next.Before
		if complete {
			cursor, high, before = next.HighWater, "", ""
		}
		result, err := tx.ExecContext(ctx, `UPDATE chat_natural_scans SET cursor=?,high_water=?,before_id=? WHERE channel_ref=? AND cursor=? AND high_water=? AND before_id=?`, cursor, high, before, expected.ChannelRef, expected.Cursor, expected.HighWater, expected.Before)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		for _, m := range messages {
			id, e := strconv.ParseUint(m.SourceID, 10, 64)
			if e != nil || id == 0 || m.ChannelRef != next.ChannelRef || m.OwnerID == "" || len(m.Text) > 8192 || m.CreatedAt.IsZero() {
				return errors.New("invalid natural message")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO chat_natural_messages(channel_ref,source_id,owner_id,reply_to,content,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, m.ChannelRef, m.SourceID, m.OwnerID, m.ReplyTo, m.Text, FormatTime(m.CreatedAt)); err != nil {
				return err
			}
		}
		if complete {
			_, err = tx.ExecContext(ctx, `UPDATE chat_natural_messages SET state='pending' WHERE channel_ref=? AND state='staged'`, next.ChannelRef)
		}
		return err
	})
}

// PendingNaturalMessages returns completed-scan owner messages oldest first.
func (db *DB) PendingNaturalMessages(ctx context.Context, channel string, limit int) ([]NaturalMessage, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid natural message limit")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT channel_ref,source_id,owner_id,reply_to,content,created_at FROM chat_natural_messages WHERE channel_ref=? AND state='pending' ORDER BY length(source_id),source_id LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NaturalMessage
	for rows.Next() {
		var m NaturalMessage
		m.Transport = "discord"
		var at string
		if err = rows.Scan(&m.ChannelRef, &m.SourceID, &m.OwnerID, &m.ReplyTo, &m.Text, &at); err != nil {
			return nil, err
		}
		m.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// FinishNaturalMessage clears processed text while retaining its replay identity.
func (db *DB) FinishNaturalMessage(ctx context.Context, channel, id string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE chat_natural_messages SET state='finished',content='',reply_to='',finished_at=? WHERE channel_ref=? AND source_id=?`, FormatTime(at), channel, id)
	return err
}
