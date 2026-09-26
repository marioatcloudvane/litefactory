package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBugParentContinuationConsumesActiveApprovedWaitOnce(t *testing.T) {
	db, e := Open(t.Context(), filepath.Join(t.TempDir(), "continuation.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	identity, analysis := repairBoundaryApproved(t, db)
	pass, e := db.GetBugAnalysisPass(t.Context(), identity.SessionID, analysis.Invocation)
	if e != nil {
		t.Fatal(e)
	}
	attempt, e := db.GetGraphNodeAttempt(t.Context(), pass.GraphAttemptID)
	if e != nil {
		t.Fatal(e)
	}
	child, e := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug_tests.dot", Digest: strings.Repeat("c", 64), CreatedAt: graphTestTime()})
	if e != nil {
		t.Fatal(e)
	}
	life, e := db.EnsureBugLifecycle(t.Context(), identity.SessionID, attempt.InstanceID, child.ID)
	if e != nil {
		t.Fatal(e)
	}
	c := BugParentContinuation{SessionID: identity.SessionID, Operation: "owner-reply:102", Cause: "analysis-approved", Reference: "review-1", ExpectedRevision: life.Revision, At: graphTestTime()}
	before, _, _, e := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan GraphInstance, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { i, e := db.ClaimBugContinuation(context.Background(), c); results <- i; errs <- e })
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var successor int64
	for i := range results {
		if successor != 0 && successor != i.ID {
			t.Fatal("duplicate successor")
		}
		successor = i.ID
		if i.CurrentNode != "record" {
			t.Fatalf("approval ran analyst: %+v", i)
		}
	}
	prior, e := db.GetGraphInstance(t.Context(), attempt.InstanceID)
	if e != nil || prior.Status != GraphSucceeded {
		t.Fatalf("old history not terminal %+v %v", prior, e)
	}
	after, _, _, e := db.CurrentBugAnalysis(t.Context(), identity.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	if after.AnalysisUsed != before.AnalysisUsed {
		t.Fatal("mechanical approval spent pass")
	}
	replay, e := db.GetBugOwnerContinuation(t.Context(), identity.SessionID, 102, 42, "/approve-bug analysis 1")
	if e != nil || replay.ID != successor {
		t.Fatalf("replay %+v %v", replay, e)
	}
	if _, e = db.GetBugOwnerContinuation(t.Context(), identity.SessionID, 102, 42, "changed reply"); e == nil {
		t.Fatal("changed reply borrowed dispatch")
	}
	c.Reference = "other"
	if _, e = db.ClaimBugContinuation(t.Context(), c); e == nil {
		t.Fatal("changed cause borrowed operation")
	}
	due, e := db.ListDueGraphDispatches(t.Context(), graphTestTime(), 10)
	if e != nil || len(due) != 1 || due[0].InstanceID != successor {
		t.Fatalf("dispatch %+v %v", due, e)
	}
}

func TestBugParentResolvedInfrastructureContinuation(t *testing.T) {
	for _, variant := range []string{"resolved", "flaky", "cleanup", "wrong-owner"} {
		t.Run(variant, func(t *testing.T) {
			db, e := Open(t.Context(), filepath.Join(t.TempDir(), "infra.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			if e = db.Migrate(t.Context()); e != nil {
				t.Fatal(e)
			}
			request := testChildRequest(t, db)
			call := testChildCreate(t, db, request)
			testChildFinishPhase(t, db, call, "regression", "blocked")
			if _, e = db.SettleBugTestCall(t.Context(), call.ID, graphTestTime()); e != nil {
				t.Fatal(e)
			}
			if e = testChildConsume(t, db, call, "blocked"); e != nil {
				t.Fatal(e)
			}
			life, e := db.EnsureBugLifecycle(t.Context(), request.SessionID, request.ParentInstance, request.ChildDefinition)
			if e != nil {
				t.Fatal(e)
			}
			if e = db.SetGraphInstanceStatus(t.Context(), request.ParentInstance, GraphSucceeded, time.Time{}, graphTestTime()); e != nil {
				t.Fatal(e)
			}
			body := fmt.Sprintf("/retry-bug-tests %d Installed the previously missing test dependency", call.ID)
			reply := GraphOwnerReply{OwnerID: 42, AuthorID: 42, CommentID: 222, TaskRef: "github:acme/api#392", SourceRef: "github:acme/api#392", Body: body, At: graphTestTime()}
			if variant == "wrong-owner" {
				reply.AuthorID = 43
			}
			if variant == "flaky" {
				if _, e = db.sql.ExecContext(t.Context(), `UPDATE bug_check_phases SET unreliable=1 WHERE session_id=?`, request.SessionID); e != nil {
					t.Fatal(e)
				}
			}
			if variant == "cleanup" {
				if _, e = db.sql.ExecContext(t.Context(), `UPDATE bug_check_phases SET cleanup=0 WHERE session_id=?`, request.SessionID); e != nil {
					t.Fatal(e)
				}
			}
			e = db.RecordBugTestResolution(t.Context(), request.SessionID, call.ID, reply)
			if variant != "resolved" {
				if e == nil {
					t.Fatal("unsafe infrastructure resolution accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			c := BugParentContinuation{SessionID: request.SessionID, Operation: "owner-reply:222", Cause: "test-resolved", Reference: fmt.Sprint(call.ID), ExpectedRevision: life.Revision, At: graphTestTime()}
			next, e := db.ClaimBugContinuation(t.Context(), c)
			if e != nil || next.CurrentNode != "test" {
				t.Fatalf("fresh test checkpoint %+v %v", next, e)
			}
			replay, e := db.GetBugOwnerContinuation(t.Context(), request.SessionID, 222, 42, body)
			if e != nil || replay.ID != next.ID {
				t.Fatalf("resolution replay %+v %v", replay, e)
			}
			var repairs int
			if e = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM bug_repairs WHERE session_id=?`, request.SessionID).Scan(&repairs); e != nil || repairs != 1 {
				t.Fatalf("resolution spent repair %d %v", repairs, e)
			}
		})
	}
}
