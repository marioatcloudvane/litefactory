package turn

import (
	"context"
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// RecoverSubmissions is called by daemon ticks before progress delivery. It has
// no clock or retry loop; obsolete authority is retired without acquiring proof.
func (p *StoryProgress) RecoverSubmissions(ctx context.Context) (err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pending, err := p.db.ListPendingSubmissions(ctx)
	if err != nil {
		return err
	}
	var faults []error
	for _, r := range pending {
		if r.EngineeringState != "pending" || r.AcceptanceState != "pending" {
			continue
		}
		recovery, err := p.db.GetSubmissionRecovery(ctx, r.RunID, r.Attempt)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		release, err := p.db.LockStoryProgress(ctx, recovery.Binding.Parent, p.lockDir)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if !recovery.Active {
			err = p.db.AbandonSubmission(ctx, r.ID)
		} else if recovery.Binding.Stage != "plan" {
			var session *SubmissionSession
			session, err = p.recoverySession(ctx, recovery)
			if err == nil {
				err = session.recoverReceipt(ctx, r)
			}
		}
		faults = append(faults, errors.Join(err, release()))
	}
	return errors.Join(faults...)
}
func (p *StoryProgress) recoverySession(ctx context.Context, recovery store.SubmissionRecovery) (*SubmissionSession, error) {
	binding := recovery.Binding
	feature, err := p.db.GetManagedFeature(ctx, binding.Parent)
	if err != nil {
		return nil, err
	}
	revisions, err := p.db.ListApprovedStoryRevisions(ctx, binding.Parent, binding.Generation)
	if err != nil {
		return nil, err
	}
	s := &SubmissionSession{binding: binding, options: SubmissionSessionOptions{DB: p.db, Progress: p, Worktree: recovery.Worktree}, scope: agent.ContributionScope{ContractVersion: 2, WorkflowVersion: feature.WorkflowVersion, Stage: binding.Stage, Stories: map[string]agent.ContributionStory{}}, stories: map[string]agent.ApprovedStory{}}
	for _, r := range revisions {
		if !slices.ContainsFunc(binding.Stories, func(b store.SubmissionStoryBinding) bool {
			return b.Story == r.Key && b.Revision == r.Hash && b.IssueID == r.IssueID
		}) {
			continue
		}
		story := agent.ApprovedStory{Key: r.Key, IssueID: r.IssueID, IssueNumber: r.IssueNumber, ParentID: r.ParentID, Revision: r.Hash, Body: r.Body, Title: r.Title}
		s.stories[r.Key] = story
		criteria, err := storyCriterionIDs(ctx, p.db, binding.Parent, binding.Generation, r.Key, r.Hash, r.Body)
		if err != nil {
			return nil, err
		}
		source := agent.ContributionStory{Revision: r.Hash, Criteria: criteria, ExecutionRequired: slices.Clone(criteria), Checks: map[string]agent.RetainedContributionCheck{}}
		if binding.Stage == "validate" {
			event, err := p.db.EngineeringEvidence(ctx, binding.Parent, binding.Generation, r.Key)
			if err == nil {
				v := agent.ValidationStory{Story: story}
				attachValidationEvidence(&v, event, feature)
				if v.Evidence != nil {
					source.SnapshotID = v.Snapshot
					source.SnapshotRetained = true
					source.ImplementationDigest = v.Evidence.ImplementationDigest
					for _, check := range v.Evidence.Tests {
						source.Checks[check.Name] = agent.RetainedContributionCheck{Story: r.Key, Revision: r.Hash, SnapshotID: v.Snapshot, ImplementationDigest: check.ImplementationDigest, Executed: true, Passed: check.Passed, Retained: true}
					}
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
		}
		s.scope.Stories[r.Key] = source
	}
	return s, nil
}
func (s *SubmissionSession) recoverReceipt(ctx context.Context, r store.SubmissionReceipt) error {
	if r.InputState == "rejected" {
		if s.binding.Stage == "validate" {
			return s.options.DB.AbandonSubmission(ctx, r.ID)
		}
		return s.options.DB.RejectSubmissionProgress(ctx, r.ID)
	}
	contribution, diagnostics := agent.ParseContribution(r.Payload, s.scopeForContribution(ctx, r.Payload))
	if !diagnostics.Valid {
		return s.options.DB.AbandonSubmission(ctx, r.ID)
	}
	story, ok := s.stories[contribution.Story]
	if !ok {
		return s.options.DB.AbandonSubmission(ctx, r.ID)
	}
	if contribution.Engineering != nil {
		if err := s.verifyEngineeringSnapshot(ctx, contribution); err != nil {
			return s.options.DB.AbandonSubmission(ctx, r.ID)
		}
	}
	if s.binding.Stage == "validate" {
		return s.recordAssessment(ctx, r, contribution, story)
	}
	return s.recordEngineering(ctx, r, contribution, story)
}
