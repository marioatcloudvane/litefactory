package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListenerStatus is the daemon's published webhook listener state. It is
// display-only: `daemon status` reads it and nothing in the daemon does.
type ListenerStatus struct {
	// ListenAddress is the bound host:port, empty when the listener is off.
	ListenAddress string
	// Listening reports that the listener is actually bound.
	Listening bool
	// DaemonStartedAt is when the publishing daemon started.
	DaemonStartedAt time.Time
	// WrittenAt is when the snapshot was last written.
	WrittenAt time.Time

	// LastHeartbeatStartedAt is when the publishing daemon last began a
	// heartbeat; zero before its first.
	LastHeartbeatStartedAt time.Time
	// DiscoveryPaused reports that the daemon is not reading comments.
	DiscoveryPaused bool
	// PauseReason says why, in the words the daemon logged; empty while running.
	PauseReason string
	// PausedSince is when the pause began; zero while running.
	PausedSince time.Time
	// ExpectedResumeAt is when discovery is expected to resume; zero while
	// running, and while paused means "when fresh rate-limit data arrives".
	ExpectedResumeAt time.Time
	// RunsInFlight is how many turns the daemon had running when it last wrote.
	RunsInFlight int
	// InFlightSessions are the sessions behind RunsInFlight, ordered by id.
	InFlightSessions []int64
	// InFlightLabels names, per in-flight session, the dispatch source that
	// started its worker (for example "backlog+graph_owner_wait" or
	// "due_schedule"). A session without a label is absent.
	InFlightLabels map[int64]string
	// SlotsUsed and SlotsTotal are the shared worker pool's occupancy; a zero
	// total means the publishing daemon did not report it.
	SlotsUsed, SlotsTotal int
	// ChatStatus is empty when chat is off, "available" once chat runs, and
	// "unavailable: <reason>" while it cannot.
	ChatStatus string
}

// RepoDiscoveryStatus is the published discovery health of one repository.
// Zero times mean "none": no delivery since start, no finished listing, no
// scheduled next listing.
type RepoDiscoveryStatus struct {
	RepoID int64
	// Owner and Name are filled by [DB.ReadHealthSnapshot] only.
	Owner string
	Name  string

	LastDeliveryAt     time.Time
	LastPollFinishedAt time.Time
	NextPollDueAt      time.Time
	PollOwed           bool
	SecretPresent      bool
	WebhookMode        string
	WrittenAt          time.Time
}

// HealthSnapshot is what [DB.ReadHealthSnapshot] returns.
type HealthSnapshot struct {
	// Found is false when no daemon has ever published a snapshot.
	Found    bool
	Listener ListenerStatus
	Repos    []RepoDiscoveryStatus
}

