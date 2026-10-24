package store

import (
	"errors"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/ticket"
	"testing"
	"time"
)

func TestOwnerInboxSnapshotScopeRetentionAndRevocation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "repo")
	v, err := db.SetConnection(ctx, ConnectionVersion{RepoID: repo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/repo", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = db.sql.ExecContext(ctx, `INSERT INTO implement_stories(parent,generation,story_key,position,branch,state,reason,created_at,updated_at,state_at,worker_session) VALUES(?,1,'story',0,'lf/story','parked','Owner guidance needed',?,?,?,?)`, session.TaskRef, FormatTime(now), FormatTime(now), FormatTime(now), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope := InboxScope{Owner: "owner", Transport: "test", Conversation: "room"}
	snap, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Total != 1 {
		t.Fatalf("total %d", snap.Total)
	}
	page, err := db.ReadInboxSnapshot(ctx, scope, snap.ID, 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Next != -1 {
		t.Fatalf("page %+v", page)
	}
	replay, err := db.ReadInboxSnapshot(ctx, scope, snap.ID, 0, 10, now)
	if err != nil || replay.Entries[0].Key != page.Entries[0].Key {
		t.Fatal("unstable replay", err)
	}
	foreign := scope
	foreign.Conversation = "other"
	if _, err = db.ReadInboxSnapshot(ctx, foreign, snap.ID, 0, 10, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("foreign scope", err)
	}
	if _, err = db.DisableConnection(ctx, repo.ID, TicketConnection, v.Revision); err != nil {
		t.Fatal(err)
	}
	page, err = db.ReadInboxSnapshot(ctx, scope, snap.ID, 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 || page.Snapshot.Coverage[0].Status != "access unavailable or configuration changed" {
		t.Fatal("revoked content visible")
	}
	if err = db.InboxSnapshotAuthorized(ctx, scope, snap.ID, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("saved content authorized", err)
	}

	if err = db.InboxSourcesAuthorized(ctx, scope, snap.ID, nil, now); err != nil {
		t.Fatal("safe navigation rejected", err)
	}
	if err = db.InboxSourcesAuthorized(ctx, scope, snap.ID, []int64{v.ID}, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("revoked source authorized", err)
	}
	if err = db.InboxSourcesAuthorized(ctx, scope, snap.ID, []int64{999999}, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("uncaptured source authorized", err)
	}

	refused, err := db.CurrentConnection(ctx, repo.ID, TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	refused.Enabled = true
	refused, err = db.SetConnection(ctx, refused, refused.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveConnectionObservation(ctx, ConnectionObservation{VersionID: refused.ID, CheckedAt: now, Status: "insufficient_permission"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InboxSnapshotAuthorized(ctx, scope, fresh.ID, now); err != nil {
		t.Fatal("coverage-only refused source denies fresh results", err)
	}
	if err = db.PurgeInboxSnapshots(ctx, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM owner_inbox_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retention", count, err)
	}
}

func TestOwnerInboxPlanMembershipUsesPublishedCurrentIndependentWaits(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "plan")
	_, err := db.SetConnection(ctx, ConnectionVersion{RepoID: repo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/plan", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared"}
	for _, key := range []string{"a", "b", "unpublished", "retired", "stale"} {
		state.Stories = append(state.Stories, planbrief.PlanStoryState{Key: key, Stage: "story", Candidate: "candidate-" + key, Draft: planbrief.PlanDraft{Key: key}})
	}
	for i := range state.Stories {
		story := &state.Stories[i]
		if err = state.OpenPlanWait(story.Key, "wait-"+story.Key, "review", int64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	state.Stories[3].Retired = true
	state.Stories[4].Candidate = "replacement"
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b", "retired", "stale"} {
		_, err = db.sql.ExecContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed) VALUES(?,1,?,1,?,?,'comment',?,2,'Review','marker',1,5,1)`, "delivery-"+key, session.ID, session.TaskRef, session.TaskRef, "wait-"+key)
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	scope := InboxScope{Owner: "owner", Transport: "chat", Conversation: "room"}
	snap, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{Kind: "review"}, now)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadInboxSnapshot(ctx, scope, snap.ID, 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].WaitID != "wait-a" || page.Entries[1].WaitID != "wait-b" {
		t.Fatalf("membership: %+v", page.Entries)
	}
	for _, d := range page.Entries {
		if d.Action == "" || d.Workflow != "plan" {
			t.Fatalf("missing policy: %+v", d)
		}
	}
	if _, err = db.CreateInboxSnapshot(ctx, scope, InboxFilter{Source: "missing"}, now); !errors.Is(err, ErrInboxFilter) {
		t.Fatal("invalid filter", err)
	}

	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_definitions(team,mode,workflow,profile_version,path,digest,created_at) VALUES('team','plan','plan',1,'plan.dot',?,?)`, "0123456789012345678901234567890123456789012345678901234567890123", FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,(SELECT MAX(id) FROM graph_definitions),'running','waiting',?)`, session.ID, FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_node_attempts(instance_id,node,attempt,status,started_at,finished_at) VALUES((SELECT MAX(id) FROM graph_instances),'set_agent',1,'succeeded',?,?)`, FormatTime(now), FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate) VALUES('set-question',(SELECT MAX(id) FROM graph_instances),(SELECT MAX(id) FROM graph_node_attempts),'set_ask','question','{"questions":[{"text":"Which stories?"}]}')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state) VALUES('set-question',(SELECT MAX(id) FROM graph_instances),(SELECT MAX(id) FROM graph_node_attempts),'waiting')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed) VALUES('set-question',1,?,1,?,?,'comment','set-question',2,'Which stories?','marker',1,8,1)`, session.ID, session.TaskRef, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	withQuestion, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if withQuestion.Total != 3 {
		t.Fatalf("graph-only question lost: %d waits", withQuestion.Total)
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',finished_at=? WHERE id=(SELECT MAX(id) FROM graph_instances)`, FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	ended, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if ended.Total != 0 {
		t.Fatalf("cancelled plan retains %d waits", ended.Total)
	}
}
