package turn

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func reconciliationPublicationFixture(t *testing.T) (*preparation, PublicationOptions, *typedPlanForge, planbrief.TypedPlan) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "reconciliation.db"))
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, db.Close()) })
	mustProgress(t, db.Migrate(t.Context()))
	p, prep, renderer, scope := typedPlanFixture(t)
	prep.promptInput.PlanGeneration = 1
	mustProgress(t, db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: prep.ref.String(), Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	mustProgress(t, db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: prep.ref.String(), Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	proposal, content, err := buildTypedPlanProposal(t.Context(), renderer, prep, scope, nil, nil, typedPlanBytes(t, p))
	mustProgress(t, err)
	forge := &typedPlanForge{}
	o := PublicationOptions{DB: db, Forge: forge, Renderer: renderer, Parent: prep.ref, Generation: 1, BotID: 9, TypedContent: content, TypedScope: scope}
	_, err = PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	forge.issues[0].State = github.StateOpen
	return prep, o, forge, p
}

func reconciliationBuildUpdate(t *testing.T, prep *preparation, o PublicationOptions, forge *typedPlanForge, p planbrief.TypedPlan) (planbrief.PlanProposal, map[string]planbrief.TypedStory) {
	t.Helper()
	stories, err := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 1)
	mustProgress(t, err)
	normalized, err := normalizeTypedUpdates(t.Context(), o.DB, o.Parent.String(), 1, stories, typedPlanBytes(t, p), o.TypedScope, nil)
	mustProgress(t, err)
	issue := forge.issues[0]
	base := planbrief.StoryProposal{Key: "US-001", IssueID: issue.ID, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, ExpectedBase: planbrief.StoryHash(issue.Title, issue.Body)}
	proposal, content, err := buildTypedPlanProposal(t.Context(), o.Renderer, prep, o.TypedScope, map[string]planbrief.StoryProposal{"US-001": base}, stories, normalized)
	mustProgress(t, err)
	return proposal, content
}

func TestStoryReconciliationTypedUpdatePreservesProseAndRetiresIDs(t *testing.T) {
	t.Parallel()
	prep, o, forge, p := reconciliationPublicationFixture(t)
	original := forge.issues[0]
	const explanation = "Evidence contract: this paragraph explains evidence only."
	const ownerText = "## Owner context\n\nFirst paragraph.\n\nSecond paragraph.\n"
	forge.issues[0].Body = strings.Replace(original.Body, "\n## Tasks", "\n"+explanation+"\n\n## Tasks", 1) + "\n" + ownerText
	p.Stories[0].Intent = "update"
	p.Stories[0].AcceptanceCriteria[0].Text = "String must be y"
	proposal, content := reconciliationBuildUpdate(t, prep, o, forge, p)
	if content["US-001"].AcceptanceCriteria[0].ID != "AC-002" || content["US-001"].Tasks[0].CriterionIDs[0] != "AC-002" {
		t.Fatalf("replacement identity/ref mapping: %+v", content["US-001"])
	}
	history, err := o.DB.ListCriterionHistory(t.Context(), o.Parent.String(), "US-001")
	mustProgress(t, err)
	if len(history) != 1 || history[0].RetiredRevision != "" || forge.updates != 0 {
		t.Fatal("preflight mutated history or issue")
	}
	o.TypedContent = content
	_, err = PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	if !strings.Contains(forge.issues[0].Body, explanation) || !strings.Contains(forge.issues[0].Body, ownerText) {
		t.Fatalf("owner text lost: %s", forge.issues[0].Body)
	}
	if !strings.HasSuffix(strings.TrimSpace(forge.issues[0].Body), planbrief.StoryGuidance) {
		t.Fatal("owner text displaced bottom guidance")
	}
	history, err = o.DB.ListCriterionHistory(t.Context(), o.Parent.String(), "US-001")
	mustProgress(t, err)
	if len(history) != 2 || history[0].ID != "AC-001" || history[0].RetiredRevision == "" || history[1].ID != "AC-002" {
		t.Fatalf("retirement missing: %+v", history)
	}
	old, err := o.DB.FindSubmissionProjection(t.Context(), o.Parent.String(), "US-001", planbrief.StoryHash(original.Title, original.Body), 1)
	mustProgress(t, err)
	if old.Body != original.Body {
		t.Fatal("historical source overwritten")
	}
	feature, err := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	if feature.Ready || feature.State != "planning" || feature.WorkflowVersion != store.WorkflowSeparated {
		t.Fatalf("semantic update advanced approval: %+v", feature)
	}
}

