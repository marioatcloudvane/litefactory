package store

import (
	"context"
	"errors"
	"sort"
)

// AlertControlScan keeps an authenticated history interval independent of the
// conversation backlog. Complete intervals retain only reserved control texts.
type AlertControlScan struct {
	Cursor, HighWater, Before string
	Messages                  []NaturalMessage
	Complete                  bool
}

// ReadAlertControlScan returns a detached local history checkpoint.
func (db *DB) ReadAlertControlScan(ctx context.Context, channel string) (AlertControlScan, bool, error) {
	l, err := db.ReadAlerts(ctx)
	if err != nil {
		return AlertControlScan{}, false, err
	}
	scan, ok := l.ControlScans[channel]
	return scan, ok, nil
}

// ActivateAlertControlScan establishes a future-only boundary once.
func (db *DB) ActivateAlertControlScan(ctx context.Context, channel, boundary string) error {
	if channel == "" || boundary == "" {
		return errors.New("invalid alert control activation")
	}
	return db.UpdateAlerts(ctx, func(l *AlertLedger) error {
		if l.ControlScans == nil {
			l.ControlScans = map[string]AlertControlScan{}
		}
		if _, ok := l.ControlScans[channel]; !ok {
			l.ControlScans[channel] = AlertControlScan{Cursor: boundary}
		}
		return nil
	})
}

// StageAlertControlScan fences progress and releases only complete intervals.
func (db *DB) StageAlertControlScan(ctx context.Context, channel string, expected, next AlertControlScan, messages []NaturalMessage, complete bool) error {
	return db.UpdateAlerts(ctx, func(l *AlertLedger) error {
		current, ok := l.ControlScans[channel]
		if !ok || current.Cursor != expected.Cursor || current.HighWater != expected.HighWater || current.Before != expected.Before || current.Complete || expected.Complete {
			return errors.New("alert control scan changed")
		}
		if next.HighWater == "" || len(messages) > 400 || len(current.Messages)+len(messages) > 4096 {
			return errors.New("alert control scan capacity unavailable")
		}
		for _, m := range messages {
			if m.ChannelRef != channel || m.SourceID == "" || m.OwnerID == "" || m.CreatedAt.IsZero() || len(m.Text) > 8192 {
				return errors.New("invalid staged alert control")
			}
		}
		current.Messages = append(current.Messages, messages...)
		current.HighWater, current.Before = next.HighWater, next.Before
		if complete {
			current.Cursor = next.HighWater
			current.HighWater = ""
			current.Before = ""
			current.Complete = true
			sort.Slice(current.Messages, func(a, b int) bool {
				x, y := current.Messages[a].SourceID, current.Messages[b].SourceID
				if len(x) != len(y) {
					return len(x) < len(y)
				}
				return x < y
			})
		}
		l.ControlScans[channel] = current
		return nil
	})
}

// FinishAlertControlMessage removes one applied command only. Receipt and
// announcement identities make a crash before this dequeue safe to replay.
func (db *DB) FinishAlertControlMessage(ctx context.Context, channel, source string) error {
	return db.UpdateAlerts(ctx, func(l *AlertLedger) error {
		current, ok := l.ControlScans[channel]
		if !ok || !current.Complete {
			return errors.New("alert control interval unavailable")
		}
		if len(current.Messages) == 0 {
			if source != "" {
				return errors.New("alert control source changed")
			}
			current.Complete = false
			l.ControlScans[channel] = current
			return nil
		}
		if current.Messages[0].SourceID != source {
			return errors.New("alert control source changed")
		}
		current.Messages = append([]NaturalMessage(nil), current.Messages[1:]...)
		if len(current.Messages) == 0 {
			current.Complete = false
		}
		l.ControlScans[channel] = current
		return nil
	})
}
