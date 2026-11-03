package daemon

import (
	"context"
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

func TestFactoryAlertRetainedInputAfterCompletion(t *testing.T) {
	for _, node := range []string{"delivered", "finished", "closed"} {
		for _, opening := range []string{"none", "queued", "confirmed", "uncertain"} {
			t.Run(node+"/"+opening, func(t *testing.T) {
				d, session, at := alertDaemonFixture(t)
				ctx := t.Context()
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				d.freeDiskBytes = func(string) (uint64, error) { return 1000, nil }
				_, err := d.db.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 40, AuthorUserID: 42, Body: "work", CreatedAt: session.CreatedAt, Classification: store.ClassificationInstruction})
				must(err)
				input, err := d.db.RetainOwnerInstruction(ctx, session.ID, 40, 42, "work", store.OwnerInputTarget{})
				must(err)
				sender := &retainedAlertSender{uncertain: opening == "uncertain"}
				drain := func(stamp time.Time) {
					t.Helper()
					for i := 0; i < 10; i++ {
						more, err := d.alerts.SendStep(ctx, stamp, "123", "456", sender)
						must(err)
						if !more {
							return
						}
					}
					t.Fatal("not idle")
				}
				if opening != "none" {
					must(d.observeAlerts(ctx, at))
					if opening == "confirmed" || opening == "uncertain" {
						drain(at)
					}
				}
				if node != "closed" {
					def, err := d.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "bug", ProfileVersion: 1, Path: "bug.dot", Digest: strings.Repeat("b", 64), CreatedAt: session.CreatedAt})
					must(err)
					g, err := d.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: node, StartedAt: session.CreatedAt})
					must(err)
					must(d.db.SetGraphInstanceStatus(ctx, g.ID, store.GraphSucceeded, time.Time{}, at.Add(-time.Hour)))
				}
				must(d.db.SetSessionState(ctx, session.ID, store.SessionFailed))
				must(d.db.ClearSessionNeedsPoll(ctx, session.ID))
				for i := 1; i <= 4; i++ {
					stamp := at.Add(time.Duration(i) * time.Minute)
					must(d.observeAlerts(ctx, stamp))
					if i >= 2 {
						drain(stamp)
					}
				}
				if opening == "uncertain" {
					if len(sender.messages) != 1 {
						t.Fatal("uncertain opening resent")
					}
					sender.uncertain = false
					drain(at.Add(10 * time.Minute))
				}
				// Persisted recovery and receipt debt must survive process restart.
				path := d.db.Path()
				must(d.db.Close())
				reopened, err := store.Open(ctx, path)
				must(err)
				d.db = reopened
				d.alerts.DB = reopened
				t.Cleanup(func() { _ = reopened.Close() })
				must(d.observeAlerts(ctx, at.Add(20*time.Minute)))
				drain(at.Add(20 * time.Minute))
				ledger, err := d.db.ReadAlerts(ctx)
				must(err)
				s := ledger.Subjects[alerts.SubjectID("no_progress", "task", session.TaskRef)]
				if s != nil && s.Active {
					t.Fatalf("completed task retains active alert: %+v", s)
				}
				want := 0
				if opening == "confirmed" || opening == "uncertain" {
					want = 2
					if s.Episodes[s.Episode].RecoveredAt.IsZero() {
						t.Fatal("missing recovery")
					}
				}
				if len(sender.messages) != want {
					t.Fatalf("sent %d notifications, want %d: %+v", len(sender.messages), want, sender.messages)
				}
				retained, err := d.db.RetainOwnerInstruction(ctx, session.ID, 40, 42, "work", store.OwnerInputTarget{})
				must(err)
				if retained != input {
					t.Fatal("retained instruction changed")
				}
			})
		}
	}
}

type retainedAlertSender struct {
	messages  []alerts.Message
	uncertain bool
}

func (s *retainedAlertSender) SendAlert(_ context.Context, m alerts.Message) alerts.Delivery {
	s.messages = append(s.messages, m)
	return alerts.Delivery{Confirmed: !s.uncertain, RemoteID: fmt.Sprint(len(s.messages))}
}
func (s *retainedAlertSender) ReconcileAlert(_ context.Context, _ alerts.Message, _ string) (string, error) {
	if !s.uncertain {
		return "1", nil
	}
	return "", nil
}

func TestFactoryAlertUnavailableScanCannotRecoverOrRepeat(t *testing.T) {
	d, session, at := alertDaemonFixture(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(d.db.SetSessionState(ctx, session.ID, store.SessionRunning))
	sender := &retainedAlertSender{}
	drain := func() {
		t.Helper()
		for i := 0; i < 10; i++ {
			more, err := d.alerts.SendStep(ctx, at, "123", "456", sender)
			must(err)
			if !more {
				return
			}
		}
		t.Fatal("not idle")
	}
	must(d.observeAlerts(ctx, at))
	drain()
	original := d.db
	repo, err := original.GetRepo(ctx, session.RepoID)
	must(err)
	must(original.SetSessionState(ctx, session.ID, store.SessionIdle))
	must(original.ClearSessionNeedsPoll(ctx, session.ID))
	// The disk observation succeeds in the ledger, then the source read fails.
	unavailable, err := store.Open(ctx, filepath.Join(t.TempDir(), "unavailable.db"))
	must(err)
	must(unavailable.Close())
	d.db = unavailable
	at = at.Add(time.Minute)
	if err = d.observeAlerts(ctx, at); err == nil {
		t.Fatal("unavailable source read accepted")
	}
	d.db = original
	ledger, err := original.ReadAlerts(ctx)
	must(err)
	key := alerts.SubjectID("no_progress", "task", session.TaskRef)
	if !ledger.Subjects[key].Active || ledger.Observers["no_progress"].Ready {
		t.Fatal("failed read supplied recovery")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = d.observeAlerts(canceled, at); err == nil {
		t.Fatal("cancelled read accepted")
	}
	// A bounded scan cannot use absence to clear an existing episode either.
	for i := 0; i < 1001; i++ {
		s, err := original.GetOrCreateSession(ctx, repo, 2000+i)
		must(err)
		must(original.SetSessionNeedsPoll(ctx, s.ID))
	}
	must(d.observeAlerts(ctx, at.Add(time.Minute)))
	drain()
	ledger, err = original.ReadAlerts(ctx)
	must(err)
	if !ledger.Subjects[key].Active || ledger.Observers["no_progress"].Ready {
		t.Fatal("incomplete scan supplied recovery")
	}
	if len(sender.messages) != 2 {
		t.Fatalf("successful subjects repeated after partial failure: %d", len(sender.messages))
	}
	for i := 0; i < 1001; i++ {
		s, err := original.GetOrCreateSession(ctx, repo, 2000+i)
		must(err)
		must(original.ClearSessionNeedsPoll(ctx, s.ID))
	}
	for i := 1; i <= 2; i++ {
		at = at.Add(time.Duration(i) * time.Minute)
		must(d.observeAlerts(ctx, at))
		drain()
	}
	ledger, err = original.ReadAlerts(ctx)
	must(err)
	if ledger.Subjects[key].Active || !ledger.Observers["no_progress"].Ready || len(sender.messages) != 3 {
		t.Fatal("complete retry failed to settle exactly once")
	}
}
