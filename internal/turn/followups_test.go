package turn

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestParseFollowup(t *testing.T) {
	key := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{{"linked", "/followup 1 " + key + " linked", true}, {"attach", "/followup 1 " + key + " attach", true}, {"missing intent", "/followup 1 " + key, false}, {"no generation", "/followup 0 " + key + " linked", false}, {"not digest", "/followup 1 abc linked", false}, {"unknown action", "/followup 1 " + key + " merge", false}, {"free prose", "please create the issue", false}, {"oversized", strings.Repeat("x", 513), false}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFollowup(tc.body)
			if (err == nil) != tc.valid {
				t.Fatalf("request=%+v err=%v", got, err)
			}
			if tc.valid && (got.Key != key || got.Generation != 1 || got.Attach != (tc.name == "attach")) {
				t.Fatalf("scope lost %+v", got)
			}
		})
	}
}

type followupTestForge struct {
	*exceptionTestForge
	db                   *store.DB
	created              []github.Issue
	attached             map[int64]bool
	creates, attachments int
	lose                 string
}

func (f *followupTestForge) ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error) {
	return f.created, nil
}
func (f *followupTestForge) CreateIssue(ctx context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	proposals, err := f.db.ListFollowups(ctx)
	if err != nil {
		return github.Issue{}, err
	}
	authorized := false
	for _, p := range proposals {
		if p.Title == title.String() && p.Body == body.String() && p.ApprovalComment > 0 && p.ActorID > 0 && p.Attempted {
			authorized = true
		}
	}
	if !authorized {
		return github.Issue{}, errors.New("create before durable exact approval and intent")
	}
	f.creates++
	if f.lose == "create before" {
		f.lose = ""
		return github.Issue{}, errors.New("uncertain create without known result")
	}
	issue := github.Issue{ID: int64(8000 + f.creates), Number: 800 + f.creates, Title: title.String(), Body: body.String(), Author: github.User{ID: 9}, State: "open"}
	f.created = append(f.created, issue)
	if f.lose == "create" {
		f.lose = ""
		return github.Issue{}, errors.New("lost create response")
	}
	return issue, nil
}
func (f *followupTestForge) GetIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	for _, i := range f.created {
		if i.Number == n {
			return i, nil
		}
	}
	return f.exceptionTestForge.GetIssue(ctx, r, n)
}
func (f *followupTestForge) ListSubIssues(ctx context.Context, r github.Repo, n int) ([]github.Issue, error) {
	items, err := f.exceptionTestForge.ListSubIssues(ctx, r, n)
	for _, i := range f.created {
		if f.attached[i.ID] {
			items = append(items, i)
		}
	}
	return items, err
}
func (f *followupTestForge) AddSubIssue(_ context.Context, _ github.Repo, _ int, id int64) error {
	f.attachments++
	if f.lose == "attach before" {
		f.lose = ""
		return errors.New("attachment denied")
	}
	f.attached[id] = true
	if f.lose == "attach" {
		f.lose = ""
		return errors.New("lost attachment response")
	}
	return nil
}
func (f *followupTestForge) PostComment(ctx context.Context, r github.Repo, n int, b redact.Body) (github.Comment, error) {
	if n == testIssue {
		proposals, err := f.db.ListFollowups(ctx)
		if err != nil || len(proposals) == 0 {
			return github.Comment{}, errors.New("proposal notice before concrete retained content")
		}
		c, err := f.progressForge.PostComment(ctx, r, n, b)
		if f.lose == "report" {
			f.lose = ""
			return github.Comment{}, errors.New("lost notice response")
		}
		return c, err
	}
	return f.exceptionTestForge.PostComment(ctx, r, n, b)
}
func followupFixture(t *testing.T) (*fixture, *followupTestForge, store.ValidationAssessment) {
	t.Helper()
	f, base, a := ownerExceptionFixture(t, "unverified")
	forge := &followupTestForge{exceptionTestForge: base, db: f.db, attached: map[int64]bool{}}
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "followup-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	mustProgress(t, f.db.RetainValidationAssessment(t.Context(), a))
	return f, forge, a
}
func firstFollowup(t *testing.T, f *fixture) store.FollowupProposal {
	t.Helper()
	all, err := f.db.ListFollowups(t.Context())
	mustProgress(t, err)
	if len(all) == 0 {
		t.Fatal("no concrete proposal")
	}
	return all[0]
}
func followupControl(q store.FollowupProposal, mode string) FollowupControl {
	return FollowupControl{Parent: q.Parent, CommentID: 123, ActorID: 9, OwnerID: 9, Body: fmt.Sprintf("/followup %d %s %s", q.Generation, q.Key, mode)}
}
func restartFollowups(t *testing.T, f *fixture, forge *followupTestForge) {
	t.Helper()
	p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "restart-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
}

