package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HoldImplementStoryTimeout durably stops a worker without spending its repair allowance.
func (db *DB) HoldImplementStoryTimeout(ctx context.Context, sessionID, attemptID int64, reason string, at time.Time) (ImplementStory, error) {
	if sessionID < 1 || attemptID < 1 || strings.TrimSpace(reason) == "" || len(reason) > maxImplementReason || at.IsZero() {
		return ImplementStory{}, errors.New("store: invalid implement timeout hold")
	}
	var out ImplementStory
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		row, err := scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE worker_session=? ORDER BY generation DESC LIMIT 1`, sessionID))
		if err != nil {
			return err
		}
		if row.TimeoutAttemptID != 0 {
			if row.TimeoutAttemptID == attemptID {
				// Before publication, enrich the authoritative stop with final
				// cleanup evidence without moving its stable park revision.
				if row.State == ImplementRunning && row.TimeoutDeliveryKey == "" {
					if _, err = tx.ExecContext(ctx, `UPDATE implement_stories SET last_failure=?,updated_at=? WHERE parent=? AND generation=? AND story_key=? AND timeout_attempt_id=?`, reason, FormatTime(at), row.Parent, row.Generation, row.Key, attemptID); err != nil {
						return err
					}
					row.LastFailure = reason
					row.UpdatedAt = at
				}
				out = row
				return nil
			}
			return ErrImplementConflict
		}
		if row.State != ImplementRunning {
			return ErrImplementConflict
		}
		// The hold itself leaves the revision unchanged; its one intended parked revision is stable.
		_, err = tx.ExecContext(ctx, `UPDATE implement_stories SET timeout_attempt_id=?,timeout_park_revision=revision+1,last_failure=?,updated_at=? WHERE parent=? AND generation=? AND story_key=? AND revision=?`, attemptID, reason, FormatTime(at), row.Parent, row.Generation, row.Key, row.Revision)
		if err != nil {
			return err
		}
		out, err = scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, row.Parent, row.Generation, row.Key))
		return err
	})
	return out, err
}

// ConfirmImplementTimeoutNotice binds the hold to its durably confirmed story notice.
func (db *DB) ConfirmImplementTimeoutNotice(ctx context.Context, parent string, generation int64, key string, attemptID int64, deliveryKey, commentID string, at time.Time) error {
	if deliveryKey == "" || commentID == "" || at.IsZero() {
		return errors.New("store: invalid timeout notice")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE implement_stories SET timeout_delivery_key=?,timeout_comment_id=?,timeout_notice_at=CASE WHEN timeout_notice_at='' THEN ? ELSE timeout_notice_at END,updated_at=CASE WHEN timeout_notice_at='' THEN ? ELSE updated_at END WHERE parent=? AND generation=? AND story_key=? AND timeout_attempt_id=? AND (timeout_delivery_key='' OR (timeout_delivery_key=? AND timeout_comment_id=?))`, deliveryKey, commentID, FormatTime(at), FormatTime(at), parent, generation, key, attemptID, deliveryKey, commentID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return ErrImplementConflict
	}
	return err
}

// ImplementTimeoutReply carries authenticated admission facts and the exact held notice.
type ImplementTimeoutReply struct {
	Source, Task, CommentID        string
	AuthorID, OwnerID              string
	Parent                         string
	Generation                     int64
	Key                            string
	TimeoutAttemptID, ParkRevision int64
	DeliveryKey, NoticeCommentID   string
	ReplyToCommentID               string
	// NoticeCreatedAt is the authenticated provider observation of this exact notice.
	NoticeCreatedAt time.Time
	// NoticePrecedesReply records authenticated provider list order for equal timestamps.
	NoticePrecedesReply bool
	Guidance            string
	At                  time.Time
}

