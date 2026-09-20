package store

import (
	"strings"
	"testing"
)

// A review is retained like any other publication and, like a question, leaves
// the instance waiting on the owner. 0049's CHECK knew only three kinds, so
// this insert failed after the agent had already done the work.
func TestRetainGraphCommentAcceptsReviewKind(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 318)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitResearchLaunch(t.Context(), ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "shape", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.md":"c2hhcGU="}`)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "shape", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	intent := GraphCommentIntent{Key: "shaped-scope", InstanceID: admitted.Instance.ID, SourceAttemptID: attempt.ID, Node: "review", Kind: "review", Candidate: []byte(`{"outcome":"completed"}`)}
	if err = db.RetainGraphComment(t.Context(), intent); err != nil {
		t.Fatalf("retain review: %v", err)
	}
	held, err := db.GetGraphComment(t.Context(), intent.Key)
	if err != nil || held.Kind != "review" || string(held.Candidate) != string(intent.Candidate) {
		t.Fatalf("retained review: %+v %v", held, err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), admitted.Instance.ID)
	if err != nil || wait.DeliveryKey != intent.Key || wait.State != "waiting" {
		t.Fatalf("review must wait on the owner: %+v %v", wait, err)
	}
	intent.Key, intent.Kind = "unnamed-kind", "scope"
	if err = db.RetainGraphComment(t.Context(), intent); err == nil {
		t.Fatal("a kind outside the four publications was retained")
	}
}
