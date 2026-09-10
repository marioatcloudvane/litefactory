package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type upgradeForge struct {
	typedPlanForge
	failNumber int
	lostReply  bool
	conflict   bool
	writes     map[int]int
}

func (f *upgradeForge) UpdateIssue(_ context.Context, _ github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	if f.failNumber == n {
		f.failNumber = 0
		return github.Issue{}, errors.New("injected publication failure")
	}
	for i := range f.issues {
		if f.issues[i].Number == n {
			f.writes[n]++
			f.updates++
			f.issues[i].Title = title.String()
			f.issues[i].Body = body.String()
			if f.conflict {
				f.conflict = false
				f.issues[i].Body += "\nCompeting owner edit.\n"
			}
			if f.lostReply {
				f.lostReply = false
				return github.Issue{}, errors.New("injected lost reply")
			}
			return f.issues[i], nil
		}
	}
	return github.Issue{}, errors.New("missing issue")
}
func upgradeBoundaryFixture(t *testing.T) (StoryContractUpgradeOptions, *upgradeForge, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := store.Open(t.Context(), path)
	mustProgress(t, err)
	mustProgress(t, db.Migrate(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	_, prep, renderer, scope := typedPlanFixture(t)
	o := StoryContractUpgradeOptions{DB: db, Parent: prep.ref, Renderer: renderer, Scope: scope, OwnerID: 42, LockDir: t.TempDir()}
	f := &upgradeForge{writes: map[int]int{}}
	f.attached = true
	o.Forge = f
	mustProgress(t, db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: o.Parent.String(), Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	mustProgress(t, db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: o.Parent.String(), Generation: 1, ContentVersion: 1, SubmissionVersion: 1}))
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("US-%03d", i+1)
		deps := []string{}
		depText := "None."
		state := "planning"
		if i == 0 {
			deps = []string{"US-003"}
			depText = "US-003"
		}
		if i == 2 {
			state = "completed"
		}
		body := report.StoryMarker(o.Parent, key) + `\n\n## User Story

As an owner, I want explicit upgrades, so that history stays intact.

## Acceptance Criteria

- [ ] AC-001: String must be x.

## Tasks

| Task ID | Description | Agent | Size | Depends On | Status |
|---|---|---|---|---|---|
| T-001 | Implement requirement | go-implementation-engineer | M | — | TODO |

## Architecture

Keep the contract pure.

## Dependencies

` + depText + "\n\n## Owner context\n\nPreserve my explanation.\n"
		body = strings.Replace(body, `\n\n`, "\n\n", 1)
		issue := github.Issue{ID: int64(200 + i), Number: 2 + i, Title: key, Body: body, State: github.StateOpen, Author: github.User{ID: 9}}
		if i == 2 {
			issue.State = "closed"
			issue.Body = strings.Replace(issue.Body, "AC-001: String must be x.", "Historical prose criterion.", 1)
		}
		f.issues = append(f.issues, issue)
		mustProgress(t, db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: o.Parent.String(), Generation: 1, Key: key, IssueID: issue.ID, IssueNumber: issue.Number, BodyHash: planbrief.StoryHash(issue.Title, issue.Body), Dependencies: deps, State: state}))
	}
	mustProgress(t, db.SetManagedFeatureReady(t.Context(), o.Parent.String(), 1, true))
	return o, f, path
}
func TestStoryContractUpgradePreviewAndExplicitApply(t *testing.T) {
	t.Parallel()
	o, f, _ := upgradeBoundaryFixture(t)
	before, err := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	oldIssues := append([]github.Issue(nil), f.issues...)
	p, err := PreviewStoryContractUpgrade(t.Context(), o)
	mustProgress(t, err)
	if len(p.Conflicts) != 0 || len(p.Items) != 2 {
		t.Fatalf("preview: %+v", p)
	}
	after, err := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(oldIssues, f.issues) || f.updates != 0 {
		t.Fatal("preview mutated state")
	}
	if _, err := o.DB.GetContractUpgrade(t.Context(), o.Parent.String()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preview intent: %v", err)
	}
	for i, item := range p.Items {
		if item.ObservedRevision != planbrief.StoryHash(oldIssues[i].Title, oldIssues[i].Body) {
			t.Fatal("observed revision differs")
		}
	}
	mustProgress(t, ApplyStoryContractUpgrade(t.Context(), o, p))
	after, err = o.DB.GetManagedFeature(t.Context(), o.Parent.String())
	mustProgress(t, err)
	if after.Generation != 2 || after.Ready || after.State != "planning" || after.WorkflowVersion != before.WorkflowVersion {
		t.Fatalf("approval/workflow: %+v", after)
	}
	if f.creates != 0 || f.writes[2] != 1 || f.writes[3] != 1 || f.writes[4] != 0 || !reflect.DeepEqual(oldIssues[2], f.issues[2]) {
		t.Fatalf("cohort writes: %+v", f.writes)
	}
	for _, i := range f.issues[:2] {
		if !strings.Contains(i.Body, "Preserve my explanation.") || !strings.HasSuffix(strings.TrimSpace(i.Body), planbrief.StoryGuidance) {
			t.Fatal("owner text or footer lost")
		}
	}
	for generation, want := range map[int]int{1: 1, 2: 2} {
		c, e := o.DB.GetGenerationContract(t.Context(), o.Parent.String(), generation)
		mustProgress(t, e)
		if c.ContentVersion != want || c.SubmissionVersion != want {
			t.Fatalf("contract %+v", c)
		}
	}
	mustProgress(t, ApplyStoryContractUpgrade(t.Context(), o, p))
	if f.updates != 2 {
		t.Fatal("completed replay published again")
	}
}
func TestStoryContractUpgradeRefusalIsReadOnly(t *testing.T) {
	for _, kind := range []string{"ambiguous", "frozen", "stale", "owner"} {
		t.Run(kind, func(t *testing.T) {
			o, f, _ := upgradeBoundaryFixture(t)
			if kind == "ambiguous" {
				f.issues[0].Body = strings.Replace(f.issues[0].Body, "## Tasks", "## Missing tasks", 1)
			}
			if kind == "frozen" {
				mustProgress(t, o.DB.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: o.Parent.String(), Generation: 1, State: "freezing"}, "planning"))
			}
			p, e := PreviewStoryContractUpgrade(t.Context(), o)
			mustProgress(t, e)
			if kind == "stale" {
				f.issues[1].Body += "\nAn intervening owner edit."
			}
			if kind == "owner" {
				o.OwnerID++
			}
			if e = ApplyStoryContractUpgrade(t.Context(), o, p); e == nil {
				t.Fatal("unsafe apply succeeded")
			}
			if f.updates != 0 || f.creates != 0 {
				t.Fatal("refusal wrote issues")
			}
			if _, e = o.DB.GetContractUpgrade(t.Context(), o.Parent.String()); !errors.Is(e, store.ErrNotFound) {
				t.Fatalf("refusal retained intent: %v", e)
			}
			if kind == "ambiguous" && (len(p.Items) != 2 || p.Items[0].ObservedRevision == "" || len(p.Conflicts) == 0) {
				t.Fatalf("incomplete conflict preview: %+v", p)
			}
		})
	}
}
func TestStoryContractUpgradeRestartResumesOriginalCohort(t *testing.T) {
	for _, kind := range []string{"partial", "lost_reply", "confirmation_conflict"} {
		t.Run(kind, func(t *testing.T) {
			o, f, path := upgradeBoundaryFixture(t)
			p, e := PreviewStoryContractUpgrade(t.Context(), o)
			mustProgress(t, e)
			switch kind {
			case "partial":
				f.failNumber = 3
			case "lost_reply":
				f.lostReply = true
			case "confirmation_conflict":
				f.conflict = true
			}
			if e = ApplyStoryContractUpgrade(t.Context(), o, p); e == nil {
				t.Fatal("injected failure succeeded")
			}
			retained, e := o.DB.GetContractUpgrade(t.Context(), o.Parent.String())
			mustProgress(t, e)
			if retained.State != "pending" || retained.ScopeHash != p.Intent.ScopeHash {
				t.Fatalf("lost intent: %+v", retained)
			}
			feature, e := o.DB.GetManagedFeature(t.Context(), o.Parent.String())
			mustProgress(t, e)
			if feature.Generation != 1 || feature.Ready {
				t.Fatal("partial publication advanced readiness")
			}
			mustProgress(t, o.DB.Close())
			o.DB, e = store.Open(t.Context(), path)
			mustProgress(t, e)
			defer func() { _ = o.DB.Close() }()
			retry, e := PreviewStoryContractUpgrade(t.Context(), o)
			mustProgress(t, e)
			if retry.Intent.ScopeHash != p.Intent.ScopeHash || retry.Intent.SourceGeneration != 1 {
				t.Fatal("restart changed cohort")
			}
			if kind == "confirmation_conflict" {
				if len(retry.Conflicts) == 0 {
					t.Fatal("competing edit not reported")
				}
				if e = ApplyStoryContractUpgrade(t.Context(), o, retry); e == nil {
					t.Fatal("competing edit overwritten")
				}
				if f.writes[2] != 1 {
					t.Fatal("conflict caused another write")
				}
				f.issues[0].Body = p.Intent.Items[0].Body
				retry, e = PreviewStoryContractUpgrade(t.Context(), o)
				mustProgress(t, e)
			}
			mustProgress(t, ApplyStoryContractUpgrade(t.Context(), o, retry))
			if f.writes[2] != 1 || f.writes[3] != 1 || f.writes[4] != 0 || f.creates != 0 {
				t.Fatalf("replay duplicated or widened scope: %+v", f.writes)
			}
		})
	}
}

