package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type recoveryTickKey struct{}

type reportRecoveryCache struct {
	mu     sync.Mutex
	items  map[string]store.ReportRecovery
	dirty  map[string]bool
	loaded bool
}

type recoveryPending struct{ cause error }

func (e *recoveryPending) Error() string { return "report recovery remains pending" }
func (e *recoveryPending) Unwrap() error { return e.cause }
func isRecoveryPending(err error) bool   { var pending *recoveryPending; return errors.As(err, &pending) }

func recoveryKey(session, trigger int64) string { return fmt.Sprintf("source:%d:%d", session, trigger) }
func recoveryTime(ctx context.Context) time.Time {
	at, _ := ctx.Value(recoveryTickKey{}).(time.Time)
	return at
}

// refreshRecoveries restores durable due times before any report retry and
// saves in-process outage debt. Local changes win over stale persisted rows.
func (d *Daemon) refreshRecoveries(ctx context.Context, at time.Time) {
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	d.initRecoveries()
	if !d.recoveries.loaded {
		rows, err := d.db.ListReportRecoveries(ctx)
		if err == nil {
			for _, row := range rows {
				held, e := d.db.DoctorRepairHeldSession(ctx, row.SessionID)
				if (e != nil || held) && row.State != "resolved" {
					continue
				}
				if _, exists := d.recoveries.items[row.Key]; exists {
					continue
				}
				if row.State == "resolved" {
					d.recoveries.dirty[row.Key] = true
				}
				if row.State != "resolved" && strings.HasPrefix(row.Key, "stopped:") && row.RunID != 0 {
					run, readErr := d.db.GetRun(ctx, row.RunID)
					if readErr == nil && (run.Reportable() || run.Status == store.RunReported) {
						completed := row
						completed.State = "resolved"
						d.recoveries.items[row.Key] = completed
						d.recoveries.dirty[row.Key] = true
						if run.Status == store.RunReported {
							continue
						}
						row.Key = recoveryKey(row.SessionID, row.TriggerID)
						row.State = "pending"
						d.recoveries.dirty[row.Key] = true
					}
				}
				if _, exists := d.recoveries.items[row.Key]; !exists {
					d.recoveries.items[row.Key] = row
				}
			}
			d.recoveries.loaded = true
		}
	}
	keys := make([]string, 0, len(d.recoveries.dirty))
	for key := range d.recoveries.dirty {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row := d.recoveries.items[key]
		held, e := d.doctorHeldSession(ctx, row.SessionID)
		if e != nil || held {
			continue
		}
		if strings.HasPrefix(key, "stopped:") && row.State == "resolved" && d.recoveries.dirty[strings.TrimPrefix(key, "stopped:")] {
			continue
		}
		if row.State == "resolved" {
			d.removeResolvedRecovery(ctx, row)
			continue
		}
		if row.NextAt.IsZero() && at.IsZero() {
			continue
		}
		if row.NextAt.IsZero() {
			row.NextAt = at.Add(d.recoveryInterval())
			d.recoveries.items[key] = row
		}
		if err := d.db.PutReportRecovery(ctx, row); err == nil {
			delete(d.recoveries.dirty, key)
		}
	}
}
func (d *Daemon) initRecoveries() {
	if d.recoveries.items == nil {
		d.recoveries.items = make(map[string]store.ReportRecovery)
		d.recoveries.dirty = make(map[string]bool)
	}
}
func (d *Daemon) recoveryInterval() time.Duration {
	if d.interval > 0 {
		return d.interval
	}
	return DefaultInterval
}

