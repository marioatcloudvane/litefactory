package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// NoticePosition keeps unresolved mutable records as well as the allocation
// watermark. Finishes and publication may arrive out of allocation order.
type NoticePosition struct {
	Source                      string
	LastID, InitialID, Revision int64
	Pending                     []int64
	InitialAt                   time.Time
	Seen                        map[string]string
}

// NoticeRecord projects existing factory facts; it contains no chat decisions.
type NoticeRecord struct {
	ID                                                                        int64
	TaskRef, Workflow, From, To, Key, Kind, StopOrigin, StopCause, StopReason string
	Run                                                                       Run
	At                                                                        time.Time
	Candidate                                                                 []byte
	CommentID                                                                 int64
	Ready, Ignore                                                             bool
}

// The streams are independent; mutable lifecycle sources retain unresolved IDs.
var noticeSources = []string{"run_starts", "run_finishes", "transitions", "owner_waits"}

// InitNoticePositions skips existing history once. Unfinished runs and
// unpublished questions remain eligible when their future event occurs.
func (db *DB) InitNoticePositions(ctx context.Context, transport string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		for _, source := range noticeSources {
			queries, err := noticeSource(source)
			if err != nil {
				return err
			}
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_notice_positions WHERE transport=? AND source=?`, transport, source).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			var last int64
			if err = tx.QueryRowContext(ctx, queries.maximum).Scan(&last); err != nil {
				return err
			}
			pending := []int64{}
			if queries.pending != "" {
				rows, err := tx.QueryContext(ctx, queries.pending)
				if err != nil {
					return err
				}
				for rows.Next() {
					var id int64
					if err = rows.Scan(&id); err != nil {
						break
					}
					pending = append(pending, id)
				}
				err = errors.Join(err, rows.Err(), rows.Close())
				if err != nil {
					return err
				}
			}
			raw, err := json.Marshal(pending)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO chat_notice_positions(transport,source,last_id,initial_id,initial_at,pending) VALUES(?,?,?,?,?,?)`, transport, source, last, last, FormatTime(at), string(raw)); err != nil {
				return err
			}
		}
		return nil
	})
}

type noticeQueries struct{ maximum, next, pending string }

func noticeSource(source string) (noticeQueries, error) {
	switch source {
	case "run_starts":
		return noticeQueries{`SELECT COALESCE(MAX(id),0) FROM runs`, `SELECT id FROM runs WHERE id>? ORDER BY id LIMIT 256`, `SELECT id FROM runs WHERE status='prelaunch'`}, nil
	case "run_finishes":
		return noticeQueries{`SELECT COALESCE(MAX(id),0) FROM runs`, `SELECT id FROM runs WHERE id>? ORDER BY id LIMIT 256`, `SELECT id FROM runs WHERE finished_at IS NULL`}, nil
	case "transitions":
		return noticeQueries{`SELECT COALESCE(MAX(id),0) FROM graph_transitions`, `SELECT id FROM graph_transitions WHERE id>? ORDER BY id LIMIT 256`, ""}, nil
	case "owner_waits":
		return noticeQueries{`SELECT COALESCE(MAX(rowid),0) FROM graph_owner_waits`, `SELECT rowid FROM graph_owner_waits WHERE rowid>? ORDER BY rowid LIMIT 256`, `SELECT w.rowid FROM graph_owner_waits w LEFT JOIN report_deliveries r ON r.delivery_key=w.delivery_key WHERE COALESCE(r.completed,0)=0`}, nil
	default:
		return noticeQueries{}, errors.New("invalid notice source")
	}
}