func TestFollowupProposalPrecedesApprovalAndDoesNotWaive(t *testing.T) {
	f, forge, a := followupFixture(t)
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	q := firstFollowup(t, f)
	if q.Title == "" || q.Body == "" || !q.Proposed || q.ApprovalComment != 0 || forge.creates != 0 {
		t.Fatalf("unexpected proposal %+v creates=%d", q, forge.creates)
	}
	if !strings.Contains(q.Body, "AC-1") || !strings.Contains(q.Body, "200") {
		t.Fatalf("proposal missing source identity: %s", q.Body)
	}
	var notice string
	for _, c := range forge.comments[testIssue] {
		notice += c.Body
	}
	if !strings.Contains(notice, q.Title) || !strings.Contains(notice, q.Key) {
		t.Fatalf("approval not concrete: %s", notice)
	}
	mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "linked")))
	got, err := f.db.GetFollowup(t.Context(), q.Key)
	mustProgress(t, err)
	if got.IssueID < 1 || !got.Reported || got.Attach || len(forge.attached) != 0 {
		t.Fatalf("linked result %+v", got)
	}
	source := forge.children[200]
	if source.State != "open" || strings.Contains(source.Body, "[x]") {
		t.Fatalf("followup changed source acceptance %+v", source)
	}
	if _, err := f.db.GetOwnerException(t.Context(), a.Parent, 123); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("followup granted waiver: %v", err)
	}
}

func TestFollowupApprovalRequiresNumericOwnerAndExactProposal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*FollowupControl)
	}{{"third party", func(c *FollowupControl) { c.ActorID = 8 }}, {"zero identity", func(c *FollowupControl) { c.OwnerID = 0; c.ActorID = 0 }}, {"wrong generation", func(c *FollowupControl) { c.Body = strings.Replace(c.Body, "/followup 1", "/followup 2", 1) }}, {"unknown content", func(c *FollowupControl) { c.Body = "/followup 1 " + strings.Repeat("b", 64) + " linked" }}, {"manual tick", func(c *FollowupControl) { c.Body = "- [x] approved" }}} {
		t.Run(tc.name, func(t *testing.T) {
			f, forge, a := followupFixture(t)
			mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
			q := firstFollowup(t, f)
			c := followupControl(q, "linked")
			tc.change(&c)
			if err := f.opts.StoryProgress.ApplyFollowup(t.Context(), c); err == nil && (tc.name == "third party" || tc.name == "zero identity") {
				t.Fatal("invalid identity accepted")
			}
			got, err := f.db.GetFollowup(t.Context(), q.Key)
			mustProgress(t, err)
			if got.ApprovalComment != 0 || forge.creates != 0 {
				t.Fatalf("unauthorized mutation %+v", got)
			}
		})
	}
}

func TestFollowupLostEffectsReconcileWithoutDuplicateIssues(t *testing.T) {
	for _, lost := range []string{"create", "attach", "attach before", "report"} {
		t.Run(lost, func(t *testing.T) {
			f, forge, a := followupFixture(t)
			mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
			q := firstFollowup(t, f)
			forge.lose = lost
			if err := f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "attach")); err == nil {
				t.Fatal("injected failure disappeared")
			}
			pending, err := f.db.GetFollowup(t.Context(), q.Key)
			mustProgress(t, err)
			if pending.Diagnostic == "" || pending.Reported {
				t.Fatalf("failure lacked durable debt %+v", pending)
			}
			if lost == "create" {
				forge.created[0].State = "closed"
			}
			restartFollowups(t, f, forge)
			mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
			got, err := f.db.GetFollowup(t.Context(), q.Key)
			mustProgress(t, err)
			if !got.Reported || !got.Attached || got.IssueID < 1 || len(forge.created) != 1 || forge.creates != 1 {
				t.Fatalf("not reconciled %+v created=%d attempts=%d", got, len(forge.created), forge.creates)
			}
			before := len(forge.comments[testIssue])
			mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "attach")))
			if len(forge.comments[testIssue]) != before || forge.creates != 1 {
				t.Fatal("duplicate approval duplicated effects")
			}
		})
	}
}

