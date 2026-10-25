package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func recordOwnerInput(t *testing.T, db *DB, sessionID, commentID int64, body string) {
	t.Helper()
	if _, err := db.RecordComment(t.Context(), Comment{SessionID: sessionID, ForgeCommentID: commentID, AuthorUserID: 42, Body: body, CreatedAt: graphTestTime(), Classification: ClassificationInstruction}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerInputAtomicTargetedHandoffAndCrashRead(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	recordOwnerInput(t, db, i.SessionID, 30, "  exact bytes\nline two  ")
	target := OwnerInputTarget{InstanceID: i.ID, Node: "review"}
	input, err := db.RetainOwnerInstruction(t.Context(), i.SessionID, 30, 42, "  exact bytes\nline two  ", target)
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, i.SessionID, 50)
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListOwnerInstructions(t.Context(), i.SessionID, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("intervening step consumed targeted input: %v %v", pending, err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "next", FinishedAt: graphTestTime(), Transition: &GraphTransition{InstanceID: i.ID, FromNode: "work", ToNode: "review", OnOutcome: "next", At: graphTestTime()}}); err != nil {
		t.Fatal(err)
	}
	b, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "review", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	// Opening the durable database again models recovery after assignment, before
	// the receiving agent starts. The original assignment and exact bytes survive.
	reopened, err := Open(t.Context(), db.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	assigned, err := reopened.ListOwnerInstructions(t.Context(), i.SessionID, b.ID)
	if err != nil || len(assigned) != 1 {
		t.Fatalf("recovered input %v %v", assigned, err)
	}
	if assigned[0].Body != input.Body || assigned[0].Target != target || assigned[0].AttemptID != b.ID {
		t.Fatalf("changed recovered assignment: %+v", assigned[0])
	}
	replay, err := reopened.RetainOwnerInstruction(t.Context(), i.SessionID, 30, 42, input.Body, target)
	if err != nil || !reflect.DeepEqual(replay, assigned[0]) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	pending, err = reopened.ListOwnerInstructions(t.Context(), i.SessionID, 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("assigned input stayed pending %v %v", pending, err)
	}
	if err = db.FinishGraphNodeAttempt(t.Context(), b.ID, GraphSucceeded, "done", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	c, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "review", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.ListOwnerInstructions(t.Context(), i.SessionID, c.ID)
	if err != nil || len(again) != 0 {
		t.Fatalf("input repeated on later step %v %v", again, err)
	}
}

func TestOwnerInputRefusedAllocationAndNoNextStepStayPending(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	recordOwnerInput(t, db, i.SessionID, 20, "change the wording")
	if _, err := db.RetainOwnerInstruction(t.Context(), i.SessionID, 20, 42, "change the wording", OwnerInputTarget{}); err != nil {
		t.Fatal(err)
	}
	// Transport/effect work cannot take owner direction.
	a := graphTestAttempt(t, db, i, 1)
	pending, err := db.ListOwnerInstructions(t.Context(), i.SessionID, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("effect consumed input %v %v", pending, err)
	}
	run := testRun(t, db, i.SessionID, 40)
	if _, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()}); !errors.Is(err, ErrGraphAttemptRunning) {
		t.Fatalf("allocation refusal %v", err)
	}
	if err = db.FinishGraphNodeAttempt(t.Context(), a.ID, GraphSucceeded, "done", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err = db.SetGraphInstanceStatus(t.Context(), i.ID, GraphSucceeded, time.Time{}, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListOwnerInstructions(t.Context(), i.SessionID, 0)
	if err != nil || len(pending) != 1 || pending[0].AttemptID != 0 {
		t.Fatalf("terminal input disappeared %v %v", pending, err)
	}
	if _, err = db.RetainOwnerInstruction(t.Context(), i.SessionID, 20, 43, "change the wording", OwnerInputTarget{}); err == nil {
		t.Fatal("foreign actor accepted")
	}
	if _, err = db.RetainOwnerInstruction(t.Context(), i.SessionID, 20, 42, "edited bytes", OwnerInputTarget{}); err == nil {
		t.Fatal("edited replay accepted")
	}
}

func TestOwnerGuidanceHistorySnapshotAndClear(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 510)
	recordOwnerInput(t, db, s.ID, 20, "/guide Use plain language.")
	first, err := db.SetOwnerGuidance(t.Context(), s.ID, 20, 42, "Use plain language.")
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, s.ID, 21)
	snap, err := db.OwnerGuidanceForRun(t.Context(), run.ID)
	if err != nil || snap != first {
		t.Fatalf("snapshot=%v %v", snap, err)
	}
	recordOwnerInput(t, db, s.ID, 30, "/guide Prefer ISO dates.")
	second, err := db.SetOwnerGuidance(t.Context(), s.ID, 30, 42, "Prefer ISO dates.")
	if err != nil {
		t.Fatal(err)
	}
	snap, err = db.OwnerGuidanceForRun(t.Context(), run.ID)
	if err != nil || snap != first {
		t.Fatalf("retry changed revision %v %v", snap, err)
	}
	current, err := db.CurrentOwnerGuidance(t.Context(), s.ID)
	if err != nil || current != second {
		t.Fatalf("current=%v %v", current, err)
	}
	recordOwnerInput(t, db, s.ID, 40, "/guide clear")
	cleared, err := db.SetOwnerGuidance(t.Context(), s.ID, 40, 42, "")
	if err != nil || cleared.Revision <= second.Revision || cleared.Body != "" {
		t.Fatalf("clear=%v %v", cleared, err)
	}
	next := testRun(t, db, s.ID, 41)
	snap, err = db.OwnerGuidanceForRun(t.Context(), next.ID)
	if err != nil || snap != cleared {
		t.Fatalf("clear snapshot=%v %v", snap, err)
	}
	if _, err = db.SetOwnerGuidance(t.Context(), s.ID, 30, 42, "edited guidance"); err == nil {
		t.Fatal("edited replay accepted")
	}
	pending, err := db.ListOwnerInstructions(t.Context(), s.ID, 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("guidance entered one-time queue: %v %v", pending, err)
	}
}

func TestOwnerInputNewAttemptAfterCrashReusesLogicalInputAndStopsAfterSuccess(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	recordOwnerInput(t, db, i.SessionID, 50, "Preserve my original direction through a crash.")
	if _, err := db.RetainOwnerInstruction(t.Context(), i.SessionID, 50, 42, "Preserve my original direction through a crash.", OwnerInputTarget{InstanceID: i.ID, Node: "work"}); err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, i.SessionID, 50)
	first, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReconcileOrphans(t.Context(), "replacement-daemon"); err != nil {
		t.Fatal(err)
	}
	orphan, err := db.GetRun(t.Context(), run.ID)
	if err != nil || orphan.Error != ReasonAbandoned {
		t.Fatalf("orphan=%+v %v", orphan, err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), first.ID, GraphCompletion{Status: GraphFailed, FinishedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	nextRun := testRun(t, db, i.SessionID, 51)
	second, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: nextRun.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := db.OwnerInstructionsForAttempt(t.Context(), second.ID)
	if err != nil || len(inputs) != 1 || inputs[0].AttemptID != first.ID || inputs[0].Body != "Preserve my original direction through a crash." {
		t.Fatalf("new-attempt recovery lost/changed assignment: %+v %v", inputs, err)
	}
	original, err := db.GetOwnerInstruction(t.Context(), i.SessionID, 50)
	if err != nil || original.AttemptID != first.ID {
		t.Fatalf("recovery reassigned source: %+v %v", original, err)
	}
	if err = db.FinishGraphNodeAttempt(t.Context(), second.ID, GraphSucceeded, "done", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	third, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: i.ID, Node: "work", RunID: nextRun.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	inputs, err = db.OwnerInstructionsForAttempt(t.Context(), third.ID)
	if err != nil || len(inputs) != 0 {
		t.Fatalf("later successful loop repeated input: %+v %v", inputs, err)
	}
}

func TestParseOwnerInputTargetSharesAdmissionAndRecoveryGrammar(t *testing.T) {
	for _, tc := range []struct {
		body string
		want OwnerInputTarget
		bad  bool
	}{
		{body: "Please tell instance=7 to use ISO dates."},
		{body: "Input-Target: instance=7 node=worker launch=9\nExact original text.", want: OwnerInputTarget{InstanceID: 7, Node: "worker", LaunchID: 9}},
		{body: "Input-Target: instance=7 node=worker instance=8", bad: true},
		{body: "Input-Target: instance=7 node=../worker", bad: true},
		{body: "Input-Target: instance=7 node=worker launch=0", bad: true},
		{body: "Input-Target: node=worker", bad: true},
	} {
		got, err := ParseOwnerInputTarget(tc.body)
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Errorf("target %q=%+v err=%v", tc.body, got, err)
		}
	}
}
