package turn

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// TypedPlanSubmission holds factory-bound input for one actual Plan attempt.
// It is never recovered from agent-written transport or treated as issue authority.
type TypedPlanSubmission struct {
	mu        sync.Mutex
	receiptID int64
	proposal  []byte
	outcome   []byte
	working   []byte
	content   map[string]planbrief.TypedStory
	scope     planbrief.ContentScope
}

func prepareTypedPlanSubmission(ctx context.Context, o Options, prep *preparation) (*PlanSubmissionCallbacks, error) {
	if prep.run.Mode != "plan" || prep.promptInput.PlanProposalPath == "" {
		return nil, nil
	}
	contract, err := o.DB.GetGenerationContract(ctx, prep.ref.String(), prep.promptInput.PlanGeneration)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if contract.ContentVersion != 2 {
		return nil, nil
	}
	forge, ok := o.SubIssueCreator.(PlanForge)
	if !ok {
		return nil, fmt.Errorf("typed Plan forge unavailable")
	}
	renderer, ok := o.SubIssueRenderer.(CompleteStoryRenderer)
	if !ok {
		return nil, fmt.Errorf("typed Plan renderer unavailable")
	}
	scope := planbrief.ContentScope{Roles: []string{"go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}, IssueRefs: []string{}}
	if prep.repo.ExternalTeam != "" {
		roster, err := teamsource.Capture(ctx, o.Paths, prep.repo.ExternalTeam, "implement")
		if err != nil {
			return nil, err
		}
		scope.Roles = nil
		for _, role := range roster.Agents {
			scope.Roles = append(scope.Roles, role.Definition.ID)
		}
	}
	existing, err := o.DB.ListManagedStories(ctx, prep.ref.String(), contract.Generation)
	if err != nil {
		return nil, err
	}
	bases := map[string]planbrief.StoryProposal{}
	callbacks := &PlanSubmissionCallbacks{Parent: prep.ref.String(), Generation: contract.Generation}
	for _, story := range existing {
		issue, err := forge.GetIssue(ctx, github.RepoFromRef(prep.ref), story.IssueNumber)
		if err != nil {
			return nil, err
		}
		if issue.ID != story.IssueID || issue.Number != story.IssueNumber {
			return nil, fmt.Errorf("typed Plan story identity changed")
		}
		base := planbrief.StoryProposal{Key: story.Key, IssueNumber: issue.Number, IssueID: issue.ID, ExpectedBase: planbrief.StoryHash(issue.Title, issue.Body), Title: issue.Title, Body: issue.Body, Dependencies: slices.Clone(story.Dependencies)}
		bases[story.Key] = base
		if story.State != "completed" {
			callbacks.Existing = append(callbacks.Existing, store.SubmissionStoryBinding{Story: story.Key, IssueID: story.IssueID, Revision: base.ExpectedBase})
		}
		// External prerequisites are allowed only after factory authorization. Existing
		// exact projections retain previously authorized references across Plan edits.
		p, e := o.DB.GetSubmissionProjection(ctx, prep.ref.String(), contract.Generation, story.Key, base.ExpectedBase)
		if e == nil {
			var typed planbrief.TypedStory
			if err := planbrief.DecodeTypedContent(p.Content, &typed); err != nil {
				return nil, err
			}
			scope.IssueRefs = append(scope.IssueRefs, typed.Dependencies.IssueRefs...)
		} else if !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
	}
	state := &TypedPlanSubmission{scope: scope}
	prep.typedPlan = state
	historicalKeys, err := historicalLegacyKeys(ctx, o.DB, prep.ref.String(), contract.Generation)
	if err != nil {
		return nil, err
	}
	history := map[string]planbrief.StoryProposal{}
	for key := range historicalKeys {
		if base, ok := bases[key]; ok {
			base.Intent = "unchanged"
			history[key] = base
		}
	}
	clarifications, err := readEstablishedClarifications(ctx, o.DB, prep, existing)
	if err != nil {
		return nil, err
	}
	build := func(ctx context.Context, raw json.RawMessage) (planbrief.PlanProposal, map[string]planbrief.TypedStory, error) {
		normalized, err := normalizeTypedUpdatesWithHistory(ctx, o.DB, prep.ref.String(), contract.Generation, existing, raw, scope, clarifications, history)
		if err != nil {
			return planbrief.PlanProposal{}, nil, err
		}
		return buildTypedPlanProposal(ctx, renderer, prep, scope, bases, existing, normalized, history)
	}
	callbacks.Validate = func(ctx context.Context, raw json.RawMessage) agent.ContributionDiagnostics {
		_, _, err := build(ctx, raw)
		d := agent.ContributionDiagnostics{Valid: err == nil, Complete: true, Diagnostics: []agent.ContributionDiagnostic{}, UncheckedPaths: []string{}}
		if err != nil {
			d.Diagnostics = append(d.Diagnostics, agent.ContributionDiagnostic{Path: "$", Code: "typed_plan", Expected: "complete valid typed Plan with unchanged identities and publishable content", Actual: "invalid content", Message: err.Error()})
		}
		return d
	}
	callbacks.Submit = func(ctx context.Context, receipt store.SubmissionReceipt, c agent.Contribution) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.receiptID != 0 {
			if state.receiptID == receipt.ID {
				return nil
			}
			return fmt.Errorf("a valid Plan outcome is already recorded for this attempt")
		}
		var proposal []byte
		var workingBytes []byte
		var content map[string]planbrief.TypedStory
		if c.Outcome == "completed" {
			bound, typed, err := build(ctx, c.Plan)
			if err != nil {
				return err
			}
			proposal, err = json.Marshal(bound)
			if err != nil {
				return err
			}
			content = typed
		}
		outcome, err := json.Marshal(planbrief.PlanOutcome{Version: 1, Parent: callbacks.Parent, Generation: callbacks.Generation, RunID: receipt.RunID, Attempt: receipt.Attempt, Outcome: c.Outcome})
		if err != nil {
			return err
		}
		root, err := os.OpenRoot(prep.worktree.Path())
		if err != nil {
			return err
		}
		defer root.Close()
		if c.Outcome == "discussion" {
			if c.Context == nil {
				return fmt.Errorf("discussion context missing")
			}
			working := planbrief.WorkingContext{Version: 1, Parent: callbacks.Parent, Branch: prep.session.Branch, Feature: prep.session.FeatureSlug, Scope: c.Context.Scope, Decisions: c.Context.Decisions, Assumptions: c.Context.Assumptions, Questions: c.Context.Questions}
			data, err := json.Marshal(working)
			if err != nil {
				return err
			}
			if _, err := planbrief.ParseWorkingContext(data); err != nil {
				return err
			}
			workingBytes = slices.Clone(data)
			if err := writeTypedPlanArtifact(root, filepath.FromSlash(prep.promptInput.PlanContextPath), data); err != nil {
				return err
			}
		} else if err := writeTypedPlanArtifact(root, planbrief.ProposalFileName, proposal); err != nil {
			return err
		}
		if err := writeTypedPlanArtifact(root, planbrief.OutcomeFileName, outcome); err != nil {
			return err
		}
		prep.promptInput.PlanAttempt = receipt.Attempt
		state.receiptID = receipt.ID
		state.proposal = slices.Clone(proposal)
		state.outcome = slices.Clone(outcome)
		state.content = content
		state.working = workingBytes
		return nil
	}
	roles, _ := json.Marshal(scope.Roles)
	callbacks.Prompt = "For this typed content contract, submit Plan discussion/completion through the supplied helper. The factory creates the identity envelope and transport. Supply all typed story sections, explicit criterion IDs, and arrays (including empty arrays). Task owner_role must come from this supplied implementation roster: " + string(roles) + ". Existing stories use their supplied story keys; the factory supplies issue identities and observed base revisions. Keep plan_brief.md shared context and its link-only Story Index. A completed receipt records input only: publication and readiness remain pending until terminal success and save."
	if len(history) > 0 {
		keys := make([]string, 0, len(history))
		for key := range history {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		callbacks.Prompt += " Completed legacy dependencies are preserved by the factory: " + strings.Join(keys, ", ") + ". Omit these keys from the typed stories array; they remain valid dependency references. Do not reconstruct their obligations."
	}
	return callbacks, nil
}

func buildTypedPlanProposal(ctx context.Context, renderer CompleteStoryRenderer, prep *preparation, scope planbrief.ContentScope, bases map[string]planbrief.StoryProposal, existing []store.ManagedStory, raw []byte, historical ...map[string]planbrief.StoryProposal) (planbrief.PlanProposal, map[string]planbrief.TypedStory, error) {
	history := map[string]planbrief.StoryProposal{}
	if len(historical) > 0 {
		history = historical[0]
	}
	typed, err := parseTypedPlanWithHistory(raw, scope, history)
	if err != nil {
		return planbrief.PlanProposal{}, nil, err
	}
	bodies, err := renderTypedPlanWithHistory(typed, scope, history)
	if err != nil {
		return planbrief.PlanProposal{}, nil, err
	}
	proposal := planbrief.PlanProposal{Version: 1, Parent: prep.ref.String(), Generation: prep.promptInput.PlanGeneration, Stories: []planbrief.StoryProposal{}}
	content := map[string]planbrief.TypedStory{}
	for i, s := range typed.Stories {
		bound, exists := bases[s.Key]
		if (s.Intent == "create") == exists {
			return proposal, nil, fmt.Errorf("story %s: intent conflicts with factory-known identity", s.Key)
		}
		// Check original semantic strings before Markdown entity escaping can
		// hide a credential-shaped value from publication redaction.
		semantic, err := json.Marshal(s)
		if err != nil {
			return proposal, nil, err
		}
		checked, err := renderer.RenderCompleteStory(ctx, report.Story{Ref: prep.ref, ID: s.Key, Title: s.Title, Excerpt: string(semantic)})
		if err != nil {
			return proposal, nil, err
		}
		if checked.Title.String() != s.Title || checked.Body.String() != report.StoryMarker(prep.ref, s.Key)+"\n\n"+string(semantic) {
			return proposal, nil, fmt.Errorf("story %s: required source content would change during sanitation or redaction", s.Key)
		}
		bound.Key = s.Key
		bound.Intent = s.Intent
		bound.Title = s.Title
		bound.Body = bodies[i]
		if exists {
			bound.Body = planbrief.PreserveStoryText(bases[s.Key].Body, bound.Body)
		}
		bound.Dependencies = slices.Clone(s.Dependencies.StoryKeys)
		rendered, err := renderer.RenderCompleteStory(ctx, report.Story{Ref: prep.ref, ID: s.Key, Title: s.Title, Excerpt: bound.Body})
		if err != nil {
			return proposal, nil, err
		}
		expected := report.StoryMarker(prep.ref, s.Key) + "\n\n" + bound.Body
		if rendered.Title.String() != s.Title || rendered.Body.String() != expected {
			return proposal, nil, fmt.Errorf("story %s: publication sanitation or redaction would alter required content; revise the contribution", s.Key)
		}
		if len(expected) > planbrief.MaxStoryBodyBytes {
			return proposal, nil, fmt.Errorf("story %s: marker-inclusive body exceeds %d bytes", s.Key, planbrief.MaxStoryBodyBytes)
		}
		if s.Intent == "unchanged" {
			base := bases[s.Key]
			if base.Title != s.Title || !planbrief.TypedStoryBodyMatches(s, base.Body) {
				return proposal, nil, fmt.Errorf("story %s: unchanged intent changes issue content", s.Key)
			}
			// Preserve factory-projected checkboxes on completed dependencies.
			bound.Body = strings.TrimPrefix(base.Body, report.StoryMarker(prep.ref, s.Key)+"\n\n")
		}
		proposal.Stories = append(proposal.Stories, bound)
		content[s.Key] = s
	}
	for _, s := range existing {
		if bound, ok := history[s.Key]; ok {
			if s.State != "completed" || bound.IssueID != s.IssueID || bound.IssueNumber != s.IssueNumber {
				return proposal, nil, fmt.Errorf("historical dependency identity changed")
			}
			bound.Body = historicalBody(bound, prep.ref.String())
			proposal.Stories = append(proposal.Stories, bound)
			continue
		}
		typed, exists := content[s.Key]
		if !exists || (s.State == "completed" && typed.Intent != "unchanged") {
			return proposal, nil, fmt.Errorf("story %s: preserve indexed and completed dependencies", s.Key)
		}
	}
	data, err := json.Marshal(proposal)
	if err != nil {
		return proposal, nil, err
	}
	if len(data) > planbrief.MaxProposalBytes {
		return proposal, nil, fmt.Errorf("factory-bound Plan transport exceeds %d bytes", planbrief.MaxProposalBytes)
	}
	return proposal, content, proposal.Validate()
}

func writeTypedPlanArtifact(root *os.Root, path string, data []byte) (err error) {
	if !filepath.IsLocal(path) {
		return fmt.Errorf("typed Plan artifact escapes worktree")
	}
	parent := filepath.Dir(path)
	// Create components separately and refuse symlinks at every boundary.
	current := "."
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, e := root.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			if e = root.Mkdir(current, 0700); e != nil {
				return e
			}
			info, e = root.Lstat(current)
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe typed Plan directory")
		}
	}
	if err := checkStoryComponents(root, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := root.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(path)
	temp := ".typed-plan-" + rand.Text()
	file, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		removeErr := dir.Remove(temp)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := checkStoryComponents(root, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	currentInfo, err := root.Lstat(parent)
	if err != nil {
		return err
	}
	pinned, err := dir.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(currentInfo, pinned) {
		return fmt.Errorf("typed Plan directory changed")
	}
	return dir.Rename(temp, name)
}

func (s *TypedPlanSubmission) checkTransport(outcome, proposal []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receiptID == 0 || !bytes.Equal(s.outcome, outcome) || !bytes.Equal(s.proposal, proposal) {
		return fmt.Errorf("typed Plan transport differs from factory-recorded contribution")
	}
	return nil
}
