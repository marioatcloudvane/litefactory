package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestProviderReplyTargetReachesDecisionBinding(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	issue := map[string]any{"id": "ticket-uuid", "identifier": "LIN-1", "creator": map[string]any{"id": "owner-uuid", "active": true}, "url": "https://linear.app/test/issue/LIN-1", "team": map[string]string{"id": "team"}, "project": map[string]string{"id": "project"}, "state": map[string]string{"id": "open", "type": "unstarted"}, "labels": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if !strings.Contains(request.Query, "parent { id issue { id } }") {
			t.Error("provider query dropped parent scope")
		}
		response := map[string]any{"data": map[string]any{"issue": issue, "comments": map[string]any{"nodes": []any{map[string]any{"id": "child-comment", "body": "/validate-ship -- accept displayed risk", "user": map[string]any{"id": "owner-uuid", "active": true}, "issue": issue, "parent": map[string]any{"id": "report-comment", "issue": map[string]string{"id": "ticket-uuid"}}}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := linear.New(linear.Config{Origin: server.URL, WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key"}, secrets.NewToken("fixture"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	identity := ticket.Identity{Provider: ticket.Linear, Origin: ticket.Origin(server.URL), RemoteID: "ticket-uuid"}
	ref := ticket.Ref{Identity: identity, Locator: ticket.Locator{Provider: ticket.Linear, Origin: identity.Origin, Container: "team/project", Key: "ticket-uuid"}}
	page, err := client.RepliesAfter(ctx, ref, ticket.Cursor{})
	if err != nil || len(page.Replies) != 1 {
		t.Fatalf("provider: %+v %v", page, err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	}()
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	check(db.Migrate(ctx))
	d := &Daemon{db: db}
	comment, err := d.legacyReply(ctx, page.Replies[0])
	check(err)
	if comment.ReplyToCommentID < 1 {
		t.Fatal("provider parent lost in compatibility conversion")
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	check(err)
	session, err := db.GetOrCreateSession(ctx, repo, 1)
	check(err)
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: comment.Author.ID, BotUserID: 99}, nil)
	check(err)
	admission, err := filter.Admit(ctx, session.ID, comment)
	check(err)
	if admission.Comment.ReplyToCommentID != comment.ReplyToCommentID {
		t.Fatal("admitted source lost parent")
	}
	trigger, err := d.projectTrigger(ctx, session.ID, comment)
	check(err)
	if trigger.ReplyToCommentID != comment.ReplyToCommentID {
		t.Fatal("trigger lost parent")
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "test", store.AgentClaude)
	check(err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "/tmp/fixture.log" }})
	check(err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
	check(err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "owner_resolve", StartedAt: now, NextDueAt: now})
	check(err)
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "owner_resolve", RunID: run.ID, StartedAt: now})
	check(err)
	check(db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "wait", FinishedAt: now}))
	check(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "owner_resolve", Kind: "question", Candidate: []byte(`{"question":"ship?"}`)}))
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "question", SessionID: session.ID, RunID: run.ID, TriggerID: 20, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "issue-comment", Purpose: "validate-question", BotUserID: 99, Body: "ship?", Marker: "question"})
	check(err)
	_, err = db.MarkReportDeliverySent(ctx, "question")
	check(err)
	// The active wait publication is distinct from the source's explicit parent.
	activeQuestion := comment.ID - 1
	_, err = db.ConfirmReportDelivery(ctx, "question", activeQuestion)
	check(err)
	_, err = db.CompleteReportDelivery(ctx, "question")
	check(err)
	wait, err := AdmitGraphOwnerReply(ctx, db, instance.ID, comment.Author.ID, session.TaskRef, session.TaskRef, "validate", comment, now)
	check(err)
	if wait.ReplyToCommentID != comment.ReplyToCommentID {
		t.Fatal("claimed wait lost parent")
	}
	st := turn.ValidateState{InstanceID: instance.ID, Revision: "0123456789ab", Commit: strings.Repeat("a", 40)}
	check(db.PutOwnerDecisionDisplay(ctx, store.OwnerDecisionDisplay{TaskRef: session.TaskRef, Action: turn.ValidateShipCommand, Target: "shown-target", Revision: st.Revision, InstanceID: instance.ID, PublicationID: comment.ReplyToCommentID, TestedCommit: st.Commit, DeliveryCommit: st.Commit, CreatedAt: now}))
	cmd, binding, err := turn.ResolveValidateDecision(ctx, turn.Options{DB: db}, turn.ActivityRequest{InstanceID: instance.ID}, st, wait)
	check(err)
	if cmd.Revision != st.Revision || binding.ReplyToCommentID != comment.ReplyToCommentID || binding.Display.PublicationID == activeQuestion {
		t.Fatalf("wrong explicit target: %s %+v", fmt.Sprint(cmd), binding)
	}
}
