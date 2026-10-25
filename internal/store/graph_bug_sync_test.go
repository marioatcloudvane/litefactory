package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBugSyncFixerClaimTraversesRetriesOnce(t *testing.T) {
	db, _, _, session := persistentBugStopFixture(t)
	at := graphTestTime()
	def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "sync.dot", Digest: strings.Repeat("d", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "fixer", StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ParkGraphNode(t.Context(), GraphStop{Key: "fixer-restart", InstanceID: inst.ID, Origin: "fixer", Cause: "failed", Reason: "retained failure", Candidate: []byte(`{}`), At: at}); err != nil {
		t.Fatal(err)
	}
	// Authentication/publication is covered by the owner-reply tests. Retain the
	// exact resulting claim here to isolate allocation through the prerequisite.
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET state='claimed',reply_id=102,reply_author_id=7,reply_body='continue',claimed_at=? WHERE delivery_key='fixer-restart'`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	gate := &BugSyncPrerequisite{SyncNode: "sync", FixerNode: "fixer"}
	if err = db.RedirectGraphBugFixerToSync(t.Context(), inst.ID, *gate, at); err != nil {
		t.Fatal(err)
	}
	if ok, e := db.GraphBugSyncEntryAllowance(t.Context(), inst.ID, *gate); e != nil || !ok {
		t.Fatalf("claimed fixer entry=%v err=%v", ok, e)
	}
	first, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: gate})
	if err != nil {
		t.Fatal(err)
	}
	complete := func(a GraphNodeAttempt, outcome, next string) {
		t.Helper()
		at = at.Add(time.Second)
		if err = db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphSucceeded, Outcome: outcome, FinishedAt: at, Transition: &GraphTransition{InstanceID: inst.ID, FromNode: a.Node, ToNode: next, OnOutcome: outcome, At: at}}); err != nil {
			t.Fatal(err)
		}
	}
	complete(first, "retry", "sync")
	if ok, e := db.GraphBugSyncEntryAllowance(t.Context(), inst.ID, *gate); e != nil || !ok {
		t.Fatalf("retry entry=%v err=%v", ok, e)
	}
	second, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: gate})
	if err != nil {
		t.Fatal(err)
	}
	complete(second, "synced", "fixer")
	// A recovered process must rerun the gate when its checkout handoff can no
	// longer be verified, without minting a second owner allowance.
	at = at.Add(time.Second)
	if err = db.RedirectGraphBugFixerToSync(t.Context(), inst.ID, *gate, at); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: gate})
	if err != nil {
		t.Fatal(err)
	}
	complete(recovered, "deferred", "fixer")
	if source, e := db.GraphBugMechanicalAllowance(t.Context(), inst.ID, "fixer", gate); e != nil || source != 0 {
		t.Fatalf("legacy gate admitted deferred preparation: source=%d err=%v", source, e)
	}
	gate.AllowDeferred = true
	source, err := db.GraphBugMechanicalAllowance(t.Context(), inst.ID, "fixer", gate)
	if err != nil || source != first.ID {
		t.Fatalf("source=%d err=%v", source, err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "sync-fixer", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: 999, StartedAt: at, LogPath: func(int) string { return "/logs/sync-fixer.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	start := GraphAttemptStart{InstanceID: inst.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: at, OwnerAllowanceSource: source, BugSyncPrerequisite: gate}
	foreign, e := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "fixer", StartedAt: at, NextDueAt: at})
	if e != nil {
		t.Fatal(e)
	}
	foreignStart := start
	foreignStart.InstanceID = foreign.ID
	if _, e = db.StartGraphNodeAttempt(t.Context(), foreignStart); !errors.Is(e, ErrGraphInvariant) {
		t.Fatalf("foreign allowance: %v", e)
	}
	fixer, err := db.StartGraphNodeAttempt(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishGraphNodeAttempt(t.Context(), fixer.ID, GraphFailed, "", at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartGraphNodeAttempt(t.Context(), start); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("replayed allowance: %v", err)
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), inst.ID)
	if err != nil || wait.OriginNode != "fixer" || wait.AllowanceAttemptID != fixer.ID || wait.ContinuationAttemptID != first.ID {
		t.Fatalf("wait=%+v err=%v", wait, err)
	}
	// Owner guidance must not refresh the cumulative sync dispatch budget.
	if err = db.RedirectGraphBugFixerToSync(t.Context(), inst.ID, *gate, at); err != nil {
		t.Fatal(err)
	}
	if ok, e := db.GraphBugSyncEntryAllowance(t.Context(), inst.ID, *gate); e != nil || ok {
		t.Fatalf("consumed entry=%v err=%v", ok, e)
	}
	for n := 3; n < 8; n++ {
		a, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: gate})
		if e != nil {
			t.Fatal(e)
		}
		complete(a, "retry", "sync")
	}
	if err = db.ParkGraphNode(t.Context(), GraphStop{Key: "sync-exhausted", InstanceID: inst.ID, Origin: "sync", Cause: "exhausted", Reason: "retained dirty checkout", Candidate: []byte(`{}`), At: at, AllowRunless: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET state='claimed',reply_id=103,reply_author_id=7,reply_body='continue',claimed_at=? WHERE delivery_key='sync-exhausted'`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	if ok, e := db.GraphBugSyncEntryAllowance(t.Context(), inst.ID, *gate); e != nil || !ok {
		t.Fatalf("claimed sync entry=%v err=%v", ok, e)
	}
	if _, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: gate}); !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("sync budget reset: %v", err)
	}

	replacement := GraphStop{Key: "sync-exhausted-reply-103", InstanceID: inst.ID, Origin: "sync", Cause: "exhausted", Reason: "retained dirty checkout", Candidate: []byte(`{}`), At: at, AllowRunless: true, ReparkReplyID: 999}
	if err = db.ParkGraphNode(t.Context(), replacement); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("foreign repark reply: %v", err)
	}
	replacement.ReparkReplyID = 103
	if err = db.ParkGraphNode(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	if err = db.ParkGraphNode(t.Context(), replacement); err != nil {
		t.Fatalf("idempotent repark: %v", err)
	}
	wait, err = db.GetGraphOwnerWait(t.Context(), inst.ID)
	if err != nil || wait.State != "waiting" || wait.DeliveryKey != replacement.Key {
		t.Fatalf("repark wait=%+v err=%v", wait, err)
	}
	var oldState string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT state FROM graph_owner_waits WHERE delivery_key='sync-exhausted'`).Scan(&oldState); err != nil || oldState != "cancelled" {
		t.Fatalf("repark predecessor=%s err=%v", oldState, err)
	}
	if err = db.RedirectGraphBugSyncToFixerStop(t.Context(), inst.ID, *gate, at); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("waiting gate rerouted: %v", err)
	}
	if err = db.RedirectGraphBugFixerToSync(t.Context(), foreign.ID, *gate, at); err != nil {
		t.Fatal(err)
	}
	if err = db.RedirectGraphBugSyncToFixerStop(t.Context(), foreign.ID, *gate, at); err != nil {
		t.Fatal(err)
	}
	redirected, err := db.GetGraphInstance(t.Context(), foreign.ID)
	if err != nil || redirected.CurrentNode != "fixer" {
		t.Fatalf("exhaustion origin=%+v err=%v", redirected, err)
	}

}

func TestBugSyncPrerequisiteKeepsAnalystMechanicalAllowance(t *testing.T) {
	db, _, _, session := persistentBugStopFixture(t)
	at := graphTestTime()
	def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "analyst-sync.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "conversation", StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ParkGraphNode(t.Context(), GraphStop{Key: "analyst-guidance", InstanceID: inst.ID, Origin: "conversation", Cause: "question", Reason: "Need owner guidance", Candidate: []byte(`{}`), At: at, AllowRunless: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET state='claimed',reply_id=104,reply_author_id=7,reply_body='analyse',claimed_at=? WHERE delivery_key='analyst-guidance'`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	gate := &BugSyncPrerequisite{SyncNode: "sync", FixerNode: "fixer"}
	source, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "conversation", StartedAt: at, Limit: 8, BugSyncPrerequisite: gate})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Second)
	if err = db.CompleteGraphNodeAttempt(t.Context(), source.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "analyse", FinishedAt: at, Transition: &GraphTransition{InstanceID: inst.ID, FromNode: "conversation", ToNode: "analyst", OnOutcome: "analyse", At: at}}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GraphBugMechanicalAllowance(t.Context(), inst.ID, "analyst", gate)
	if err != nil || got != source.ID {
		t.Fatalf("analyst allowance=%d err=%v", got, err)
	}
}

