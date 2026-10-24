package store

import (
	"context"
	"fmt"
	"time"
)

// ActiveTaskWork is one task with a live run or a running graph step. Mode
// and Node come from the graph when a step runs, else from the run.
type ActiveTaskWork struct {
	SessionID, RepoID int64
	IssueNumber       int
	TaskRef           string
	Mode              string
	Node              string
	Since             time.Time
}

// QueuedTaskWork is one task owed a start: a graph step due or retrying, or a
// session carrying the backlog flag. DueAt is zero for backlog-only sessions.
type QueuedTaskWork struct {
	SessionID, RepoID int64
	IssueNumber       int
	TaskRef           string
	Node              string
	DueAt             time.Time
	Attempts          int
}

// BackgroundWork is chat-side work that runs outside the task scheduler: a
// standards author draft, a log analysis or a chat diagnosis.
type BackgroundWork struct {
	Kind, What string
	Since      time.Time
	// Queued marks a diagnosis that is accepted but not yet being worked on.
	Queued bool
}

// ListActiveTaskWork returns every task with a prelaunch or running run, or a
// running graph node attempt, one entry per task ordered by session id.
func (db *DB) ListActiveTaskWork(ctx context.Context) ([]ActiveTaskWork, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT s.id, s.repo_id, s.issue_number, s.task_ref, x.mode, x.node, x.since FROM (
    SELECT session_id, mode, '' AS node, started_at AS since FROM runs WHERE status IN ('prelaunch','running')
    UNION ALL
    SELECT i.session_id, CASE WHEN g.mode<>'' THEN g.mode ELSE g.workflow END, a.node, a.started_at
    FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions g ON g.id=i.definition_id
    WHERE a.status='running'
) x JOIN sessions s ON s.id=x.session_id ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("store: list active task work: %w", err)
	}
	defer rows.Close()
	var out []ActiveTaskWork
	for rows.Next() {
		var w ActiveTaskWork
		var since string
		if err := rows.Scan(&w.SessionID, &w.RepoID, &w.IssueNumber, &w.TaskRef, &w.Mode, &w.Node, &since); err != nil {
			return nil, fmt.Errorf("store: scan active task work: %w", err)
		}
		if w.Since, err = ParseTime(since); err != nil {
			return nil, fmt.Errorf("store: scan active task work: %w", err)
		}
		// One task: the graph step names where it is; the earliest start wins.
		if n := len(out); n > 0 && out[n-1].SessionID == w.SessionID {
			last := &out[n-1]
			if last.Node == "" && w.Node != "" {
				last.Mode, last.Node = w.Mode, w.Node
			}
			if w.Since.Before(last.Since) {
				last.Since = w.Since
			}
			continue
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list active task work: %w", err)
	}
	return out, nil
}

// ListQueuedTaskWork returns pending graph steps that are due or waiting out
// a failed start, then backlog-flagged sessions with no such step, ordered by
// due time and session id. A step parked for a later time it has never tried
// (an await, an owner wait) is scheduled, not queued, and is left out.
func (db *DB) ListQueuedTaskWork(ctx context.Context, now time.Time) ([]QueuedTaskWork, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT s.id, s.repo_id, s.issue_number, s.task_ref, d.node, d.due_at, d.attempts FROM graph_dispatches d JOIN sessions s ON s.id=d.session_id
WHERE d.status='pending' AND (d.due_at<=? OR d.attempts>0)
UNION ALL
SELECT s.id, s.repo_id, s.issue_number, s.task_ref, '', '', 0 FROM sessions s
WHERE s.needs_poll=1 AND NOT EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.session_id=s.id AND d.status='pending' AND (d.due_at<=? OR d.attempts>0))
ORDER BY 6, 1`, FormatTime(now), FormatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: list queued task work: %w", err)
	}
	defer rows.Close()
	var out []QueuedTaskWork
	for rows.Next() {
		var w QueuedTaskWork
		var due string
		if err := rows.Scan(&w.SessionID, &w.RepoID, &w.IssueNumber, &w.TaskRef, &w.Node, &due, &w.Attempts); err != nil {
			return nil, fmt.Errorf("store: scan queued task work: %w", err)
		}
		if due != "" {
			if w.DueAt, err = ParseTime(due); err != nil {
				return nil, fmt.Errorf("store: scan queued task work: %w", err)
			}
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list queued task work: %w", err)
	}
	return out, nil
}

// ListBackgroundWork returns chat-side work in progress: standards author
// attempts whose lease still holds, running log analyses, and chat diagnoses,
// a claimed one as running and an unclaimed one as queued.
func (db *DB) ListBackgroundWork(ctx context.Context, now time.Time) ([]BackgroundWork, error) {
	stamp := FormatTime(now)
	rows, err := db.sql.QueryContext(ctx, `
SELECT 'standards draft', s.mode||' '||s.target_id, json_extract(a.attempt_json,'$.created_at'), 0
FROM standards_authoring_sessions s JOIN standards_authoring_attempts a ON a.id=s.active_attempt
WHERE a.lease_until>? AND json_extract(a.attempt_json,'$.outcome')='running'
UNION ALL
SELECT 'log analysis', id, created_at, 0 FROM log_analyses WHERE state='running'
UNION ALL
SELECT 'chat diagnosis', CASE WHEN target_ref<>'' THEN target_ref ELSE message_id END, created_at,
       CASE WHEN answer_attempts>0 AND next_attempt_at>? THEN 0 ELSE 1 END
FROM chat_inbox WHERE command='diagnose' AND state='working'
ORDER BY 3`, stamp, stamp)
	if err != nil {
		return nil, fmt.Errorf("store: list background work: %w", err)
	}
	defer rows.Close()
	var out []BackgroundWork
	for rows.Next() {
		var w BackgroundWork
		var since string
		if err := rows.Scan(&w.Kind, &w.What, &since, &w.Queued); err != nil {
			return nil, fmt.Errorf("store: scan background work: %w", err)
		}
		if w.Since, err = ParseTime(since); err != nil {
			return nil, fmt.Errorf("store: scan background work: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list background work: %w", err)
	}
	return out, nil
}
