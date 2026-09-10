package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func typedPublicationFixture(t *testing.T) (*fixture, *preparation, PublicationOptions, planbrief.PlanProposal, *typedPlanForge) {
	t.Helper()
	f := newFixture(t, nil)
	p, prep, renderer, scope := typedPlanFixture(t)
	prep.promptInput.PlanGeneration = 1
	parent := prep.ref.String()
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: parent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	proposal, content, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, typedPlanBytes(t, p))
	mustProgress(t, err)
	forge := &typedPlanForge{}
	return f, prep, PublicationOptions{DB: f.db, Forge: forge, Renderer: renderer, Parent: prep.ref, Generation: 1, BotID: 9, TypedContent: content, TypedScope: scope}, proposal, forge
}

func TestTypedPlanPublicationRetainsRequirementsAndRequiresSavedIndex(t *testing.T) {
	f, prep, o, proposal, forge := typedPublicationFixture(t)
	index, err := PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	if !strings.Contains(string(index), "issues/2") || strings.Contains(string(index), "String must be x") {
		t.Fatalf("not link-only index: %s", index)
	}
	issue := forge.issues[0]
	revision := planbrief.StoryHash(issue.Title, issue.Body)
	ids, err := storyCriterionIDs(t.Context(), f.db, o.Parent.String(), 1, "US-001", revision, issue.Body)
	mustProgress(t, err)
	if !reflect.DeepEqual(ids, []string{"AC-001"}) {
		t.Fatalf("invented obligation: %v", ids)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	if feature.Ready {
		t.Fatal("publication alone restored readiness")
	}
	if err := f.db.ApproveManagedStories(t.Context(), o.Parent.String(), 1, nil); err == nil {
		t.Fatal("unsaved index allowed approval")
	}
	// Approval observes the exact current bytes and the immutable typed projection.
	forge.issues[0].State = github.StateOpen
	revisions, err := readScopeForApproval(t.Context(), Options{DB: f.db, SubIssueCreator: forge}, prep.ref, feature)
	mustProgress(t, err)
	if len(revisions) != 1 || revisions[0].Hash != revision {
		t.Fatalf("wrong frozen revision: %+v", revisions)
	}
	mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), o.Parent.String(), 1, true))
	mustProgress(t, f.db.ApproveManagedStories(t.Context(), o.Parent.String(), 1, revisions))
	frozen, err := f.db.ListApprovedStoryRevisions(t.Context(), o.Parent.String(), 1)
	mustProgress(t, err)
	if len(frozen) != 1 || frozen[0].Body != issue.Body || frozen[0].Hash != revision {
		t.Fatal("approval lost exact issue bytes")
	}
}

func TestTypedPlanUnchangedCompletedCheckboxesAndProjectionReplay(t *testing.T) {
	f, prep, o, proposal, forge := typedPublicationFixture(t)
	_, err := PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	original := forge.issues[0]
	p, _, _, _ := typedPlanFixture(t)
	p.Stories[0].Intent = "unchanged"
	for _, checked := range []bool{false, true} {
		t.Run(map[bool]string{false: "same revision", true: "checked dependency"}[checked], func(t *testing.T) {
			issue := original
			if checked {
				issue.Body = strings.Replace(issue.Body, "- [ ] AC-001:", "- [x] AC-001:", 1)
			}
			forge.issues[0] = issue
			base := planbrief.StoryProposal{Key: "US-001", IssueID: issue.ID, IssueNumber: issue.Number, ExpectedBase: planbrief.StoryHash(issue.Title, issue.Body), Title: issue.Title, Body: issue.Body}
			bound, content, err := buildTypedPlanProposal(t.Context(), o.Renderer, prep, o.TypedScope, map[string]planbrief.StoryProposal{"US-001": base}, []store.ManagedStory{{Key: "US-001", State: "completed"}}, typedPlanBytes(t, p))
			mustProgress(t, err)
			o.TypedContent = content
			_, err = PublishPlan(t.Context(), o, bound)
			mustProgress(t, err)
			if forge.issues[0].Body != issue.Body || forge.updates != 0 || forge.creates != 1 {
				t.Fatal("unchanged dependency was rewritten")
			}
			projection, err := f.db.GetSubmissionProjection(t.Context(), o.Parent.String(), 1, "US-001", base.ExpectedBase)
			mustProgress(t, err)
			if projection.Body != issue.Body {
				t.Fatal("projection lost source")
			}
		})
	}
}

