package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
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
	due, err := db.ListDueGraphInstances(ctx, at.Add(365*24*time.Hour), 10)
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
	due, err = db.ListDueGraphInstances(ctx, at.Add(time.Hour), 10)
	check(err)
	if len(due) != 2 {
		t.Fatalf("claimed reply not due with unrelated work: %+v", due)
	}
}

type replyRecoveryForge struct {
	Forge
	state string
	err   error
}

func (f replyRecoveryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{State: f.state}, f.err
}

func TestStoppedBugReplyRecoveryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                                          string
		author                                        int64
		state                                         string
		observation                                   error
		terminal, noAdmission, waiting, child, locked bool
		want                                          bool
	}{
		{name: "owner open", author: 7, state: github.StateOpen, want: true},
		{name: "third party", author: 8, state: github.StateOpen},
		{name: "held lock", author: 7, state: github.StateOpen, locked: true},
		{name: "closed", author: 7, state: "closed"},
		{name: "observation failed", author: 7, observation: errors.New("offline")},
		{name: "terminal", author: 7, state: github.StateOpen, terminal: true},
		{name: "absent admission", author: 7, state: github.StateOpen, noAdmission: true},
		{name: "question wait", author: 7, state: github.StateOpen, waiting: true},
		{name: "child reply", author: 7, state: github.StateOpen, child: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
			check(err)
			t.Cleanup(func() { check(db.Close()) })
			check(db.Migrate(ctx))
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "bug", RemoteURL: "git@github.com:acme/bug.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			check(err)
			session, err := db.GetOrCreateSession(ctx, repo, 466)
			check(err)
			if !tc.noAdmission {
				_, err = db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 1})
				check(err)
			}
			_, err = db.AdmitWorkflowPath(ctx, session.ID, "graph")
			check(err)
			pin, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64)})
			check(err)
			at := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
			var instance store.GraphInstance
			if tc.noAdmission {
				instance, err = db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "conversation", StartedAt: at})
				check(err)
			} else {
				admitted, e := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: session.ID, TriggerID: 1, DefinitionID: pin.ID, Start: "conversation", ContentDigest: pin.Digest, TopologyDigest: pin.Digest, Files: []byte(`{"bug.dot":"eA=="}`)})
				check(e)
				instance = admitted.Instance
			}
			if tc.waiting {
				a, e := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: at})
				check(e)
				check(db.CompleteGraphNodeAttempt(ctx, a.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at}))
				check(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "question", InstanceID: instance.ID, SourceAttemptID: a.ID, Node: "question", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
			}
			dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
			check(err)
			check(db.CancelGraphDispatch(ctx, dispatch.ID, "startup refused", at))
			if tc.terminal {
				check(db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphCancelled, time.Time{}, at))
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil)).With(slog.Int64("session_id", session.ID))
			filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 7, OwnerHandle: "owner", BotUserID: 9, BotHandle: "bot"}, logger)
			check(err)
			d := &Daemon{db: db, filter: filter, forge: replyRecoveryForge{state: tc.state, err: tc.observation}, instanceID: "recovery-test"}
			work := &task{log: logger, worker: &workerState{}, session: session, forgeRepo: github.Repo{Owner: "acme", Name: "bug"}, tickAt: at}
			trigger := turn.Trigger{CommentID: 10, AuthorID: tc.author, Body: "resume"}
			if tc.child {
				trigger.PlanReply = &planbrief.PlanOwnerReply{}
			}
			if tc.locked {
				acquired, e := db.AcquireSessionLock(ctx, session.ID, "other-daemon")
				check(e)
				if !acquired {
					t.Fatal("fixture lock not acquired")
				}
			}
			_, err = d.recoverGraphReply(ctx, work, trigger, turn.Command{Kind: turn.KindFreeText, Text: "resume"})
			if tc.observation == nil {
				check(err)
			} else if err == nil {
				t.Fatal("observation failure ignored")
			}
			got, err := db.LatestGraphDispatch(ctx, instance.ID)
			check(err)
			if (got.ID != dispatch.ID) != tc.want {
				t.Fatalf("recovery=%+v want new=%v", got, tc.want)
			}
			assertLog := func(reason string) {
				t.Helper()
				for _, want := range []string{"level=INFO", "comment_id=10", fmt.Sprintf("session_id=%d", session.ID), reason} {
					if !strings.Contains(logs.String(), want) {
						t.Fatalf("missing %q: %s", want, &logs)
					}
				}
			}
			if tc.name == "closed" {
				assertLog("closed issue")
			}
			if tc.locked {
				assertLog("waiting for the session lock")
				before, exists, e := db.GetWatermark(ctx, session.ID)
				check(e)
				holder, e := db.GetSession(ctx, session.ID)
				check(e)
				if holder.LockOwner != "other-daemon" || work.worker.status != workerDeferred || exists && before.LastCommentID >= 10 {
					t.Fatal("lock deferral changed ownership or consumed input")
				}
				size := logs.Len()
				_, e = d.recoverGraphReply(ctx, work, trigger, turn.Command{Kind: turn.KindFreeText})
				check(e)
				if logs.Len() != size {
					t.Fatal("unchanged lock deferral logged repeatedly")
				}
				check(db.ReleaseSessionLock(ctx, session.ID, "other-daemon"))
				_, e = d.recoverGraphReply(ctx, work, trigger, turn.Command{Kind: turn.KindFreeText})
				check(e)
				retry, e := db.LatestGraphDispatch(ctx, instance.ID)
				check(e)
				if retry.ID == dispatch.ID {
					t.Fatal("released lock did not permit recovery")
				}
			}
			if tc.want {
				assertLog("scheduled recovery at node conversation")
				ordinaryBug, e := d.isBugParentDispatch(ctx, dispatch)
				check(e)
				recoveredBug, e := d.isBugParentDispatch(ctx, got)
				check(e)
				if !ordinaryBug || recoveredBug {
					t.Fatal("Bug closure observation and cancelled-work recovery lost their distinct gates")
				}
				if got.InstanceID != instance.ID || got.Node != instance.CurrentNode || got.Attempts != 0 {
					t.Fatalf("changed target: %+v", got)
				}
				check(db.CancelGraphDispatch(ctx, got.ID, "later stop", at))
				before, _, e := db.GetWatermark(ctx, session.ID)
				check(e)
				_, err = d.recoverGraphReply(ctx, work, trigger, turn.Command{Kind: turn.KindFreeText, Text: "resume"})
				check(err)
				replay, err := db.LatestGraphDispatch(ctx, instance.ID)
				check(err)
				assertLog("already consumed")
				after, _, e := db.GetWatermark(ctx, session.ID)
				check(e)
				if before != after {
					t.Fatal("consumed replay wrote watermark")
				}
				if replay.ID != got.ID || replay.Status != "cancelled" {
					t.Fatal("consumed reply restarted later stop")
				}
			}
		})
	}
}

