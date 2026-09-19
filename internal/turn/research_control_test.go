package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchControlRetainedOutput(t *testing.T) {
	for _, response := range []string{"ask", "done", "missing", "invalid"} {
		t.Run(response, func(t *testing.T) {
			db, session, _ := agentResultFixture(t, "research")
			raw := `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_research_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=2];
work [lf_action=lf_research_agent,lf_outcomes=success,lf_limit=2];
check [lf_action=lf_research_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=2];
question [lf_action=lf_research_comment,lf_kind=question,lf_outcomes=published];
report [lf_action=lf_research_comment,lf_kind=final,lf_outcomes=published];wait [lf_terminal=true];end [lf_terminal=true];
loop -> work [on=true];loop -> report [on=false];work -> check [on=success];check -> question [on=true];check -> loop [on=false];question -> wait [on=published];report -> end [on=published];}`
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
			def, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: p.Team, Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: p.Digest, CreatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: p.Start, StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			req := GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at}
			var specs graphexec.Registry
			h := ResearchControlActivity{Profile: p, Context: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
				return agentresult.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "research", QuestionsAllowed: true}, nil
			}}
			if err := RegisterResearchControl(&specs, &req.Activities, h); err != nil {
				t.Fatal(err)
			}
			first, err := ExecuteNodeAction(t.Context(), Options{DB: db}, req)
			if err != nil || first.NextNode != "work" {
				t.Fatalf("first default: %+v %v", first, err)
			}
			a, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", Limit: 2, StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			payload := `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Done","payload":{"research":{"request":"Inspect"}}}}`
			if response == "ask" {
				payload = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Scope needed","comment":"Findings","questions":[{"id":"Q-1","audience":"business","text":"Which boundary?"}],"payload":{"research":{"request":"Inspect"}}}}`
			}
			if response == "invalid" {
				payload = `{"version":1,"response":null}`
			}
			completion := store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "work", ToNode: "check", OnOutcome: "success", At: at}}
			if response != "missing" {
				completion.Outputs = []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(payload)}}
			}
			if err := db.CompleteGraphNodeAttempt(t.Context(), a.ID, completion); err != nil {
				t.Fatal(err)
			}
			// Reopen the actual database before control reads retained output;
			// no process-local result or condition state may decide the route.
			path := db.Path()
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = store.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			next, err := ExecuteNodeAction(t.Context(), Options{DB: db}, req)
			if response == "invalid" || response == "missing" {
				if err == nil {
					t.Fatalf("invalid retained result: %+v", next)
				}
				failed, loadErr := db.GetGraphNodeAttempt(t.Context(), next.AttemptID)
				if loadErr != nil || failed.Status != store.GraphFailed {
					t.Fatalf("failed control attempt: %+v %v", failed, loadErr)
				}
				return
			}
			want := "question"
			if response == "done" {
				want = "loop"
			}
			if err != nil || next.NextNode != want {
				t.Fatalf("decision: %+v %v", next, err)
			}
			if response == "done" {
				last, err := ExecuteNodeAction(t.Context(), Options{DB: db}, req)
				if err != nil || last.NextNode != "report" {
					t.Fatalf("done exit: %+v %v", last, err)
				}
			}
			attempts, err := db.ListGraphNodeAttempts(t.Context(), instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, attempt := range attempts {
				if strings.EqualFold(attempt.Node, "work") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("control invoked another agent: %d", count)
			}
		})
	}
}
