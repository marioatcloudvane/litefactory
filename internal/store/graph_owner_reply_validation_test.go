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
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	check(err)
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "shape", StartedAt: at, NextDueAt: at})
	check(err)
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "shape", RunID: run.ID, Limit: 3, StartedAt: at})
	check(err)
	check(db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: at}))
	check(db.RetainGraphComment(ctx, GraphCommentIntent{Key: "shape-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "question", Kind: "question", Candidate: []byte(`{"outcome":"incomplete"}`)}))
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
	return db, GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, QuestionCommentID: 101, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Mode: "research", DeliveryKey: "shape-question", Body: "Use storage boundaries", At: at.Add(time.Minute)}
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