func (d *Daemon) recoveryAllowed(ctx context.Context, source store.ReportRecovery, at time.Time) (bool, error) {
	if !at.IsZero() {
		if _, held := d.backoff.hold(at); held {
			return false, nil
		}
	}
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	d.initRecoveries()
	row, exists := d.recoveries.items[source.Key]
	if !exists && !d.recoveries.loaded && d.db != nil {
		saved, err := d.db.GetReportRecovery(ctx, source.Key)
		if err == nil {
			row = saved
			exists = true
			d.recoveries.items[source.Key] = saved
			if saved.State == "resolved" {
				d.recoveries.dirty[source.Key] = true
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return false, err
		}
	}
	if !exists || row.State == "resolved" {
		return true, nil
	}
	if at.IsZero() || row.NextAt.IsZero() || at.Before(row.NextAt) {
		return false, nil
	}
	return true, nil
}

func (d *Daemon) recoveryFailure(ctx context.Context, source store.ReportRecovery, at time.Time, err error) {
	var rate *github.RateLimitError
	if errors.As(err, &rate) && !at.IsZero() {
		d.backoff.fail(at, rate.RetryAfter)
	}
	reason := "report recovery failed"
	sanitizer := d.diagnosticRedactor
	if sanitizer == nil {
		sanitizer = redact.New(redact.Options{Environ: []string{}})
	}
	if body, redactErr := sanitizer.Apply(ctx, err.Error(), 1024); redactErr == nil {
		reason = body.String()
	}
	state := "pending"
	var delivery *turn.DeliveryError
	if errors.As(err, &delivery) {
		source.DeliveryKey = delivery.Key
		state = delivery.State
	}
	if source.State == "stopped" {
		state = "stopped"
	}
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	d.initRecoveries()
	previous, exists := d.recoveries.items[source.Key]
	if previous.State == "resolved" {
		exists = false
	}
	source.Attempts = 0
	if exists {
		source.Attempts = previous.Attempts + 1
		source.Escalated = previous.Escalated
		if source.RunID == 0 {
			source.RunID = previous.RunID
		}
		if source.Task == "" {
			source.Task = previous.Task
		}
		if source.DeliveryKey == "" {
			source.DeliveryKey = previous.DeliveryKey
		}
	}
	source.State = state
	source.Reason = reason
	delay := d.recoveryInterval()
	for i := 0; i < source.Attempts && delay < MaxBackoff; i++ {
		delay *= 2
	}
	if delay > MaxBackoff {
		delay = MaxBackoff
	}
	if !at.IsZero() {
		source.NextAt = at.Add(delay)
	}
	escalate := source.Attempts == 3 && !source.Escalated
	if escalate {
		source.Escalated = true
	}
	d.recoveries.items[source.Key] = source
	d.recoveries.dirty[source.Key] = true
	if d.db != nil && !source.NextAt.IsZero() {
		if saveErr := d.db.PutReportRecovery(context.WithoutCancel(ctx), source); saveErr == nil {
			delete(d.recoveries.dirty, source.Key)
		}
	}
	if !exists || escalate || previous.State != state {
		d.log.ErrorContext(ctx, "report recovery pending", slog.String("task", source.Task), slog.String("delivery", source.DeliveryKey), slog.String("recovery", source.Key), slog.Int64("run_id", source.RunID), slog.String("state", state), slog.String("reason", reason), slog.Int("recovery_attempts", source.Attempts), slog.Bool("escalated", source.Escalated), slog.Time("next_eligible", source.NextAt))
	}
}
func (d *Daemon) recoverySucceeded(ctx context.Context, key string) {
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	row, exists := d.recoveries.items[key]
	if !exists || row.State == "resolved" {
		return
	}
	row.State = "resolved"
	d.recoveries.items[key] = row
	d.recoveries.dirty[key] = true
	d.removeResolvedRecovery(context.WithoutCancel(ctx), row)
	d.log.InfoContext(ctx, "report recovery resolved", slog.String("task", row.Task), slog.String("delivery", row.DeliveryKey), slog.String("recovery", key), slog.Int64("run_id", row.RunID))
}

// removeResolvedRecovery checkpoints completion before deleting the live debt.
// The caller holds recoveries.mu; failed writes retain dirty state for refresh.
func (d *Daemon) removeResolvedRecovery(ctx context.Context, row store.ReportRecovery) {
	if d.db == nil {
		return
	}
	held, err := d.doctorHeldSession(ctx, row.SessionID)
	if err != nil || held || (strings.HasPrefix(row.Key, "stopped:") && d.recoveries.dirty[strings.TrimPrefix(row.Key, "stopped:")]) {
		return
	}
	if row.NextAt.IsZero() {
		// Source-only outage debt has no deadline. Resolved rows are never due.
		row.NextAt = time.Unix(0, 0).UTC()
	}
	if err := d.db.PutReportRecovery(ctx, row); err != nil {
		return
	}
	if err := d.db.DeleteReportRecovery(ctx, row.Key); err != nil {
		return
	}
	delete(d.recoveries.items, row.Key)
	delete(d.recoveries.dirty, row.Key)
}

// guardedRecoveryAllowed consults durable holds only after the source is due.
// A failed hold read must never turn each ineligible tick into a new attempt.
func (d *Daemon) guardedRecoveryAllowed(ctx context.Context, source store.ReportRecovery, at time.Time) (bool, error) {
	allowed, err := d.recoveryAllowed(ctx, source, at)
	if err != nil || !allowed {
		return allowed, err
	}
	held, err := d.doctorHeldSession(ctx, source.SessionID)
	return !held && err == nil, err
}

// guardedReportAttempt keeps persistence/exclusion checks at the production
// effect boundary. reportAttempt owns cadence and outage debt independently.
func (d *Daemon) guardedReportAttempt(ctx context.Context, source store.ReportRecovery, attempt func() (turn.Posted, error)) (turn.Posted, error) {
	return d.reportAttempt(ctx, source, func() (turn.Posted, error) {
		held, err := d.doctorHeldSession(ctx, source.SessionID)
		if err != nil {
			return turn.Posted{Skipped: true}, err
		}
		if held {
			return turn.Posted{Skipped: true}, &recoveryPending{}
		}
		return attempt()
	})
}

func (d *Daemon) reportAttempt(ctx context.Context, source store.ReportRecovery, attempt func() (turn.Posted, error)) (turn.Posted, error) {
	at := recoveryTime(ctx)
	allowed, err := d.recoveryAllowed(ctx, source, at)
	if err != nil {
		d.recoveryFailure(ctx, source, at, err)
		return turn.Posted{Skipped: true}, &recoveryPending{err}
	}
	if !allowed {
		return turn.Posted{Skipped: true}, &recoveryPending{}
	}
	posted, err := attempt()
	if isRecoveryPending(err) {
		return posted, err
	}
	if err != nil {
		d.recoveryFailure(ctx, source, at, err)

		return posted, &recoveryPending{err}
	}
	d.recoverySucceeded(ctx, source.Key)
	return posted, nil
}

func (d *Daemon) reportTurn(ctx context.Context, result turn.Result) (turn.Posted, error) {
	source := store.ReportRecovery{Key: recoveryKey(result.SessionID, result.TriggerCommentID), SessionID: result.SessionID, TriggerID: result.TriggerCommentID, RunID: result.Run.ID, Task: result.Ref.String()}
	return d.guardedReportAttempt(ctx, source, func() (turn.Posted, error) { return d.reporter.ReportTurn(ctx, result) })
}
func (d *Daemon) reportRun(ctx context.Context, rep turn.RunReport) (turn.Posted, error) {
	source := store.ReportRecovery{Key: recoveryKey(rep.SessionID, rep.CommentID), SessionID: rep.SessionID, TriggerID: rep.CommentID, RunID: rep.RunID, Task: rep.Run.Ref.String()}
	return d.guardedReportAttempt(ctx, source, func() (turn.Posted, error) { return d.reporter.ReportRun(ctx, rep) })
}
func (d *Daemon) reportReply(ctx context.Context, req turn.ReplyRequest) (turn.Posted, error) {
	source := store.ReportRecovery{Key: recoveryKey(req.SessionID, req.CommentID), SessionID: req.SessionID, TriggerID: req.CommentID, Task: req.Ref.String()}
	return d.guardedReportAttempt(ctx, source, func() (turn.Posted, error) { return d.reporter.ReportReply(ctx, req) })
}

// A blocked source retains this task's ordering but needs no worker until due.
func (d *Daemon) sessionRecoveryHeld(sessionID int64, at time.Time) bool {
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	for _, row := range d.recoveries.items {
		if row.State != "resolved" && row.SessionID == sessionID && (row.NextAt.IsZero() || at.Before(row.NextAt)) {
			return true
		}
	}
	return false
}

func (d *Daemon) resumeReport(ctx context.Context, t *task, triggerID int64) (bool, error) {
	// Bug domain journals own publication, including recovery of native runs.
	admission, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
	if err == nil && admission.Workflow == "bug" {
		return false, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return true, err
	}
	reporter, ok := d.reporter.(interface {
		ReportPending(context.Context, int64, int64) (turn.Posted, bool, error)
	})
	if !ok {
		return false, nil
	}
	source := store.ReportRecovery{Key: recoveryKey(t.session.ID, triggerID), SessionID: t.session.ID, TriggerID: triggerID, Task: t.ref.String()}
	allowed, err := d.guardedRecoveryAllowed(ctx, source, t.tickAt)
	if err != nil {
		d.recoveryFailure(ctx, source, t.tickAt, err)
		return true, &recoveryPending{err}
	}
	if !allowed {
		return true, &recoveryPending{}
	}
	_, pending, err := reporter.ReportPending(ctx, t.session.ID, triggerID)
	if err != nil {
		d.recoveryFailure(ctx, source, t.tickAt, err)
		return true, &recoveryPending{err}
	}
	if pending {
		if err := d.retireModeSelections(ctx, t.session.ID); err != nil {
			d.recoveryFailure(ctx, source, t.tickAt, &turn.DeliveryError{State: "cleanup", Cause: err})
			return true, &recoveryPending{err}
		}
		d.recoverySucceeded(ctx, source.Key)
	}
	return pending, nil
}
