package store

import (
	"strings"
	"testing"
)

// stoppedLaunchRows is one seeded Bug start: the launch admitted by the
// private start trigger, an optional session run launched by that trigger and
// an optional completed start-node attempt that moved to the analyst.
type stoppedLaunchRows struct {
	db       *DB
	instance GraphInstance
	session  Session
	segment  int64
	run      Run
	attempt  GraphNodeAttempt
}

func seedStoppedLaunch(t *testing.T, withRun, finished, started bool) *stoppedLaunchRows {
	t.Helper()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 10)
	if _, err := db.AdmitWorkflow(ctx, TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	at := graphTestTime()
	def, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: strings.Repeat("c", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	trigger := BugStartTrigger(1)
	admitted, err := db.AdmitWorkflowLaunch(ctx, WorkflowLaunch{Workflow: "bug", SessionID: session.ID, TriggerID: trigger, DefinitionID: def.ID, Start: "conversation", ContentDigest: def.Digest, TopologyDigest: def.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	rows := &stoppedLaunchRows{db: db, session: session}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, FirstSegmentOrdinal, "conv", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	rows.segment = segment.ID
	if withRun {
		rows.run = rows.addRun(t, trigger, finished)
	}
	if started {
		rows.attempt, err = db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "conversation", Limit: 64, StartedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.CompleteGraphNodeAttempt(ctx, rows.attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "analyse", FinishedAt: at, InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "conversation", ToNode: "analyst", OnOutcome: "analyse", At: at}}); err != nil {
			t.Fatal(err)
		}
	}
	rows.instance = rows.reread(t)
	return rows
}

func (r *stoppedLaunchRows) addRun(t *testing.T, trigger int64, finished bool) Run {
	t.Helper()
	run, err := r.db.AllocateRun(t.Context(), AllocateRunParams{SessionID: r.session.ID, SegmentID: r.segment, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: trigger, StartedAt: graphTestTime(), LogPath: func(n int) string { return t.TempDir() + "/run.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if finished {
		if run, err = r.db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunFailed, Error: "stopped before the analyst"}); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func (r *stoppedLaunchRows) reread(t *testing.T) GraphInstance {
	t.Helper()
	instance, err := r.db.LatestStandaloneBugInstance(t.Context(), r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func (r *stoppedLaunchRows) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := r.db.sql.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestStoppedBugLaunch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                     string
		withRun, finished, start bool
		mutate                   func(t *testing.T, r *stoppedLaunchRows)
		want                     bool
	}{
		{name: "no attempt and one finished launched run", withRun: true, finished: true, want: true},
		{name: "start attempt moved to the analyst and one finished launched run", withRun: true, finished: true, start: true, want: true},
		{name: "running run", withRun: true},
		{name: "running run after the start attempt", withRun: true, start: true},
		{name: "run whose trigger is not a launch of this instance", withRun: true, finished: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			r.addRun(t, 500, true)
		}},
		{name: "owner wait", withRun: true, finished: true, start: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			// The wait alone, without the question intent it normally refers to,
			// so the graph comment check cannot be what refuses it.
			conn, err := r.db.sql.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{`PRAGMA foreign_keys=OFF`, `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state) VALUES('stopped-wait',?,?,'waiting')`, `PRAGMA foreign_keys=ON`} {
				args := []any{}
				if strings.HasPrefix(q, "INSERT") {
					args = []any{r.instance.ID, r.attempt.ID}
				}
				if _, err = conn.ExecContext(t.Context(), q, args...); err != nil {
					t.Fatal(err)
				}
			}
			if err = conn.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = r.db.GetGraphOwnerWait(t.Context(), r.instance.ID); err != nil {
				t.Fatalf("wait not seeded: %v", err)
			}
			if comments, err := r.db.ListGraphComments(t.Context(), r.instance.ID, "", 1); err != nil || len(comments) != 0 {
				t.Fatalf("wait seeded with a comment: %+v %v", comments, err)
			}
		}},
		{name: "dispatch", withRun: true, finished: true, start: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			if _, err := r.db.ParkGraphInstance(t.Context(), r.instance.ID, "analyst", graphTestTime(), graphTestTime()); err != nil {
				t.Fatal(err)
			}
			// Only the dispatch row remains as the difference.
			r.exec(t, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, r.instance.ID)
			if _, err := r.db.LatestGraphDispatch(t.Context(), r.instance.ID); err != nil {
				t.Fatalf("dispatch not seeded: %v", err)
			}
		}},
		{name: "graph comment", withRun: true, finished: true, start: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			if err := r.db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: "stopped-final", InstanceID: r.instance.ID, SourceAttemptID: r.attempt.ID, Node: "conversation", Kind: "final", Candidate: []byte(`{"comment":"done"}`)}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "analyst attempt", withRun: true, finished: true, start: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			a, err := r.db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: r.instance.ID, Node: "analyst", Limit: 64, StartedAt: graphTestTime()})
			if err != nil {
				t.Fatal(err)
			}
			if err = r.db.CompleteGraphNodeAttempt(t.Context(), a.ID, GraphCompletion{Status: GraphFailed, FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "zero runs"},
		{name: "zero runs after the start attempt", start: true},
		{name: "no attempt standing at the analyst", withRun: true, finished: true, mutate: func(t *testing.T, r *stoppedLaunchRows) {
			r.exec(t, `UPDATE graph_instances SET current_node='analyst' WHERE id=?`, r.instance.ID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := seedStoppedLaunch(t, tc.withRun, tc.finished, tc.start)
			if tc.mutate != nil {
				tc.mutate(t, r)
				r.instance = r.reread(t)
			}
			got, err := r.db.StoppedBugLaunch(t.Context(), r.instance, BugStartAnalystNode)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("StoppedBugLaunch=%t want %t instance=%+v", got, tc.want, r.instance)
			}
			ev, err := r.db.BugStartEvidence(t.Context(), r.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if ev.Stopped != tc.want {
				t.Fatalf("BugStartEvidence.Stopped=%t want %t", ev.Stopped, tc.want)
			}
		})
	}
	t.Run("no analyst node", func(t *testing.T) {
		t.Parallel()
		r := seedStoppedLaunch(t, true, true, false)
		if got, err := r.db.StoppedBugLaunch(t.Context(), r.instance, ""); err != nil || got {
			t.Fatalf("empty analyst accepted: %t %v", got, err)
		}
	})
}
