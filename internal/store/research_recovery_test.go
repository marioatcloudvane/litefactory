package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRecoverResearchDispatchPreservesHistoryAndBudget(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 474)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	at := graphTestTime()
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	launch := ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "removed", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"role":"b2xk"}`)}
	first, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, Node: "removed", Limit: 1, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	finding := []byte(`{"finding":"retained"}`)
	err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: GraphRunning, Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: finding}}})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := db.ParkGraphInstance(t.Context(), first.Instance.ID, "removed", at, at)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := db.ReserveGraphDispatch(t.Context(), dispatch.ID, at)
	if err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	launch.TriggerID = dispatch.TriggerID()
	launch.Start = "investigate"
	launch.Files = []byte(`{"role":"bmV3"}`)
	successor, err := db.RecoverResearchDispatch(t.Context(), first.Instance.ID, launch)
	if err != nil {
		t.Fatal(err)
	}
	if successor.PredecessorID != first.Instance.ID || successor.Instance.CurrentNode != "investigate" || successor.Instance.ID == first.Instance.ID {
		t.Fatalf("successor: %+v", successor)
	}
	prior, err := db.GetGraphInstance(t.Context(), first.Instance.ID)
	if err != nil || prior.Status != GraphCancelled {
		t.Fatalf("prior: %+v %v", prior, err)
	}
	historical, err := db.ResearchLaunchFiles(t.Context(), first.Instance.ID)
	if err != nil || !bytes.Equal(historical, []byte(`{"role":"b2xk"}`)) {
		t.Fatalf("history: %s %v", historical, err)
	}
	outputs, err := db.ListGraphOutputsBounded(t.Context(), attempt.ID, 1024, 8)
	if err != nil || len(outputs) != 1 || !bytes.Equal(outputs[0].Payload, finding) {
		t.Fatalf("findings: %+v %v", outputs, err)
	}
	moved, err := db.LatestGraphDispatch(t.Context(), successor.Instance.ID)
	if err != nil || moved.InstanceID != successor.Instance.ID || moved.Node != "investigate" {
		t.Fatalf("dispatch: %+v %v", moved, err)
	}
	_, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: successor.Instance.ID, Node: "investigate", Limit: 1, StartedAt: at, ResearchPriorNodes: map[int64]string{first.Instance.ID: "removed"}})
	if !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("ancestor budget reset: %v", err)
	}
	attempts, err := db.ListGraphNodeAttempts(t.Context(), successor.Instance.ID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("exhaustion fabricated attempts: %+v %v", attempts, err)
	}
}

func TestResearchCompatibleGraphKeepsParkedInstance(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 475)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	launch := ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "investigate", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"graph":"b2xk"}`)}
	first, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	launch.TriggerID++
	launch.ContentDigest = strings.Repeat("d", 64)
	launch.TopologyDigest = strings.Repeat("e", 64)
	launch.Files = []byte(`{"graph":"bmV3"}`)
	launch.ResearchCompatible = true
	current, err := db.AdmitResearchLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	if current.Instance.ID != first.Instance.ID || current.Instance.CurrentNode != "investigate" || current.PredecessorID != 0 {
		t.Fatalf("compatible update restarted graph: %+v", current)
	}
	historical, err := db.ResearchLaunchFiles(t.Context(), first.Instance.ID)
	if err != nil || !bytes.Equal(historical, launch.Files) {
		t.Fatalf("current launch capture unavailable: %s %v", historical, err)
	}
}

func TestRecoverResearchDispatchDefersUnsettledWork(t *testing.T) {
	for _, variant := range []string{"running attempt", "pending comment"} {
		t.Run(variant, func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 476)
			if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			launch := ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "removed", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"role":"b2xk"}`)}
			first, err := db.AdmitResearchLaunch(t.Context(), launch)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, Node: "removed", StartedAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			want := ErrGraphAttemptRunning
			if variant == "pending comment" {
				err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning})
				if err != nil {
					t.Fatal(err)
				}
				err = db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: "original-delivery", InstanceID: first.Instance.ID, SourceAttemptID: attempt.ID, Node: "report", Kind: "final", Candidate: []byte(`{"comment":"retained bytes"}`)})
				if err != nil {
					t.Fatal(err)
				}
				want = ErrWorkflowEffectsPending
			}
			launch.TriggerID = graphTriggerBase + 1
			launch.Start = "investigate"
			_, err = db.RecoverResearchDispatch(t.Context(), first.Instance.ID, launch)
			if !errors.Is(err, want) {
				t.Fatalf("unsettled work lost: %v, want %v", err, want)
			}
			prior, err := db.GetGraphInstance(t.Context(), first.Instance.ID)
			if err != nil || prior.Status != GraphRunning || prior.CurrentNode != "removed" {
				t.Fatalf("rejected recovery changed prior: %+v %v", prior, err)
			}
		})
	}
}
