package daemon

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func alertDaemonFixture(t *testing.T) (*Daemon, store.Session, time.Time) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 1335)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	at := session.CreatedAt.Add(3 * time.Hour)
	d := &Daemon{db: db, alerts: engine, pool: newPool(1), diskPath: t.TempDir(), minFreeDiskBytes: 100, freeDiskBytes: func(string) (uint64, error) { return 50, nil }, now: func() time.Time { return at }, log: slog.New(slog.DiscardHandler)}
	return d, session, at
}

func TestFactoryAlertObservationContinuesWithFullPoolAndDiskHold(t *testing.T) {
	t.Parallel()
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	if _, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 99, OwnerHandle: "owner", Payload: "work", DueAt: at}); err != nil {
		t.Fatal(err)
	}
	if !d.pool.acquire(1) {
		t.Fatal("reserve full pool")
	}
	defer d.pool.release(1)
	if err := d.observeAlerts(ctx, at); err != nil {
		t.Fatal(err)
	}
	if err := d.observeAlerts(ctx, at.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err := d.db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{alerts.SubjectID("disk_low", "factory disk", "daemon-volume"), alerts.SubjectID("pool_blocked", "shared pool", "shared"), alerts.SubjectID("no_progress", "task", session.TaskRef)} {
		if ledger.Subjects[key] == nil || !ledger.Subjects[key].Active {
			t.Fatalf("missing active %s: %+v", key, ledger.Subjects)
		}
	}
	used, total := d.pool.capacity()
	if used != 1 || total != 1 {
		t.Fatalf("observer altered scheduling capacity %d/%d", used, total)
	}
	if len(ledger.Notifications) < 3 {
		t.Fatalf("no durable openings: %+v", ledger.Notifications)
	}
}

func TestFactoryAlertQueueCaptureRequiresConsumedSourceToRecover(t *testing.T) {
	t.Parallel()
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	d.alertQueueStopped(ctx, &task{session: session, commentUnderWay: 55, tickAt: at})
	key := alerts.SubjectID("task_stuck", "queue", fmt.Sprintf("%d:55", session.ID))
	if err := d.observeAlerts(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err := d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key] == nil || !ledger.Subjects[key].Active {
		t.Fatalf("stop capture %+v %v", ledger.Subjects[key], err)
	}
	if _, _, err = d.db.BootstrapWatermark(ctx, session.ID, 54); err != nil {
		t.Fatal(err)
	}
	if err = d.observeAlerts(ctx, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.AdvanceWatermark(ctx, session.ID, 55); err != nil {
		t.Fatal(err)
	}
	if err = d.observeAlerts(ctx, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || !ledger.Subjects[key].Active {
		t.Fatalf("one clean prematurely recovered %+v %v", ledger.Subjects[key], err)
	}
	if err = d.observeAlerts(ctx, at.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key].Active {
		t.Fatalf("consumed source did not recover %+v %v", ledger.Subjects[key], err)
	}
}

func TestFactoryAlertNoProgressNeedsTerminalSourceEvidence(t *testing.T) {
	t.Parallel()
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	if err := d.db.SetSessionState(ctx, session.ID, store.SessionRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.observeAlerts(ctx, at); err != nil {
		t.Fatal(err)
	}
	key := alerts.SubjectID("no_progress", "task", session.TaskRef)
	ledger, err := d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key] == nil || !ledger.Subjects[key].Active {
		t.Fatalf("stuck source missing %+v %v", ledger.Subjects[key], err)
	}
	if err = d.db.ClearSessionNeedsPoll(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = d.db.SetSessionState(ctx, session.ID, store.SessionIdle); err != nil {
		t.Fatal(err)
	}
	if err = d.observeAlerts(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || !ledger.Subjects[key].Active {
		t.Fatalf("first terminal observation recovered %+v %v", ledger.Subjects[key], err)
	}
	if err = d.observeAlerts(ctx, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key].Active {
		t.Fatalf("terminal evidence not recovered %+v %v", ledger.Subjects[key], err)
	}
}

func TestFactoryAlertDeadDispatchKeepsEvidenceAfterCleanup(t *testing.T) {
	t.Parallel()
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	definition, err := d.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := d.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := d.db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphFailed, time.Time{}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	dead, err := d.db.ListDeadGraphDispatches(ctx)
	if err != nil || len(dead) != 1 {
		t.Fatalf("dead source=%+v %v", dead, err)
	}
	d.alertDeadDispatch(ctx, dead[0], at.Add(time.Minute))
	key := alerts.SubjectID("task_stuck", "dispatch", fmt.Sprint(dispatch.ID))
	ledger, err := d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key] == nil || !ledger.Subjects[key].Active {
		t.Fatalf("dead source not retained %+v %v", ledger.Subjects[key], err)
	}
	if cancelled, err := d.db.CancelDeadGraphDispatch(ctx, dispatch.ID, at.Add(2*time.Minute)); err != nil || !cancelled {
		t.Fatalf("cleanup=%t %v", cancelled, err)
	}
	if err = d.observeAlerts(ctx, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || !ledger.Subjects[key].Active {
		t.Fatalf("one cleanup observation recovered %+v %v", ledger.Subjects[key], err)
	}
	if err = d.observeAlerts(ctx, at.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ledger, err = d.db.ReadAlerts(ctx)
	if err != nil || ledger.Subjects[key].Active {
		t.Fatalf("cleanup did not settle source %+v %v", ledger.Subjects[key], err)
	}
}

func TestFactoryAlertNeedsPollWithoutWorkDoesNotOpenProgress(t *testing.T) {
	t.Parallel()
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	if err := d.observeAlerts(ctx, at); err != nil {
		t.Fatal(err)
	}
	ledger, err := d.db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := alerts.SubjectID("no_progress", "task", session.TaskRef)
	if subject := ledger.Subjects[key]; subject != nil && subject.Active {
		t.Fatalf("discovery flag falsely opened no-progress: %+v", subject)
	}
}

func TestFactoryAlertBackoffExcludesPendingButStillObservesRunningWork(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			t.Parallel()
			d, session, at := alertDaemonFixture(t)
			ctx := t.Context()
			if _, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 99, OwnerHandle: "owner", Payload: "work", DueAt: at}); err != nil {
				t.Fatal(err)
			}
			if running {
				if err := d.db.SetSessionState(ctx, session.ID, store.SessionRunning); err != nil {
					t.Fatal(err)
				}
			}
			if !d.pool.acquire(1) {
				t.Fatal("reserve full pool")
			}
			defer d.pool.release(1)
			d.backoff.fail(at, time.Hour)
			for _, stamp := range []time.Time{at, at.Add(31 * time.Minute)} {
				if err := d.observeAlerts(ctx, stamp); err != nil {
					t.Fatal(err)
				}
			}
			ledger, err := d.db.ReadAlerts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			pool := ledger.Subjects[alerts.SubjectID("pool_blocked", "shared pool", "shared")]
			if pool != nil && pool.Active {
				t.Fatalf("backoff work counted as a pool waiter: %+v", pool)
			}
			progress := ledger.Subjects[alerts.SubjectID("no_progress", "task", session.TaskRef)]
			if progress == nil || progress.Active != running {
				t.Fatalf("running=%t progress=%+v", running, progress)
			}
		})
	}
}
