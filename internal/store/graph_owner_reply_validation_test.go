package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClaimGraphOwnerReply_RejectsInvalidAdmissionBeforeStorage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*GraphOwnerReply)
	}{
		{"negative parent", func(in *GraphOwnerReply) { in.ReplyToCommentID = -1 }},
		{"missing owner", func(in *GraphOwnerReply) { in.OwnerID = 0 }},
		{"non-owner author", func(in *GraphOwnerReply) { in.AuthorID = 8 }},
		{"missing reply identity", func(in *GraphOwnerReply) { in.CommentID = 0 }},
		{"missing question identity", func(in *GraphOwnerReply) { in.QuestionCommentID = 0 }},
		{"missing mode", func(in *GraphOwnerReply) { in.Mode = "" }},
		{"missing task", func(in *GraphOwnerReply) { in.TaskRef = "" }},
		{"unrelated source", func(in *GraphOwnerReply) { in.SourceRef = "github:example/repo#342" }},
		{"missing publication correlation", func(in *GraphOwnerReply) { in.DeliveryKey = "" }},
		{"empty reply", func(in *GraphOwnerReply) { in.Body = "" }},
		{"whitespace reply", func(in *GraphOwnerReply) { in.Body = " \t\n" }},
		{"oversized reply", func(in *GraphOwnerReply) { in.Body = strings.Repeat("a", 65537) }},
		{"byte limit rather than rune count", func(in *GraphOwnerReply) { in.Body = strings.Repeat("é", 32769) }},
		{"missing event time", func(in *GraphOwnerReply) { in.At = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := GraphOwnerReply{InstanceID: 1, OwnerID: 7, AuthorID: 7, CommentID: 12, QuestionCommentID: 11, TaskRef: "github:example/repo#341", SourceRef: "github:example/repo#341", Mode: "research", DeliveryKey: "question-1", Body: "Continue with the saved findings.", At: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)}
			tc.mutate(&in)
			// Invalid external input must be rejected without consulting storage.
			var db *DB
			got, err := db.ClaimGraphOwnerReply(t.Context(), in)
			if !errors.Is(err, ErrGraphInvariant) {
				t.Fatalf("error=%v, want ErrGraphInvariant", err)
			}
			if got != (GraphOwnerWait{}) {
				t.Fatalf("invalid admission returned a claim: %+v", got)
			}
		})
	}
}

// predatingReplyFixture is a Research instance whose node published a question
// (comment 101) and now waits for its answer. complete controls whether the
// question's delivery has been completed.
func predatingReplyFixture(t *testing.T, complete bool) (*DB, GraphOwnerReply) {
	t.Helper()
	return ownerReplyFixture(t, complete, "research", "shape", "question", "question")
}

