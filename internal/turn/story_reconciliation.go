package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// normalizeTypedUpdates applies permanent identity history to a Plan's explicit
// semantic proposal. Wording changes conservatively replace obligations; only
// proven whitespace equivalence preserves an existing ID automatically.
func normalizeTypedUpdates(ctx context.Context, db *store.DB, parent string, generation int, existing []store.ManagedStory, raw []byte, scope planbrief.ContentScope, clarifications map[string]map[string]string) ([]byte, error) {
	plan, err := planbrief.ParseTypedPlan(raw, scope)
	if err != nil {
		return nil, err
	}
	byKey := map[string]store.ManagedStory{}
	for _, s := range existing {
		byKey[s.Key] = s
	}
	for i := range plan.Stories {
		s := &plan.Stories[i]
		stored, ok := byKey[s.Key]
		if !ok {
			continue
		}
		prior, err := db.FindSubmissionProjection(ctx, parent, s.Key, stored.BodyHash, generation)
		if err != nil {
			return nil, fmt.Errorf("story %s prior typed requirements unavailable: %w", s.Key, err)
		}
		var old planbrief.TypedStory
		if err := planbrief.DecodeTypedContent(prior.Content, &old); err != nil {
			return nil, err
		}
		if stored.State == "completed" {
			if s.Intent != "unchanged" || !planbrief.EquivalentTypedStory(old, *s) {
				return nil, fmt.Errorf("story %s: completed requirements remain historical; propose separately approved follow-up work", s.Key)
			}
			continue
		}
		if s.Intent == "unchanged" && !planbrief.EquivalentTypedStory(old, *s) {
			s.Intent = "update"
		}
		history, err := db.ListCriterionHistory(ctx, parent, s.Key)
		if err != nil {
			return nil, err
		}
		used := map[string]bool{}
		oldText := map[string]string{}
		maxID := 0
		for _, h := range history {
			used[h.ID] = true
			n, _ := strconv.Atoi(strings.TrimPrefix(h.ID, "AC-"))
			maxID = max(maxID, n)
		}
		for _, c := range old.AcceptanceCriteria {
			oldText[c.ID] = c.Text
			used[c.ID] = true
			n, _ := strconv.Atoi(strings.TrimPrefix(c.ID, "AC-"))
			maxID = max(maxID, n)
		}
		// Reserve every explicitly new ID first so remapping cannot collide with
		// another criterion from this same contribution.
		for _, c := range s.AcceptanceCriteria {
			n, _ := strconv.Atoi(strings.TrimPrefix(c.ID, "AC-"))
			maxID = max(maxID, n)
		}
		replacements := map[string]string{}
		for j := range s.AcceptanceCriteria {
			c := &s.AcceptanceCriteria[j]
			previous, existed := oldText[c.ID]
			if !existed && used[c.ID] {
				return nil, fmt.Errorf("story %s: retired criterion %s cannot be reused", s.Key, c.ID)
			}
			clarified := clarifications[s.Key][c.ID] == c.Text
			if existed && !clarified && strings.Join(strings.Fields(previous), " ") != strings.Join(strings.Fields(c.Text), " ") {
				maxID++
				if maxID > 999999 {
					return nil, fmt.Errorf("story %s: no unused criterion IDs remain", s.Key)
				}
				next := fmt.Sprintf("AC-%03d", maxID)
				replacements[c.ID] = next
				c.ID = next
			}
		}
		for j := range s.Tasks {
			for k, id := range s.Tasks[j].CriterionIDs {
				if next := replacements[id]; next != "" {
					s.Tasks[j].CriterionIDs[k] = next
				}
			}
		}
	}
	if err := plan.Validate(scope); err != nil {
		return nil, err
	}
	return json.Marshal(plan)
}

// An explicit clarification is established only by the authenticated, durably
// admitted child comment that triggered this Plan. An agent cannot self-declare
// a clarification or reuse a declaration on another story, generation or text.
// Ordinary "Clarify AC-001: ..." text still follows existing Plan admission and
// never selects a mode or unlocks an active generation.
func readEstablishedClarifications(ctx context.Context, db *store.DB, prep *preparation, stories []store.ManagedStory) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	r, err := db.GetStoryRefinement(ctx, prep.session.ID, prep.run.TriggerCommentID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if r.ActorID <= 0 || r.Generation != prep.promptInput.PlanGeneration || r.Consumed {
		return out, nil
	}
	body := strings.TrimSpace(r.Body)
	if !strings.HasPrefix(body, "Clarify ") {
		return out, nil
	}
	id, text, ok := strings.Cut(strings.TrimPrefix(body, "Clarify "), ": ")
	if !ok || !planbrief.ValidCriterionID(id) || strings.TrimSpace(text) == "" {
		return out, nil
	}
	for _, s := range stories {
		if s.IssueID == r.ChildID && s.IssueNumber == r.ChildNumber && s.State != "completed" {
			out[s.Key] = map[string]string{id: text}
			break
		}
	}
	return out, nil
}