func TestFollowupUncertainCreateNeverBlindlyRetries(t *testing.T) {
	f, forge, a := followupFixture(t)
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	q := firstFollowup(t, f)
	forge.lose = "create before"
	if err := f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "linked")); err == nil {
		t.Fatal("uncertain result accepted")
	}
	restartFollowups(t, f, forge)
	if err := f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent); err == nil {
		t.Fatal("missing reconciliation accepted")
	}
	if forge.creates != 1 || len(forge.created) != 0 {
		t.Fatal("blind duplicate create")
	}
}

func TestFollowupReconciliationRejectsUntrustedOrAmbiguousMarkers(t *testing.T) {
	for _, kind := range []string{"untrusted", "duplicate", "changed body"} {
		t.Run(kind, func(t *testing.T) {
			f, forge, a := followupFixture(t)
			mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
			q := firstFollowup(t, f)
			forge.lose = "create"
			if err := f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "linked")); err == nil {
				t.Fatal("injection failed")
			}
			switch kind {
			case "untrusted":
				forge.created[0].Author.ID = 8
			case "duplicate":
				duplicate := forge.created[0]
				duplicate.ID++
				duplicate.Number++
				forge.created = append(forge.created, duplicate)
			case "changed body":
				forge.created[0].Body += "\nchanged after approval"
			}
			restartFollowups(t, f, forge)
			if err := f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent); err == nil {
				t.Fatal("ambiguous reconciliation accepted")
			}
			got, err := f.db.GetFollowup(t.Context(), q.Key)
			mustProgress(t, err)
			if got.Reported || forge.creates != 1 {
				t.Fatalf("ambiguous effect completed %+v", got)
			}
		})
	}
}

func TestFollowupSeparateAttachmentAndImmutableContent(t *testing.T) {
	f, forge, a := followupFixture(t)
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	q := firstFollowup(t, f)
	mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "linked")))
	changed := q
	changed.Title += " changed"
	if err := f.db.RetainFollowup(t.Context(), changed); err == nil {
		t.Fatal("same approved key accepted changed content")
	}
	changed.Key = strings.Repeat("c", 64)
	mustProgress(t, f.db.RetainFollowup(t.Context(), changed))
	fresh, err := f.db.GetFollowup(t.Context(), changed.Key)
	mustProgress(t, err)
	if fresh.ApprovalComment != 0 || fresh.IssueID != 0 {
		t.Fatal("new content inherited approval")
	}
	c := followupControl(q, "attach")
	c.CommentID = 124
	mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), c))
	got, err := f.db.GetFollowup(t.Context(), q.Key)
	mustProgress(t, err)
	if !got.Attached || got.AttachmentComment != 124 || forge.creates != 1 {
		t.Fatalf("separate attachment lost authority %+v", got)
	}
	children, err := forge.ListSubIssues(t.Context(), github.Repo{}, testIssue)
	mustProgress(t, err)
	found := false
	for _, i := range children {
		if i.ID == got.IssueID {
			found = true
		}
	}
	if !found {
		t.Fatal("approved attachment omitted from readiness inventory")
	}
}

