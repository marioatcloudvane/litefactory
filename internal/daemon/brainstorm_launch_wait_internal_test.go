package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// An admitted launch whose first Plan turn is still running is judged by its
// seed and first agent while the turn runs, and settles as created but not
// started once the wait passes, without touching the launch.
func TestBrainstormLaunchInFlightAdmittedWithoutBriefEndsAfterWait(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	if ok, err := f.db.MoveBrainstormLaunchAttempt(t.Context(), a.ID, store.BrainstormLaunchReserved, store.BrainstormLaunchRunning, "", 0, "", f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	a.State = store.BrainstormLaunchRunning
	f.admit(t, a.TriggerID())
	f.run(t, a.TriggerID(), store.RunRunning)

	f.now = f.at.Add(brainstormStartWait - 1)
	settled, err := f.d.boundOpenBrainstormAttempt(t.Context(), f.item, a, f.now)
	if err != nil || settled {
		t.Fatalf("before the wait: settled=%v %v", settled, err)
	}
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStarting {
		t.Fatalf("wait ended early: %+v", item)
	}
	f.now = f.at.Add(brainstormStartWait)
	if settled, err = f.d.boundOpenBrainstormAttempt(t.Context(), item, a, f.now); err != nil || !settled {
		t.Fatalf("after the wait: settled=%v %v", settled, err)
	}
	item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStartFailed || item.IssueNumber != 10 || item.FailedLaunchAttempts != 0 {
		t.Fatalf("after the wait: %+v", item)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if len(attempts) != 1 || attempts[0].ID != a.ID || attempts[0].State != store.BrainstormLaunchRunning {
		t.Fatalf("the wait changed the launch: %+v", attempts)
	}
}

// Seed complete and first agent dispatched mark the item started while the
// launch turn is still running.
func TestBrainstormLaunchInFlightStartsOnceSeededAndAgentDispatched(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	admitted, _ := f.admit(t, a.TriggerID())
	f.run(t, a.TriggerID(), store.RunRunning)
	if err := f.db.FinishBrainstormSeed(t.Context(), admitted.LaunchID, store.BrainstormSeedComplete, "", f.at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "set_agent", StartedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	settled, err := f.d.boundOpenBrainstormAttempt(t.Context(), f.item, a, f.now)
	if err != nil || !settled {
		t.Fatalf("settled=%v %v", settled, err)
	}
	if item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID); item.State != store.BrainstormItemPlanStarted {
		t.Fatalf("not started: %+v", item)
	}
}

// An attempt that is deferred on every pass (lock held, prelaunch) and never
// admitted settles as created but not started after the wait, through the
// launch step, keeping its one open attempt.
func TestBrainstormLaunchNeverAdmittedEndsNotStartedAfterWait(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	var handoff error
	if outcome, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, &handoff, f.at); err != nil || outcome != store.BrainstormLaunchOutcomeDeferred {
		t.Fatalf("deferral: %q %v", outcome, err)
	}
	f.now = f.at.Add(brainstormStartWait)
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if err := f.d.stepBrainstormLaunch(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStartFailed || item.FailedLaunchAttempts != 0 {
		t.Fatalf("never-admitted launch kept waiting: %+v", item)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if len(attempts) != 1 || attempts[0].ID != a.ID || !attempts[0].Open() {
		t.Fatalf("the wait changed the launch: %+v", attempts)
	}
}