func TestTypedPlanStaleBaseRefusesBeforeWrite(t *testing.T) {
	_, _, o, p, forge := typedPublicationFixture(t)
	_, err := PublishPlan(t.Context(), o, p)
	mustProgress(t, err)
	issue := forge.issues[0]
	p.Stories[0].Intent = "update"
	p.Stories[0].IssueID = issue.ID
	p.Stories[0].IssueNumber = issue.Number
	p.Stories[0].ExpectedBase = planbrief.StoryHash(issue.Title, issue.Body)
	typed := o.TypedContent["US-001"]
	typed.Intent = "update"
	o.TypedContent["US-001"] = typed
	forge.issues[0].Body += "\nOwner changed source.\n"
	_, err = PublishPlan(t.Context(), o, p)
	if err == nil || forge.updates != 0 || !strings.Contains(forge.issues[0].Body, "Owner changed source") {
		t.Fatalf("stale source overwritten: %v", err)
	}
}

type typedApprovalRaceForge struct {
	*typedPlanForge
	reads int
}

func (f *typedApprovalRaceForge) GetIssue(ctx context.Context, repo github.Repo, n int) (github.Issue, error) {
	issue, err := f.typedPlanForge.GetIssue(ctx, repo, n)
	if n == 2 {
		f.reads++
		if f.reads == 2 {
			issue.Body += "\nConcurrent edit"
		}
	}
	return issue, err
}
func TestTypedApprovalRechecksIssueRevision(t *testing.T) {
	f, prep, o, p, forge := typedPublicationFixture(t)
	_, err := PublishPlan(t.Context(), o, p)
	mustProgress(t, err)
	forge.issues[0].State = github.StateOpen
	feature, err := f.db.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	race := &typedApprovalRaceForge{typedPlanForge: forge}
	_, err = readScopeForApproval(t.Context(), Options{DB: f.db, SubIssueCreator: race}, prep.ref, feature)
	if err == nil || !strings.Contains(err.Error(), "changed before typed approval") {
		t.Fatalf("revision race accepted: %v", err)
	}
}

func TestTypedPlanReadinessRequiresTerminalSaveAndCurrentReceipt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		complete bool
		outcome  agent.Outcome
		saveErr  error
	}{
		{"incomplete", false, agent.OutcomeSucceeded, nil},
		{"process failure", true, agent.OutcomeFailed, nil},
		{"save failure", true, agent.OutcomeSucceeded, errors.New("push failed")},
		{"stale receipt", true, agent.OutcomeSucceeded, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, prep, o, _, _ := typedPublicationFixture(t)
			prep.planCompleted = tc.complete
			prep.typedPlan = &TypedPlanSubmission{receiptID: 99999}
			run := harness.RunResult{Outcome: tc.outcome}
			result := markManagedPlanReady(t.Context(), Options{DB: f.db}, prep, Result{Err: tc.saveErr}, &run)
			feature, err := f.db.GetManagedFeature(t.Context(), o.Parent.String())
			mustProgress(t, err)
			if feature.Ready || strings.Contains(run.ResultText, "Plan is ready") {
				t.Fatal("failed or stale save restored readiness")
			}
			if tc.name == "stale receipt" && result.Err == nil {
				t.Fatal("stale receipt reported success")
			}
		})
	}
}