// ResumeImplementTimeout durably consumes a reply once. An early reply remains pending
// until the exact timeout park finishes; replay cannot clear a subsequent hold.
func (db *DB) ResumeImplementTimeout(ctx context.Context, in ImplementTimeoutReply) (ImplementStory, bool, error) {
	if in.Source == "" || in.Task == "" || in.CommentID == "" || in.AuthorID == "" || in.AuthorID != in.OwnerID || in.Parent == "" || in.Generation < 1 || in.Key == "" || in.TimeoutAttemptID < 1 || in.ParkRevision < 1 || in.DeliveryKey == "" || in.NoticeCommentID == "" || (in.ReplyToCommentID != "" && in.ReplyToCommentID != in.NoticeCommentID) || in.At.IsZero() || len(in.Guidance) > maxImplementGuidance {
		return ImplementStory{}, false, errors.New("store: invalid authenticated implement timeout reply")
	}
	var out ImplementStory
	var resumed bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		// A replay has no authority over any current row, including a later timeout.
		var existing int
		err := tx.QueryRowContext(ctx, `SELECT applied FROM implement_timeout_replies WHERE source=? AND task=? AND comment_id=? AND story_key=?`, in.Source, in.Task, in.CommentID, in.Key).Scan(&existing)
		if err == nil {
			out, err = scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, in.Parent, in.Generation, in.Key))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, in.Parent, in.Generation, in.Key))
		if err != nil {
			return err
		}
		if row.TimeoutAttemptID != in.TimeoutAttemptID || row.TimeoutParkRevision != in.ParkRevision || row.TimeoutDeliveryKey != in.DeliveryKey || row.TimeoutCommentID != in.NoticeCommentID || (row.State != ImplementRunning && row.State != ImplementParked) {
			return ErrImplementConflict
		}
		var latest int64
		if err = tx.QueryRowContext(ctx, `SELECT MAX(generation) FROM implement_stories WHERE parent=?`, in.Parent).Scan(&latest); err != nil {
			return err
		}
		if latest != in.Generation {
			return ErrImplementConflict
		}
		var noticeAt, remoteNoticeAt string
		if err = tx.QueryRowContext(ctx, `SELECT timeout_notice_at,timeout_notice_remote_at FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, in.Parent, in.Generation, in.Key).Scan(&noticeAt, &remoteNoticeAt); err != nil {
			return err
		}
		noticeTime, err := ParseTime(noticeAt)
		if err != nil {
			return err
		}
		if !in.NoticeCreatedAt.IsZero() {
			observed := FormatTime(in.NoticeCreatedAt)
			if in.NoticeCreatedAt.After(noticeTime) || (remoteNoticeAt != "" && remoteNoticeAt != observed) {
				return ErrImplementConflict
			}
			if remoteNoticeAt == "" {
				if _, err = tx.ExecContext(ctx, `UPDATE implement_stories SET timeout_notice_remote_at=? WHERE parent=? AND generation=? AND story_key=? AND timeout_attempt_id=?`, observed, in.Parent, in.Generation, in.Key, in.TimeoutAttemptID); err != nil {
					return err
				}
				remoteNoticeAt = observed
			}
		}
		if remoteNoticeAt != "" {
			noticeTime, err = ParseTime(remoteNoticeAt)
			if err != nil {
				return err
			}
		}
		if in.Task != in.Parent {
			var number int
			if err = tx.QueryRowContext(ctx, `SELECT issue_number FROM managed_stories WHERE parent_task=? AND generation=? AND story_key=?`, in.Parent, in.Generation, in.Key).Scan(&number); err != nil {
				return ErrImplementConflict
			}
			hash := strings.LastIndex(in.Parent, "#")
			if hash < 0 || in.Task != fmt.Sprintf("%s#%d", in.Parent[:hash], number) {
				return ErrImplementConflict
			}
		}
		if in.At.Before(noticeTime) || (in.At.Equal(noticeTime) && (!in.NoticePrecedesReply || in.NoticeCreatedAt.IsZero())) {
			return fmt.Errorf("%w: reply predates confirmed notice", ErrImplementConflict)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO implement_timeout_replies(source,task,comment_id,parent,generation,story_key,timeout_attempt_id,park_revision,delivery_key,notice_comment_id,guidance,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, in.Source, in.Task, in.CommentID, in.Parent, in.Generation, in.Key, in.TimeoutAttemptID, in.ParkRevision, in.DeliveryKey, in.NoticeCommentID, in.Guidance, FormatTime(in.At))
		if err != nil {
			return err
		}
		out, resumed, err = applyPendingImplementTimeoutReplyTx(ctx, tx, row, in.At)
		return err
	})
	return out, resumed, err
}

func applyPendingImplementTimeoutReplyTx(ctx context.Context, tx *sql.Tx, row ImplementStory, at time.Time) (ImplementStory, bool, error) {
	if row.State != ImplementParked || row.TimeoutAttemptID == 0 {
		return row, false, nil
	}
	var source, task, comment, guidance string
	err := tx.QueryRowContext(ctx, `SELECT source,task,comment_id,guidance FROM implement_timeout_replies WHERE parent=? AND generation=? AND story_key=? AND timeout_attempt_id=? AND park_revision=? AND delivery_key=? AND notice_comment_id=? AND applied=0 ORDER BY created_at,comment_id LIMIT 1`, row.Parent, row.Generation, row.Key, row.TimeoutAttemptID, row.Revision, row.TimeoutDeliveryKey, row.TimeoutCommentID).Scan(&source, &task, &comment, &guidance)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE implement_stories SET state='waiting',guidance=?,guidance_question=reason,reason='',park_origin='',attempts=0,last_failure='',timeout_attempt_id=0,timeout_park_revision=0,timeout_delivery_key='',timeout_comment_id='',timeout_notice_at='',timeout_notice_remote_at='',revision=revision+1,updated_at=?,state_at=? WHERE parent=? AND generation=? AND story_key=? AND revision=?`, guidance, FormatTime(at), FormatTime(at), row.Parent, row.Generation, row.Key, row.Revision)
	if err != nil {
		return row, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE implement_timeout_replies SET applied=1,applied_at=? WHERE source=? AND task=? AND comment_id=? AND story_key=?`, FormatTime(at), source, task, comment, row.Key)
	if err != nil {
		return row, false, err
	}
	out, err := scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, row.Parent, row.Generation, row.Key))
	return out, true, err
}

// ListImplementWaitingStories finds eligible state candidates for shared readiness checks.
func (db *DB) ListImplementWaitingStories(ctx context.Context) ([]ImplementStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+implementColumns+` FROM implement_stories i WHERE state=? AND timeout_attempt_id=0 AND EXISTS(SELECT 1 FROM implement_timeout_replies r WHERE r.parent=i.parent AND r.generation=i.generation AND r.story_key=i.story_key AND r.applied=1 AND r.applied_at=i.state_at) AND generation=(SELECT MAX(generation) FROM implement_stories WHERE parent=i.parent) AND parent NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY parent,generation,position`, ImplementWaiting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImplementStory
	for rows.Next() {
		row, err := scanImplementStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// HasImplementTimeoutReply reports a consumed or pending authenticated source reply.
func (db *DB) HasImplementTimeoutReply(ctx context.Context, source, task, comment string) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM implement_timeout_replies WHERE source=? AND task=? AND comment_id=?`, source, task, comment).Scan(&n)
	return n > 0, err
}

// ListImplementTimeoutHolds discovers stopped work whose notice or park may need
// factory-only settlement after restart. It grants no provider retry authority.
func (db *DB) ListImplementTimeoutHolds(ctx context.Context) ([]ImplementStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+implementColumns+` FROM implement_stories i WHERE timeout_attempt_id>0 AND generation=(SELECT MAX(generation) FROM implement_stories WHERE parent=i.parent) AND parent NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY parent,generation,position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImplementStory
	for rows.Next() {
		row, err := scanImplementStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
