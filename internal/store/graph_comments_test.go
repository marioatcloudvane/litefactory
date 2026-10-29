package store

import (
	"strings"
	"testing"
	"time"
)

type publicationResumeFixture struct {
	db       *DB
	session  Session
	pin      GraphDefinition
	instance GraphInstance
	resume   GraphPublicationResume
}

// newPublicationResumeFixture leaves an instance failed at node after its
// comment attempt's delivery of kind was later confirmed and completed.
func newPublicationResumeFixture(t *testing.T, node, kind string) publicationResumeFixture {
	t.Helper()
	ctx, at := t.Context(), graphTestTime()
	db := newTestDB(t)
	session := testSession(t, db, 1246)
	pin, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "investigate", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 22)
	source, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "investigate", RunID: run.ID, Limit: 8, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, source.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: at, Transition: &GraphTransition{InstanceID: instance.ID, FromNode: "investigate", ToNode: node, OnOutcome: "success", At: at}}); err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: node, RunID: run.ID, Limit: 8, StartedAt: at, FactoryStep: true})
	if err != nil {
		t.Fatal(err)
	}
	key := "resume-" + node
	if err = db.RetainGraphComment(ctx, GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: source.ID, Node: node, Kind: kind, Candidate: []byte(`{"outcome":"completed"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.EnsureReportDelivery(ctx, ReportDelivery{Version: 1, Key: key, SessionID: session.ID, RunID: run.ID, TriggerID: 22, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "research-" + kind, BotUserID: 99, Body: "body", Marker: key}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphFailed, FinishedAt: at, InstanceStatus: GraphFailed}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmReportDelivery(ctx, key, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteReportDelivery(ctx, key); err != nil {
		t.Fatal(err)
	}
	if instance, err = db.GetGraphInstance(ctx, instance.ID); err != nil {
		t.Fatal(err)
	}
	return publicationResumeFixture{db: db, session: session, pin: pin, instance: instance, resume: GraphPublicationResume{InstanceID: instance.ID, AttemptID: attempt.ID, SourceAttemptID: source.ID, DeliveryKey: key, Target: "shapeloop", At: at.Add(time.Minute)}}
}

func (f publicationResumeFixture) state(t *testing.T) (GraphInstance, int, int) {
	t.Helper()
	instance, err := f.db.GetGraphInstance(t.Context(), f.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), f.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	published := 0
	for _, tr := range transitions {
		if tr.OnOutcome == "published" {
			published++
		}
	}
	due, err := f.db.ListDueGraphDispatches(t.Context(), f.resume.At.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	return instance, published, len(due)
}

func TestResumeConfirmedGraphPublicationIsExactAndIdempotent(t *testing.T) {
	f := newPublicationResumeFixture(t, "report", "final")
	for range 2 {
		if _, err := f.db.ResumeConfirmedGraphPublication(t.Context(), f.resume); err != nil {
			t.Fatal(err)
		}
	}
	instance, published, due := f.state(t)
	if instance.Status != GraphRunning || instance.CurrentNode != "shapeloop" || !instance.NextDueAt.Equal(f.resume.At) || published != 1 || due != 1 {
		t.Fatalf("resume: %+v published=%d due=%d", instance, published, due)
	}
	dispatch, err := f.db.LatestGraphDispatch(t.Context(), f.instance.ID)
	if err != nil || dispatch.Node != "shapeloop" || dispatch.Status != "pending" {
		t.Fatalf("dispatch: %+v %v", dispatch, err)
	}
}

// A resumed question waits for its owner: no dispatch is owed.
func TestResumeConfirmedGraphQuestionWaitsWithoutDispatch(t *testing.T) {
	f := newPublicationResumeFixture(t, "ask", "question")
	f.resume.Target = "waiting"
	resumed, err := f.db.ResumeConfirmedGraphPublication(t.Context(), f.resume)
	if err != nil || !resumed {
		t.Fatalf("resume: %v %v", resumed, err)
	}
	instance, published, due := f.state(t)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	if err != nil || wait.State != "waiting" || instance.Status != GraphRunning || instance.CurrentNode != "waiting" || !instance.NextDueAt.IsZero() || published != 1 || due != 0 {
		t.Fatalf("question resume: %+v wait=%+v published=%d due=%d %v", instance, wait, published, due, err)
	}
}

func TestResumeConfirmedGraphPublicationGuards(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(t *testing.T, f *publicationResumeFixture)
	}{
		{"newer instance in the same workflow", func(t *testing.T, f *publicationResumeFixture) {
			if _, err := f.db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: f.session.ID, DefinitionID: f.pin.ID, CurrentNode: "investigate", StartedAt: graphTestTime()}); err != nil {
				t.Fatal(err)
			}
		}},
		{"newer instance of another workflow", func(t *testing.T, f *publicationResumeFixture) {
			plan, err := f.db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: f.session.ID, DefinitionID: plan.ID, CurrentNode: "plan", StartedAt: graphTestTime()}); err != nil {
				t.Fatal(err)
			}
		}},
		{"other source attempt", func(_ *testing.T, f *publicationResumeFixture) { f.resume.SourceAttemptID++ }},
		{"other publication", func(_ *testing.T, f *publicationResumeFixture) { f.resume.DeliveryKey = "resume-other" }},
		{"not the latest attempt", func(_ *testing.T, f *publicationResumeFixture) { f.resume.AttemptID-- }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newPublicationResumeFixture(t, "report", "final")
			c.mutate(t, &f)
			resumed, err := f.db.ResumeConfirmedGraphPublication(t.Context(), f.resume)
			if err != nil || resumed {
				t.Fatalf("guard did not refuse: %v %v", resumed, err)
			}
			instance, published, due := f.state(t)
			if instance.Status != GraphFailed || instance.CurrentNode != "report" || published != 0 || due != 0 {
				t.Fatalf("refused resume changed state: %+v published=%d due=%d", instance, published, due)
			}
		})
	}
}
