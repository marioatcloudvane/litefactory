package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrAnalystScheduleConflict identifies a stale mutation or expired writer.
var ErrAnalystScheduleConflict = errors.New("analyst schedule: stale version or fenced writer")

// AnalystSchedule retains opt-in intent and the current immutable config reference.
type AnalystSchedule struct {
	ID               string          `json:"id"`
	CreationKey      string          `json:"creation_key"`
	RequestDigest    string          `json:"request_digest"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	RepositoryID     int64           `json:"repository_id"`
	Config           json.RawMessage `json:"config"`
	Version          int64           `json:"version"`
	Generation       int64           `json:"generation"`
	Enabled          bool            `json:"enabled"`
	IntervalSeconds  int64           `json:"interval_seconds"`
	Anchor           time.Time       `json:"anchor"`
	NextDue          time.Time       `json:"next_due"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Operator         string          `json:"operator"`
	PausedAt         time.Time       `json:"paused_at,omitempty"`
	ReanalysisOf     int64           `json:"reanalysis_of,omitempty"`
	ReanalysisReason string          `json:"reanalysis_reason,omitempty"`
}

// AnalystScheduleMutation is a local-owner compare-and-swap edit.
type AnalystScheduleMutation struct {
	Config          json.RawMessage
	Name            string
	IntervalSeconds int64
	Enabled         *bool
	Operator        string
	ReanalysisOf    int64
	Reason          string
}

// AnalystSourceWork records assigned evidence and host-observed coverage.
type AnalystSourceWork struct {
	SourceKey   string          `json:"source_key"`
	RunID       int64           `json:"run_id,omitempty"`
	TaskRef     string          `json:"task_ref,omitempty"`
	IssueNumber int             `json:"issue_number,omitempty"`
	Kind        string          `json:"kind"`
	State       string          `json:"state"`
	Offered     bool            `json:"offered"`
	Cited       bool            `json:"cited"`
	Covered     bool            `json:"covered"`
	Reason      string          `json:"reason,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// AnalystOccurrence freezes one bounded pass and its fenced launch identity.
type AnalystOccurrence struct {
	ReanalysisOf int64               `json:"reanalysis_of,omitempty"`
	ID           int64               `json:"id"`
	ScheduleID   string              `json:"schedule_id"`
	Version      int64               `json:"version"`
	Generation   int64               `json:"generation"`
	RepositoryID int64               `json:"repository_id"`
	Kind         string              `json:"kind"`
	FirstDue     time.Time           `json:"first_due"`
	LastDue      time.Time           `json:"last_due"`
	MissedSlots  int64               `json:"missed_slots"`
	Cutoff       time.Time           `json:"cutoff"`
	State        string              `json:"state"`
	Disposition  string              `json:"disposition"`
	Reason       string              `json:"reason,omitempty"`
	AnalysisID   string              `json:"analysis_id,omitempty"`
	StartKey     string              `json:"start_key"`
	Fence        int64               `json:"fence"`
	LeaseUntil   time.Time           `json:"lease_until"`
	Config       json.RawMessage     `json:"config"`
	Works        []AnalystSourceWork `json:"works"`
	MorePending  bool                `json:"more_pending"`
	Standards    json.RawMessage     `json:"standards,omitempty"`
	Coverage     json.RawMessage     `json:"coverage,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	LaunchedAt   time.Time           `json:"launched_at,omitempty"`
	FinishedAt   time.Time           `json:"finished_at,omitempty"`
}

// AnalystProposalNotice is an immutable reference to an exact unapproved version.
type AnalystProposalNotice struct {
	OriginAt        time.Time `json:"origin_at"`
	ID              int64     `json:"id"`
	ScheduleID      string    `json:"schedule_id"`
	Version         int64     `json:"config_version"`
	OccurrenceID    int64     `json:"occurrence_id"`
	AnalysisID      string    `json:"analysis_id"`
	ProposalID      string    `json:"proposal_id"`
	ProposalVersion int64     `json:"proposal_version"`
	BaseRevision    int64     `json:"base_revision"`
	ChangeDigest    string    `json:"change_digest"`
	ResultDigest    string    `json:"result_digest"`
	OriginStatus    string    `json:"origin_status"`
	SchemaVersion   int       `json:"schema_version"`
	CreatedAt       time.Time `json:"created_at"`
}

