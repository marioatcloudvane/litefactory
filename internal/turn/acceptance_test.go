package turn

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type acceptanceTestForge struct {
	*progressForge
	db    *store.DB
	lose  string
	order []string
}

func (f *acceptanceTestForge) PostComment(ctx context.Context, r github.Repo, n int, b redact.Body) (github.Comment, error) {
	decisions, err := f.db.ListPendingAcceptance(ctx)
	if err != nil || len(decisions) == 0 {
		return github.Comment{}, errors.New("report attempted before retained decision")
	}
	f.order = append(f.order, "report")
	c, err := f.progressForge.PostComment(ctx, r, n, b)
	if f.lose == "report" {
		f.lose = ""
		return github.Comment{}, errors.New("lost report response")
	}
	return c, err
}
func (f *acceptanceTestForge) UpdateIssue(_ context.Context, _ github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	f.order = append(f.order, "body")
	i := f.children[n]
	i.Title = title.String()
	i.Body = body.String()
	f.children[n] = i
	if f.lose == "body" {
		f.lose = ""
		return github.Issue{}, errors.New("lost body response")
	}
	return i, nil
}
func (f *acceptanceTestForge) SetStoryState(ctx context.Context, r github.Repo, pn int, pi int64, n int, id int64, closed bool) error {
	if closed && (!strings.Contains(f.children[n].Body, "[x]") || len(f.comments[n]) == 0) {
		return errors.New("premature closure")
	}
	f.order = append(f.order, "close")
	err := f.progressForge.SetStoryState(ctx, r, pn, pi, n, id, closed)
	if f.lose == "close" {
		f.lose = ""
		return errors.New("lost close response")
	}
	return err
}
func acceptanceFixture(t *testing.T, status string) (*fixture, *acceptanceTestForge, store.ValidationAssessment) {
	t.Helper()
	f := newFixture(t, nil)
	base, renderer := setupVersionedProgressScope(t, f, store.WorkflowSeparated)
	forge := &acceptanceTestForge{progressForge: base, db: f.db}
	p, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(t.TempDir(), "locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = p
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "session", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	s := revisions[0]
	v := agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: agent.ApprovedStory{Key: s.Key, IssueNumber: s.IssueNumber, IssueID: s.IssueID, ParentID: s.ParentID, Body: s.Body, Revision: s.Hash}, Criteria: []string{"AC-1"}, Snapshot: strings.Repeat("a", 64), EvidenceStatus: status, EvidenceReason: "original check record"}}}
	input, _ := json.Marshal(v)
	output, _ := json.Marshal(validationOutputFor(v))
	a := store.ValidationAssessment{RunID: run.ID, Parent: v.Parent, Generation: 1, ContextJSON: string(input), ResultJSON: string(output)}
	return f, forge, a
}
func retainAcceptanceFixture(t *testing.T, f *fixture, a store.ValidationAssessment) {
	t.Helper()
	mustProgress(t, f.db.RetainValidationAssessment(t.Context(), a))
	mustProgress(t, retainAcceptance(t.Context(), f.db, a))
}
func TestAcceptanceRecoversEachLostResponseWithoutDuplicateEffects(t *testing.T) {
	for _, lost := range []string{"", "report", "body", "close"} {
		name := lost
		if name == "" {
			name = "no response loss"
		}
		t.Run(name, func(t *testing.T) {
			f, forge, a := acceptanceFixture(t, "passed")
			forge.lose = lost
			retainAcceptanceFixture(t, f, a)
			err := f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent)
			if lost != "" && err == nil {
				t.Fatal("fault not reported")
			}
			if lost == "" {
				mustProgress(t, err)
			}
			// A newly constructed service must recover entirely from durable debt and remote facts.
			p, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 9, filepath.Join(t.TempDir(), "restart-locks"))
			mustProgress(t, err)
			mustProgress(t, p.deliverAcceptance(t.Context(), a.Parent))
			mustProgress(t, retainAcceptance(t.Context(), f.db, a))
			mustProgress(t, p.deliverAcceptance(t.Context(), a.Parent))
			if forge.children[200].State != "closed" || forge.children[201].State != "open" {
				t.Fatalf("independent closure wrong: %+v", forge.children)
			}
			if len(forge.comments[200]) != 1 || strings.Join(forge.order, ",") != "report,body,close" {
				t.Fatalf("duplicate or misordered effects: %v comments=%v", forge.order, forge.comments)
			}
			pending, err := f.db.ListPendingAcceptance(t.Context())
			mustProgress(t, err)
			if len(pending) != 0 {
				t.Fatalf("undelivered decisions %+v", pending)
			}
		})
	}
}
func TestAcceptancePreservesFailedAndMissingEvidence(t *testing.T) {
	for _, status := range []string{"unverified", "failed"} {
		t.Run(status, func(t *testing.T) {
			f, forge, a := acceptanceFixture(t, status)
			retainAcceptanceFixture(t, f, a)
			mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
			if forge.children[200].State != "open" || strings.Contains(forge.children[200].Body, "[x]") {
				t.Fatal("unproven acceptance fabricated")
			}
			want := "unverified"
			if status == "failed" {
				want = "unmet"
			}
			if len(forge.comments[200]) != 1 || !strings.Contains(forge.comments[200][0].Body, "AC-1: "+want) {
				t.Fatalf("missing criterion-specific factual explanation: %+v", forge.comments)
			}
		})
	}
}
func TestAcceptanceConflictPreservesLiveTextAndLeavesRecoverableDebt(t *testing.T) {
	f, forge, a := acceptanceFixture(t, "passed")
	retainAcceptanceFixture(t, f, a)
	child := forge.children[200]
	child.Body = strings.ReplaceAll(child.Body, "scoped result", "changed requirement")
	forge.children[200] = child
	if err := f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent); err == nil {
		t.Fatal("changed requirement accepted")
	}
	if forge.children[200].Body != child.Body || forge.children[200].State != "open" {
		t.Fatal("conflict overwritten or closed")
	}
	pending, err := f.db.ListPendingAcceptance(t.Context())
	mustProgress(t, err)
	if len(pending) != 1 || pending[0].Diagnostic == "" {
		t.Fatalf("conflict debt missing %+v", pending)
	}
	if len(forge.comments[200]) != 1 || !strings.Contains(forge.comments[200][0].Body, "Projection blocked") {
		t.Fatal("conflict not reported")
	}
	child.Body = pending[0].FrozenBody + "\n## Tasks\nOwner note preserved.\n"
	forge.children[200] = child
	mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
	if forge.children[200].State != "closed" || !strings.Contains(forge.children[200].Body, "Owner note preserved") {
		t.Fatal("repair failed")
	}
}
func TestAcceptanceExternallyClosedDoesNotCreateProof(t *testing.T) {
	f, forge, a := acceptanceFixture(t, "unverified")
	child := forge.children[200]
	child.State = "closed"
	forge.children[200] = child
	retainAcceptanceFixture(t, f, a)
	pending, err := f.db.ListPendingAcceptance(t.Context())
	mustProgress(t, err)
	if len(pending) != 1 || pending[0].Accepted {
		t.Fatal("remote closure fabricated acceptance")
	}
	mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
	if strings.Contains(forge.children[200].Body, "[x]") {
		t.Fatal("remote closure checked unverified criterion")
	}
}