// ListNoticeRecords reads a bounded allocation page plus unresolved records.
// All rows close before returning, so rendering and meter reads hold no cursor.
func (db *DB) ListNoticeRecords(ctx context.Context, transport, source string, at time.Time) (NoticePosition, []NoticeRecord, error) {
	p := NoticePosition{Source: source}
	var pending, seen, initial string
	if err := db.sql.QueryRowContext(ctx, `SELECT last_id,initial_id,initial_at,pending,seen,revision FROM chat_notice_positions WHERE transport=? AND source=?`, transport, source).Scan(&p.LastID, &p.InitialID, &initial, &pending, &seen, &p.Revision); err != nil {
		return p, nil, err
	}
	if err := json.Unmarshal([]byte(pending), &p.Pending); err != nil {
		return p, nil, err
	}
	if err := json.Unmarshal([]byte(seen), &p.Seen); err != nil {
		return p, nil, err
	}
	parsedInitial, err := time.Parse(time.RFC3339Nano, initial)
	if err != nil {
		return p, nil, err
	}
	p.InitialAt = parsedInitial
	queries, err := noticeSource(source)
	if err != nil {
		return p, nil, err
	}
	// All SQL comes from the closed source switch above.
	ids, err := db.sql.QueryContext(ctx, queries.next, p.LastID)
	if err != nil {
		return p, nil, err
	}
	selected := append([]int64(nil), p.Pending...)
	for ids.Next() {
		var id int64
		if err = ids.Scan(&id); err != nil {
			break
		}
		selected = append(selected, id)
	}
	err = errors.Join(err, ids.Err(), ids.Close())
	if err != nil {
		return p, nil, err
	}
	// Archival may remove graph rows and SQLite can then reuse their IDs.
	// Revisit fresh records whose identity differs from the saved position.
	if source == "transitions" || source == "owner_waits" {
		extra, err := db.freshNoticeIDs(ctx, source, p, seen, at)
		if err != nil {
			return p, nil, err
		}
		selected = append(selected, extra...)
	}
	unique := make(map[int64]bool)
	records := make([]NoticeRecord, 0, len(selected))
	for _, id := range selected {
		if unique[id] {
			continue
		}
		unique[id] = true
		r, err := db.readNoticeRecord(ctx, source, id)
		if errors.Is(err, sql.ErrNoRows) {
			r = NoticeRecord{ID: id, Ignore: true, Ready: true}
		} else if err != nil {
			return p, nil, err
		}
		records = append(records, r)
	}
	return p, records, nil
}

// The bounded fallback excludes already-read facts using position metadata,
// never message history. This also works when the highest source row is archived.
func (db *DB) freshNoticeIDs(ctx context.Context, source string, p NoticePosition, seen string, at time.Time) ([]int64, error) {
	var query string
	switch source {
	case "transitions":
		query = `SELECT t.id FROM graph_transitions t JOIN graph_instances i ON i.id=t.instance_id JOIN sessions s ON s.id=i.session_id WHERE t.id<=? AND t.at>=? AND t.at>? AND NOT EXISTS(SELECT 1 FROM json_each(?) e WHERE e.key=CAST(t.id AS TEXT) AND e.value=s.task_ref||'@'||t.at) ORDER BY t.id LIMIT 256`
	case "owner_waits":
		query = `SELECT w.rowid FROM graph_owner_waits w JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key JOIN graph_instances i ON i.id=w.instance_id JOIN sessions s ON s.id=i.session_id LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id WHERE w.rowid<=? AND COALESCE(c.created_at,a.finished_at,i.started_at)>=? AND COALESCE(c.created_at,a.finished_at,i.started_at)>? AND NOT EXISTS(SELECT 1 FROM json_each(?) e WHERE e.key=CAST(w.rowid AS TEXT) AND e.value=s.task_ref||'@'||COALESCE(c.created_at,a.finished_at,i.started_at)) ORDER BY w.rowid LIMIT 256`
	default:
		return nil, errors.New("invalid recyclable notice source")
	}
	rows, err := db.sql.QueryContext(ctx, query, p.LastID, FormatTime(at.Add(-24*time.Hour)), FormatTime(p.InitialAt), seen)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	return ids, errors.Join(err, rows.Err(), rows.Close())
}

