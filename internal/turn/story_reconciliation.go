package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

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
