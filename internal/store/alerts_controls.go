package store

import (
	"context"
	"time"
)

// PendingNaturalAlertControls returns completed-scan controls in source order,
// independently of provider availability and ordinary conversation backlog.
func (db *DB) PendingNaturalAlertControls(ctx context.Context, channel string) ([]NaturalMessage, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT channel_ref,source_id,owner_id,reply_to,content,created_at FROM chat_natural_messages WHERE channel_ref=? AND state='control_pending' ORDER BY length(source_id),source_id LIMIT 1000`, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NaturalMessage
	for rows.Next() {
		var m NaturalMessage
		var at string
		if err = rows.Scan(&m.ChannelRef, &m.SourceID, &m.OwnerID, &m.ReplyTo, &m.Text, &at); err != nil {
			return nil, err
		}
		m.Transport = "discord"
		m.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
