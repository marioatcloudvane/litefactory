package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/store"
)

// serveAlerts has its own joined, bounded cadence. Neither observation nor
// delivery borrows run slots or waits for a conversation provider.
func (d *Daemon) serveAlerts(ctx context.Context, wg *sync.WaitGroup) {
	if d.alerts == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.alerts.SyncConfiguration(ctx); err != nil {
			d.log.WarnContext(ctx, "alert configuration projection unavailable")
		}
		timer := time.NewTicker(30 * time.Second)
		defer timer.Stop()
		for {
			scan, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := d.observeAlerts(scan, d.alertClock())
			cancel()
			if err != nil {
				d.log.WarnContext(ctx, "factory alert observation unavailable")
			}
			d.alertMu.RLock()
			sender, channel, owner := d.alertSender, d.alertChannel, d.alertOwner
			d.alertMu.RUnlock()
			send, cancel := context.WithTimeout(ctx, 20*time.Second)
			for range 32 {
				var more bool
				more, err = d.alerts.SendStep(send, d.alertClock(), channel, owner, sender)
				if err != nil || !more {
					break
				}
			}
			cancel()
			if err != nil {
				d.log.WarnContext(ctx, "factory alert journal or delivery unavailable")
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
func (d *Daemon) observeAlerts(ctx context.Context, at time.Time) (resultErr error) {
	defer func() {
		if resultErr != nil {
			for _, rule := range []string{"pool_blocked", "no_progress", "task_stuck", "delivery_stuck", "provider_failure"} {
				_ = d.alerts.Readiness(ctx, rule, at, false, "observation_unavailable")
			}
		}
	}()
	read := d.freeDiskBytes
	if read == nil {
		read = bugchecks.FreeDiskBytes
	}
	var free uint64
	var probeErr error
	if d.diskPath != "" && d.minFreeDiskBytes > 0 {
		free, probeErr = read(d.diskPath)
	}
	if err := d.alerts.Disk(ctx, at, free, d.minFreeDiskBytes, probeErr); err != nil {
		return err
	}
	tasks, complete, err := d.db.AlertTaskSources(ctx, at)
	if err != nil {
		return err
	}
	// A standing dispatch backoff is a deliberate wait for pending work. It
	// never excuses an already running task. A failed connection lookup has
	// no affirmative wait evidence and makes this snapshot unavailable.
	for i := range tasks {
		if tasks[i].Running || (!tasks[i].HasWork && !tasks[i].Waiting) {
			continue
		}
		until, held := d.connectionHold(sessionContext(ctx, tasks[i].SessionID), at)
		if held && until.IsZero() {
			complete = false
		} else if held {
			tasks[i].Waiting = true
			tasks[i].Eligible = false
		}
	}
	waiting := false
	used, total := d.pool.capacity()
	for _, task := range tasks {
		if !complete || (!task.HasWork && !task.Waiting) {
			continue
		}
		if err := d.alerts.Progress(ctx, task, at); err != nil {
			return err
		}
		if task.Eligible && !task.Waiting && used >= total {
			waiting = true
		}
	}
	if err := d.alerts.Pool(ctx, at, waiting, complete); err != nil {
		return err
	}
	reports, reportsComplete, err := d.db.AlertReportSources(ctx)
	if err != nil {
		return err
	}
	for _, report := range reports {
		first, err := d.db.AlertReportUncertainSince(ctx, report.Key, at)
		if err != nil {
			return err
		}
		condition := "neutral"
		if at.Sub(first) > time.Duration(d.alerts.Settings.DeliveryWaitMinutes)*time.Minute {
			condition = "failing"
		}
		if err := d.alerts.Observe(ctx, alerts.Observation{Rule: "delivery_stuck", Kind: "report", Key: report.Key, TaskRef: report.Task, At: at, ID: fmt.Sprintf("report:%s:%d", report.Key, at.UnixNano()), Condition: condition, Severity: "warning", Summary: "A task report has remained unconfirmed beyond its delivery delay.", Action: "Inspect the original report delivery in doctor. Do not replay the report."}); err != nil {
			return err
		}
	}
	l, err := d.db.ReadAlerts(ctx)
	if err != nil {
		return err
	}
	for _, s := range l.Subjects {
		if !s.Active {
			continue
		}
		clean := false
		switch s.Rule {
		case "delivery_stuck":
			if reportsComplete {
				clean, err = d.db.AlertReportSettled(ctx, s.Key)
			}
		case "task_stuck":
			clean, err = d.db.AlertTaskSettled(ctx, s.Kind, s.Key)
		case "no_progress":
			if complete {
				found := false
				for _, t := range tasks {
					if t.Task == s.Key {
						found = true
						break
					}
				}
				if !found {
					clean, err = d.db.AlertTaskTerminal(ctx, s.Key)
				}
			}
		}
		if err != nil {
			return err
		}
		if clean {
			if err := d.alerts.Observe(ctx, alerts.Observation{Rule: s.Rule, Kind: s.Kind, Key: s.Key, At: at, Condition: "clean", Severity: s.Severity, RecoveryEvidence: fmt.Sprintf("settled:%s:%d", s.Key, at.UnixNano()), EvidenceAt: at}); err != nil {
				return err
			}
		}
	}
	if err := d.alerts.ConfirmProviderSuccess(ctx, at); err != nil {
		return err
	}
	for _, rule := range []string{"no_progress", "pool_blocked", "task_stuck", "delivery_stuck", "provider_failure"} {
		ready := complete
		if rule == "delivery_stuck" {
			ready = reportsComplete
		}
		reason := ""
		if !ready {
			reason = "observation_unavailable"
		}
		if err := d.alerts.Readiness(ctx, rule, at, ready, reason); err != nil {
			return err
		}
	}
	return nil
}
func (d *Daemon) alertQueueStopped(ctx context.Context, t *task) {
	if d.alerts == nil {
		return
	}
	key := fmt.Sprintf("%d:%d", t.session.ID, t.commentUnderWay)
	kind := "queue"
	if dispatch, ok := store.GraphTriggerDispatch(t.commentUnderWay); ok {
		kind = "dispatch"
		key = fmt.Sprint(dispatch)
	} else if t.scheduled != nil {
		kind = "schedule"
		key = fmt.Sprint(t.scheduled.ID)
	}
	if err := d.alerts.Observe(ctx, alerts.Observation{Rule: "task_stuck", Kind: kind, Key: key, TaskRef: t.session.TaskRef, At: t.tickAt, ID: "queue-stop:" + kind + ":" + key, Condition: "failing", Severity: "warning", Summary: "A task queue stopped with an unconsumed input.", Action: "Inspect the task report and retained doctor evidence before choosing the next action."}); err != nil {
		d.log.WarnContext(ctx, "queue alert evidence unavailable")
	}
}
func (d *Daemon) alertDeadDispatch(ctx context.Context, dispatch store.GraphDispatch, at time.Time) {
	if d.alerts == nil {
		return
	}
	session, err := d.db.GetSession(ctx, dispatch.SessionID)
	if err != nil {
		return
	}
	key := fmt.Sprint(dispatch.ID)
	if err := d.alerts.Observe(ctx, alerts.Observation{Rule: "task_stuck", Kind: "dispatch", Key: key, TaskRef: session.TaskRef, At: at, ID: "dead-dispatch:" + key, Condition: "failing", Severity: "warning", Summary: "A task dispatch can no longer run on its original workflow.", Action: "Inspect the task's workflow settlement or replacement in doctor."}); err != nil {
		d.log.WarnContext(ctx, "dispatch alert evidence unavailable")
	}
}

func providerAlertClass(class string) string {
	switch class {
	case "auth":
		return "authentication"
	case "http_5xx":
		return "5xx"
	default:
		return class
	}
}
func recordProviderAlert(ctx context.Context, e *alerts.Engine, provider string, req agent.Request, result *agent.RunResult, runErr error, at time.Time) error {
	class := providerAlertClass(string(agent.ProviderOutcome(result, runErr)))
	id := fmt.Sprintf("run:%s:%d:%s:%s:%s", req.Ref.String(), req.RunOrdinal, req.Session.Ref, req.Session.Resume, req.Path)
	if req.Usage != nil && req.Usage.AttemptID != "" {
		id = "operation:" + req.Usage.AttemptID
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return e.Provider(cleanup, alerts.ProviderOutcome{ID: id, Provider: provider, Account: "local-default", Source: req.Ref.String(), Class: class, At: at})
}