func TestAcceptanceRetainsHistoricalSnapshotAfterLaterAssessment(t *testing.T) {
	f, forge, a := acceptanceFixture(t, "passed")
	retainAcceptanceFixture(t, f, a)
	old, err := f.db.ListPendingAcceptance(t.Context())
	mustProgress(t, err)
	if len(old) != 1 {
		t.Fatal(old)
	}
	original := old[0]
	mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "session", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "later.log" }})
	mustProgress(t, err)
	var scope agent.ValidationContext
	mustProgress(t, json.Unmarshal([]byte(a.ContextJSON), &scope))
	scope.Stories[0].Snapshot = strings.Repeat("c", 64)
	scope.Stories[0].EvidenceStatus = "unverified"
	scope.Stories[0].EvidenceReason = "later issue changed implementation"
	raw, _ := json.Marshal(scope)
	output, _ := json.Marshal(validationOutputFor(scope))
	later := a
	later.RunID = run.ID
	later.ContextJSON = string(raw)
	later.ResultJSON = string(output)
	retainAcceptanceFixture(t, f, later)
	mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
	pending, err := f.db.ListPendingAcceptance(t.Context())
	mustProgress(t, err)
	if len(pending) != 0 || forge.children[200].State != "closed" || len(forge.comments[200]) != 1 {
		t.Fatalf("historical acceptance expired: pending=%+v remote=%+v", pending, forge.children[200])
	}
	// Replaying the original immutable record still succeeds after later work.
	mustProgress(t, f.db.RetainAcceptanceDecision(t.Context(), original))
	if !strings.Contains(forge.comments[200][0].Body, original.Snapshot) || strings.Contains(forge.comments[200][0].Body, scope.Stories[0].Snapshot) {
		t.Fatal("historical proof attribution changed")
	}
}

