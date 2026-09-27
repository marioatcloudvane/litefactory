package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type prJourneyForge struct {
	p              github.DeliveryPR
	target         string
	creates, reads int
	lost, hidden   bool
}

func (f *prJourneyForge) GetDeliveryHead(_ context.Context, _ github.Repo, branch string) (string, error) {
	if branch == "main" {
		return f.target, nil
	}
	return f.p.Head, nil
}
func (f *prJourneyForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	if f.creates == 0 || f.hidden {
		return nil, nil
	}
	return []github.DeliveryPR{f.p}, nil
}
func (f *prJourneyForge) GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error) {
	f.reads++
	return f.p, nil
}
func (f *prJourneyForge) CreateDeliveryPR(_ context.Context, _ github.Repo, branch, target string, _ redact.Body, body redact.Body) (github.DeliveryPR, error) {
	f.creates++
	f.p.Branch = branch
	f.p.Target = target
	f.p.Body = body.String()
	if f.lost {
		return github.DeliveryPR{}, errors.New("controlled lost create reply")
	}
	return f.p, nil
}

func prJourneySetup(t *testing.T) (*store.DB, string, store.BugTestCall, *prJourneyForge, *turn.BugPRActivity, turn.BugPRInput) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pr.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := store.BugPRJourneyFixture(t, db)
	f := &prJourneyForge{p: github.DeliveryPR{ID: 4000, Number: 40, CreatorID: 99, Head: c.Request.Binding.SourceCommit, State: "open", HeadRepo: "acme/api", BaseRepo: "acme/api"}, target: c.Request.Binding.TargetCommit}
	h := &turn.BugPRActivity{Forge: f, BotID: 99, Redactor: redact.New(redact.Options{Environ: []string{}}), Observe: func(context.Context, store.BugCheckBinding) error { return nil }}
	in := turn.BugPRInput{SessionID: c.Request.SessionID, ChildCallID: c.ID, Operation: "prepare-1", IntentOperation: "task-pr", Branch: "lf/385-bug", At: time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)}
	if _, err = db.FreezeSessionSlug(t.Context(), in.SessionID, "bug", in.Branch); err != nil {
		t.Fatal(err)
	}
	return db, path, c, f, h, in
}
func prJourneyPublish(t *testing.T, db *store.DB, r store.BugDeliveryReview) {
	t.Helper()
	task := fmt.Sprintf("github:%s#%d", r.Evidence.Identity.Repository, r.Evidence.Identity.IssueNumber)
	if err := db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: r.DeliveryKey, InstanceID: r.Evidence.Child.Request.ParentInstance, SourceAttemptID: r.Evidence.Child.Request.ParentAttempt, Node: "review", Kind: "review", Candidate: []byte(r.Body)}); err != nil {
		t.Fatal(err)
	}
	_, err := db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: r.DeliveryKey, Version: 1, SessionID: r.SessionID, Task: task, Destination: task, TriggerNamespace: "bug-review", Purpose: "delivery-review", BotUserID: r.BotID, Body: r.Body, Marker: r.DeliveryKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(t.Context(), r.DeliveryKey); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmReportDelivery(t.Context(), r.DeliveryKey, 500); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteReportDelivery(t.Context(), r.DeliveryKey); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmBugDeliveryReview(t.Context(), r.SessionID, r.Digest); err != nil {
		t.Fatal(err)
	}
}
func prJourneyReply(r store.BugDeliveryReview, in turn.BugPRInput, body string) *store.GraphOwnerReply {
	task := fmt.Sprintf("github:%s#%d", r.Evidence.Identity.Repository, r.Evidence.Identity.IssueNumber)
	return &store.GraphOwnerReply{InstanceID: r.Evidence.Child.Request.ParentInstance, OwnerID: r.Evidence.Identity.OwnerID, AuthorID: r.Evidence.Identity.OwnerID, CommentID: 501, QuestionCommentID: 500, TaskRef: task, SourceRef: task, DeliveryKey: r.DeliveryKey, Body: body, At: in.At}
}
func TestBugPRJourneyLostCreateReopensWithoutDuplicate(t *testing.T) {
	db, path, _, f, h, in := prJourneySetup(t)
	f.lost = true
	out, err := h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "blocked" || f.creates != 1 {
		t.Fatalf("lost create: %+v %v creates=%d", out, err, f.creates)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	f.hidden = true
	in.Operation = "recover-hidden"
	out, err = h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "blocked" || f.creates != 1 {
		t.Fatalf("hidden effect duplicated: %+v %v %d", out, err, f.creates)
	}
	f.hidden = false
	in.Operation = "recover-visible"
	out, err = h.Prepare(t.Context(), db, in)
	if err != nil || out.Review == nil || f.creates != 1 || f.reads == 0 {
		t.Fatalf("recovery: %+v %v creates=%d reads=%d", out, err, f.creates, f.reads)
	}
	if out.Review.PR.ID != 4000 {
		t.Fatal("wrong recovered PR")
	}
}
func TestBugPRJourneyPublishedApprovalReplayAndDrift(t *testing.T) {
	db, _, _, f, h, in := prJourneySetup(t)
	out, err := h.Prepare(t.Context(), db, in)
	if err != nil || out.Review == nil {
		t.Fatalf("prepare: %+v %v", out, err)
	}
	r := *out.Review
	in.Operation = "approval"
	in.ReviewDigest = r.Digest
	in.Reply = prJourneyReply(r, in, r.ApprovalCommand())
	if _, err = h.Prepare(t.Context(), db, in); err == nil {
		t.Fatal("unpublished approval accepted")
	}
	prJourneyPublish(t, db, r)
	in.Operation = "wrong-owner"
	in.Reply.AuthorID++
	if _, err = h.Prepare(t.Context(), db, in); err == nil {
		t.Fatal("wrong owner accepted")
	}
	in.Reply.AuthorID--
	in.Operation = "approved"
	for range 2 {
		out, err = h.Prepare(t.Context(), db, in)
		if err != nil || out.Outcome != "approved" {
			t.Fatalf("approval replay: %+v %v", out, err)
		}
	}
	if _, decision, err := db.ApprovedBugDelivery(t.Context(), in.SessionID, r.Digest); err != nil || decision.CommentID != 501 {
		t.Fatalf("handoff: %+v %v", decision, err)
	}
	f.target = strings.Repeat("e", 40)
	in.Operation = "target-drift"
	in.Reply = nil
	out, err = h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "stale" {
		t.Fatalf("drift: %+v %v", out, err)
	}
	if _, _, err = db.ApprovedBugDelivery(t.Context(), in.SessionID, r.Digest); err == nil {
		t.Fatal("stale approval retained authority")
	}
	if f.creates != 1 {
		t.Fatalf("created %d PRs", f.creates)
	}
}
func TestBugPRJourneyFeedbackPreservesBudgetAndBlocksApproval(t *testing.T) {
	for _, kind := range []string{"clarify", "repair", "reshape", "discussion"} {
		t.Run(kind, func(t *testing.T) {
			db, _, c, f, h, in := prJourneySetup(t)
			out, err := h.Prepare(t.Context(), db, in)
			if err != nil || out.Review == nil {
				t.Fatalf("prepare: %+v %v", out, err)
			}
			r := *out.Review
			prJourneyPublish(t, db, r)
			before, _, _, err := db.CurrentBugAnalysis(t.Context(), in.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			body := "/bug-review " + r.Digest + " " + kind + " Explain the evidence"
			if kind == "discussion" {
				body = "What does this mean?"
			}
			in.Operation = "feedback"
			in.ReviewDigest = r.Digest
			in.Reply = prJourneyReply(r, in, body)
			out, err = h.Prepare(t.Context(), db, in)
			if err != nil {
				t.Fatal(err)
			}
			want := kind
			if want == "discussion" {
				want = "clarify"
			}
			if out.Outcome != want {
				t.Fatalf("outcome=%s want=%s", out.Outcome, want)
			}
			_, revision, err := db.BugDeliveryPrerequisites(t.Context(), in.SessionID, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			reply := *in.Reply
			reply.CommentID++
			reply.Body = r.ApprovalCommand()
			if _, err = db.ApplyBugDeliveryReply(t.Context(), in.SessionID, r.Digest, reply, revision); err == nil {
				t.Fatal("unresolved feedback approved")
			}
			after, _, _, err := db.CurrentBugAnalysis(t.Context(), in.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if before.AnalysisUsed != after.AnalysisUsed || store.BugPRJourneyRepairCount(t, db, in.SessionID) != 1 {
				t.Fatal("feedback spent budget")
			}
			if f.creates != 1 {
				t.Fatal("feedback duplicated PR")
			}
		})
	}
}

func TestBugPRJourneyRejectsSourceAndTaskMismatchBeforeCreate(t *testing.T) {
	for _, kind := range []string{"source", "task", "target"} {
		t.Run(kind, func(t *testing.T) {
			db, _, _, f, h, in := prJourneySetup(t)
			switch kind {
			case "source":
				h.Observe = func(context.Context, store.BugCheckBinding) error { return errors.New("controlled dirty source") }
			case "task":
				in.SessionID++
			case "target":
				f.target = strings.Repeat("f", 40)
			}
			out, err := h.Prepare(t.Context(), db, in)
			if err == nil && out.Outcome != "stale" && out.Outcome != "blocked" {
				t.Fatalf("invalid %s progressed: %+v", kind, out)
			}
			if f.creates != 0 {
				t.Fatalf("invalid %s created %d PRs", kind, f.creates)
			}
		})
	}
}

func TestBugPRJourneyRetainsReturnedIdentityAfterBadReadback(t *testing.T) {
	db, _, _, f, h, in := prJourneySetup(t)
	f.p.CreatorID = 17
	out, err := h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "blocked" {
		t.Fatalf("mismatched creator: %+v %v", out, err)
	}
	intent, err := db.GetBugPRIntent(t.Context(), in.SessionID)
	if err != nil || intent.PR == nil || intent.PR.ID != 4000 {
		t.Fatalf("returned identity lost: %+v %v", intent, err)
	}
	in.Operation = "recover-conflict"
	out, err = h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "blocked" || f.creates != 1 {
		t.Fatalf("conflict replaced PR: %+v %v creates=%d", out, err, f.creates)
	}
}
