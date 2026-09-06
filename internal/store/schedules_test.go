package store

import (
	"testing"
	"time"
)

func TestScheduleReplayAndLifecycle(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 100)
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)

	first, created, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 10, OwnerHandle: "owner", Payload: "/implement T-1", DueAt: now,
	})
	if err != nil || !created {
		t.Fatalf("create first = (%+v, %t, %v), want created schedule", first, created, err)
	}
	replayed, created, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 10, OwnerHandle: "owner", Payload: "changed", DueAt: now.Add(time.Hour),
	})
	if err != nil || created || replayed.ID != first.ID || replayed.Payload != first.Payload {
		t.Fatalf("replay = (%+v, %t, %v), want original unchanged schedule", replayed, created, err)
	}

	second, created, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 11, OwnerHandle: "owner", Payload: "/implement T-2", Delay: 2 * time.Hour, PrerequisiteIDs: []int64{first.ID},
	})
	if err != nil || !created || !second.DueAt.IsZero() {
		t.Fatalf("create dependent = (%+v, %t, %v), want pending schedule without due time", second, created, err)
	}
	reserved, ok, err := db.ReserveSchedule(t.Context(), first.ID, now)
	if err != nil || !ok || reserved.Status != ScheduleRunning {
		t.Fatalf("reserve = (%+v, %t, %v), want running", reserved, ok, err)
	}
	finishedAt := now.Add(15 * time.Minute)
	if _, err := db.FinishSchedule(t.Context(), first.ID, ScheduleSucceeded, 0, finishedAt); err != nil {
		t.Fatalf("finish first: %v", err)
	}
	second, err = db.GetSchedule(t.Context(), second.ID)
	if err != nil || !second.DueAt.Equal(finishedAt.Add(2*time.Hour)) {
		t.Fatalf("dependent due = %s, %v; want %s", second.DueAt, err, finishedAt.Add(2*time.Hour))
	}
	eligible, err := db.ListEligibleSchedules(t.Context(), second.DueAt, 10)
	if err != nil || len(eligible) != 1 || eligible[0].ID != second.ID {
		t.Fatalf("eligible = %+v, %v; want dependent", eligible, err)
	}
}

func TestSchedulePrerequisitesAreIssueScopedAndFailuresBlock(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	firstSession := testSession(t, db, 101)
	secondSession := testSession(t, db, 102)
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	first, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: firstSession.ID, SourceCommentID: 1, OwnerHandle: "owner", Payload: "one", DueAt: now})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: secondSession.ID, SourceCommentID: 1, OwnerHandle: "owner", Payload: "two", Delay: time.Hour, PrerequisiteIDs: []int64{first.ID}}); err == nil {
		t.Fatal("created cross-session prerequisite")
	}
	dependent, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: firstSession.ID, SourceCommentID: 2, OwnerHandle: "owner", Payload: "two", Delay: time.Hour, PrerequisiteIDs: []int64{first.ID}})
	if err != nil {
		t.Fatalf("create dependent: %v", err)
	}
	if _, ok, err := db.ReserveSchedule(t.Context(), first.ID, now); err != nil || !ok {
		t.Fatalf("reserve: %t, %v", ok, err)
	}
	if _, err := db.FinishSchedule(t.Context(), first.ID, ScheduleFailed, 0, now); err != nil {
		t.Fatalf("fail first: %v", err)
	}
	blocked, err := db.GetSchedule(t.Context(), dependent.ID)
	if err != nil || blocked.Status != ScheduleBlocked {
		t.Fatalf("blocked = %+v, %v; want blocked", blocked, err)
	}
	toReply, err := db.ListUnreportedBlockedDescendants(t.Context(), first.ID)
	if err != nil || len(toReply) != 1 || toReply[0].ID != dependent.ID {
		t.Fatalf("blocked replies = (%+v, %v), want dependent", toReply, err)
	}
	if err := db.MarkScheduleBlockedReply(t.Context(), dependent.ID, now); err != nil {
		t.Fatalf("record blocked reply: %v", err)
	}
	toReply, err = db.ListUnreportedBlockedDescendants(t.Context(), first.ID)
	if err != nil || len(toReply) != 0 {
		t.Fatalf("blocked replies after record = (%+v, %v), want none", toReply, err)
	}
	count, err := db.CountPendingSchedules(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("pending count = %d, %v; want 0", count, err)
	}
}