type bugRecoveryOwnershipReporter struct {
	Reporter
	calls int
}

func (r *bugRecoveryOwnershipReporter) ReportPending(context.Context, int64, int64) (turn.Posted, bool, error) {
	r.calls++
	return turn.Posted{}, false, nil
}

func TestBugReportRecoveryOwnership(t *testing.T) {
	for _, tc := range []struct {
		purpose string
		run     bool
		want    int
	}{
		{"reply:bug_comment_not_applied:schedule:0", false, 1},
		{"reply:missing_team:schedule:0", false, 1},
		{"reply:unsupported_workflow:schedule:0", false, 1},
		{"reply:bug_comment_not_applied:schedule:0", true, 0},
		{"reply:bug_comment_not_applied_other:schedule:0", false, 0},
		{"reply:graph_resume_refused:schedule:0", false, 0},
		{"bug-question", false, 0},
	} {
		t.Run(fmt.Sprintf("%s/run=%t", tc.purpose, tc.run), func(t *testing.T) {
			d, _, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			session, err := d.db.GetOrCreateSession(ctx, repo, 774)
			check(err)
			_, err = d.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 1})
			check(err)
			var runID int64
			if tc.run {
				segment, e := d.db.GetOrCreateSegment(ctx, session.ID, 1, "bug", store.AgentClaude)
				check(e)
				run, e := d.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "bug", Agent: store.AgentClaude, TriggerCommentID: 40, LogPath: func(int) string { return "fixture.log" }})
				check(e)
				_, e = d.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed})
				check(e)
				runID = run.ID
			}
			_, err = d.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "ownership", Version: 1, SessionID: session.ID, RunID: runID, TriggerID: 40, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: tc.purpose, BotUserID: 99, Body: "retained", Marker: "ownership"})
			check(err)
			probe := &bugRecoveryOwnershipReporter{}
			d.reporter = probe
			_, err = d.resumeReport(ctx, &task{session: session, tickAt: d.now()}, 40)
			check(err)
			if probe.calls != tc.want {
				t.Fatalf("shared recovery calls=%d want=%d", probe.calls, tc.want)
			}
		})
	}
}
