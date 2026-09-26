package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBugParentRecoveryPreservesFactoryIdentityAndRefusesAgentRelaunch(t *testing.T) {
	for _, label := range []string{"merge", "analyst", "fixer", "unknown", "bound-agent"} {
		t.Run(label, func(t *testing.T) {
			node := label
			if label == "bound-agent" {
				node = "analyst"
			}
			db := newTestDB(t)
			if e := db.Migrate(t.Context()); e != nil {
				t.Fatal(e)
			}
			identity, analysis := repairBoundaryApproved(t, db)
			pass, e := db.GetBugAnalysisPass(t.Context(), identity.SessionID, analysis.Invocation)
			if e != nil {
				t.Fatal(e)
			}
			original, e := db.GetGraphNodeAttempt(t.Context(), pass.GraphAttemptID)
			if e != nil {
				t.Fatal(e)
			}
			parent, e := db.GetGraphInstance(t.Context(), original.InstanceID)
			if e != nil {
				t.Fatal(e)
			}
			instance, e := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: identity.SessionID, DefinitionID: parent.DefinitionID, CurrentNode: node, StartedAt: graphTestTime()})
			if e != nil {
				t.Fatal(e)
			}
			child, e := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug_tests.dot", Digest: strings.Repeat("c", 64), CreatedAt: graphTestTime()})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.EnsureBugLifecycle(t.Context(), identity.SessionID, instance.ID, child.ID); e != nil {
				t.Fatal(e)
			}
			var runID int64
			if label == "bound-agent" {
				runID = original.RunID
				if runID <= 0 {
					t.Fatal("fixture lacks real run binding")
				}
			}
			attempt, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: node, RunID: runID, StartedAt: graphTestTime(), Limit: 1})
			if e != nil {
				t.Fatal(e)
			}
			before, _, _, e := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
			if e != nil {
				t.Fatal(e)
			}
			if e = db.ReconcileBugParentGraphs(t.Context(), graphTestTime()); e != nil {
				t.Fatal(e)
			}
			if e = db.ReconcileBugParentGraphs(t.Context(), graphTestTime()); e != nil {
				t.Fatal(e)
			}
			recovered, e := db.GetGraphNodeAttempt(t.Context(), attempt.ID)
			if e != nil {
				t.Fatal(e)
			}
			after, _, _, e := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
			if e != nil || before != after {
				t.Fatalf("recovery changed analysis budget: %+v %+v %v", before, after, e)
			}
			if node == "merge" {
				current, e := db.CurrentBugFactoryAttempt(t.Context(), instance.ID, node)
				if e != nil || current.ID != attempt.ID || recovered.Status != GraphRunning {
					t.Fatalf("factory identity lost: %+v %+v %v", current, recovered, e)
				}
				dispatches, e := db.ListDueGraphDispatches(t.Context(), graphTestTime(), 100)
				if e != nil || len(dispatches) != 1 || dispatches[0].Node != node {
					t.Fatalf("duplicate or missing factory dispatch: %+v %v", dispatches, e)
				}
				if _, e = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: node, StartedAt: graphTestTime(), Limit: 1}); !errors.Is(e, ErrGraphAttemptRunning) {
					t.Fatalf("new attempt permitted: %v", e)
				}
			} else {
				event := fmt.Sprintf("restart-attempt:%d", attempt.ID)
				if e = db.BindBugRestartReport(t.Context(), identity.SessionID, event, "canonical-report-key", "Rendered retained failure"); e != nil {
					t.Fatal(e)
				}
				if e = db.BindBugRestartReport(t.Context(), identity.SessionID, event, "canonical-report-key", "Rendered retained failure"); e != nil {
					t.Fatal(e)
				}
				if e = db.BindBugRestartReport(t.Context(), identity.SessionID, event, "another-key", "Changed failure"); e == nil {
					t.Fatal("rebound restart projection")
				}
				if recovered.Status != GraphFailed {
					t.Fatalf("agent gap allowed relaunch: %+v", recovered)
				}
				if _, e = db.CurrentBugFactoryAttempt(t.Context(), instance.ID, node); !errors.Is(e, ErrNotFound) {
					t.Fatalf("substantive node exposed as factory: %v", e)
				}
				life, e := db.GetBugLifecycle(t.Context(), identity.SessionID)
				if e != nil || life.Disposition != "blocked" {
					t.Fatalf("failure lost %+v %v", life, e)
				}
			}
		})
	}
}