func (db *DB) readNoticeRecord(ctx context.Context, source string, id int64) (NoticeRecord, error) {
	r := NoticeRecord{ID: id}
	var at sql.NullString
	var err error
	switch source {
	case "run_starts", "run_finishes":
		var started, finished sql.NullString
		err = db.sql.QueryRowContext(ctx, `SELECT r.id,s.task_ref,r.mode,COALESCE(NULLIF(r.workflow,''),r.mode),r.status,COALESCE(r.semantic_outcome,''),COALESCE(r.recovery_reason,''),COALESCE(r.error,''),r.started_at,r.finished_at FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=?`, id).Scan(&r.Run.ID, &r.TaskRef, &r.Run.Mode, &r.Workflow, &r.Run.Status, &r.Run.SemanticOutcome, &r.Run.RecoveryReason, &r.Run.Error, &started, &finished)
		if err != nil {
			return r, err
		}
		r.Run.StartedAt, err = scanTime(started)
		if err != nil {
			return r, err
		}
		r.Run.FinishedAt, err = scanTime(finished)
		if err != nil {
			return r, err
		}
		if source == "run_starts" {
			r.At = r.Run.StartedAt
			r.Ready = r.Run.Status != RunPrelaunch
			// Refused prelaunch allocations never crossed the execution boundary.
			r.Ignore = r.Run.SemanticOutcome == "refused"
		} else {
			r.At = r.Run.FinishedAt
			r.Ready = !r.At.IsZero()
		}
	case "transitions":
		err = db.sql.QueryRowContext(ctx, `SELECT s.task_ref,COALESCE(NULLIF(d.workflow,''),d.mode),t.from_node,t.to_node,t.at FROM graph_transitions t JOIN graph_instances i ON i.id=t.instance_id JOIN graph_definitions d ON d.id=i.definition_id JOIN sessions s ON s.id=i.session_id WHERE t.id=?`, id).Scan(&r.TaskRef, &r.Workflow, &r.From, &r.To, &at)
		r.Ready = true
	case "owner_waits":
		var completed bool
		err = db.sql.QueryRowContext(ctx, `SELECT s.task_ref,COALESCE(NULLIF(d.workflow,''),d.mode),w.delivery_key,c.kind,c.candidate,COALESCE(c.stop_origin,''),COALESCE(c.stop_cause,''),COALESCE(c.stop_reason,''),COALESCE(a.run_id,0),COALESCE(r.comment_id,0),COALESCE(r.completed,0),COALESCE(c.created_at,a.finished_at,i.started_at) FROM graph_owner_waits w JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id JOIN sessions s ON s.id=i.session_id LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id LEFT JOIN report_deliveries r ON r.delivery_key=w.delivery_key WHERE w.rowid=?`, id).Scan(&r.TaskRef, &r.Workflow, &r.Key, &r.Kind, &r.Candidate, &r.StopOrigin, &r.StopCause, &r.StopReason, &r.Run.ID, &r.CommentID, &completed, &at)
		r.Ignore = r.Kind != "question"
		r.Ready = r.Ignore || (completed && r.CommentID > 0)
	}
	if err != nil {
		return r, err
	}
	if source == "owner_waits" && r.StopOrigin != "" && r.Run.ID > 0 {
		r.Run, err = db.GetRun(ctx, r.Run.ID)
		if err != nil {
			return r, err
		}
	}
	if source == "owner_waits" && r.StopCause == "diagnosis_question" {
		r.Candidate, err = db.noticeDiagnosisQuestions(ctx, r.Key)
		if err != nil {
			return r, err
		}
	}
	if source == "transitions" || source == "owner_waits" {
		r.At, err = scanTime(at)
	}
	return r, err
}

// A Bug question's reporting intent contains an analysis summary. Read only the
// actual questions from the exact immutable diagnosis revision it references.
func (db *DB) noticeDiagnosisQuestions(ctx context.Context, key string) ([]byte, error) {
	var raw []byte
	err := db.sql.QueryRowContext(ctx, `SELECT o.payload FROM graph_comment_intents c JOIN graph_node_attempts a ON a.instance_id=c.instance_id JOIN graph_outputs o ON o.node_attempt_id=a.id WHERE c.delivery_key=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')='diagnosis' AND json_extract(CAST(o.payload AS TEXT),'$.revision')=json_extract(CAST(c.candidate AS TEXT),'$.diagnosis.revision') AND json_extract(CAST(o.payload AS TEXT),'$.digest')=json_extract(CAST(c.candidate AS TEXT),'$.diagnosis.digest') ORDER BY o.id DESC LIMIT 1`, key).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var diagnosis GraphDiagnosis
	if err := json.Unmarshal(raw, &diagnosis); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Questions []buganalysis.BugQuestion `json:"questions"`
	}{Questions: diagnosis.Analysis.Questions})
}

