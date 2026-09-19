package store

import (
	"errors"
	"testing"
)

func TestResearchLatestAttemptAndLimitsSurviveRestart(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	if _, err := db.LatestGraphNodeAttempt(t.Context(), instance.ID, "work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("initial attempt: %v", err)
	}
	attempt := graphTestAttempt(t, db, instance, 1)
	if err := db.FinishGraphNodeAttempt(t.Context(), attempt.ID, GraphFailed, "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	got, err := reopened.LatestGraphNodeAttempt(t.Context(), instance.ID, attempt.Node)
	if err != nil || got.ID != attempt.ID || got.Status != GraphFailed {
		t.Fatalf("retained failed attempt: %+v %v", got, err)
	}
	_, err = reopened.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: attempt.Node, Limit: 1, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("restart reset persisted limit: %v", err)
	}
}

func TestResearchSuccessfulContinuationRetainsAttemptBudget(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	first := graphTestAttempt(t, db, instance, 2)
	if err := db.CompleteGraphNodeAttempt(t.Context(), first.ID, GraphCompletion{
		Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(),
		Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: []byte(`{"response":"ask"}`)}},
	}); err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	second := graphTestAttempt(t, reopened, instance, 2)
	if second.Attempt != 2 || second.ID == first.ID {
		t.Fatalf("continuation reset attempt identity: first=%+v second=%+v", first, second)
	}
	if err := reopened.FinishGraphNodeAttempt(t.Context(), second.ID, GraphSucceeded, "success", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	_, err = reopened.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: second.Node, Limit: 2, StartedAt: graphTestTime()})
	if !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("successful continuation reset persisted limit: %v", err)
	}
	latest, err := reopened.LatestGraphNodeAttempt(t.Context(), instance.ID, second.Node)
	if err != nil || latest.ID != second.ID {
		t.Fatalf("latest continuation: %+v %v", latest, err)
	}
	other, err := reopened.CreateGraphInstance(t.Context(), GraphInstance{SessionID: instance.SessionID, DefinitionID: instance.DefinitionID, CurrentNode: second.Node, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.LatestGraphNodeAttempt(t.Context(), other.ID, second.Node); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attempt leaked across instances: %v", err)
	}
}