func readAnalystSchedule(ctx context.Context, tx *sql.Tx, id string) (AnalystSchedule, error) {
	var a AnalystSchedule
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT record_json FROM analyst_schedules WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal([]byte(raw), &a)
	return a, err
}
func writeAnalystSchedule(ctx context.Context, tx *sql.Tx, a AnalystSchedule, version bool) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE analyst_schedules SET version=?,enabled=?,next_due=?,record_json=? WHERE id=?`, a.Version, a.Enabled, FormatTime(a.NextDue), string(raw), a.ID); err != nil {
		return err
	}
	if version {
		_, err = tx.ExecContext(ctx, `INSERT INTO analyst_schedule_versions(schedule_id,version,record_json) VALUES(?,?,?)`, a.ID, a.Version, string(raw))
	}
	return err
}
func validAnalystSchedule(a AnalystSchedule) error {
	if a.ID == "" || a.CreationKey == "" || len(a.CreationKey) > 128 || a.RequestDigest == "" || a.Name == "" || a.RepositoryID <= 0 || a.Operator == "" || !json.Valid(a.Config) || len(a.Config) > 64<<10 || a.IntervalSeconds < 3600 || a.IntervalSeconds > (1<<63-1)/int64(time.Second) {
		return errors.New("analyst schedule: invalid configuration")
	}
	if a.Kind != "logs" && a.Kind != "bugs" && a.Kind != "drift" {
		return errors.New("analyst schedule: invalid analyst kind")
	}
	return nil
}

// CreateAnalystSchedule saves disabled intent or returns an identical creation key.
func (db *DB) CreateAnalystSchedule(ctx context.Context, a AnalystSchedule) (AnalystSchedule, error) {
	if err := validAnalystSchedule(a); err != nil {
		return a, err
	}
	a.Version = 1
	a.Generation = 1
	a.Enabled = false
	a.CreatedAt = nowUTC()
	a.UpdatedAt = a.CreatedAt
	a.NextDue = time.Time{}
	a.Anchor = time.Time{}
	a.PausedAt = a.CreatedAt
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "schedule", ID: a.ID, Version: a.Version}
	}), func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM analyst_schedules WHERE creation_key=?`, a.CreationKey).Scan(&id)
		if err == nil {
			old, e := readAnalystSchedule(ctx, tx, id)
			if e != nil {
				return e
			}
			if old.RequestDigest != a.RequestDigest {
				return ErrAnalystScheduleConflict
			}
			a = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		raw, e := json.Marshal(a)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO analyst_schedules(id,creation_key,request_digest,repository_id,version,enabled,next_due,record_json) VALUES(?,?,?,?,1,0,?,?)`, a.ID, a.CreationKey, a.RequestDigest, a.RepositoryID, FormatTime(a.NextDue), string(raw))
		if e != nil {
			return e
		}
		return writeAnalystSchedule(ctx, tx, a, true)
	})
	return a, err
}

// GetAnalystSchedule reads current intent without provider access.
func (db *DB) GetAnalystSchedule(ctx context.Context, id string) (AnalystSchedule, error) {
	var a AnalystSchedule
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; a, e = readAnalystSchedule(ctx, tx, id); return e })
	return a, err
}

// GetAnalystScheduleVersion reads a retained immutable configuration.
func (db *DB) GetAnalystScheduleVersion(ctx context.Context, id string, version int64) (AnalystSchedule, error) {
	var a AnalystSchedule
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT record_json FROM analyst_schedule_versions WHERE schedule_id=? AND version=?`, id, version).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &a)
	}
	return a, err
}
func analystPage(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

// ListAnalystSchedules returns a bounded stable-ID page.
func (db *DB) ListAnalystSchedules(ctx context.Context, after string, limit int) ([]AnalystSchedule, error) {
	out := []AnalystSchedule{}
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM analyst_schedules WHERE id>? ORDER BY id LIMIT ?`, after, analystPage(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AnalystSchedule
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MutateAnalystSchedule atomically applies an expected-version owner edit.
func (db *DB) MutateAnalystSchedule(ctx context.Context, id string, expected int64, m AnalystScheduleMutation, now time.Time) (AnalystSchedule, error) {
	var a AnalystSchedule
	err := db.standardsWrite(withStandardsConversationEffect(ctx, func() StandardsConversationEffect {
		return StandardsConversationEffect{DomainKind: "schedule", ID: a.ID, Version: a.Version}
	}), func(tx *sql.Tx) error {
		var e error
		a, e = readAnalystSchedule(ctx, tx, id)
		if e != nil {
			return e
		}
		if a.Version != expected || m.Operator == "" {
			return ErrAnalystScheduleConflict
		}
		if len(m.Config) > 0 {
			a.Config = m.Config
		}
		if m.Name != "" {
			a.Name = m.Name
		}
		if m.IntervalSeconds != 0 && m.IntervalSeconds != a.IntervalSeconds {
			a.IntervalSeconds = m.IntervalSeconds
			if a.Enabled && !a.Anchor.IsZero() {
				interval := time.Duration(a.IntervalSeconds) * time.Second
				lower := now
				var priorRaw string
				priorErr := tx.QueryRowContext(ctx, `SELECT record_json FROM analyst_occurrences WHERE schedule_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&priorRaw)
				if priorErr == nil {
					var prior AnalystOccurrence
					if e = json.Unmarshal([]byte(priorRaw), &prior); e != nil {
						return e
					}
					if prior.LastDue.After(lower) {
						lower = prior.LastDue
					}
				} else if !errors.Is(priorErr, sql.ErrNoRows) {
					return priorErr
				}
				a.NextDue = a.Anchor
				if !lower.Before(a.Anchor) {
					a.NextDue = a.Anchor.Add((lower.Sub(a.Anchor)/interval + 1) * interval)
				}
			}
		}
		if m.Enabled != nil && *m.Enabled != a.Enabled {
			a.Enabled = *m.Enabled
			if a.Enabled {
				a.Anchor = now
				a.NextDue = now
				// A backward clock cannot recreate a nominal slot already owned.
				var priorRaw string
				priorErr := tx.QueryRowContext(ctx, `SELECT record_json FROM analyst_occurrences WHERE schedule_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&priorRaw)
				if priorErr == nil {
					var prior AnalystOccurrence
					if e = json.Unmarshal([]byte(priorRaw), &prior); e != nil {
						return e
					}
					if !now.After(prior.LastDue) {
						a.NextDue = prior.LastDue.Add(time.Duration(a.IntervalSeconds) * time.Second)
						a.Anchor = a.NextDue
					}
				} else if !errors.Is(priorErr, sql.ErrNoRows) {
					return priorErr
				}
				if !a.PausedAt.IsZero() {
					a.PausedAt = time.Time{}
				}
			} else {
				a.PausedAt = now
			}
		}
		if m.ReanalysisOf != 0 {
			if m.Reason == "" {
				return errors.New("analyst schedule: reanalysis requires reason")
			}
			o, e := readAnalystOccurrence(ctx, tx, m.ReanalysisOf)
			if e != nil {
				return e
			}
			if o.ScheduleID != id || o.State == "reserved" || o.State == "launched" {
				return ErrAnalystScheduleConflict
			}
			a.Generation++
			a.ReanalysisOf = m.ReanalysisOf
			a.ReanalysisReason = m.Reason
			if a.Enabled {
				a.NextDue = now
			}
		}
		a.Operator = m.Operator
		a.Version++
		a.UpdatedAt = now
		if e = validAnalystSchedule(a); e != nil {
			return e
		}
		return writeAnalystSchedule(ctx, tx, a, true)
	})
	return a, err
}
func readAnalystOccurrence(ctx context.Context, tx *sql.Tx, id int64) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT record_json FROM analyst_occurrences WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &o)
	}
	return o, err
}
func writeAnalystOccurrence(ctx context.Context, tx *sql.Tx, o AnalystOccurrence) error {
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE analyst_occurrences SET active=?,record_json=? WHERE id=?`, o.State == "reserved" || o.State == "launched", string(raw), o.ID)
	return err
}

// GetAnalystOccurrence reads retained activity and source coverage.
func (db *DB) GetAnalystOccurrence(ctx context.Context, id int64) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; o, e = readAnalystOccurrence(ctx, tx, id); return e })
	return o, err
}

