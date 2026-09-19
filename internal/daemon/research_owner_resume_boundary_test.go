package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchOwnerAdmissionPublicationAndOtherDueWork(t *testing.T) {
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "owner.db"))
	check(err)
	t.Cleanup(func() { check(db.Close()) })
	check(db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "research", RemoteURL: "git@github.com:acme/research.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	check(err)
	session, err := db.GetOrCreateSession(ctx, repo, 341)
	check(err)
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "research", store.AgentClaude)
	check(err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "owner.log" }})
	check(err)
	at := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	check(err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "research", StartedAt: at, NextDueAt: at})
	check(err)
	source, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "research", RunID: run.ID, Limit: 3, StartedAt: at})
	check(err)
	check(db.CompleteGraphNodeAttempt(ctx, source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning}))
	check(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "owner-question", InstanceID: instance.ID, SourceAttemptID: source.ID, Node: "question", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
	otherSession, err := db.GetOrCreateSession(ctx, repo, 342)
	check(err)
	other, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: otherSession.ID, DefinitionID: definition.ID, CurrentNode: "research", StartedAt: at, NextDueAt: at})
	check(err)
	due, err := pollDueGraphInstances(ctx, db, at.Add(365*24*time.Hour), 10)
	check(err)
	if len(due) != 1 || due[0].ID != other.ID {
		t.Fatalf("owner wait expired or excluded unrelated work: %+v", due)
	}
	comment := github.Comment{ID: 102, Body: "Use storage boundaries", Author: github.User{ID: 7, Login: "renamed-owner"}}
	admit := func(c github.Comment) (store.GraphOwnerWait, error) {
		return AdmitResearchOwnerReply(ctx, db, instance.ID, 7, session.TaskRef, session.TaskRef, "research", c, at.Add(time.Hour))
	}
	if _, err = admit(comment); err == nil {
		t.Fatal("no report intent supplied authority")
	}
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "owner-question", SessionID: session.ID, RunID: run.ID, TriggerID: 20, Task: session.TaskRef, Destination: "acme/research#341", TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Which boundary?", Marker: "owner-question"})
	check(err)
	if _, err = admit(comment); err == nil {
		t.Fatal("pending publication supplied authority")
	}
	_, err = db.MarkReportDeliverySent(ctx, "owner-question")
	check(err)
	if _, err = admit(comment); err == nil {
		t.Fatal("uncertain publication supplied authority")
	}
	_, err = db.ConfirmReportDelivery(ctx, "owner-question", 101)
	check(err)
	if _, err = admit(comment); err == nil {
		t.Fatal("uncompleted delivery supplied authority")
	}
	_, err = db.CompleteReportDelivery(ctx, "owner-question")
	check(err)
	spoof := comment
	spoof.Author.ID = 8
	spoof.Author.Login = "owner"
	if _, err = admit(spoof); err == nil {
		t.Fatal("login spoof authorized reply")
	}
	stale := comment
	stale.ID = 101
	if _, err = admit(stale); err == nil {
		t.Fatal("stale comment admitted")
	}
	wait, err := admit(comment)
	check(err)
	if wait.State != "claimed" || wait.ReplyAuthorID != 7 || wait.ReplyID != 102 || wait.SourceAttemptID != source.ID {
		t.Fatalf("claim: %+v", wait)
	}
	if _, err = admit(comment); err == nil {
		t.Fatal("duplicate event admitted")
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
	check(err)
	if len(attempts) != 1 || attempts[0].Status != store.GraphSucceeded {
		t.Fatal("admission launched an agent")
	}
	due, err = pollDueGraphInstances(ctx, db, at.Add(time.Hour), 10)
	check(err)
	if len(due) != 2 {
		t.Fatalf("claimed reply not due with unrelated work: %+v", due)
	}
}