// reconcilePresentationIssue admits proven presentation changes only in a
// planning scope. Semantic edits stay pending for the existing Plan machinery;
// publication there owns its pre-write and post-write conflict journal.
func reconcilePresentationIssue(ctx context.Context, db *store.DB, forge PlanForge, parent naming.Ref, story store.ManagedStory, issue github.Issue) (github.Issue, error) {
	contract, err := db.GetGenerationContract(ctx, parent.String(), story.Generation)
	if errors.Is(err, store.ErrNotFound) || (err == nil && contract.ContentVersion != 2) {
		return issue, nil
	}
	if err != nil {
		return issue, err
	}
	feature, err := db.GetManagedFeature(ctx, parent.String())
	if err != nil {
		return issue, err
	}
	if feature.State != "planning" || feature.Generation != story.Generation || story.State == "completed" {
		return issue, fmt.Errorf("pending: approved requirements stay frozen; reopen unfinished scope through the parent Plan")
	}
	prior, err := db.FindSubmissionProjection(ctx, parent.String(), story.Key, story.BodyHash, story.Generation)
	if err != nil {
		return issue, err
	}
	var old planbrief.TypedStory
	if err := planbrief.DecodeTypedContent(prior.Content, &old); err != nil {
		return issue, err
	}
	scope := planbrief.ContentScope{}
	// Existing authorized roles/references survive inline parsing, never inferred
	// from an edited issue's request for extra capabilities.
	for _, task := range old.Tasks {
		scope.Roles = append(scope.Roles, task.OwnerRole)
	}
	scope.IssueRefs = append(scope.IssueRefs, old.Dependencies.IssueRefs...)
	history, err := db.ListCriterionHistory(ctx, parent.String(), story.Key)
	if err != nil {
		return issue, err
	}
	ids := make([]string, len(history))
	for i, h := range history {
		ids[i] = h.ID
	}
	result, err := planbrief.ReconcileTypedStory(old, issue.Title, issue.Body, ids, scope)
	if err != nil {
		return issue, err
	}
	if !result.PresentationOnly {
		return issue, fmt.Errorf("pending: the edited requirements need the next published and approved parent Plan")
	}
	if result.Body != issue.Body {
		return issue, fmt.Errorf("pending: identifier or guidance updates need the parent Plan before approval")
	}
	// Dependencies are a feature graph, not merely a locally valid string list.
	all, err := db.ListManagedStories(ctx, parent.String(), story.Generation)
	if err != nil {
		return issue, err
	}
	graph := map[string][]string{}
	for _, s := range all {
		graph[s.Key] = s.Dependencies
	}
	graph[story.Key] = result.Story.Dependencies.StoryKeys
	if err := reconciledDependencyGraph(graph); err != nil {
		return issue, err
	}
	current, err := forge.GetIssue(ctx, github.RepoFromRef(parent), issue.Number)
	if err != nil {
		return issue, err
	}
	if current.ID != issue.ID || current.Title != issue.Title || current.Body != issue.Body {
		return issue, fmt.Errorf("pending: the issue changed while reading it; preserve the competing edit and retry")
	}
	content, err := json.Marshal(result.Story)
	if err != nil {
		return issue, err
	}
	criteria := make([]string, len(result.Story.AcceptanceCriteria))
	for i, c := range result.Story.AcceptanceCriteria {
		criteria[i] = c.ID
	}
	projection := store.SubmissionProjection{Parent: parent.String(), Generation: story.Generation, Story: story.Key, IssueID: issue.ID, Revision: planbrief.StoryHash(issue.Title, issue.Body), ContentVersion: 2, Title: issue.Title, Body: issue.Body, Content: content, Criteria: criteria, RetiredCriteria: result.RetiredIDs}
	if err := retainTypedPublicationProjection(ctx, db, projection, result.Story); err != nil {
		return issue, err
	}
	story.BodyHash = projection.Revision
	story.Dependencies = result.Story.Dependencies.StoryKeys
	if err := db.PutManagedStory(ctx, story); err != nil {
		return issue, err
	}
	return issue, nil
}

func reconciledDependencyGraph(graph map[string][]string) error {
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("pending: story dependencies contradict one another in a cycle at %s", key)
		}
		if done[key] {
			return nil
		}
		deps, ok := graph[key]
		if !ok {
			return fmt.Errorf("pending: dependency %s is not an attached story", key)
		}
		visiting[key] = true
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[key] = false
		done[key] = true
		return nil
	}
	for key := range graph {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}