// ListAnalystOccurrences returns a bounded activity page.
func (db *DB) ListAnalystOccurrences(ctx context.Context, schedule string, after int64, limit int) ([]AnalystOccurrence, error) {
	out := []AnalystOccurrence{}
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM analyst_occurrences WHERE (?='' OR schedule_id=?) AND id>? ORDER BY id LIMIT ?`, schedule, schedule, after, analystPage(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o AnalystOccurrence
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DueAnalystSchedules orders eligible intent by oldest due time then ID.
func (db *DB) DueAnalystSchedules(ctx context.Context, now time.Time, limit int) ([]AnalystSchedule, error) {
	out := []AnalystSchedule{}
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM analyst_schedules WHERE enabled=1 AND next_due<=? AND NOT EXISTS(SELECT 1 FROM analyst_occurrences o WHERE o.repository_id=analyst_schedules.repository_id AND o.active=1) ORDER BY next_due,id LIMIT ?`, FormatTime(now), analystPage(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AnalystSchedule
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReserveAnalystOccurrence coalesces elapsed slots under exclusive repository ownership.
func (db *DB) ReserveAnalystOccurrence(ctx context.Context, id string, expected int64, now time.Time, lease time.Duration) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	if lease <= 0 || lease > time.Hour {
		return o, errors.New("analyst schedule: invalid lease")
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, e := readAnalystSchedule(ctx, tx, id)
		if e != nil {
			return e
		}
		if !a.Enabled || a.Version != expected || a.NextDue.After(now) {
			return ErrAnalystScheduleConflict
		}
		interval := time.Duration(a.IntervalSeconds) * time.Second
		slots := int64(now.Sub(a.NextDue)/interval) + 1
		o = AnalystOccurrence{ScheduleID: id, ReanalysisOf: a.ReanalysisOf, Version: a.Version, Generation: a.Generation, RepositoryID: a.RepositoryID, Kind: a.Kind, FirstDue: a.NextDue, LastDue: a.NextDue.Add(time.Duration(slots-1) * interval), MissedSlots: slots - 1, Cutoff: now, State: "reserved", Disposition: "not_evaluated", Fence: 1, LeaseUntil: now.Add(lease), Config: a.Config, Works: []AnalystSourceWork{}, CreatedAt: now}
		slot := FormatTime(o.FirstDue)
		if o.ReanalysisOf != 0 {
			slot += fmt.Sprintf(":reanalysis:%d", o.Generation)
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO analyst_occurrences(schedule_id,repository_id,slot,active,record_json) VALUES(?,?,?,1,'{}')`, id, a.RepositoryID, slot)
		if e != nil {
			return e
		}
		o.ID, e = res.LastInsertId()
		if e != nil {
			return e
		}
		o.StartKey = fmt.Sprintf("schedule:%s:occurrence:%d", id, o.ID)
		if e = writeAnalystOccurrence(ctx, tx, o); e != nil {
			return e
		}
		a.NextDue = o.LastDue.Add(interval)
		a.ReanalysisOf = 0
		return writeAnalystSchedule(ctx, tx, a, false)
	})
	return o, err
}

// AnalystSourceQuery narrows terminal metadata discovery within saved authority.
type AnalystSourceQuery struct {
	After    time.Time
	TaskRefs []string
	Modes    []string
	Limit    int
}

// SelectAnalystSources freezes bounded authorized terminal membership in the same transaction.
func (db *DB) SelectAnalystSources(ctx context.Context, id, fence int64, q AnalystSourceQuery, now time.Time) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	if q.Limit < 1 || q.Limit > 5 || len(q.TaskRefs) > 50 || len(q.Modes) > 20 {
		return o, errors.New("analyst schedule: invalid source limits")
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var e error
		o, e = checkAnalystWriter(ctx, tx, id, fence, now)
		if e != nil {
			return e
		}
		if o.State != "reserved" {
			return ErrAnalystScheduleConflict
		}
		if len(o.Works) > 0 {
			return nil
		}
		tasks, _ := json.Marshal(q.TaskRefs)
		modes, _ := json.Marshal(q.Modes)
		rows, e := tx.QueryContext(ctx, `SELECT r.id,s.task_ref,s.issue_number,CASE WHEN r.workflow='bug' THEN 'bug' ELSE r.mode END FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.repo_id=? AND r.status IN ('succeeded','failed','reported') AND r.finished_at IS NOT NULL AND r.finished_at<=? AND r.finished_at>=? AND (?='null' OR ?='[]' OR s.task_ref IN (SELECT value FROM json_each(?))) AND (?='null' OR ?='[]' OR CASE WHEN r.workflow='bug' THEN 'bug' ELSE r.mode END IN (SELECT value FROM json_each(?))) AND ((?=0 AND NOT EXISTS(SELECT 1 FROM analyst_source_work w WHERE w.schedule_id=? AND w.source_key='run:'||r.id)) OR (?<>0 AND EXISTS(SELECT 1 FROM analyst_source_work w WHERE w.occurrence_id=? AND w.source_key='run:'||r.id))) ORDER BY r.finished_at DESC,r.id DESC LIMIT 50`, o.RepositoryID, FormatTime(o.Cutoff), FormatTime(q.After), string(tasks), string(tasks), string(tasks), string(modes), string(modes), string(modes), o.ReanalysisOf, o.ScheduleID, o.ReanalysisOf, o.ReanalysisOf)
		if e != nil {
			return e
		}
		var candidates []AnalystSourceWork
		for rows.Next() {
			var w AnalystSourceWork
			var mode string
			if e = rows.Scan(&w.RunID, &w.TaskRef, &w.IssueNumber, &mode); e != nil {
				_ = rows.Close()
				return e
			}
			w.SourceKey = fmt.Sprintf("run:%d", w.RunID)
			w.Kind = "run"
			w.State = "selected"
			candidates = append(candidates, w)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		o.MorePending = len(candidates) > q.Limit
		if len(candidates) > q.Limit {
			candidates = candidates[:q.Limit]
		}
		o.Works = candidates
		for _, w := range o.Works {
			if e = insertAnalystWork(ctx, tx, o, w); e != nil {
				return e
			}
		}
		return writeAnalystOccurrence(ctx, tx, o)
	})
	return o, err
}
func insertAnalystWork(ctx context.Context, tx *sql.Tx, o AnalystOccurrence, w AnalystSourceWork) error {
	raw, e := json.Marshal(w)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO analyst_source_work(schedule_id,generation,source_key,occurrence_id,record_json) VALUES(?,?,?,?,?)`, o.ScheduleID, o.Generation, w.SourceKey, o.ID, string(raw))
	return e
}
func checkAnalystWriter(ctx context.Context, tx *sql.Tx, id, fence int64, now time.Time) (AnalystOccurrence, error) {
	o, e := readAnalystOccurrence(ctx, tx, id)
	if e != nil {
		return o, e
	}
	if o.Fence != fence || (o.State != "reserved" && o.State != "launched") || !o.LeaseUntil.After(now) {
		return o, ErrAnalystScheduleConflict
	}
	return o, nil
}

// SetAnalystSources supports pinned local drift windows and retains missing mapping gaps.
func (db *DB) SetAnalystSources(ctx context.Context, id, fence int64, works []AnalystSourceWork, now time.Time) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	if len(works) > 5 {
		return o, errors.New("analyst schedule: too many sources")
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var e error
		o, e = checkAnalystWriter(ctx, tx, id, fence, now)
		if e != nil {
			return e
		}
		if o.State != "reserved" {
			return ErrAnalystScheduleConflict
		}
		if len(o.Works) > 0 {
			return ErrAnalystScheduleConflict
		}
		for _, w := range works {
			if w.SourceKey == "" {
				return errors.New("analyst schedule: missing source identity")
			}
			if e = insertAnalystWork(ctx, tx, o, w); e != nil {
				return e
			}
		}
		o.Works = works
		return writeAnalystOccurrence(ctx, tx, o)
	})
	return o, err
}