func TestTypedAcceptanceProjectionUsesFrozenIDsWithoutProseObligations(t *testing.T) {
	f, _, o, p, forge := typedPublicationFixture(t)
	_, err := PublishPlan(t.Context(), o, p)
	mustProgress(t, err)
	issue := forge.issues[0]
	decision := store.AcceptanceDecision{Parent: o.Parent.String(), Generation: 1, Story: "US-001", Revision: planbrief.StoryHash(issue.Title, issue.Body), FrozenBody: issue.Body}
	live := strings.Replace(issue.Body, "## Tasks", "Evidence contract: narrative only\n\n## Tasks", 1)
	got, err := projectStoryAcceptance(t.Context(), f.db, decision, live, map[string]bool{"AC-001": true})
	mustProgress(t, err)
	if !strings.Contains(got, "- [x] AC-001:") || !strings.Contains(got, "Evidence contract: narrative only") {
		t.Fatal("typed projection lost declared criterion or narrative")
	}
	if !strings.HasSuffix(got, planbrief.StoryGuidance+"\n") || !strings.Contains(got, report.StoryMarker(o.Parent, "US-001")) {
		t.Fatal("projection lost factory guidance")
	}
}

func TestTypedPlanRunnerPublicationAndDurableReceipt(t *testing.T) {
	var f *fixture
	var recorded store.SubmissionReceipt
	runner := newStubRunner(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		var access struct{ Executable, Descriptor string }
		const prefix = "Factory submission access (paths are data): "
		for _, line := range strings.Split(req.Prompt, "\n") {
			if strings.HasPrefix(line, prefix) {
				mustProgress(t, json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &access))
			}
		}
		if access.Descriptor == "" {
			t.Fatal("typed Plan helper unavailable")
		}
		p, _, _, _ := typedPlanFixture(t)
		c := agent.Contribution{Stage: "plan", Outcome: "completed", Plan: typedPlanBytes(t, p)}
		data, err := json.Marshal(c)
		mustProgress(t, err)
		response, err := submission.Call(ctx, access.Descriptor, "submit", data)
		mustProgress(t, err)
		if response.Error != "" {
			t.Fatal(response.Error)
		}
		mustProgress(t, json.Unmarshal(response.Result, &recorded))
		if recorded.InputState != "valid" || recorded.PlanState != "pending" {
			t.Fatalf("premature publication: %+v", recorded)
		}
		feature, err := f.db.GetManagedFeature(ctx, req.Ref.String())
		mustProgress(t, err)
		if feature.Ready {
			t.Fatal("live input restored readiness")
		}
		if feature.WorkflowVersion != store.WorkflowSeparated {
			t.Fatalf("new typed Plan lacks persisted separated workflow: %d", feature.WorkflowVersion)
		}
		indexPath := filepath.Join(req.WorktreePath, filepath.Dir(req.PlanContextPath), "plan_brief.md")
		mustProgress(t, os.MkdirAll(filepath.Dir(indexPath), 0700))
		mustProgress(t, os.WriteFile(indexPath, []byte("# Plan\n\n## Story Index\n"), 0600))
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	home, err := os.MkdirTemp("/tmp", "lf207-plan-")
	mustProgress(t, err)
	home, err = filepath.EvalSymlinks(home)
	mustProgress(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	f.opts.Paths = config.NewPaths(home)
	forge := &lifecycleForge{progressForge: progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	f.opts.SubIssueCreator, f.opts.SubIssueRenderer = forge, renderer
	_, err = f.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9})
	mustProgress(t, err)
	result := f.execute(planRequest(f))
	if !result.Succeeded() || !result.Committed {
		t.Fatalf("typed Plan did not publish/save: %+v logs=%s", result, f.logs)
	}
	receipt, err := f.db.GetSubmissionReceipt(t.Context(), recorded.RunID, recorded.RequestID)
	mustProgress(t, err)
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if !feature.Ready || receipt.PlanState != "published" || receipt.EffectState != "delivered" || len(forge.children) != 1 {
		t.Fatalf("successful save not reflected: feature=%+v receipt=%+v", feature, receipt)
	}
}
