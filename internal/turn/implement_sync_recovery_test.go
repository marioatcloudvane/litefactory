package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementWorkerSyncFailedSettlementRecoversBeforeFreshAdmission(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		name := "unfinished parking"
		if resumed {
			name = "owner resumed"
		}
		t.Run(name, func(t *testing.T) {
			f, forge, req := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
				t.Fatal("recovery must not launch worker")
				return nil, nil
			}))
			enableWorkerSyncFixture(f)
			seedImplementStory(t, f)
			definition := mustCaptureImplement(t)
			at := time.Now()
			_, err := f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "graph")
			mustProgress(t, err)
			pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "implement", ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest, CreatedAt: at})
			mustProgress(t, err)
			files, err := json.Marshal(definition.Files)
			mustProgress(t, err)
			topology, err := graphexec.SemanticDigest(definition.Profile)
			mustProgress(t, err)
			admitted, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "implement", SessionID: f.session.ID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: "sync_before_worker", ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin)})
			mustProgress(t, err)
			at = admitted.Instance.StartedAt
			row := implementWorkerRow(t, f)
			for i := 0; i < 8; i++ {
				eventAt := at.Add(time.Duration(i) * time.Second)
				attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "sync_before_worker", LaunchID: admitted.LaunchID, StartedAt: eventAt})
				mustProgress(t, err)
				raw, err := json.Marshal(syncBaseOutput{Working: row.Branch, Base: "main", Result: "retry", Reason: "retained unpublished work prevents synchronization"})
				mustProgress(t, err)
				mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "retry", FinishedAt: eventAt, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: eventAt}}, Transition: &store.GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "sync_before_worker", ToNode: "sync_before_worker", OnOutcome: "retry", At: eventAt}}))
			}
			mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), admitted.Instance.ID, store.GraphFailed, time.Time{}, at.Add(9*time.Second)))
			if resumed {
				row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "owner saw blockage", At: at.Add(10 * time.Second)})
				mustProgress(t, err)
				row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: row.Revision, At: at.Add(11 * time.Second)})
				mustProgress(t, err)
				row, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: at.Add(12 * time.Second)})
				mustProgress(t, err)
				mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), row.Parent, row.Generation, row.Key, f.session.ID, at.Add(12*time.Second)))
				row = implementWorkerRow(t, f)
			}
			req.Trigger.CommentID++
			rt := &implementRuntime{opts: f.opts, req: req, worker: &row, definition: definition}
			if resumed {
				recovered, err := rt.recoverWorkerSyncParking(t.Context())
				mustProgress(t, err)
				if recovered {
					t.Fatal("old exhausted instance reparked owner-resumed story")
				}
				if got := implementWorkerRow(t, f); got.State != store.ImplementRunning {
					t.Fatalf("owner resume lost: %+v", got)
				}
				if len(forge.all()) != 0 {
					t.Fatal("old blockage reported after owner resume")
				}
			} else {
				_, err = rt.admit(t.Context())
				mustProgress(t, err)
				got := implementWorkerRow(t, f)
				if got.State != store.ImplementParked || got.Attempts != 0 || got.LastFailure != "" || !strings.Contains(got.Reason, "cumulative limit of 8") {
					t.Fatalf("failed settlement not recovered safely: %+v", got)
				}
				if len(forge.all()) != 1 {
					t.Fatal("recovery did not report current blockage")
				}
				latest, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
				mustProgress(t, err)
				if latest.ID != admitted.Instance.ID {
					t.Fatal("fresh admission created a worker instance before settlement")
				}
			}
		})
	}
}