type analystOccurrenceContextKey struct{}
type analystOccurrenceLease struct {
	id, fence int64
	now       time.Time
}

// WithAnalystOccurrence binds upstream allocation to an enabled fenced reservation.
func WithAnalystOccurrence(ctx context.Context, id, fence int64, now time.Time) context.Context {
	return context.WithValue(ctx, analystOccurrenceContextKey{}, analystOccurrenceLease{id, fence, now})
}

// attachAnalystAllocation runs only inside the upstream allocation transaction.
func attachAnalystAllocation(ctx context.Context, tx *sql.Tx, kind, id, startKey string) error {
	lease, ok := ctx.Value(analystOccurrenceContextKey{}).(analystOccurrenceLease)
	if !ok {
		return nil
	}
	o, e := checkAnalystWriter(ctx, tx, lease.id, lease.fence, lease.now)
	if e != nil {
		return e
	}
	if o.State != "reserved" || o.Kind != kind || o.StartKey != startKey {
		return ErrAnalystScheduleConflict
	}
	a, e := readAnalystSchedule(ctx, tx, o.ScheduleID)
	if e != nil {
		return e
	}
	if !a.Enabled {
		return ErrAnalystScheduleConflict
	}
	o.AnalysisID = id
	o.State = "launched"
	o.LaunchedAt = nowUTC()
	return writeAnalystOccurrence(ctx, tx, o)
}