func TestStoryReconciliationClarificationRequiresAdmittedExactBinding(t *testing.T) {
	for _, testCase := range []string{"matching", "unadmitted", "wrong comment", "wrong child", "wrong generation", "wrong text", "consumed", "invalid actor"} {
		t.Run(testCase, func(t *testing.T) {
			prep, o, forge, p := reconciliationPublicationFixture(t)
			repo, err := o.DB.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "worktrees")})
			mustProgress(t, err)
			prep.session, err = o.DB.GetOrCreateSession(t.Context(), repo, 1)
			mustProgress(t, err)
			prep.run.TriggerCommentID = 900
			const clarified = "The string's value must be x"
			r := store.StoryRefinement{SessionID: prep.session.ID, CommentID: 900, ChildNumber: forge.issues[0].Number, ChildID: forge.issues[0].ID, Generation: 1, ActorID: 42, Body: "Clarify AC-001: " + clarified}
			switch testCase {
			case "wrong comment":
				prep.run.TriggerCommentID = 901
			case "wrong child":
				r.ChildID++
			case "wrong generation":
				r.Generation = 2
			case "invalid actor":
				r.ActorID = 0
			}
			if testCase != "unadmitted" {
				err = o.DB.AdmitStoryRefinement(t.Context(), r)
				if testCase == "invalid actor" {
					if err == nil {
						t.Fatal("unauthenticated actor admitted")
					}
				} else {
					mustProgress(t, err)
				}
			}
			if testCase == "consumed" {
				mustProgress(t, o.DB.ConsumeStoryRefinement(t.Context(), r.SessionID, r.CommentID))
			}
			stories, err := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 1)
			mustProgress(t, err)
			clarifications, err := readEstablishedClarifications(t.Context(), o.DB, prep, stories)
			mustProgress(t, err)
			p.Stories[0].Intent = "update"
			p.Stories[0].AcceptanceCriteria[0].Text = clarified
			if testCase == "wrong text" {
				p.Stories[0].AcceptanceCriteria[0].Text = "String must be y"
			}
			normalized, err := normalizeTypedUpdates(t.Context(), o.DB, o.Parent.String(), 1, stories, typedPlanBytes(t, p), o.TypedScope, clarifications)
			mustProgress(t, err)
			result, err := planbrief.ParseTypedPlan(normalized, o.TypedScope)
			mustProgress(t, err)
			want := "AC-002"
			if testCase == "matching" {
				want = "AC-001"
			}
			if result.Stories[0].AcceptanceCriteria[0].ID != want {
				t.Fatalf("got ID %s, want %s", result.Stories[0].AcceptanceCriteria[0].ID, want)
			}
			if forge.updates != 0 {
				t.Fatal("clarification preflight changed issue")
			}
		})
	}
}

func TestStoryReconciliationPresentationApprovalRechecksCurrentBytes(t *testing.T) {
	for _, testCase := range []string{"presentation", "semantic", "race", "locked"} {
		t.Run(testCase, func(t *testing.T) {
			prep, o, forge, _ := reconciliationPublicationFixture(t)
			original := forge.issues[0]
			forge.issues[0].Body = strings.Replace(original.Body, "String must be x", "String must be\n  x", 1)
			if testCase == "semantic" {
				forge.issues[0].Body = strings.Replace(original.Body, "String must be x", "String must be y", 1)
			}
			feature, err := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
			mustProgress(t, err)
			if testCase == "locked" {
				feature.State = "implementing"
				mustProgress(t, o.DB.CompareManagedFeature(t.Context(), feature, "planning"))
				stories, err := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 1)
				mustProgress(t, err)
				_, err = reconcilePresentationIssue(t.Context(), o.DB, forge, prep.ref, stories[0], forge.issues[0])
				if err == nil || !strings.Contains(err.Error(), "frozen") {
					t.Fatalf("active scope changed: %v", err)
				}
				return
			}
			var source SubIssueCreator = forge
			if testCase == "race" {
				source = &typedApprovalRaceForge{typedPlanForge: forge}
			}
			revisions, err := readScopeForApproval(t.Context(), Options{DB: o.DB, SubIssueCreator: source}, prep.ref, feature)
			if testCase != "presentation" {
				if err == nil {
					t.Fatalf("%s edit acquired approval", testCase)
				}
				if testCase == "semantic" && !strings.Contains(err.Error(), "Plan") {
					t.Fatalf("missing ordinary-language pending explanation: %v", err)
				}
				return
			}
			mustProgress(t, err)
			if len(revisions) != 1 || revisions[0].Body != forge.issues[0].Body {
				t.Fatalf("approval did not freeze current bytes: %+v", revisions)
			}
			mustProgress(t, o.DB.SetManagedFeatureReady(t.Context(), o.Parent.String(), 1, true))
			mustProgress(t, o.DB.ApproveManagedStories(t.Context(), o.Parent.String(), 1, revisions))
			frozen, err := o.DB.ListApprovedStoryRevisions(t.Context(), o.Parent.String(), 1)
			mustProgress(t, err)
			if len(frozen) != 1 || frozen[0].Body != forge.issues[0].Body || forge.updates != 0 {
				t.Fatal("presentation approval rewrote or lost source")
			}
			old, err := o.DB.FindSubmissionProjection(t.Context(), o.Parent.String(), "US-001", planbrief.StoryHash(original.Title, original.Body), 1)
			mustProgress(t, err)
			if old.Body != original.Body {
				t.Fatal("old projection lost")
			}
		})
	}
}

