package store

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func overviewWaitReceipt(t *testing.T, db *DB, session Session, key string, confirmed *time.Time) {
	t.Helper()
	var at any
	if confirmed != nil {
		at = FormatTime(*confirmed)
	}
	_, err := db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed,confirmed_at,confirmed_url) VALUES(?,1,?,1,?,?,'comment',?,2,'Please review the saved candidate','marker',1,5,1,?,?)`, key, session.ID, session.TaskRef, session.TaskRef, key, at, "https://github.com/owner/repo/issues/10#issuecomment-5")
	if err != nil {
		t.Fatal(err)
	}
}

func TestOverviewWaitsIndependentCurrentPlansOrderingAndLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	// No ticket connection is configured: registered facts remain visible.
	state := planbrief.PlanState{SharedRevision: "shared"}
	for i := 0; i < 55; i++ {
		key := fmt.Sprintf("story-%03d", i)
		state.Stories = append(state.Stories, planbrief.PlanStoryState{Key: key, Stage: "story", Candidate: "candidate-" + key, Draft: planbrief.PlanDraft{Key: key}})
	}
	for _, s := range state.Stories {
		if err = state.OpenPlanWait(s.Key, "wait-"+s.Key, "review", 1); err != nil {
			t.Fatal(err)
		}
	}
	state.Stories[0].Candidate = "replacement"
	state.Stories[1].Retired = true
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 54; i++ {
		at := old.Add(time.Duration(i) * time.Hour)
		overviewWaitReceipt(t, db, session, fmt.Sprintf("wait-story-%03d", i), &at)
	}
	out, err := db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 50 || !out.HasMore || !slices.Contains(out.Warnings, "story_ticket_identity_unknown") {
		t.Fatalf("bounds: %+v", out)
	}
	if out.Entries[0].WaitID != "wait-story-002" || out.Entries[49].WaitID != "wait-story-051" {
		t.Fatalf("oldest ordering: %s .. %s", out.Entries[0].WaitID, out.Entries[49].WaitID)
	}
	w := out.Entries[0]
	if w.Since == nil || w.SinceProvenance != "publication_confirmation" || w.URL == "" || w.TaskRef != session.TaskRef || !strings.Contains(w.Summary, "Please review") {
		t.Fatalf("lost facts: %+v", w)
	}
}

func overviewGraphWait(t *testing.T, db *DB, session Session, key, workflow, node, state string, at time.Time) int64 {
	t.Helper()
	ctx := t.Context()
	r, err := db.sql.ExecContext(ctx, `INSERT INTO graph_definitions(team,mode,workflow,profile_version,path,digest,created_at) VALUES(?,?,?,1,'graph.dot',?,?)`, key, workflow, workflow, strings.Repeat("a", 64), FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	def, _ := r.LastInsertId()
	r, err = db.sql.ExecContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running',?,?)`, session.ID, def, node, FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,node,kind,candidate,stop_origin,stop_cause,stop_reason,created_at) VALUES(?,?,?,'question','{}','factory','capacity','waiting',?)`, key, id, node, FormatTime(at))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,state,origin_node) VALUES(?,?,?,?)`, key, id, state, node)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOverviewWaitsGraphEligibilityAndApproximateUnknownTimes(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	ctx := t.Context()
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, state := range []string{"waiting", "claimed", "allocated", "cancelled", "waiting", "waiting"} {
		s, err := db.GetOrCreateSession(ctx, repo, i+1)
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("graph-%d", i)
		id := overviewGraphWait(t, db, s, key, "implement", "ask", state, old)
		overviewWaitReceipt(t, db, s, key, nil)
		if i == 4 {
			_, err = db.sql.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',finished_at=? WHERE id=?`, FormatTime(old), id)
			if err != nil {
				t.Fatal(err)
			}
		}
		if i == 5 {
			_, err = db.sql.ExecContext(ctx, `UPDATE report_deliveries SET completed=0 WHERE delivery_key=?`, key)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	out, err := db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].WaitID != "graph-0" || out.Entries[0].SinceProvenance != "publication_intent_approximate" {
		t.Fatalf("eligibility: %+v", out)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_comment_intents SET created_at=NULL WHERE delivery_key='graph-0'`); err != nil {
		t.Fatal(err)
	}
	out, err = db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Since != nil || out.Entries[0].SinceProvenance != "unknown" {
		t.Fatalf("unknown wait lost: %+v", out)
	}
}

func TestOverviewWaitsPlanDecodeBudgetIsExplicit(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	ctx := t.Context()
	s, err := db.GetOrCreateSession(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	overviewWaitReceipt(t, db, s, "historical", nil)
	_, err = db.sql.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,?)`, s.TaskRef, strings.Repeat(" ", 1048577))
	if err != nil {
		t.Fatal(err)
	}
	out, err := db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !out.HasMore || len(out.Entries) != 0 || len(out.Warnings) != 1 || out.Warnings[0] != "plan_wait_decode_truncated" {
		t.Fatalf("budget hidden: %+v", out)
	}
}

func TestOverviewWaitsPlanSetAssembledAndClaimedRevisions(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	ctx := t.Context()
	session, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Revision: "set-current"}, Stories: []planbrief.PlanStoryState{{Key: "story", Stage: "story", Candidate: "candidate", Draft: planbrief.PlanDraft{Key: "story"}}}}
	if err = state.OpenPlanWait("story", "story-wait", "review", 1); err != nil {
		t.Fatal(err)
	}
	state.AssembledWait = state.PlanSetRevision()
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(ctx, repo, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.BindLegacySessionOrigin(ctx, child.ID, "https://api.github.com"); err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number) VALUES(?,'story',1,1,20)`, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	graph := overviewGraphWait(t, db, session, "set-wait", "plan", "set_review", "waiting", old)
	overviewWaitReceipt(t, db, session, "set-wait", nil)
	_, err = db.sql.ExecContext(ctx, `UPDATE report_deliveries SET body='Review set-current' WHERE delivery_key='set-wait'`)
	if err != nil {
		t.Fatal(err)
	}
	overviewWaitReceipt(t, db, session, "plan-assembled:"+state.AssembledWait, nil)
	overviewWaitReceipt(t, db, session, "story-wait", nil)
	out, err := db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 3 {
		t.Fatalf("independent waits: %+v", out)
	}
	seen := map[string]bool{}
	for _, w := range out.Entries {
		seen[w.Kind] = true
		if w.Kind == "review" && (w.TaskRef != child.TaskRef || w.Ticket.Key != "20" || w.TaskURL != "https://github.com/owner/repo/issues/20") {
			t.Fatalf("story ticket identity lost: %+v", w)
		}
	}
	if !seen["assembled-review"] || !seen["set-review"] || !seen["review"] {
		t.Fatalf("kinds: %+v", seen)
	}
	// Allocating the story's reply leaves stale Plan state but must suppress
	// that publication even when the graph representation uses its delivery ID.
	_, err = db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated' WHERE delivery_key='set-wait'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,node,kind,candidate,stop_origin,stop_cause,stop_reason,created_at) VALUES('story-wait',?,'story_review','question','{}','factory','capacity','waiting',?)`, graph, FormatTime(old))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,state,origin_node) VALUES('story-wait',?,'claimed','story_review')`, graph)
	if err != nil {
		t.Fatal(err)
	}
	out, err = db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Kind != "assembled-review" {
		t.Fatalf("claimed/allocated waits retained: %+v", out)
	}
}

func TestOverviewWaitsRetainedTicketBuildsHistoricalCommentLink(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	ctx := t.Context()
	session, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.BindLegacySessionOrigin(ctx, session.ID, "https://github.enterprise.test/api/v3"); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	overviewGraphWait(t, db, session, "historical-question", "implement", "ask", "waiting", old)
	overviewWaitReceipt(t, db, session, "historical-question", nil)
	_, err = db.sql.ExecContext(ctx, `UPDATE report_deliveries SET confirmed_url=NULL WHERE delivery_key='historical-question'`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := db.ListOverviewWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("waits: %+v", out)
	}
	w := out.Entries[0]
	if w.Ticket.Origin != "https://github.enterprise.test/api/v3" || w.TaskURL != "https://github.enterprise.test/owner/repo/issues/10" || w.URL != w.TaskURL+"#issuecomment-5" {
		t.Fatalf("retained identity/link: %+v", w)
	}
}
