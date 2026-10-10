package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResearchAdmissionImmutablePathAndDefinitionBoundaries(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 343)
	selected, err := db.AdmitResearchPath(t.Context(), session.ID, "legacy")
	if err != nil || selected != "legacy" {
		t.Fatalf("legacy: %s %v", selected, err)
	}
	selected, err = db.AdmitResearchPath(t.Context(), session.ID, "graph")
	if err != nil || selected != "legacy" {
		t.Fatalf("legacy migrated: %s %v", selected, err)
	}
	session = testSession(t, db, 344)
	selected, err = db.AdmitResearchPath(t.Context(), session.ID, "graph")
	if err != nil || selected != "graph" {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	launch := ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "loop", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.md":"b2xk"}`), Reply: "investigate"}
	if exists, err := db.HasWorkflowLaunch(t.Context(), session.ID, launch.TriggerID); err != nil || exists {
		t.Fatalf("fresh input has a launch: %v %v", exists, err)
	}
	first, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil || replay.Instance.ID != first.Instance.ID || replay.LaunchID != first.LaunchID {
		t.Fatalf("launch replay: %+v %v", replay, err)
	}
	if exists, err := db.HasWorkflowLaunch(t.Context(), session.ID, launch.TriggerID); err != nil || !exists {
		t.Fatalf("retained input lacks its launch: %v %v", exists, err)
	}
	launch.ContentDigest = strings.Repeat("d", 64)
	launch.Files = []byte(`{"research.md":"bmV3"}`)
	if _, err := db.AdmitResearchLaunch(t.Context(), launch); err == nil {
		t.Fatal("same trigger changed captured bytes")
	}
	launch.TriggerID++
	updated, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil || updated.Instance.ID != first.Instance.ID || updated.Instance.DefinitionID != definition.ID {
		t.Fatalf("instruction update repinned foundation: %+v %v", updated, err)
	}
	launch.TriggerID++
	launch.TopologyDigest = strings.Repeat("e", 64)
	launch.Start = "new-start"
	successor, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil || successor.Instance.ID == first.Instance.ID || successor.Instance.CurrentNode != "new-start" || successor.PredecessorID != first.Instance.ID {
		t.Fatalf("topology successor: %+v %v", successor, err)
	}
	prior, reply, err := db.WorkflowPredecessor(t.Context(), successor.Instance.ID)
	if err != nil || prior != first.Instance.ID || reply != "investigate" {
		t.Fatalf("saved predecessor/reply lost: %d %q %v", prior, reply, err)
	}
	old, err := db.GetGraphInstance(t.Context(), first.Instance.ID)
	if err != nil || old.Status != GraphCancelled || old.DefinitionID != definition.ID {
		t.Fatalf("mutated old instance: %+v %v", old, err)
	}
}

func TestResearchAdmissionRefusesUncertainPredecessorPublication(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 346)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	launch := ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "work", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"role":"eA=="}`)}
	first, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, Node: "work", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	if err = db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: "owed-report", InstanceID: first.Instance.ID, SourceAttemptID: a.ID, Node: "report", Kind: "final", Candidate: []byte(`{"outcome":"completed"}`)}); err != nil {
		t.Fatal(err)
	}
	launch.TriggerID++
	launch.TopologyDigest = strings.Repeat("d", 64)
	if _, err = db.AdmitResearchLaunch(t.Context(), launch); !errors.Is(err, ErrResearchEffectsPending) {
		t.Fatalf("uncertain predecessor allowed successor: %v", err)
	}
	// Cancellation retains the debt; it never supplies publication evidence.
	if err = db.SetGraphInstanceStatus(t.Context(), first.Instance.ID, GraphCancelled, time.Time{}, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitResearchLaunch(t.Context(), launch); !errors.Is(err, ErrResearchEffectsPending) {
		t.Fatalf("cancellation erased publication debt: %v", err)
	}
}

// An open attempt blocks a successor only while its instance runs; one left on
// a finished instance by an interrupted process does not.
func TestWorkflowAdmissionIgnoresOpenAttemptsOfFinishedInstances(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 587)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	launch := WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "budget", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"role":"eA=="}`)}
	first, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, Node: "budget", Limit: 8, StartedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	launch.TriggerID++
	if _, err = db.AdmitWorkflowLaunch(t.Context(), launch); err == nil || !strings.Contains(err.Error(), "workflow invocation already active") {
		t.Fatalf("a running instance with an open attempt must refuse: %v", err)
	}
	if err = db.SetGraphInstanceStatus(t.Context(), first.Instance.ID, GraphFailed, time.Time{}, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	launch.TriggerID++
	successor, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil || successor.Instance.ID == first.Instance.ID || successor.PredecessorID != first.Instance.ID || successor.Instance.Status != GraphRunning {
		t.Fatalf("failed instance with an orphaned attempt blocked its successor: %+v %v", successor, err)
	}
}