func TestStoryContractUpgradeNextTypedPlanRetainsCompletedLegacyDependency(t *testing.T) {
	t.Parallel()
	o, f, _ := upgradeBoundaryFixture(t)
	old := f.issues[2]
	p, e := PreviewStoryContractUpgrade(t.Context(), o)
	mustProgress(t, e)
	mustProgress(t, ApplyStoryContractUpgrade(t.Context(), o, p))
	keys, e := historicalLegacyKeys(t.Context(), o.DB, o.Parent.String(), 2)
	mustProgress(t, e)
	if len(keys) != 1 || keys["US-003"].State != "completed" {
		t.Fatalf("legacy dependencies: %+v", keys)
	}
	histories := map[string]planbrief.StoryProposal{}
	bases := map[string]planbrief.StoryProposal{}
	for i, issue := range f.issues {
		key := fmt.Sprintf("US-%03d", i+1)
		bases[key] = planbrief.StoryProposal{Key: key, Intent: "unchanged", IssueID: issue.ID, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, ExpectedBase: planbrief.StoryHash(issue.Title, issue.Body), Dependencies: []string{}}
	}
	histories["US-003"] = bases["US-003"]
	typed := planbrief.TypedPlan{Summary: "Keep historical dependencies", Stories: []planbrief.TypedStory{}}
	for _, item := range p.Intent.Items {
		var story planbrief.TypedStory
		mustProgress(t, json.Unmarshal(item.Content, &story))
		typed.Stories = append(typed.Stories, story)
	}
	existing, e := o.DB.ListManagedStories(t.Context(), o.Parent.String(), 2)
	mustProgress(t, e)
	raw, e := normalizeTypedUpdatesWithHistory(t.Context(), o.DB, o.Parent.String(), 2, existing, typedPlanBytes(t, typed), o.Scope, nil, histories)
	mustProgress(t, e)
	_, prep, _, _ := typedPlanFixture(t)
	prep.promptInput.PlanGeneration = 2
	proposal, content, e := buildTypedPlanProposal(t.Context(), o.Renderer, prep, o.Scope, bases, existing, raw, histories)
	mustProgress(t, e)
	if len(proposal.Stories) != 3 || len(content) != 2 {
		t.Fatal("historical dependency was dropped or synthesized")
	}
	_, e = PublishPlan(t.Context(), PublicationOptions{DB: o.DB, Forge: f, Renderer: o.Renderer, Parent: o.Parent, Generation: 2, BotID: 9, TypedContent: content, TypedScope: o.Scope}, proposal)
	mustProgress(t, e)
	if !reflect.DeepEqual(f.issues[2], old) || f.writes[4] != 0 {
		t.Fatal("typed Plan changed completed legacy issue")
	}
	if len(content["US-001"].Dependencies.StoryKeys) != 1 || content["US-001"].Dependencies.StoryKeys[0] != "US-003" {
		t.Fatal("historical dependency edge lost")
	}
}

func TestStoryContractUpgradeDispatchKeepsFrozenGenerationContract(t *testing.T) {
	t.Parallel()
	o, _, path := upgradeBoundaryFixture(t)
	p, e := PreviewStoryContractUpgrade(t.Context(), o)
	mustProgress(t, e)
	mustProgress(t, ApplyStoryContractUpgrade(t.Context(), o, p))
	mustProgress(t, o.DB.Close())
	o.DB, e = store.Open(t.Context(), path)
	mustProgress(t, e)
	defer func() { _ = o.DB.Close() }()
	old := &agent.ApprovedBundle{Parent: o.Parent.String(), Generation: 1}
	session, e := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: o.DB, Bundle: old})
	mustProgress(t, e)
	if session != nil {
		t.Fatal("old frozen generation implicitly received typed helper")
	}
	current := &agent.ApprovedBundle{Parent: o.Parent.String(), Generation: 2}
	session, e = StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: o.DB, Bundle: current})
	if session != nil || e == nil || !strings.Contains(e.Error(), "progress service missing") {
		t.Fatalf("new generation did not select typed submission path: %v", e)
	}
	// This asserts persisted dispatch selection only; no live adapter capability.
}