// AdmitNotices commits deduplicated rows and the source position together.
// A revision fence rejects a collector working from an outdated read.
func (db *DB) AdmitNotices(ctx context.Context, transport string, before, after NoticePosition, notices []ChatNotice) error {
	if before.Source != after.Source || after.LastID < before.LastID {
		return errors.New("invalid notice position")
	}
	seen := make(map[string]string, len(after.Seen))
	for id, fingerprint := range after.Seen {
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return err
		}
		seen[id] = fingerprint
	}
	seenJSON, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(after.Pending)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE chat_notice_positions SET last_id=?,pending=?,seen=?,revision=revision+1 WHERE transport=? AND source=? AND revision=?`, after.LastID, string(raw), string(seenJSON), transport, before.Source, before.Revision)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		for _, n := range notices {
			if n.Transport != transport || n.MessageID == "" || n.ChannelRef == "" || n.Command == "" || n.TaskRef == "" || n.CreatedAt.IsZero() || n.ReplyText == "" {
				return errors.New("invalid chat notice")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO chat_notices(transport,message_id,channel_ref,command,task_ref,state,next_attempt_at,reply_text,created_at) VALUES(?,?,?,?,?,'pending',?,?,?) ON CONFLICT(transport,message_id) DO NOTHING`, n.Transport, n.MessageID, n.ChannelRef, n.Command, n.TaskRef, FormatTime(n.CreatedAt), n.ReplyText, FormatTime(n.CreatedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ChatNotice shares the reply delivery state without an interaction handle.
type ChatNotice struct {
	ChatReply
	TaskRef string
}

// ClaimChatNotice uses the same lease and attempt fence as command replies.
func (db *DB) ClaimChatNotice(ctx context.Context, transport string, at time.Time) (ChatReply, error) {
	var r ChatReply
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_notices WHERE transport=? AND state IN ('pending','sent') AND next_attempt_at<=? ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
		if err != nil {
			return err
		}
		r.Attempts++
		r.State = "sent"
		r.NextAttemptAt = at.Add(time.Minute)
		_, err = tx.ExecContext(ctx, `UPDATE chat_notices SET state='sent',attempts=?,next_attempt_at=? WHERE transport=? AND message_id=?`, r.Attempts, FormatTime(r.NextAttemptAt), r.Transport, r.MessageID)
		return err
	})
	return r, err
}

// ReadChatNotice exposes delivery state for operational inspection.
func (db *DB) ReadChatNotice(ctx context.Context, transport, key string) (ChatReply, error) {
	return scanChat(db.sql.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_notices WHERE transport=? AND message_id=?`, transport, key))
}

// FinishChatNotice clears text on either terminal outcome.
func (db *DB) FinishChatNotice(ctx context.Context, r ChatReply, failed bool, at time.Time) error {
	state := "confirmed"
	var last any
	if failed {
		state = "failed"
		last = FormatTime(at)
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_notices SET state=?,reply_text='',finished_at=?,last_error_at=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, state, FormatTime(at), last, r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// RetryChatNotice records timing without copying remote error bodies.
func (db *DB) RetryChatNotice(ctx context.Context, r ChatReply, next, at time.Time) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_notices SET state='pending',next_attempt_at=?,last_error_at=? WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, FormatTime(next), FormatTime(at), r.Transport, r.MessageID, r.Attempts)
	return chatAffected(result, err)
}

// PurgeChatNotices leaves only source positions after terminal retention.
func (db *DB) PurgeChatNotices(ctx context.Context, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM chat_notices WHERE state IN ('confirmed','failed') AND finished_at<=?`, FormatTime(at.Add(-24*time.Hour)))
	return err
}
