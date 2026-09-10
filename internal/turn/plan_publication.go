package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// PlanForge is the managed publication surface. All mutation inputs have
// already passed report's redaction and completeness checks.
type PlanForge interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error)
	ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error)
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	AddSubIssue(context.Context, github.Repo, int, int64) error
	UpdateIssue(context.Context, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
}

// CompleteStoryRenderer ensures publication receives redacted, complete story content.
type CompleteStoryRenderer interface {
	RenderCompleteStory(context.Context, report.Story) (report.SubIssue, error)
}

// PublicationOptions binds publication dependencies to a factory-selected parent and generation.
type PublicationOptions struct {
	DB           *store.DB
	Forge        PlanForge
	Renderer     CompleteStoryRenderer
	Parent       naming.Ref
	Generation   int
	BotID        int64
	TypedContent map[string]planbrief.TypedStory
	TypedScope   planbrief.ContentScope
}

// PublishPlan validates the entire batch before making changes. The returned
// index is link-only and must be saved before the caller reports readiness.
// A failed batch remains durable and replayable; no SQL transaction spans IO.
func PublishPlan(ctx context.Context, o PublicationOptions, p planbrief.PlanProposal) ([]byte, error) {
	if o.DB == nil || o.Forge == nil || o.Renderer == nil || o.BotID < 1 {
		return nil, fmt.Errorf("plan publication dependencies missing")
	}
	if err := o.Parent.Validate(); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Parent != o.Parent.String() || p.Generation != o.Generation {
		return nil, fmt.Errorf("plan proposal does not match supplied parent/generation")
	}
	contract, contractErr := o.DB.GetGenerationContract(ctx, p.Parent, p.Generation)
	historical, historyErr := historicalLegacyKeys(ctx, o.DB, p.Parent, p.Generation)
	if historyErr != nil {
		return nil, historyErr
	}
	if contractErr != nil && !errors.Is(contractErr, store.ErrNotFound) {
		return nil, contractErr
	}
	if contractErr == nil && contract.ContentVersion == 2 {
		if len(o.TypedContent)+len(historical) != len(p.Stories) {
			return nil, fmt.Errorf("typed generation requires complete typed content")
		}
		for _, story := range p.Stories {
			if old, ok := historical[story.Key]; ok {
				if story.Intent != "unchanged" || story.IssueID != old.IssueID || story.IssueNumber != old.IssueNumber {
					return nil, fmt.Errorf("completed legacy dependency must remain unchanged")
				}
				continue
			}
			typed, ok := o.TypedContent[story.Key]
			if !ok {
				return nil, fmt.Errorf("typed story content missing")
			}
			body, err := report.RenderTypedStory(typed, o.TypedScope)
			if err != nil {
				return nil, err
			}
			bodyMatches := body == story.Body
			if story.Intent != "create" {
				bodyMatches = planbrief.TypedStoryBodyMatches(typed, story.Body)
			}
			if story.Intent == "unchanged" {
				bodyMatches = planbrief.TypedStoryBodyMatches(typed, story.Body)
			}
			if !bodyMatches || typed.Title != story.Title || typed.Intent != story.Intent {
				return nil, fmt.Errorf("typed story transport differs from validated content")
			}
		}
	} else if len(o.TypedContent) != 0 {
		return nil, fmt.Errorf("legacy generation cannot implicitly upgrade typed content")
	}
	repo := github.RepoFromRef(o.Parent)
	parent, err := o.Forge.GetIssue(ctx, repo, o.Parent.Issue)
	if err != nil {
		return nil, err
	}
	if parent.ID < 1 || parent.Number != o.Parent.Issue {
		return nil, fmt.Errorf("invalid plan parent")
	}
	rendered := make([]report.SubIssue, len(p.Stories))
	var faults []error
	for i, s := range p.Stories {
		r, err := o.Renderer.RenderCompleteStory(ctx, report.Story{Ref: o.Parent, ID: s.Key, Title: s.Title, Excerpt: s.Body})
		if err != nil {
			faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
		}
		if typed, ok := o.TypedContent[s.Key]; ok {
			if typed.Title != s.Title || r.Title.String() != s.Title || r.Body.String() != report.StoryMarker(o.Parent, s.Key)+"\n\n"+s.Body {
				faults = append(faults, fmt.Errorf("story %s: rendered/redacted content differs from typed contract", s.Key))
			}
		}
		rendered[i] = r
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	if err := o.DB.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: p.Parent, Generation: p.Generation, State: "planning"}); err != nil {
		return nil, err
	}
	feature, err := o.DB.GetManagedFeature(ctx, p.Parent)
	if err != nil {
		return nil, err
	}
	if feature.Generation != p.Generation || feature.State != "planning" {
		return nil, fmt.Errorf("plan generation is no longer editable")
	}
	if err := o.DB.SetManagedFeatureReady(ctx, p.Parent, p.Generation, false); err != nil {
		return nil, err
	}
	managed, err := o.DB.ListManagedStories(ctx, p.Parent, p.Generation)
	if err != nil {
		return nil, err
	}
	completed := map[string]store.ManagedStory{}
	proposed := map[string]planbrief.StoryProposal{}
	for _, story := range p.Stories {
		proposed[story.Key] = story
	}
	for _, story := range managed {
		proposal, exists := proposed[story.Key]
		if !exists {
			faults = append(faults, fmt.Errorf("story %s: indexed identity must remain in the proposal", story.Key))
		}
		if story.State == "completed" {
			completed[story.Key] = story
			if !exists || proposal.Intent != "unchanged" || proposal.IssueID != story.IssueID || proposal.IssueNumber != story.IssueNumber {
				faults = append(faults, fmt.Errorf("story %s is completed; keep it unchanged as a dependency and create a successor for new scope", story.Key))
			}
		}
	}
	existing, err := o.Forge.ListSubIssues(ctx, repo, o.Parent.Issue)
	if err != nil {
		return nil, err
	}
	// Duplicates block before mutations, including forged marker collisions.
	for i, s := range p.Stories {
		count := 0
		for _, issue := range existing {
			if exactStoryMarker(issue.Body, rendered[i].Marker) {
				count++
			}
		}
		if count > 1 {
			faults = append(faults, fmt.Errorf("story %s: duplicate marker", s.Key))
		}
		if s.Intent != "create" {
			issue, err := o.Forge.GetIssue(ctx, repo, s.IssueNumber)
			if err != nil {
				faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
				continue
			}
			if _, historical := historical[s.Key]; historical && (s.Title != issue.Title || s.Body != historicalBody(planbrief.StoryProposal{Key: s.Key, Body: issue.Body}, o.Parent.String())) {
				faults = append(faults, fmt.Errorf("completed legacy dependency %s source differs", s.Key))
				continue
			}
			if err := checkPublicationBase(ctx, o, parent, s, issue, planbrief.StoryHash(rendered[i].Title.String(), rendered[i].Body.String())); err != nil {
				faults = append(faults, err)
			}
		}
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	var index strings.Builder
	index.WriteString("## Story Index\n\n| ID | Title | Sub-issue |\n| --- | --- | --- |\n")
	for i, s := range p.Stories {
		issue, err := publishStory(ctx, o, parent, s, rendered[i])
		if err != nil {
			return nil, fmt.Errorf("story %s: %w", s.Key, err)
		}
		entry := store.ManagedStory{ParentTask: p.Parent, Generation: p.Generation, Key: s.Key, IssueNumber: issue.Number, IssueID: issue.ID, BodyHash: planbrief.StoryHash(issue.Title, issue.Body), Dependencies: s.Dependencies, State: "planning"}
		if _, done := completed[s.Key]; !done {
			if err := o.DB.PutManagedStory(ctx, entry); err != nil {
				return nil, err
			}
		}
		if typed, ok := o.TypedContent[s.Key]; ok {
			if issue.Title != rendered[i].Title.String() || issue.Body != rendered[i].Body.String() {
				return nil, fmt.Errorf("story %s: published source confirmation differs", s.Key)
			}
			content, err := json.Marshal(typed)
			if err != nil {
				return nil, err
			}
			criteria := make([]string, len(typed.AcceptanceCriteria))
			for j, c := range typed.AcceptanceCriteria {
				criteria[j] = c.ID
			}
			projection := store.SubmissionProjection{Parent: p.Parent, Generation: p.Generation, Story: s.Key, IssueID: issue.ID, Revision: entry.BodyHash, ContentVersion: 2, Title: issue.Title, Body: issue.Body, Content: content, Criteria: criteria}
			if err := retainTypedPublicationProjection(ctx, o.DB, projection, typed); err != nil {
				return nil, err
			}
		}
		// URL is derived from validated numeric identity, never remote Markdown.
		fmt.Fprintf(&index, "| %s | %s | [#%d](https://github.com/%s/%s/issues/%d) |\n", s.Key, strings.ReplaceAll(issue.Title, "|", "\\|"), issue.Number, o.Parent.Owner, o.Parent.Repo, issue.Number)
	}
	return []byte(index.String()), nil
}
func checkPublicationBase(ctx context.Context, o PublicationOptions, parent github.Issue, s planbrief.StoryProposal, issue github.Issue, desired string) error {
	managed, err := o.DB.ListManagedStories(ctx, o.Parent.String(), o.Generation)
	if err != nil {
		return err
	}
	known := false
	for _, stored := range managed {
		if stored.Key == s.Key && stored.IssueID == s.IssueID && stored.IssueNumber == s.IssueNumber {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("story %s: identity is not managed by this generation", s.Key)
	}
	if issue.ID != s.IssueID || issue.Number != s.IssueNumber {
		return fmt.Errorf("story %s: issue identity mismatch", s.Key)
	}
	relation, err := o.Forge.GetParentIssue(ctx, github.RepoFromRef(o.Parent), issue.Number)
	if err != nil {
		return err
	}
	if relation.ID != parent.ID || relation.Number != parent.Number {
		return fmt.Errorf("story %s: unsupported parent relationship", s.Key)
	}
	if planbrief.StoryHash(issue.Title, issue.Body) != s.ExpectedBase {
		op, err := o.DB.GetStoryRevisionOperation(ctx, revisionOperationKey(o, s, desired))
		if err == nil && op.DesiredHash == desired && planbrief.StoryHash(issue.Title, issue.Body) == desired {
			return nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return fmt.Errorf("story %s: current base changed; reload before editing", s.Key)
	}
	return nil
}
func publishStory(ctx context.Context, o PublicationOptions, parent github.Issue, s planbrief.StoryProposal, r report.SubIssue) (github.Issue, error) {
	repo := github.RepoFromRef(o.Parent)
	desired := planbrief.StoryHash(r.Title.String(), r.Body.String())
	if s.Intent != "create" {
		issue, err := o.Forge.GetIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			return issue, err
		}
		if err := checkPublicationBase(ctx, o, parent, s, issue, desired); err != nil {
			return issue, err
		}
		if s.Intent == "unchanged" {
			return issue, nil
		}
		key := revisionOperationKey(o, s, desired)
		_, err = o.DB.GetStoryRevisionOperation(ctx, key)
		if errors.Is(err, store.ErrNotFound) {
			err = o.DB.BeginStoryRevisionOperation(ctx, store.StoryRevisionOperation{OperationKey: key, ParentTask: o.Parent.String(), Generation: o.Generation, Key: s.Key, ExpectedBase: s.ExpectedBase, DesiredHash: desired})
		}
		if err != nil {
			return github.Issue{}, err
		}
		if planbrief.StoryHash(issue.Title, issue.Body) != desired {
			if _, err := o.Forge.UpdateIssue(ctx, repo, s.IssueNumber, r.Title, r.Body); err != nil {
				return github.Issue{}, err
			}
		}
		issue, err = o.Forge.GetIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			return issue, err
		}
		if issue.ID != s.IssueID || planbrief.StoryHash(issue.Title, issue.Body) != desired {
			return issue, fmt.Errorf("post-write conflict; concurrent GitHub edits cannot be atomically excluded")
		}
		if err := o.DB.DeliverStoryRevisionOperation(ctx, key); err != nil {
			return issue, err
		}
		return issue, nil
	}
	op, err := o.DB.GetStoryPublication(ctx, o.Parent.String(), o.Generation, s.Key)
	fresh := errors.Is(err, store.ErrNotFound)
	if fresh {
		op = store.StoryPublication{ParentTask: o.Parent.String(), Generation: o.Generation, Key: s.Key, OperationKey: planbrief.StoryHash(o.Parent.String(), fmt.Sprintf("%d/%s", o.Generation, s.Key)), DesiredHash: desired}
		if err := o.DB.BeginStoryPublication(ctx, op); err != nil {
			return github.Issue{}, err
		}
	} else if err != nil {
		return github.Issue{}, err
	}
	if op.DesiredHash != desired {
		return github.Issue{}, fmt.Errorf("pending publication differs; reconcile original proposal first")
	}
	var issue github.Issue
	if op.IssueNumber > 0 {
		issue, err = o.Forge.GetIssue(ctx, repo, op.IssueNumber)
		if err != nil {
			return issue, err
		}
	} else {
		// A durable pending intent may mean the create response was lost. Search
		// the complete open-issue listing using exact marker plus numeric bot ID;
		// absence on retry is ambiguous (e.g. an issue was closed), never permission
		// to create a second issue.
		candidates, e := o.Forge.ListIssues(ctx, repo, time.Time{})
		if e != nil {
			return issue, e
		}
		matches := 0
		for _, candidate := range candidates {
			if exactStoryMarker(candidate.Body, r.Marker) {
				matches++
				issue = candidate
			}
		}
		if matches > 1 {
			return issue, fmt.Errorf("duplicate publication marker")
		}
		if matches == 0 {
			if !fresh {
				return issue, fmt.Errorf("ambiguous create delivery; reconcile pending operation before retry")
			}
			issue, err = o.Forge.CreateIssue(ctx, repo, r.Title, r.Body)
			if err != nil {
				return issue, err
			}
		}
	}
	if issue.ID < 1 || issue.Author.ID != o.BotID || !exactStoryMarker(issue.Body, r.Marker) || planbrief.StoryHash(issue.Title, issue.Body) != desired {
		return issue, fmt.Errorf("created issue provenance or content mismatch")
	}
	if op.IssueID != 0 && op.IssueID != issue.ID {
		return issue, fmt.Errorf("publication identity changed")
	}
	op.IssueID = issue.ID
	op.IssueNumber = issue.Number
	if err := o.DB.SaveStoryPublication(ctx, op); err != nil {
		return issue, err
	}
	attached, err := o.Forge.ListSubIssues(ctx, repo, o.Parent.Issue)
	if err != nil {
		return issue, err
	}
	found := false
	for _, child := range attached {
		if child.ID == issue.ID && child.Number == issue.Number {
			found = true
		}
	}
	if !found {
		if err := o.Forge.AddSubIssue(ctx, repo, o.Parent.Issue, issue.ID); err != nil {
			return issue, err
		}
	}
	relation, err := o.Forge.GetParentIssue(ctx, repo, issue.Number)
	if err != nil {
		return issue, err
	}
	if relation.ID != parent.ID || relation.Number != parent.Number {
		return issue, fmt.Errorf("attachment verification failed")
	}
	verified, err := o.Forge.GetIssue(ctx, repo, issue.Number)
	if err != nil {
		return issue, err
	}
	if verified.ID != issue.ID || planbrief.StoryHash(verified.Title, verified.Body) != desired {
		return issue, fmt.Errorf("publication verification conflict")
	}
	op.Delivered = true
	if err := o.DB.SaveStoryPublication(ctx, op); err != nil {
		return issue, err
	}
	return verified, nil
}
func exactStoryMarker(body, marker string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSuffix(line, "\r") == marker {
			return true
		}
	}
	return false
}