type reconciliationConflictForge struct {
	*typedPlanForge
	conflict bool
	desired  github.Issue
}

func (f *reconciliationConflictForge) UpdateIssue(ctx context.Context, repo github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	issue, err := f.typedPlanForge.UpdateIssue(ctx, repo, n, title, body)
	if err != nil {
		return issue, err
	}
	f.desired = issue
	if f.conflict {
		f.issues[0].Body += "\nConcurrent owner edit.\n"
	}
	return issue, nil
}

func TestStoryReconciliationPostWriteConflictRetainsRecoverableIntent(t *testing.T) {
	t.Parallel()
	prep, o, forge, p := reconciliationPublicationFixture(t)
	p.Stories[0].Intent = "update"
	p.Stories[0].Architecture.Summary = "New component design"
	proposal, content := reconciliationBuildUpdate(t, prep, o, forge, p)
	o.TypedContent = content
	racer := &reconciliationConflictForge{typedPlanForge: forge, conflict: true}
	o.Forge = racer
	_, err := PublishPlan(t.Context(), o, proposal)
	if err == nil || !strings.Contains(err.Error(), "post-write conflict") {
		t.Fatalf("conflict claimed successful: %v", err)
	}
	key := revisionOperationKey(o, proposal.Stories[0], planbrief.StoryHash(racer.desired.Title, racer.desired.Body))
	op, err := o.DB.GetStoryRevisionOperation(t.Context(), key)
	mustProgress(t, err)
	if op.Delivered {
		t.Fatal("conflicted intent marked delivered")
	}
	_, err = PublishPlan(t.Context(), o, proposal)
	if err == nil || forge.updates != 1 || !strings.Contains(forge.issues[0].Body, "Concurrent owner edit.") {
		t.Fatalf("retry erased competing edit: %v", err)
	}
	// A later observed resolution to the exact desired bytes can settle the same
	// durable operation without another write or another issue creation.
	forge.issues[0] = racer.desired
	racer.conflict = false
	_, err = PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	op, err = o.DB.GetStoryRevisionOperation(t.Context(), key)
	mustProgress(t, err)
	if !op.Delivered || forge.updates != 1 || forge.creates != 1 {
		t.Fatalf("replay duplicated effects: %+v", op)
	}
}

func TestStoryReconciliationRetiredIDCannotReturnInLaterPlan(t *testing.T) {
	t.Parallel()
	prep, o, forge, p := reconciliationPublicationFixture(t)
	p.Stories[0].Intent = "update"
	p.Stories[0].AcceptanceCriteria[0].Text = "String must be y"
	proposal, content := reconciliationBuildUpdate(t, prep, o, forge, p)
	o.TypedContent = content
	_, err := PublishPlan(t.Context(), o, proposal)
	mustProgress(t, err)
	before, err := o.DB.ListCriterionHistory(t.Context(), o.Parent.String(), "US-001")
	mustProgress(t, err)
	stories, err := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 1)
	mustProgress(t, err)
	_, err = normalizeTypedUpdates(t.Context(), o.DB, o.Parent.String(), 1, stories, typedPlanBytes(t, p), o.TypedScope, nil)
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("retired ID returned: %v", err)
	}
	after, err := o.DB.ListCriterionHistory(t.Context(), o.Parent.String(), "US-001")
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected preflight changed history")
	}
	if _, err := o.DB.GetSubmissionProjection(t.Context(), o.Parent.String(), 2, "US-001", stories[0].BodyHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("new generation invented: %v", err)
	}
}