func ownerReplyFixture(t *testing.T, complete bool, workflow, source, node, kind string) (*DB, GraphOwnerReply) {
	t.Helper()
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 9, 24, 15, 44, 9, 0, time.UTC)
	db := newTestDB(t)
	repo, err := db.UpsertRepo(ctx, Repo{Path: t.TempDir(), Forge: ForgeGitHub, Owner: "acme", Name: "research", RemoteURL: "git@github.com:acme/research.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	check(err)
	session, err := db.GetOrCreateSession(ctx, repo, 378)
	check(err)
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "research", AgentClaude)
	check(err)
	run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "shape.log" }})
	check(err)
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: workflow, ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	check(err)
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: source, StartedAt: at, NextDueAt: at})
	check(err)
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: source, RunID: run.ID, Limit: 3, StartedAt: at})
	check(err)
	check(db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: at}))
	check(db.RetainGraphComment(ctx, GraphCommentIntent{Key: "shape-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: node, Kind: kind, Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err = db.EnsureReportDelivery(ctx, ReportDelivery{Key: "shape-question", SessionID: session.ID, RunID: run.ID, TriggerID: 20, Task: session.TaskRef, Destination: "acme/research#378", TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Which boundary?", Marker: "shape-question"})
	check(err)
	_, err = db.MarkReportDeliverySent(ctx, "shape-question")
	check(err)
	_, err = db.ConfirmReportDelivery(ctx, "shape-question", 101)
	check(err)
	if complete {
		_, err = db.CompleteReportDelivery(ctx, "shape-question")
		check(err)
	}
	return db, GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, QuestionCommentID: 101, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Mode: workflow, DeliveryKey: "shape-question", Body: "Use storage boundaries", At: at.Add(time.Minute)}
}

// A comment written before the open question was published (#378) is refused
// with its own sentinel, never the generic invariant, and claims nothing.
// Every other mismatch keeps the generic invariant.
func TestClaimGraphOwnerReply_PredatingCommentHasItsOwnSentinel(t *testing.T) {
	db, reply := predatingReplyFixture(t, true)
	for _, id := range []int64{100, 101} {
		early := reply
		early.CommentID = id
		got, err := db.ClaimGraphOwnerReply(t.Context(), early)
		if !errors.Is(err, ErrGraphReplyPredatesQuestion) {
			t.Fatalf("comment %d: error=%v, want ErrGraphReplyPredatesQuestion", id, err)
		}
		if errors.Is(err, ErrGraphInvariant) {
			t.Fatalf("comment %d: predating comment still reported as a generic invariant: %v", id, err)
		}
		if got != (GraphOwnerWait{}) {
			t.Fatalf("comment %d: refused comment returned a claim: %+v", id, got)
		}
	}
	for name, mutate := range map[string]func(*GraphOwnerReply){
		"wrong task": func(in *GraphOwnerReply) {
			in.TaskRef = "github:acme/research#999"
			in.SourceRef = in.TaskRef
		},
		"wrong mode":     func(in *GraphOwnerReply) { in.Mode = "plan" },
		"wrong question": func(in *GraphOwnerReply) { in.QuestionCommentID = 99 },
	} {
		for _, id := range []int64{100, 102} {
			bad := reply
			bad.CommentID = id
			mutate(&bad)
			if _, err := db.ClaimGraphOwnerReply(t.Context(), bad); !errors.Is(err, ErrGraphInvariant) || errors.Is(err, ErrGraphReplyPredatesQuestion) {
				t.Fatalf("%s, comment %d: error=%v, want only ErrGraphInvariant", name, id, err)
			}
		}
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), reply.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if wait.State != "waiting" || wait.ReplyID != 0 {
		t.Fatalf("refusals changed the open question: %+v", wait)
	}
	// The genuine answer, newer than the question, is still admitted.
	claimed, err := db.ClaimGraphOwnerReply(t.Context(), reply)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.State != "claimed" || claimed.ReplyID != reply.CommentID {
		t.Fatalf("genuine reply not claimed: %+v", claimed)
	}
}

// Before the question's publication is completed there is no published
// question to predate: the generic invariant still applies.
func TestClaimGraphOwnerReply_IncompleteQuestionKeepsGenericInvariant(t *testing.T) {
	db, reply := predatingReplyFixture(t, false)
	for _, id := range []int64{100, 102} {
		in := reply
		in.CommentID = id
		if _, err := db.ClaimGraphOwnerReply(t.Context(), in); !errors.Is(err, ErrGraphInvariant) || errors.Is(err, ErrGraphReplyPredatesQuestion) {
			t.Fatalf("comment %d with incomplete delivery: error=%v, want only ErrGraphInvariant", id, err)
		}
	}
}

func TestValidateResolverReplyWithInheritedRun(t *testing.T) {
	for _, legacy := range []string{"new", "waiting", "claimed"} {
		t.Run(legacy, func(t *testing.T) {
			db, reply := ownerReplyFixture(t, true, "validate", "owner_resolve", "owner_ask", "question")
			ctx := t.Context()
			wait, err := db.GetGraphOwnerWait(ctx, reply.InstanceID)
			if err != nil || !wait.AllowRunless {
				t.Fatalf("new wait must allow a factory reply: %+v %v", wait, err)
			}
			oldShape := func() {
				t.Helper()
				if _, err := db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET allow_runless=0 WHERE delivery_key=?`, reply.DeliveryKey); err != nil {
					t.Fatal(err)
				}
			}
			if legacy == "waiting" {
				oldShape()
			}
			start := GraphAttemptStart{InstanceID: reply.InstanceID, Node: "owner_resolve", Limit: 8, StartedAt: reply.At}
			if _, err := db.StartGraphNodeAttempt(ctx, start); !errors.Is(err, ErrGraphOwnerWait) {
				t.Fatalf("unclaimed reply: %v", err)
			}
			for _, mutate := range []func(*GraphOwnerReply){
				func(r *GraphOwnerReply) { r.AuthorID++ },
				func(r *GraphOwnerReply) { r.CommentID = r.QuestionCommentID },
				func(r *GraphOwnerReply) { r.QuestionCommentID++ },
				func(r *GraphOwnerReply) { r.InstanceID++ },
			} {
				bad := reply
				mutate(&bad)
				if _, err := db.ClaimGraphOwnerReply(ctx, bad); err == nil {
					t.Fatal("invalid reply admitted")
				}
			}
			if _, err := db.ClaimGraphOwnerReply(ctx, reply); err != nil {
				t.Fatal(err)
			}
			if legacy == "claimed" {
				oldShape()
			}
			wrong := start
			wrong.Node = "repair"
			if _, err := db.sql.ExecContext(ctx, `UPDATE graph_instances SET current_node='repair' WHERE id=?`, reply.InstanceID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.StartGraphNodeAttempt(ctx, wrong); !errors.Is(err, ErrGraphInvariant) || !strings.Contains(err.Error(), "reply allocation must resume") {
				t.Fatalf("wrong origin: %v", err)
			}
			if _, err := db.sql.ExecContext(ctx, `UPDATE graph_instances SET current_node='owner_resolve' WHERE id=?`, reply.InstanceID); err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartGraphNodeAttempt(ctx, start)
			if err != nil {
				t.Fatal(err)
			}
			wait, err = db.GetGraphOwnerWait(ctx, reply.InstanceID)
			if err != nil || wait.State != "allocated" || wait.ContinuationAttemptID != attempt.ID || attempt.RunID != 0 || attempt.Node != "owner_resolve" {
				t.Fatalf("runless continuation: %+v %+v %v", wait, attempt, err)
			}
			if _, err := db.StartGraphNodeAttempt(ctx, start); !errors.Is(err, ErrGraphAttemptRunning) {
				t.Fatalf("duplicate allocation: %v", err)
			}
		})
	}
}

func TestValidateResolverRunlessExceptionIsNarrow(t *testing.T) {
	for _, tc := range []struct{ workflow, source, node, kind string }{
		{"research", "owner_resolve", "owner_ask", "question"},
		{"plan", "owner_resolve", "owner_ask", "question"},
		{"implement", "owner_resolve", "owner_ask", "question"},
		{"validate", "repair", "owner_ask", "question"},
		{"validate", "owner_resolve", "other_question", "question"},
		{"validate", "owner_resolve", "owner_ask", "review"},
	} {
		t.Run(strings.Join([]string{tc.workflow, tc.source, tc.node, tc.kind}, "/"), func(t *testing.T) {
			db, reply := ownerReplyFixture(t, true, tc.workflow, tc.source, tc.node, tc.kind)
			if _, err := db.ClaimGraphOwnerReply(t.Context(), reply); err != nil {
				t.Fatal(err)
			}
			if _, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: reply.InstanceID, Node: tc.source, Limit: 8, StartedAt: reply.At}); !errors.Is(err, ErrGraphInvariant) {
				t.Fatalf("agent-backed wait accepted runless continuation: %v", err)
			}
		})
	}
}

func TestValidateResolverQuestionMustBePublished(t *testing.T) {
	db, reply := ownerReplyFixture(t, false, "validate", "owner_resolve", "owner_ask", "question")
	if _, err := db.ClaimGraphOwnerReply(t.Context(), reply); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("unpublished wait admitted: %v", err)
	}
}