func revisionOperationKey(o PublicationOptions, s planbrief.StoryProposal, desired string) string {
	return planbrief.StoryHash(o.Parent.String(), fmt.Sprintf("%d/%s/%s/%s", o.Generation, s.Key, s.ExpectedBase, desired))
}

// Publication intent is transport metadata, not an issue requirement. Reusing
// an exact projection leaves its historical bytes immutable across replanning.
func retainTypedPublicationProjection(ctx context.Context, db *store.DB, projection store.SubmissionProjection, typed planbrief.TypedStory) error {
	prior, err := db.GetSubmissionProjection(ctx, projection.Parent, projection.Generation, projection.Story, projection.Revision)
	if errors.Is(err, store.ErrNotFound) {
		history, err := db.ListCriterionHistory(ctx, projection.Parent, projection.Story)
		if err != nil {
			return err
		}
		active := map[string]bool{}
		for _, id := range projection.Criteria {
			active[id] = true
		}
		retired := map[string]bool{}
		for _, id := range projection.RetiredCriteria {
			retired[id] = true
		}
		for _, h := range history {
			if h.RetiredRevision == "" && !active[h.ID] && !retired[h.ID] {
				projection.RetiredCriteria = append(projection.RetiredCriteria, h.ID)
			}
		}
		return db.PutSubmissionProjection(ctx, projection)
	}
	if err != nil {
		return err
	}
	var original planbrief.TypedStory
	if err := planbrief.DecodeTypedContent(prior.Content, &original); err != nil {
		return err
	}
	original.Intent = typed.Intent
	if prior.IssueID != projection.IssueID || prior.Title != projection.Title || prior.Body != projection.Body || !reflect.DeepEqual(original, typed) {
		return fmt.Errorf("typed publication conflicts with immutable issue projection")
	}
	return nil
}