func TestFollowupRefusedControlsRemainRecoverableForValidApproval(t *testing.T) {
	f, forge, a := followupFixture(t)
	mustProgress(t, f.opts.StoryProgress.retainFollowups(t.Context(), a.Parent))
	q := firstFollowup(t, f)
	if q.Proposed {
		t.Fatal("retention claimed remote proposal")
	}
	if err := f.opts.StoryProgress.ApplyFollowup(t.Context(), followupControl(q, "linked")); err != nil {
		t.Fatalf("unproposed control cannot be consumed: %v", err)
	}
	if forge.creates != 0 {
		t.Fatal("unproposed issue created")
	}
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	for idx, body := range []string{"/followup broken", "/followup 2 " + q.Key + " linked", "/followup 1 " + strings.Repeat("d", 64) + " linked"} {
		c := followupControl(q, "linked")
		c.CommentID = int64(200 + idx)
		c.Body = body
		mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), c))
		before := len(forge.comments[testIssue])
		mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), c))
		if len(forge.comments[testIssue]) != before {
			t.Fatal("terminal refusal duplicated comment")
		}
	}
	c := followupControl(q, "linked")
	c.CommentID = 205
	mustProgress(t, f.opts.StoryProgress.ApplyFollowup(t.Context(), c))
	got, err := f.db.GetFollowup(t.Context(), q.Key)
	mustProgress(t, err)
	if got.ApprovalComment != 205 || !got.Reported {
		t.Fatalf("refusal blocked later correct approval %+v", got)
	}
	pending, err := f.db.ListFollowupAssessments(t.Context())
	mustProgress(t, err)
	if len(pending) != 0 {
		t.Fatal("materialized assessment remains permanently pending")
	}
}

// Issue #370 (C5): a submission-v2 marker row for the same parent, with no
// follow-up materialization record (production runs 430/564), must not block
// delivery of an approved follow-up on the next tick.
func TestFollowupDeliveryContinuesPastSubmissionMarker(t *testing.T) {
	f, forge, a := followupFixture(t)
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	q := firstFollowup(t, f)
	if !q.Proposed || q.Reported {
		t.Fatalf("fixture proposal not awaiting approval: %+v", q)
	}
	// Approval is retained; its creation and report remain pending until the tick.
	mustProgress(t, f.db.ApproveFollowup(t.Context(), q.Key, 123, 9, false))
	// Keep the v1 fixture assessment out of the acceptance projection so this
	// tick exercises follow-up delivery only.
	mustProgress(t, f.db.MaterializeValidationAssessment(t.Context(), a.RunID))
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "session", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 4300, LogPath: func(int) string { return "marker.log" }})
	mustProgress(t, err)
	mustProgress(t, f.db.RetainValidationAssessment(t.Context(), store.ValidationAssessment{RunID: run.ID, Parent: a.Parent, Generation: a.Generation, ContextJSON: store.SubmissionAssessmentMarker, ResultJSON: store.SubmissionAssessmentMarker}))
	mustProgress(t, f.db.MaterializeValidationAssessment(t.Context(), run.ID))
	if err := f.opts.StoryProgress.Poll(t.Context()); err != nil {
		if strings.Contains(err.Error(), "invalid validation scope or output size") {
			t.Fatalf("marker reached the v1 validation parser: %v", err)
		}
		t.Fatal(err)
	}
	got, err := f.db.GetFollowup(t.Context(), q.Key)
	mustProgress(t, err)
	if !got.Reported || got.IssueID < 1 || forge.creates != 1 {
		t.Fatalf("approved follow-up not delivered past marker: %+v creates=%d", got, forge.creates)
	}
	pending, err := f.db.ListFollowupAssessments(t.Context())
	mustProgress(t, err)
	if len(pending) != 0 {
		t.Fatalf("follow-up assessments remain pending: %+v", pending)
	}
}

func TestFollowupLostProposalNoticeRecoversWithoutCreating(t *testing.T) {
	f, forge, a := followupFixture(t)
	forge.lose = "report"
	if err := f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent); err == nil {
		t.Fatal("lost proposal notice ignored")
	}
	q := firstFollowup(t, f)
	if q.Proposed || q.Diagnostic == "" || forge.creates != 0 {
		t.Fatalf("unconfirmed proposal progressed %+v", q)
	}
	before := len(forge.comments[testIssue])
	restartFollowups(t, f, forge)
	mustProgress(t, f.opts.StoryProgress.deliverFollowups(t.Context(), a.Parent))
	got, err := f.db.GetFollowup(t.Context(), q.Key)
	mustProgress(t, err)
	if !got.Proposed || got.ApprovalComment != 0 || forge.creates != 0 || len(forge.comments[testIssue]) != before {
		t.Fatalf("lost proposal replay duplicated or approved %+v", got)
	}
}