func TestAcceptScheduleProposalIsAtomicAndReplaySafe(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 103)
	due := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)
	proposal, created, err := db.CreateOrGetScheduleProposal(t.Context(), CreateScheduleProposalParams{
		SessionID: session.ID, SourceCommentID: 20, OwnerHandle: "owner", Description: "overnight", Items: []ScheduleProposalItem{{Payload: "/implement T-1", DueAt: due}, {Payload: "/implement T-2", DueAt: due.Add(time.Hour)}},
	})
	if err != nil || !created {
		t.Fatalf("create proposal = (%+v, %t, %v)", proposal, created, err)
	}
	schedules, accepted, err := db.AcceptScheduleProposal(t.Context(), proposal.ID, due.Add(-time.Hour))
	if err != nil || !accepted || len(schedules) != 2 {
		t.Fatalf("accept = (%+v, %t, %v), want two schedules", schedules, accepted, err)
	}
	if schedules[0].SourceCommentID != schedules[1].SourceCommentID || schedules[0].TriggerID() == schedules[1].TriggerID() || !IsScheduleTrigger(schedules[0].TriggerID()) {
		t.Fatalf("proposal schedules must retain one source anchor but receive distinct private triggers: %+v", schedules)
	}
	replayed, accepted, err := db.AcceptScheduleProposal(t.Context(), proposal.ID, due)
	if err != nil || accepted || len(replayed) != 2 || replayed[0].ID != schedules[0].ID {
		t.Fatalf("replay accept = (%+v, %t, %v), want stored schedules", replayed, accepted, err)
	}
	count, err := db.CountPendingSchedules(t.Context())
	if err != nil || count != 2 {
		t.Fatalf("pending count = %d, %v; want 2", count, err)
	}
}

func TestReconcileOrphansRecoversScheduleReservations(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 104)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)
	withoutRun, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: session.ID, SourceCommentID: 30, OwnerHandle: "owner", Payload: "/implement T-1", DueAt: now})
	if err != nil {
		t.Fatalf("create no-run schedule: %v", err)
	}
	withRun, _, err := db.CreateOrGetSchedule(t.Context(), CreateScheduleParams{SessionID: session.ID, SourceCommentID: 31, OwnerHandle: "owner", Payload: "/implement T-2", DueAt: now})
	if err != nil {
		t.Fatalf("create run schedule: %v", err)
	}
	if _, ok, err := db.ReserveSchedule(t.Context(), withoutRun.ID, now); err != nil || !ok {
		t.Fatalf("reserve no-run schedule: %t, %v", ok, err)
	}
	if _, ok, err := db.ReserveSchedule(t.Context(), withRun.ID, now); err != nil || !ok {
		t.Fatalf("reserve run schedule: %t, %v", ok, err)
	}
	run := testRun(t, db, session.ID, withRun.TriggerID())
	if _, err := db.ReconcileOrphans(t.Context(), "new-instance"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	requeued, err := db.GetSchedule(t.Context(), withoutRun.ID)
	if err != nil || requeued.Status != SchedulePending || !requeued.StartedAt.IsZero() {
		t.Fatalf("reservation without run = %+v, %v; want pending", requeued, err)
	}
	finished, err := db.GetSchedule(t.Context(), withRun.ID)
	if err != nil || finished.Status != ScheduleFailed || finished.RunID != run.ID {
		t.Fatalf("reservation with abandoned run = %+v, %v; want failed run %d", finished, err, run.ID)
	}
}
