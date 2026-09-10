package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// StoryContractUpgradeOptions is an owner CLI boundary, never an agent helper.
type StoryContractUpgradeOptions struct {
	DB       *store.DB
	Forge    PlanForge
	Renderer CompleteStoryRenderer
	Parent   naming.Ref
	OwnerID  int64
	Scope    planbrief.ContentScope
	LockDir  string
}

// UpgradeObservation records the exact current revision alongside retained intent.
type UpgradeObservation struct {
	Story            string `json:"story"`
	IssueNumber      int    `json:"issue_number"`
	ObservedRevision string `json:"observed_revision"`
	ExpectedRevision string `json:"expected_revision"`
	DesiredRevision  string `json:"desired_revision"`
	Conflict         string `json:"conflict,omitempty"`
}

// StoryContractUpgradePreview is read-only; readiness is never implied by valid conversion.
type StoryContractUpgradePreview struct {
	Parent           string                `json:"parent"`
	SourceGeneration int                   `json:"source_generation"`
	TargetGeneration int                   `json:"target_generation"`
	FromContract     int                   `json:"from_contract"`
	ToContract       int                   `json:"to_contract"`
	State            string                `json:"state"`
	Items            []UpgradeObservation  `json:"items"`
	Conflicts        []string              `json:"conflicts"`
	Intent           store.ContractUpgrade `json:"-"`
}

func validateUpgradeOptions(o StoryContractUpgradeOptions) error {
	if o.DB == nil || o.Forge == nil || o.Renderer == nil || o.OwnerID <= 0 {
		return fmt.Errorf("upgrade requires configured numeric owner, database, forge and renderer")
	}
	return o.Parent.Validate()
}

// PreviewStoryContractUpgrade lists the whole unfinished cohort without changing
// issues, publication intent, contract selection or readiness.
func PreviewStoryContractUpgrade(ctx context.Context, o StoryContractUpgradeOptions) (StoryContractUpgradePreview, error) {
	p := StoryContractUpgradePreview{Parent: o.Parent.String(), FromContract: 1, ToContract: 2, State: "preview", Items: []UpgradeObservation{}, Conflicts: []string{}}
	if err := validateUpgradeOptions(o); err != nil {
		return p, err
	}
	feature, err := o.DB.GetManagedFeature(ctx, p.Parent)
	if err != nil {
		return p, err
	}
	p.SourceGeneration = feature.Generation
	p.TargetGeneration = feature.Generation + 1
	retained, err := o.DB.GetContractUpgrade(ctx, p.Parent)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	if err == nil {
		p.Intent = retained
		p.State = retained.State
		p.SourceGeneration = retained.SourceGeneration
		p.TargetGeneration = retained.TargetGeneration
		if retained.OwnerID != o.OwnerID {
			p.Conflicts = append(p.Conflicts, "retained upgrade belongs to a different configured numeric owner")
		}
		if retained.State == "completed" {
			return p, nil
		}
		for _, item := range retained.Items {
			observation, e := observeUpgradeItem(ctx, o, item)
			if e != nil {
				observation.Conflict = e.Error()
				p.Conflicts = append(p.Conflicts, e.Error())
			}
			p.Items = append(p.Items, observation)
		}
		return p, nil
	}
	if err := o.DB.ContractUpgradeSafety(ctx, p.Parent, feature.Generation); err != nil {
		p.Conflicts = append(p.Conflicts, err.Error())
	}
	stories, err := o.DB.ListManagedStories(ctx, p.Parent, feature.Generation)
	if err != nil {
		return p, err
	}
	graph := map[string][]string{}
	for _, s := range stories {
		graph[s.Key] = s.Dependencies
	}
	if err := reconciledDependencyGraph(graph); err != nil {
		p.Conflicts = append(p.Conflicts, err.Error())
	}
	u := store.ContractUpgrade{Parent: p.Parent, SourceGeneration: p.SourceGeneration, TargetGeneration: p.TargetGeneration, OwnerID: o.OwnerID, State: "pending", Items: []store.ContractUpgradeItem{}}
	for _, s := range stories {
		if s.State == "completed" {
			continue
		}
		observation := UpgradeObservation{Story: s.Key, IssueNumber: s.IssueNumber}
		item, err := convertUpgradeIssue(ctx, o, s)
		observation.ObservedRevision = item.SourceRevision
		observation.ExpectedRevision = item.SourceRevision
		if err != nil {
			observation.Conflict = err.Error()
			p.Conflicts = append(p.Conflicts, fmt.Sprintf("%s: %v", s.Key, err))
		} else {
			observation.ObservedRevision = item.SourceRevision
			observation.ExpectedRevision = item.SourceRevision
			observation.DesiredRevision = item.Revision
			u.Items = append(u.Items, item)
		}
		p.Items = append(p.Items, observation)
	}
	if len(p.Items) == 0 {
		p.Conflicts = append(p.Conflicts, "the feature has no unfinished stories to upgrade")
	}
	u.ScopeHash, err = upgradeScopeHash(u)
	if err != nil {
		return p, err
	}
	p.Intent = u
	return p, nil
}