// FinalizeAnalystOccurrence commits source dispositions and exact proposal notices atomically.
func (db *DB) FinalizeAnalystOccurrence(ctx context.Context, o AnalystOccurrence, notices []AnalystProposalNotice, now time.Time) error {
	if o.State == "reserved" || o.State == "launched" || o.State == "" || len(notices) > 10 {
		return errors.New("analyst schedule: invalid finalization")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		old, e := checkAnalystWriter(ctx, tx, o.ID, o.Fence, now)
		if e != nil {
			return e
		}
		if old.ScheduleID != o.ScheduleID || old.AnalysisID != o.AnalysisID || len(old.Works) != len(o.Works) {
			return ErrAnalystScheduleConflict
		}
		for i, w := range o.Works {
			if w.SourceKey != old.Works[i].SourceKey {
				return ErrAnalystScheduleConflict
			}
			raw, e := json.Marshal(w)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE analyst_source_work SET record_json=? WHERE schedule_id=? AND generation=? AND source_key=? AND occurrence_id=?`, string(raw), o.ScheduleID, o.Generation, w.SourceKey, o.ID); e != nil {
				return e
			}
		}
		for _, n := range notices {
			var linked int
			query := `SELECT COUNT(*) FROM log_analysis_proposals WHERE analysis_id=? AND proposal_id=? AND version=?`
			switch old.Kind {
			case "drift":
				query = `SELECT COUNT(*) FROM drift_proposal_links WHERE analysis_id=? AND proposal_id=? AND version=?`
			case "bugs":
				query = `SELECT COUNT(*) FROM prevention_analysis_proposals WHERE analysis_id=? AND proposal_id=? AND proposal_version=?`
			}
			if e = tx.QueryRowContext(ctx, query, old.AnalysisID, n.ProposalID, n.ProposalVersion).Scan(&linked); e != nil {
				return e
			}
			if linked != 1 {
				return ErrAnalystScheduleConflict
			}
			p, e := readStandardsProposal(ctx, tx, n.ProposalID, n.ProposalVersion)
			if e != nil {
				return e
			}
			if p.BaseRevision != n.BaseRevision || p.ChangeDigest != n.ChangeDigest || p.ResultDigest != n.ResultDigest {
				return ErrAnalystScheduleConflict
			}
			n.ID = 0
			n.ScheduleID = o.ScheduleID
			n.Version = o.Version
			n.OccurrenceID = o.ID
			n.AnalysisID = o.AnalysisID
			n.OriginStatus = "unapproved"
			n.SchemaVersion = 1
			n.CreatedAt = now
			n.OriginAt = p.CreatedAt
			rawBytes, e := json.Marshal(n)
			if e != nil {
				return e
			}
			res, e := tx.ExecContext(ctx, `INSERT INTO analyst_proposal_notices(schedule_id,occurrence_id,proposal_id,proposal_version,record_json) VALUES(?,?,?,?,?)`, n.ScheduleID, n.OccurrenceID, n.ProposalID, n.ProposalVersion, string(rawBytes))
			if e != nil {
				return e
			}
			n.ID, e = res.LastInsertId()
			if e != nil {
				return e
			}
			// The stable identifier is projected from the row at read time; notice bytes remain immutable.
		}
		if o.FinishedAt.IsZero() {
			o.FinishedAt = now
		}
		o.LaunchedAt = old.LaunchedAt
		return writeAnalystOccurrence(ctx, tx, o)
	})
}

// ListAnalystProposalNotices returns exact notices in stable ID order.
func (db *DB) ListAnalystProposalNotices(ctx context.Context, schedule string, after int64, limit int) ([]AnalystProposalNotice, error) {
	out := []AnalystProposalNotice{}
	rows, e := db.sql.QueryContext(ctx, `SELECT id,record_json FROM analyst_proposal_notices WHERE (?='' OR schedule_id=?) AND id>? ORDER BY id LIMIT ?`, schedule, schedule, after, analystPage(limit))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var n AnalystProposalNotice
		var id int64
		var raw string
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &n); e != nil {
			return nil, e
		}
		n.ID = id
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetAnalystProposalNotice reads one immutable notice.
func (db *DB) GetAnalystProposalNotice(ctx context.Context, id int64) (AnalystProposalNotice, error) {
	var n AnalystProposalNotice
	var raw string
	e := db.sql.QueryRowContext(ctx, `SELECT record_json FROM analyst_proposal_notices WHERE id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal([]byte(raw), &n)
	}
	n.ID = id
	return n, e
}

// RecoverAnalystOccurrence acquires expired bookkeeping with a new fence, never relaunching a model.
func (db *DB) RecoverAnalystOccurrence(ctx context.Context, id int64, now time.Time, lease time.Duration) (AnalystOccurrence, error) {
	var o AnalystOccurrence
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var e error
		o, e = readAnalystOccurrence(ctx, tx, id)
		if e != nil {
			return e
		}
		if (o.State != "reserved" && o.State != "launched") || o.LeaseUntil.After(now) {
			return ErrAnalystScheduleConflict
		}
		o.Fence++
		o.LeaseUntil = now.Add(lease)
		return writeAnalystOccurrence(ctx, tx, o)
	})
	return o, err
}