func TestAcceptanceOwnerWaivedSeamChecksWithoutFabricatingPass(t *testing.T) {
	f, forge, a := acceptanceFixture(t, "unverified")
	mustProgress(t, f.db.RetainValidationAssessment(t.Context(), a))
	var scope agent.ValidationContext
	mustProgress(t, json.Unmarshal([]byte(a.ContextJSON), &scope))
	s := scope.Stories[0]
	findings := store.AcceptanceFindings{Criteria: []store.AcceptanceFinding{{Name: "AC-1", Status: "unverified", Classification: "blocking", Evidence: "No test execution available", WaiverReason: "Owner accepts missing proof"}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		findings.Reviews = append(findings.Reviews, store.AcceptanceFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: "retained review"})
	}
	data, _ := json.Marshal(findings)
	d := store.AcceptanceDecision{RunID: a.RunID, Parent: a.Parent, Generation: 1, Story: s.Story.Key, IssueNumber: s.Story.IssueNumber, IssueID: s.Story.IssueID, ParentID: s.Story.ParentID, Revision: s.Story.Revision, Snapshot: s.Snapshot, FrozenBody: s.Story.Body, FindingsJSON: string(data), Accepted: true}
	// This is the factory-authorized projection seam, not agent waiver admission.
	mustProgress(t, f.db.RetainAcceptanceDecision(t.Context(), d))
	mustProgress(t, f.db.MaterializeValidationAssessment(t.Context(), a.RunID))
	mustProgress(t, f.opts.StoryProgress.deliverAcceptance(t.Context(), a.Parent))
	if forge.children[200].State != "closed" || !strings.Contains(forge.children[200].Body, "[x]") {
		t.Fatal("authorized waived obligation not projected")
	}
	comment := forge.comments[200][0].Body
	for _, want := range []string{"AC-1: unverified", "No test execution available", "OWNER-WAIVED: Owner accepts missing proof", "original finding retained"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("waiver erased fact %q: %s", want, comment)
		}
	}
}

func TestAcceptanceEvidenceFloorReplayIsIdempotent(t *testing.T) {
	for _, status := range []string{"failed", "unverified"} {
		t.Run(status, func(t *testing.T) {
			_, _, a := acceptanceFixture(t, status)
			var scope agent.ValidationContext
			mustProgress(t, json.Unmarshal([]byte(a.ContextJSON), &scope))
			first, err := agent.ParseValidationOutput([]byte(a.ResultJSON), &scope)
			mustProgress(t, err)
			raw, err := json.Marshal(first)
			mustProgress(t, err)
			second, err := agent.ParseValidationOutput(raw, &scope)
			mustProgress(t, err)
			again, err := json.Marshal(second)
			mustProgress(t, err)
			if string(raw) != string(again) {
				t.Fatalf("retained normalized result changed on reparse:\n%s\n%s", raw, again)
			}
		})
	}
}

func TestAcceptanceRejectsIncompleteAndMisattributedAssessmentsBeforeEffects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*agent.ValidationOutput)
	}{
		{"missing proof", func(o *agent.ValidationOutput) { o.Stories[0].Criteria[0].Evidence = "" }},
		{"omitted criterion", func(o *agent.ValidationOutput) { o.Stories[0].Criteria = nil }},
		{"duplicate criterion", func(o *agent.ValidationOutput) {
			o.Stories[0].Criteria = append(o.Stories[0].Criteria, o.Stories[0].Criteria[0])
		}},
		{"wrong snapshot", func(o *agent.ValidationOutput) { o.Stories[0].Snapshot = strings.Repeat("d", 64) }},
		{"wrong revision", func(o *agent.ValidationOutput) { o.Stories[0].Revision = strings.Repeat("e", 64) }},
		{"wrong numeric identity", func(o *agent.ValidationOutput) { o.Stories[0].IssueID++ }},
		{"missing review", func(o *agent.ValidationOutput) { o.Stories[0].Reviews = o.Stories[0].Reviews[:2] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, forge, a := acceptanceFixture(t, "passed")
			var output agent.ValidationOutput
			mustProgress(t, json.Unmarshal([]byte(a.ResultJSON), &output))
			tc.change(&output)
			raw, _ := json.Marshal(output)
			a.ResultJSON = string(raw)
			mustProgress(t, f.db.RetainValidationAssessment(t.Context(), a))
			if err := retainAcceptance(t.Context(), f.db, a); err == nil {
				t.Fatal("invalid review authorized acceptance")
			}
			pending, err := f.db.ListPendingAcceptance(t.Context())
			mustProgress(t, err)
			if len(pending) != 0 || len(forge.order) != 0 || forge.children[200].State != "open" {
				t.Fatal("invalid assessment created effects")
			}
		})
	}
}
