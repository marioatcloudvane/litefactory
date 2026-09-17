package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func (s *SubmissionSession) recordEngineering(ctx context.Context, r store.SubmissionReceipt, c agent.Contribution, story agent.ApprovedStory) error {
	var evidence *agent.StoryEvidence
	if c.Engineering != nil {
		if c.Capture != nil {
			captured, err := c.Capture.SnapshotEvidence()
			if err != nil {
				return err
			}
			snapshot := store.SubmissionSnapshot{RunID: s.binding.RunID, Attempt: s.binding.Attempt, Reference: c.Engineering.SnapshotID, Parent: s.binding.Parent, Generation: s.binding.Generation, Story: story.Key, Revision: story.Revision, Digest: captured.ImplementationDigest}
			for _, file := range captured.Files {
				snapshot.Files = append(snapshot.Files, store.SubmissionSnapshotFile{Path: file.Path, Content: file.Content, SHA256: file.SHA256, Deleted: file.Deleted})
			}
			if err := s.options.DB.RetainSubmissionSnapshot(ctx, snapshot); err != nil {
				return err
			}
			for i, check := range c.Capture.Checks {
				result, err := json.Marshal(captured.Tests[i])
				if err != nil {
					return err
				}
				provenance, err := json.Marshal(struct {
					Source, Attempt, CheckID string
					RunID                    int64
				}{"agent-reported executed check; not independently observed", s.binding.Attempt, check.ID, s.binding.RunID})
				if err != nil {
					return err
				}
				if err := s.options.DB.RetainSubmissionCheck(ctx, store.SubmissionCheck{RunID: s.binding.RunID, Attempt: s.binding.Attempt, Reference: check.ID, SnapshotRef: snapshot.Reference, Name: check.Name, Executed: true, Passed: check.Passed, Provenance: provenance, Result: result}); err != nil {
					return err
				}
			}
		}
		snapshot, err := s.options.DB.GetSubmissionSnapshot(ctx, s.binding.RunID, c.Engineering.SnapshotID)
		if err != nil {
			return err
		}
		evidence = &agent.StoryEvidence{ImplementationDigest: snapshot.Digest, Acceptance: []agent.StoryCheck{}, Review: []agent.StoryCheck{}, Tests: []agent.StoryCheck{}}
		for _, file := range snapshot.Files {
			evidence.Files = append(evidence.Files, agent.StorySnapshotFile{Path: file.Path, Content: file.Content, SHA256: file.SHA256, Deleted: file.Deleted})
		}
		for _, id := range c.Engineering.CheckIDs {
			check, err := s.options.DB.GetSubmissionCheck(ctx, s.binding.RunID, snapshot.Reference, id)
			if err != nil {
				return err
			}
			var fact agent.StoryCheck
			if err := json.Unmarshal(check.Result, &fact); err != nil {
				return err
			}
			evidence.Tests = append(evidence.Tests, fact)
		}
		if c.Assessment != nil {
			for _, f := range c.Assessment.Criteria {
				evidence.Acceptance = append(evidence.Acceptance, legacyContributionCheck(f, evidence.ImplementationDigest, r.ID))
			}
			for _, f := range c.Assessment.Reviews {
				evidence.Review = append(evidence.Review, legacyContributionCheck(f, evidence.ImplementationDigest, r.ID))
			}
		}
	}
	event := agent.StoryEvent{Version: 1, Parent: s.binding.Parent, RunID: s.binding.RunID, Generation: s.binding.Generation, Story: story.Key, IssueID: story.IssueID, Revision: story.Revision, Attempt: r.StoryAttempt, Sequence: r.Sequence, Kind: c.Outcome, Reason: c.Reason, Evidence: evidence}
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	effect := store.StoryProgressEffect{RunID: event.RunID, Sequence: event.Sequence, Parent: event.Parent, Generation: event.Generation, Story: story.Key, IssueNumber: story.IssueNumber, IssueID: story.IssueID, ParentID: story.ParentID, Revision: story.Revision, Kind: event.Kind, Detail: c.Reason}
	if evidence != nil {
		effect.ImplementationDigest = evidence.ImplementationDigest
		effect.Detail = "Factory-bound contribution retained with complete snapshot bytes and agent-reported executed scoped checks. Acceptance and remote issue effects are reported separately."
	}
	return s.options.DB.CommitSubmissionProgress(ctx, r.ID, store.StoryProgressEvent{RunID: event.RunID, Sequence: event.Sequence, Story: event.Story, Attempt: event.Attempt, Kind: event.Kind, Digest: agent.StoryDigest(payload), Payload: payload}, effect)
}
func legacyContributionCheck(f agent.ContributionFinding, digest string, receiptID int64) agent.StoryCheck {
	// The authoritative list remains verbatim in the immutable receipt. Refer to
	// it from the compatibility event instead of truncating 16 full explanations
	// into a legacy field whose old string budget is smaller.
	evidence := fmt.Sprintf("Submission receipt %d retains %d attributed evidence explanations for %s.", receiptID, len(f.Evidence), f.Name)
	return agent.StoryCheck{Name: f.Name, Passed: f.Status == "satisfied", Evidence: evidence, ImplementationDigest: digest}
}