// ListActiveAnalystOccurrences bounds receipt reconciliation independently of history.
func (db *DB) ListActiveAnalystOccurrences(ctx context.Context, limit int) ([]AnalystOccurrence, error) {
	out := []AnalystOccurrence{}
	rows, e := db.sql.QueryContext(ctx, `SELECT record_json FROM analyst_occurrences WHERE active=1 ORDER BY id LIMIT ?`, analystPage(limit))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var o AnalystOccurrence
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &o); e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// HasAnalystSource preserves ordinary membership across configuration and generation changes.
func (db *DB) HasAnalystSource(ctx context.Context, schedule string, generation int64, key string) (bool, error) {
	var found int
	e := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM analyst_source_work WHERE schedule_id=? AND source_key=?)`, schedule, key).Scan(&found)
	return found != 0, e
}

// LatestCoveredAnalystDrift reads the last fully examined accepted drift endpoint.
func (db *DB) LatestCoveredAnalystDrift(ctx context.Context, schedule string, generation int64) (AnalystSourceWork, error) {
	var w AnalystSourceWork
	var raw string
	e := db.sql.QueryRowContext(ctx, `SELECT w.record_json FROM analyst_source_work w JOIN analyst_occurrences o ON o.id=w.occurrence_id WHERE w.schedule_id=? AND o.active=0 AND json_extract(o.record_json,'$.state')='completed' AND json_extract(w.record_json,'$.covered')=1 AND json_extract(w.record_json,'$.kind')='drift' ORDER BY o.id DESC LIMIT 1`, schedule).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal([]byte(raw), &w)
	}
	return w, e
}

// UnresolvedAnalystDrift returns a pinned window requiring deliberate reanalysis.
func (db *DB) UnresolvedAnalystDrift(ctx context.Context, schedule string) (AnalystSourceWork, error) {
	var w AnalystSourceWork
	var record string
	e := db.sql.QueryRowContext(ctx, `SELECT w.record_json FROM analyst_source_work w JOIN analyst_occurrences o ON o.id=w.occurrence_id WHERE w.schedule_id=? AND o.active=0 AND json_extract(w.record_json,'$.kind')='drift' AND json_extract(w.record_json,'$.covered')=0 AND NOT EXISTS(SELECT 1 FROM analyst_source_work done WHERE done.schedule_id=w.schedule_id AND done.source_key=w.source_key AND json_extract(done.record_json,'$.covered')=1) ORDER BY o.id DESC LIMIT 1`, schedule).Scan(&record)
	if errors.Is(e, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal([]byte(record), &w)
	}
	return w, e
}
