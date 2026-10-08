package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// DoctorEvidenceRecord is an explicitly selected issue-owned projection. Bodies,
// aggregate blobs and arbitrary paths are deliberately not selected by this read.
type DoctorEvidenceRecord struct {
	Identity   string    `json:"identity,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	State      string    `json:"state"`
	Content    string    `json:"content,omitempty"`
}

// DoctorEvidence is a short consistent database snapshot, not a global snapshot.
type DoctorEvidence struct {
	Atomicity  string                 `json:"atomicity"`
	TaskRef    string                 `json:"task_ref"`
	CapturedAt time.Time              `json:"captured_at"`
	SessionID  int64                  `json:"session_id,omitempty"`
	Provider   string                 `json:"provider,omitempty"`
	Records    []DoctorEvidenceRecord `json:"records"`
	Gaps       []string               `json:"gaps"`
}

// ReadDoctorEvidence never creates a session, advances a cursor or traverses
// sibling histories. Each category is bounded in SQL before allocation.
func (db *DB) ReadDoctorEvidence(ctx context.Context, ref naming.Ref) (DoctorEvidence, error) {
	if err := ref.Validate(); err != nil {
		return DoctorEvidence{}, err
	}
	out := DoctorEvidence{Atomicity: "Database rows share one short transaction; local files and process observations are captured separately", TaskRef: ref.String(), CapturedAt: nowUTC(), Records: []DoctorEvidenceRecord{}, Gaps: []string{"Remote issue state and fresh comments not collected"}}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var task, state string
		var needsPoll int
		var lockOwner string
		err := tx.QueryRowContext(ctx, `SELECT s.id,s.task_ref,s.state,s.needs_poll,COALESCE(s.lock_owner,'') FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE r.forge=? AND lower(r.owner)=lower(?) AND lower(r.name)=lower(?) AND s.issue_number=?`, ref.Forge, ref.Owner, ref.Repo, ref.Issue).Scan(&out.SessionID, &task, &state, &needsPoll, &lockOwner)
		if errors.Is(err, sql.ErrNoRows) {
			out.Gaps = append(out.Gaps, "No workflow session exists for this issue")
			return nil
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(task, ref.String()) {
			return errors.New("doctor evidence task identity mismatch")
		}
		out.Records = append(out.Records, DoctorEvidenceRecord{Kind: "session", ID: fmt.Sprint(out.SessionID), State: state, Content: fmt.Sprintf("needs_poll=%d", needsPoll)})
		if lockOwner != "" {
			out.Records = append(out.Records, DoctorEvidenceRecord{Kind: "session_lock", ID: fmt.Sprint(out.SessionID), State: lockOwner})
		}
		err = tx.QueryRowContext(ctx, `SELECT agent FROM segments WHERE session_id=? ORDER BY ordinal DESC LIMIT 1`, out.SessionID).Scan(&out.Provider)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		queries := []struct {
			kind, query string
			limit       int
		}{
			{"run", `SELECT CAST(id AS TEXT),status FROM runs WHERE session_id=? ORDER BY ordinal DESC LIMIT 101`, 100},
			{"comment", `SELECT CAST(forge_comment_id AS TEXT),classification FROM comments WHERE session_id=? ORDER BY forge_comment_id DESC LIMIT 31`, 30},
			{"watermark", `SELECT CAST(session_id AS TEXT),CAST(last_comment_id AS TEXT) FROM watermarks WHERE session_id=?`, 1},
			{"schedule", `SELECT CAST(id AS TEXT),status FROM schedules WHERE session_id=? ORDER BY id DESC LIMIT 101`, 100},
			{"report", `SELECT delivery_key,CASE WHEN completed=1 THEN 'completed' WHEN comment_id IS NOT NULL THEN 'confirmed' WHEN possibly_sent=1 THEN 'uncertain' ELSE 'pending' END FROM report_deliveries WHERE session_id=? ORDER BY rowid DESC LIMIT 101`, 100},
			{"graph", `SELECT CAST(id AS TEXT),status FROM graph_instances WHERE session_id=? ORDER BY id DESC LIMIT 101`, 100},
			{"report_recovery", `SELECT recovery_key,state FROM report_recoveries WHERE session_id=? ORDER BY rowid DESC LIMIT 101`, 100},
			{"graph_dispatch", `SELECT CAST(d.id AS TEXT),d.status FROM graph_dispatches d JOIN graph_instances g ON g.id=d.instance_id WHERE g.session_id=? ORDER BY d.id DESC LIMIT 101`, 100},
			{"graph_attempt", `SELECT CAST(a.id AS TEXT),a.status FROM graph_node_attempts a JOIN graph_instances g ON g.id=a.instance_id WHERE g.session_id=? ORDER BY a.id DESC LIMIT 101`, 100},
			{"owner_wait", `SELECT w.delivery_key,w.state FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id WHERE g.session_id=? ORDER BY w.rowid DESC LIMIT 101`, 100},
		}
		for _, q := range queries {
			rows, err := tx.QueryContext(ctx, q.query, out.SessionID)
			if err != nil {
				return err
			}
			count := 0
			for rows.Next() {
				var r DoctorEvidenceRecord
				r.Kind = q.kind
				if err := rows.Scan(&r.ID, &r.State); err != nil {
					return errors.Join(err, rows.Close())
				}
				count++
				if count > q.limit {
					out.Gaps = append(out.Gaps, q.kind+" rows truncated")
					continue
				}
				if len(r.ID) > 256 || len(r.State) > 128 {
					return errors.Join(errors.New("doctor evidence metadata exceeds bounds"), rows.Close())
				}
				out.Records = append(out.Records, r)
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				return err
			}
		}

		rows, err := tx.QueryContext(ctx, `SELECT forge_comment_id,CASE WHEN length(CAST(body AS BLOB))<=8192 THEN body ELSE '' END,length(CAST(body AS BLOB)) FROM comments WHERE session_id=? ORDER BY forge_comment_id DESC LIMIT 30`, out.SessionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, size int64
			var body string
			if err := rows.Scan(&id, &body, &size); err != nil {
				return errors.Join(err, rows.Close())
			}
			if size > 8192 {
				out.Gaps = append(out.Gaps, fmt.Sprintf("Comment %d body omitted: exceeds 8192 bytes", id))
				continue
			}
			out.Records = append(out.Records, DoctorEvidenceRecord{Kind: "comment_body", ID: fmt.Sprint(id), State: "captured", Content: body})
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		// Project only the selected child, never retain a parent's aggregate JSON.
		rows, err = tx.QueryContext(ctx, `SELECT d.story_key,d.stage FROM plan_story_drafts d JOIN sessions p ON p.task_ref=d.parent WHERE p.repo_id=(SELECT repo_id FROM sessions WHERE id=?) AND d.issue_number=? ORDER BY d.rowid DESC LIMIT 31`, out.SessionID, ref.Issue)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var key, stage string
			if err := rows.Scan(&key, &stage); err != nil {
				return errors.Join(err, rows.Close())
			}
			count++
			if count > 30 {
				out.Gaps = append(out.Gaps, "Story parent projections truncated")
				continue
			}
			if len(key) > 256 || len(stage) > 128 {
				return errors.Join(errors.New("doctor story metadata exceeds bounds"), rows.Close())
			}
			out.Records = append(out.Records, DoctorEvidenceRecord{Kind: "story", ID: key, State: stage})
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		rows, err = tx.QueryContext(ctx, `SELECT CAST(i.rowid AS TEXT),i.state FROM implement_stories i WHERE i.parent=? OR EXISTS(SELECT 1 FROM managed_stories m WHERE m.parent_task=i.parent AND m.generation=i.generation AND m.story_key=i.story_key AND substr(m.parent_task,1,instr(m.parent_task,'#'))||m.issue_number=?) ORDER BY i.rowid DESC LIMIT 31`, out.TaskRef, out.TaskRef)
		if err != nil {
			return err
		}
		count = 0
		for rows.Next() {
			var id, state string
			if err = rows.Scan(&id, &state); err != nil {
				return errors.Join(err, rows.Close())
			}
			count++
			if count > 30 {
				out.Gaps = append(out.Gaps, "Implement story rows truncated")
				continue
			}
			out.Records = append(out.Records, DoctorEvidenceRecord{Kind: "story", ID: id, State: state})
		}
		return errors.Join(rows.Err(), rows.Close())
	})
	if err != nil {
		return DoctorEvidence{}, fmt.Errorf("read doctor evidence: %w", err)
	}
	for i := range out.Records {
		out.Records[i].CapturedAt = out.CapturedAt
	}
	return out, nil
}

// DoctorLocalSources contains trusted roots only for collector use, never agent input.
type DoctorLocalSources struct{ ClonePath, WorktreeRoot, WorktreePath, Branch string }

// DoctorSources resolves recorded roots through the selected exact session.
func (db *DB) DoctorSources(ctx context.Context, ref naming.Ref) (DoctorLocalSources, error) {
	var out DoctorLocalSources
	err := db.sql.QueryRowContext(ctx, `SELECT r.path,r.worktree_root,COALESCE(s.worktree_path,''),COALESCE(s.branch,'') FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE lower(s.task_ref)=lower(?) AND s.issue_number=? AND r.forge=? AND lower(r.owner)=lower(?) AND lower(r.name)=lower(?)`, ref.String(), ref.Issue, ref.Forge, ref.Owner, ref.Repo).Scan(&out.ClonePath, &out.WorktreeRoot, &out.WorktreePath, &out.Branch)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}