func convertUpgradeIssue(ctx context.Context, o StoryContractUpgradeOptions, s store.ManagedStory) (store.ContractUpgradeItem, error) {
	item := store.ContractUpgradeItem{Story: s.Key, IssueID: s.IssueID, IssueNumber: s.IssueNumber}
	issue, err := o.Forge.GetIssue(ctx, github.RepoFromRef(o.Parent), s.IssueNumber)
	if err != nil {
		return item, err
	}
	item.Title = issue.Title
	item.SourceBody = issue.Body
	item.SourceRevision = planbrief.StoryHash(issue.Title, issue.Body)
	parent, err := o.Forge.GetParentIssue(ctx, github.RepoFromRef(o.Parent), s.IssueNumber)
	if err != nil {
		return item, err
	}
	root, err := o.Forge.GetIssue(ctx, github.RepoFromRef(o.Parent), o.Parent.Issue)
	if err != nil {
		return item, err
	}
	if issue.ID != s.IssueID || issue.Number != s.IssueNumber || parent.ID != root.ID || parent.Number != root.Number || root.Number != o.Parent.Issue || root.ID <= 0 || issue.State != github.StateOpen {
		return item, fmt.Errorf("issue is not the expected open attached child")
	}
	item.Title = issue.Title
	item.SourceBody = issue.Body
	item.SourceRevision = planbrief.StoryHash(issue.Title, issue.Body)
	typed, err := planbrief.ConvertLegacyStory(s.Key, issue.Title, issue.Body, s.Dependencies, o.Scope)
	if err != nil {
		return item, err
	}
	content, err := json.Marshal(typed)
	if err != nil {
		return item, err
	}
	checked, err := o.Renderer.RenderCompleteStory(ctx, report.Story{Ref: o.Parent, ID: s.Key, Title: issue.Title, Excerpt: string(content)})
	if err != nil {
		return item, err
	}
	if checked.Title.String() != issue.Title || checked.Body.String() != report.StoryMarker(o.Parent, s.Key)+"\n\n"+string(content) {
		return item, fmt.Errorf("required content would change during redaction")
	}
	body, err := report.RenderTypedStory(typed, o.Scope)
	if err != nil {
		return item, err
	}
	body = planbrief.PreserveLegacyStoryText(issue.Body, body)
	rendered, err := o.Renderer.RenderCompleteStory(ctx, report.Story{Ref: o.Parent, ID: s.Key, Title: issue.Title, Excerpt: body})
	if err != nil {
		return item, err
	}
	if rendered.Title.String() != issue.Title || rendered.Body.String() != report.StoryMarker(o.Parent, s.Key)+"\n\n"+body {
		return item, fmt.Errorf("converted issue would change during publication redaction")
	}
	item.Body = rendered.Body.String()
	item.Revision = planbrief.StoryHash(issue.Title, item.Body)
	item.Content = content
	item.Dependencies = typed.Dependencies.StoryKeys
	for _, c := range typed.AcceptanceCriteria {
		item.Criteria = append(item.Criteria, c.ID)
	}
	return item, nil
}