// ReplaceHealthSnapshot replaces the whole snapshot in one transaction: the
// listener row is upserted and every repository row not in repos is dropped, so
// a removed repository leaves nothing behind.
func (db *DB) ReplaceHealthSnapshot(ctx context.Context, listener ListenerStatus, repos []RepoDiscoveryStatus) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO daemon_listener_status (id, listen_address, listening, daemon_started_at, written_at,
    last_heartbeat_started_at, discovery_paused, pause_reason, paused_since, expected_resume_at, runs_in_flight, chat_status,
    in_flight_sessions, slots_used, slots_total)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET listen_address = excluded.listen_address, listening = excluded.listening,
    daemon_started_at = excluded.daemon_started_at, written_at = excluded.written_at,
    last_heartbeat_started_at = excluded.last_heartbeat_started_at, discovery_paused = excluded.discovery_paused,
    pause_reason = excluded.pause_reason, paused_since = excluded.paused_since,
    expected_resume_at = excluded.expected_resume_at, runs_in_flight = excluded.runs_in_flight,
    chat_status = excluded.chat_status, in_flight_sessions = excluded.in_flight_sessions,
    slots_used = excluded.slots_used, slots_total = excluded.slots_total`,
			listener.ListenAddress, boolInt(listener.Listening), FormatTime(listener.DaemonStartedAt), FormatTime(listener.WrittenAt),
			nullTime(listener.LastHeartbeatStartedAt), boolInt(listener.DiscoveryPaused), listener.PauseReason,
			nullTime(listener.PausedSince), nullTime(listener.ExpectedResumeAt), listener.RunsInFlight, listener.ChatStatus,
			joinIDs(listener.InFlightSessions, listener.InFlightLabels), listener.SlotsUsed, listener.SlotsTotal); err != nil {
			return fmt.Errorf("write listener status: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM repo_discovery_status`); err != nil {
			return fmt.Errorf("clear repository status: %w", err)
		}
		for _, r := range repos {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO repo_discovery_status
    (repo_id, last_delivery_at, last_poll_finished_at, next_poll_due_at, poll_owed, secret_present, webhook_mode, written_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				r.RepoID, nullTime(r.LastDeliveryAt), nullTime(r.LastPollFinishedAt), nullTime(r.NextPollDueAt),
				boolInt(r.PollOwed), boolInt(r.SecretPresent), r.WebhookMode, FormatTime(r.WrittenAt)); err != nil {
				return fmt.Errorf("write status of repository %d: %w", r.RepoID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: replace health snapshot: %w", err)
	}
	return nil
}

// ReadHealthSnapshot reads the published snapshot, repositories ordered by
// owner and name. A database nobody has published to yields Found == false.
func (db *DB) ReadHealthSnapshot(ctx context.Context) (HealthSnapshot, error) {
	var snap HealthSnapshot
	var listening, paused int
	var started, written, inFlight string
	var beat, pausedSince, resumeAt sql.NullString
	err := db.sql.QueryRowContext(ctx,
		`SELECT listen_address, listening, daemon_started_at, written_at, last_heartbeat_started_at,
       discovery_paused, pause_reason, paused_since, expected_resume_at, runs_in_flight, chat_status,
       in_flight_sessions, slots_used, slots_total
FROM daemon_listener_status WHERE id = 1`).
		Scan(&snap.Listener.ListenAddress, &listening, &started, &written, &beat,
			&paused, &snap.Listener.PauseReason, &pausedSince, &resumeAt, &snap.Listener.RunsInFlight, &snap.Listener.ChatStatus,
			&inFlight, &snap.Listener.SlotsUsed, &snap.Listener.SlotsTotal)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return HealthSnapshot{}, nil
	case err != nil:
		return HealthSnapshot{}, fmt.Errorf("store: read listener status: %w", err)
	}
	snap.Found = true
	snap.Listener.Listening = listening != 0
	if snap.Listener.DaemonStartedAt, err = ParseTime(started); err != nil {
		return HealthSnapshot{}, fmt.Errorf("store: parse listener status: %w", err)
	}
	if snap.Listener.WrittenAt, err = ParseTime(written); err != nil {
		return HealthSnapshot{}, fmt.Errorf("store: parse listener status: %w", err)
	}
	snap.Listener.DiscoveryPaused = paused != 0
	if snap.Listener.InFlightSessions, snap.Listener.InFlightLabels, err = splitIDs(inFlight); err != nil {
		return HealthSnapshot{}, fmt.Errorf("store: parse listener status: %w", err)
	}
	for _, f := range []struct {
		dst *time.Time
		src sql.NullString
	}{{&snap.Listener.LastHeartbeatStartedAt, beat}, {&snap.Listener.PausedSince, pausedSince}, {&snap.Listener.ExpectedResumeAt, resumeAt}} {
		if *f.dst, err = scanTime(f.src); err != nil {
			return HealthSnapshot{}, fmt.Errorf("store: parse listener status: %w", err)
		}
	}

	rows, err := db.sql.QueryContext(ctx, `
SELECT s.repo_id, r.owner, r.name, s.last_delivery_at, s.last_poll_finished_at, s.next_poll_due_at,
       s.poll_owed, s.secret_present, s.webhook_mode, s.written_at
FROM repo_discovery_status s JOIN repos r ON r.id = s.repo_id
ORDER BY r.owner, r.name, s.repo_id`)
	if err != nil {
		return HealthSnapshot{}, fmt.Errorf("store: read repository status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r                    RepoDiscoveryStatus
			delivery, poll, next sql.NullString
			owed, secret         int
			written              string
		)
		if err := rows.Scan(&r.RepoID, &r.Owner, &r.Name, &delivery, &poll, &next, &owed, &secret, &r.WebhookMode, &written); err != nil {
			return HealthSnapshot{}, fmt.Errorf("store: scan repository status: %w", err)
		}
		r.PollOwed, r.SecretPresent = owed != 0, secret != 0
		for _, f := range []struct {
			dst *time.Time
			src sql.NullString
		}{{&r.LastDeliveryAt, delivery}, {&r.LastPollFinishedAt, poll}, {&r.NextPollDueAt, next}} {
			if *f.dst, err = scanTime(f.src); err != nil {
				return HealthSnapshot{}, fmt.Errorf("store: parse status of repository %d: %w", r.RepoID, err)
			}
		}
		if r.WrittenAt, err = ParseTime(written); err != nil {
			return HealthSnapshot{}, fmt.Errorf("store: parse status of repository %d: %w", r.RepoID, err)
		}
		snap.Repos = append(snap.Repos, r)
	}
	if err := rows.Err(); err != nil {
		return HealthSnapshot{}, fmt.Errorf("store: read repository status: %w", err)
	}
	return snap, nil
}

// joinIDs and splitIDs keep a short id list in one display-only column. An
// entry is "id" or, when the id has a label, "id=label"; a label never holds a
// comma or an equals sign. Values written before labels existed are id-only.
func joinIDs(ids []int64, labels map[int64]string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
		if label := strings.NewReplacer(",", "+", "=", "+").Replace(labels[id]); label != "" {
			parts[i] += "=" + label
		}
	}
	return strings.Join(parts, ",")
}

func splitIDs(s string) ([]int64, map[int64]string, error) {
	if s == "" {
		return nil, nil, nil
	}
	var ids []int64
	var labels map[int64]string
	for _, part := range strings.Split(s, ",") {
		raw, label, labelled := strings.Cut(part, "=")
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		if labelled && label != "" {
			if labels == nil {
				labels = make(map[int64]string)
			}
			labels[id] = label
		}
	}
	return ids, labels, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