func (s *SubmissionSession) recordAssessment(ctx context.Context, r store.SubmissionReceipt, c agent.Contribution, story agent.ApprovedStory) error {
	findings := store.AcceptanceFindings{}
	for _, f := range c.Assessment.Criteria {
		entry, err := retainedListFinding(f)
		if err != nil {
			return err
		}
		findings.Criteria = append(findings.Criteria, entry)
	}
	for _, f := range c.Assessment.Reviews {
		entry, err := retainedListFinding(f)
		if err != nil {
			return err
		}
		findings.Reviews = append(findings.Reviews, entry)
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		return err
	}
	decision := store.AcceptanceDecision{RunID: r.RunID, Parent: s.binding.Parent, Generation: s.binding.Generation, Story: story.Key, IssueNumber: story.IssueNumber, IssueID: story.IssueID, ParentID: story.ParentID, Revision: story.Revision, Snapshot: c.Assessment.SnapshotID, FrozenBody: story.Body, FindingsJSON: string(encoded), Accepted: findings.Accepted()}
	return s.options.DB.CommitSubmissionAssessment(ctx, r.ID, decision)
}
func retainedListFinding(f agent.ContributionFinding) (store.AcceptanceFinding, error) {
	// Keep the exact list, including check IDs, inside the existing retained
	// presentation field. No attribution is lost through prose concatenation.
	evidence, err := json.Marshal(f.Evidence)
	return store.AcceptanceFinding{Name: f.Name, Status: f.Status, Classification: f.Classification, Evidence: string(evidence)}, err
}

func storyAcceptanceCoveredFrozen(ctx context.Context, db *store.DB, parent string, generation int, story, revision, body string, checks []agent.StoryCheck) error {
	required, err := storyCriterionIDs(ctx, db, parent, generation, story, revision, body)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, check := range checks {
		names[check.Name] = true
	}
	for _, name := range required {
		if !names[name] {
			return fmt.Errorf("missing scoped evidence for %s", name)
		}
	}
	return nil
}

// verifyEngineeringSnapshot applies the same current-byte boundary to uploaded
// captures and retained references. Reusing a reference cannot certify later edits.
func (s *SubmissionSession) verifyEngineeringSnapshot(ctx context.Context, c agent.Contribution) error {
	var evidence agent.StoryEvidence
	if c.Capture != nil {
		var err error
		evidence, err = c.Capture.SnapshotEvidence()
		if err != nil {
			return err
		}
	} else {
		snapshot, err := s.options.DB.GetSubmissionSnapshot(ctx, s.binding.RunID, c.Engineering.SnapshotID)
		if err != nil {
			return err
		}
		evidence = agent.StoryEvidence{ImplementationDigest: snapshot.Digest}
		for _, file := range snapshot.Files {
			evidence.Files = append(evidence.Files, agent.StorySnapshotFile{Path: file.Path, Content: file.Content, SHA256: file.SHA256, Deleted: file.Deleted})
		}
	}
	root, err := os.OpenRoot(s.options.Worktree)
	if err != nil {
		return err
	}
	return errors.Join(verifyStorySnapshot(root, evidence), root.Close())
}