func TestBugExhaustedSyncReparksReroutedFixerClaim(t *testing.T) {
	for _, route := range []string{"missing", "wrong_outcome", "valid"} {
		t.Run(route, func(t *testing.T) {
			db, _, _, session := persistentBugStopFixture(t)
			at := graphTestTime()
			def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "repark-sync.dot", Digest: strings.Repeat("f", 64), CreatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			inst, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "sync", StartedAt: at, NextDueAt: at})
			if err != nil {
				t.Fatal(err)
			}
			gate := BugSyncPrerequisite{SyncNode: "sync", FixerNode: "fixer"}
			for n := 0; n < 8; n++ {
				a, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: &gate})
				if e != nil {
					t.Fatal(e)
				}
				at = at.Add(time.Second)
				outcome, next := "retry", "sync"
				if n == 7 {
					outcome, next = "current", "fixer"
				}
				if e = db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphSucceeded, Outcome: outcome, FinishedAt: at, Transition: &GraphTransition{InstanceID: inst.ID, FromNode: "sync", ToNode: next, OnOutcome: outcome, At: at}}); e != nil {
					t.Fatal(e)
				}
			}
			if err = db.ParkGraphNode(t.Context(), GraphStop{Key: "fixer-blocked", InstanceID: inst.ID, Origin: "fixer", Cause: "failed", Reason: "Retained earlier fixer work", Candidate: []byte(`{}`), At: at}); err != nil {
				t.Fatal(err)
			}
			if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_owner_waits SET state='claimed',reply_id=105,reply_author_id=7,reply_body='continue',claimed_at=? WHERE delivery_key='fixer-blocked'`, FormatTime(at)); err != nil {
				t.Fatal(err)
			}
			if route == "valid" {
				if err = db.RedirectGraphBugFixerToSync(t.Context(), inst.ID, gate, at); err != nil {
					t.Fatal(err)
				}
			} else {
				// Corrupt/missing routing evidence must not release the original claim.
				if _, err = db.sql.ExecContext(t.Context(), `UPDATE graph_instances SET current_node='sync' WHERE id=?`, inst.ID); err != nil {
					t.Fatal(err)
				}
				if route == "wrong_outcome" {
					if _, err = db.RecordGraphTransition(t.Context(), GraphTransition{InstanceID: inst.ID, FromNode: "fixer", ToNode: "sync", OnOutcome: "retry", At: at}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err = db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "sync", Limit: 8, StartedAt: at, BugSyncPrerequisite: &gate}); !errors.Is(err, ErrGraphAttemptLimit) {
				t.Fatalf("exhausted sync reservation: %v", err)
			}
			stop := GraphStop{Key: "sync-refused", InstanceID: inst.ID, Origin: "sync", Cause: "exhausted", Reason: "Cumulative synchronization budget exhausted", Candidate: []byte(`{}`), At: at, AllowRunless: true, ReparkReplyID: 999}
			if err = db.ParkGraphNode(t.Context(), stop); !errors.Is(err, ErrGraphInvariant) {
				t.Fatalf("foreign reply repark: %v", err)
			}
			stop.ReparkReplyID = 105
			err = db.ParkGraphNode(t.Context(), stop)
			if route != "valid" {
				if !errors.Is(err, ErrGraphInvariant) {
					t.Fatalf("unproven reroute repark: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wait, err := db.GetGraphOwnerWait(t.Context(), inst.ID)
			if err != nil || wait.State != "waiting" || wait.OriginNode != "sync" || wait.DeliveryKey != stop.Key {
				t.Fatalf("new wait=%+v err=%v", wait, err)
			}
			var state, origin string
			if err = db.sql.QueryRowContext(t.Context(), `SELECT state,origin_node FROM graph_owner_waits WHERE delivery_key='fixer-blocked'`).Scan(&state, &origin); err != nil || state != "cancelled" || origin != "fixer" {
				t.Fatalf("original claim state=%s origin=%s err=%v", state, origin, err)
			}
			attempts, err := db.ListGraphNodeAttempts(t.Context(), inst.ID)
			if err != nil || len(attempts) != 8 {
				t.Fatalf("attempt count=%d err=%v", len(attempts), err)
			}
		})
	}
}