func upgradeScopeHash(u store.ContractUpgrade) (string, error) {
	u.ScopeHash = ""
	u.State = "pending"
	data, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func observeUpgradeItem(ctx context.Context, o StoryContractUpgradeOptions, item store.ContractUpgradeItem) (UpgradeObservation, error) {
	observation := UpgradeObservation{Story: item.Story, IssueNumber: item.IssueNumber, ExpectedRevision: item.SourceRevision, DesiredRevision: item.Revision}
	issue, err := o.Forge.GetIssue(ctx, github.RepoFromRef(o.Parent), item.IssueNumber)
	if err != nil {
		return observation, err
	}
	observation.ObservedRevision = planbrief.StoryHash(issue.Title, issue.Body)
	parent, err := o.Forge.GetParentIssue(ctx, github.RepoFromRef(o.Parent), item.IssueNumber)
	if err != nil {
		return observation, err
	}
	root, err := o.Forge.GetIssue(ctx, github.RepoFromRef(o.Parent), o.Parent.Issue)
	if err != nil {
		return observation, err
	}
	if issue.ID != item.IssueID || issue.Number != item.IssueNumber || parent.ID != root.ID || parent.Number != o.Parent.Issue || root.Number != o.Parent.Issue || root.ID <= 0 || issue.State != github.StateOpen {
		return observation, fmt.Errorf("%s: issue identity, state or direct attachment changed", item.Story)
	}
	if observation.ObservedRevision != item.SourceRevision && observation.ObservedRevision != item.Revision {
		return observation, fmt.Errorf("%s: issue changed since upgrade preview; competing edits remain preserved", item.Story)
	}
	return observation, nil
}

// ApplyStoryContractUpgrade resumes the same immutable cohort after failure.
// GET/PATCH/GET detects observable conflicts, not an atomic forge transaction.
func ApplyStoryContractUpgrade(ctx context.Context, o StoryContractUpgradeOptions, p StoryContractUpgradePreview) (err error) {
	if err := validateUpgradeOptions(o); err != nil {
		return err
	}
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("upgrade refused: %s", strings.Join(p.Conflicts, "; "))
	}
	u := p.Intent
	digest, err := upgradeScopeHash(u)
	if err != nil {
		return err
	}
	if u.Parent != o.Parent.String() || u.OwnerID != o.OwnerID || u.ScopeHash != digest {
		return fmt.Errorf("upgrade preview binding differs")
	}
	if o.LockDir == "" {
		return fmt.Errorf("upgrade requires a configured local lock directory")
	}
	unlock, err := o.DB.LockStoryProgress(ctx, u.Parent, o.LockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	retained, readErr := o.DB.GetContractUpgrade(ctx, u.Parent)
	if readErr != nil && !errors.Is(readErr, store.ErrNotFound) {
		return readErr
	}
	if readErr == nil {
		if retained.ScopeHash != u.ScopeHash {
			return fmt.Errorf("another upgrade owns the retained scope")
		}
		if retained.State == "completed" {
			return nil
		}
	}
	// Recheck every observed base before even the first remote mutation.
	for _, item := range u.Items {
		if _, err := observeUpgradeItem(ctx, o, item); err != nil {
			return err
		}
	}
	if err := o.DB.BeginContractUpgrade(ctx, u); err != nil {
		return err
	}
	for _, item := range u.Items {
		observation, err := observeUpgradeItem(ctx, o, item)
		if err != nil {
			return err
		}
		if observation.ObservedRevision != item.Revision {
			body := strings.TrimPrefix(item.Body, report.StoryMarker(o.Parent, item.Story)+"\n\n")
			r, err := o.Renderer.RenderCompleteStory(ctx, report.Story{Ref: o.Parent, ID: item.Story, Title: item.Title, Excerpt: body})
			if err != nil {
				return err
			}
			if r.Title.String() != item.Title || r.Body.String() != item.Body {
				return fmt.Errorf("retained upgrade bytes would change during redaction")
			}
			if _, err := o.Forge.UpdateIssue(ctx, github.RepoFromRef(o.Parent), item.IssueNumber, r.Title, r.Body); err != nil {
				return err
			}
		}
		confirmed, err := observeUpgradeItem(ctx, o, item)
		if err != nil {
			return err
		}
		if confirmed.ObservedRevision != item.Revision {
			return fmt.Errorf("%s: post-write confirmation remains pending", item.Story)
		}
		if err := o.DB.ConfirmContractUpgradeItem(ctx, u.Parent, u.ScopeHash, item.Story); err != nil {
			return err
		}
	}
	for _, item := range u.Items {
		current, err := observeUpgradeItem(ctx, o, item)
		if err != nil {
			return err
		}
		if current.ObservedRevision != item.Revision {
			return fmt.Errorf("%s: source changed before contract selection", item.Story)
		}
	}
	return o.DB.FinishContractUpgrade(ctx, u.Parent, u.ScopeHash)
}
