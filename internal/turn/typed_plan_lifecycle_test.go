package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// typedPublicationOptions names the parent and content a seeded typed story
// was published under.
type typedPublicationOptions struct {
	DB         *store.DB
	Parent     naming.Ref
	TypedScope planbrief.ContentScope
}

// typedPublicationFixture seeds the state graph Plan leaves for one published
// typed story: the issue under its parent, the managed story row and the
// immutable typed projection of its exact bytes.
func typedPublicationFixture(t *testing.T) (*fixture, *preparation, typedPublicationOptions, *typedPlanForge) {
	t.Helper()
	f := newFixture(t, nil)
	p, prep, renderer, scope := typedPlanFixture(t)
	parent := prep.ref.String()
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: parent, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}))
	story := p.Stories[0]
	body, _, err := report.RenderTypedStoryParts(story, scope)
	mustProgress(t, err)
	rendered, err := renderer.RenderCompleteStory(t.Context(), report.Story{Ref: prep.ref, ID: story.Key, Title: story.Title, Excerpt: body})
	mustProgress(t, err)
	forge := &typedPlanForge{}
	issue, err := forge.CreateIssue(t.Context(), github.RepoFromRef(prep.ref), rendered.Title, rendered.Body)
	mustProgress(t, err)
	mustProgress(t, forge.AddSubIssue(t.Context(), github.RepoFromRef(prep.ref), prep.ref.Issue, issue.ID))
	revision := planbrief.StoryHash(issue.Title, issue.Body)
	mustProgress(t, f.db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: parent, Generation: 1, Key: story.Key, IssueNumber: issue.Number, IssueID: issue.ID, BodyHash: revision, Dependencies: []string{}, State: "planning"}))
	content, err := json.Marshal(story)
	mustProgress(t, err)
	criteria := make([]string, len(story.AcceptanceCriteria))
	for i, c := range story.AcceptanceCriteria {
		criteria[i] = c.ID
	}
	mustProgress(t, retainTypedPublicationProjection(t.Context(), f.db, store.SubmissionProjection{Parent: parent, Generation: 1, Story: story.Key, IssueID: issue.ID, Revision: revision, ContentVersion: 2, Title: issue.Title, Body: issue.Body, Content: content, Criteria: criteria}, story))
	return f, prep, typedPublicationOptions{DB: f.db, Parent: prep.ref, TypedScope: scope}, forge
}

func TestTypedPlanPublicationRetainsRequirementsAndRequiresReadiness(t *testing.T) {
	f, prep, o, forge := typedPublicationFixture(t)
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
		t.Fatal("approval succeeded before readiness")
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
	f, prep, o, forge := typedPublicationFixture(t)
	forge.issues[0].State = github.StateOpen
	feature, err := f.db.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	race := &typedApprovalRaceForge{typedPlanForge: forge}
	_, err = readScopeForApproval(t.Context(), Options{DB: f.db, SubIssueCreator: race}, prep.ref, feature)
	if err == nil || !strings.Contains(err.Error(), "changed before typed approval") {
		t.Fatalf("revision race accepted: %v", err)
	}
}

func TestTypedAcceptanceProjectionUsesFrozenIDsWithoutProseObligations(t *testing.T) {
	f, _, o, forge := typedPublicationFixture(t)
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

// A graph Plan story issue carries the typed layout, not the legacy prose
// sections. Its typed projection is what readiness checks, so approval accepts
// it; the legacy section check applies only to prose generations.
func TestTypedApprovalAcceptsGraphPlanStoryLayout(t *testing.T) {
	f, prep, o, forge := typedPublicationFixture(t)
	issue := forge.issues[0]
	published, err := f.db.GetSubmissionProjection(t.Context(), o.Parent.String(), 1, "US-001", planbrief.StoryHash(issue.Title, issue.Body))
	mustProgress(t, err)

	body := "# Userstory\n\nAs an owner I want typed stories.\n\n# Task\n\n- T-001\n\n# Acceptance Criteria\n\n- AC-001\n\n# Architecture\n\nKeep ownership.\n"
	if planbrief.ValidateStoryBody(body) == nil {
		t.Fatal("fixture body unexpectedly carries the legacy sections")
	}
	revision := planbrief.StoryHash(issue.Title, body)
	graph := published
	graph.Revision, graph.Body = revision, body
	mustProgress(t, f.db.PutSubmissionProjection(t.Context(), graph))
	forge.issues[0].Body, forge.issues[0].State = body, github.StateOpen
	stories, err := f.db.ListManagedStories(t.Context(), o.Parent.String(), 1)
	mustProgress(t, err)
	story := stories[0]
	story.BodyHash = revision
	mustProgress(t, f.db.PutManagedStory(t.Context(), story))

	feature, err := f.db.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	revisions, err := readScopeForApproval(t.Context(), Options{DB: f.db, SubIssueCreator: forge}, prep.ref, feature)
	if err != nil {
		t.Fatalf("a typed graph Plan story was refused: %v", err)
	}
	if len(revisions) != 1 || revisions[0].Body != body || revisions[0].Hash != revision {
		t.Fatalf("wrong frozen revision: %+v", revisions)
	}
}
