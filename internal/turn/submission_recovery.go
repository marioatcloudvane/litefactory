package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// SubmissionRecoveryError identifies exactly which run, attempt, receipt and
// phase a recovery fault originated from, plus the story when the receipt
// carries one (plan-stage receipts do not). Every fault raised while
// recovering a pending receipt is wrapped in one of these before it is
// joined into a caller's error, so:
//   - a foreground caller (scoped to one run/attempt) can trust that any
//     joined fault genuinely belongs to it, never to an unrelated run
//     (AC-003) — the scoped store query already guarantees this by
//     construction, this type makes it inspectable too;
//   - the daemon-wide maintenance sweep's logged error still names the
//     originating run, attempt, receipt, phase and story even though it
//     processes every run together (AC-004).
type SubmissionRecoveryError struct {
	RunID     int64
	Attempt   string
	ReceiptID int64
	Story     string
	Phase     string
	Err       error
}

func (e *SubmissionRecoveryError) Error() string {
	if e.Story != "" {
		return fmt.Sprintf("recover submission run %d attempt %s receipt %d story %s (%s): %v", e.RunID, e.Attempt, e.ReceiptID, e.Story, e.Phase, e.Err)
	}
	return fmt.Sprintf("recover submission run %d attempt %s receipt %d (%s): %v", e.RunID, e.Attempt, e.ReceiptID, e.Phase, e.Err)
}

func (e *SubmissionRecoveryError) Unwrap() error { return e.Err }

// OwnedBy reports whether this fault belongs to the given run and attempt,
// so a foreground caller can confirm a joined recovery error is genuinely
// its own without depending on string matching.
func (e *SubmissionRecoveryError) OwnedBy(runID int64, attempt string) bool {
	return e.RunID == runID && e.Attempt == attempt
}

// RecoverSubmissions is called by the daemon-wide maintenance sweep before
// progress delivery, across every run's pending receipts. This discovery is
// intentionally global (AC-004): a broken receipt belonging to one run must
// not prevent reconciliation of another, healthy run's receipts in the same
// sweep. It has no clock or retry loop; obsolete authority is retired
// without acquiring proof.
func (p *StoryProgress) RecoverSubmissions(ctx context.Context) error {
	return p.recoverSubmissions(ctx, nil)
}

// RecoverSubmissionsForAttempt scopes recovery to exactly one trusted run and
// attempt. Every foreground call site (submission checkpoints, session
// Close, terminal Validate intake) uses this instead of RecoverSubmissions,
// so a broken pending receipt belonging to an unrelated run/attempt can
// never be discovered, processed, or surfaced as this call's error (AC-003).
// Genuine failures belonging to runID/attempt itself still surface normally.
func (p *StoryProgress) RecoverSubmissionsForAttempt(ctx context.Context, runID int64, attempt string) error {
	if runID < 1 || attempt == "" {
		return fmt.Errorf("store: invalid submission recovery scope")
	}
	return p.recoverSubmissions(ctx, &submissionRecoveryScope{runID: runID, attempt: attempt})
}

// submissionRecoveryScope narrows RecoverSubmissions to one trusted run and
// attempt. A nil scope keeps the daemon-wide sweep's global behavior.
type submissionRecoveryScope struct {
	runID   int64
	attempt string
}

func (p *StoryProgress) recoverSubmissions(ctx context.Context, scope *submissionRecoveryScope) error {
	if scope == nil {
		// A heartbeat must not wait for foreground reconciliation.
		if !p.mu.TryLock() {
			return nil
		}
	} else {
		p.mu.Lock()
	}
	defer p.mu.Unlock()
	var pending []store.SubmissionReceipt
	var err error
	if scope != nil {
		pending, err = p.db.ListPendingSubmissionsForAttempt(ctx, scope.runID, scope.attempt)
	} else {
		pending, err = p.db.ListPendingSubmissions(ctx)
	}
	if err != nil {
		return err
	}
	var faults []error
	for _, r := range pending {
		if r.EngineeringState != "pending" || r.AcceptanceState != "pending" {
			continue
		}
		if fault := p.recoverOne(ctx, r); fault != nil {
			faults = append(faults, fault)
		}
	}
	return errors.Join(faults...)
}

// recoverOne recovers exactly one pending receipt. Every fault is wrapped in
// a *SubmissionRecoveryError naming the run, attempt, receipt, story and
// phase it came from (AC-004), whether this call arrived through the scoped
// foreground path or the global daemon-wide sweep.
func (p *StoryProgress) recoverOne(ctx context.Context, r store.SubmissionReceipt) error {
	wrap := func(phase string, err error) error {
		if err == nil {
			return nil
		}
		return &SubmissionRecoveryError{RunID: r.RunID, Attempt: r.Attempt, ReceiptID: r.ID, Story: r.Story, Phase: phase, Err: err}
	}
	recovery, err := p.db.GetSubmissionRecovery(ctx, r.RunID, r.Attempt)
	if err != nil {
		return wrap("load recovery binding", err)
	}
	release, err := p.db.LockStoryProgress(ctx, recovery.Binding.Parent, p.lockDir)
	if err != nil {
		return wrap("lock story progress", err)
	}
	var recoverErr error
	if !recovery.Active {
		recoverErr = p.db.AbandonSubmission(ctx, r.ID)
	} else if recovery.Binding.Stage != "plan" {
		var session *SubmissionSession
		session, recoverErr = p.recoverySession(ctx, recovery)
		if recoverErr == nil {
			recoverErr = session.recoverReceipt(ctx, r)
		}
	}
	return errors.Join(wrap("recover receipt", recoverErr), wrap("release story progress lock", release()))
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
		criteria, required, err := storyCriteria(ctx, p.db, binding.Parent, binding.Generation, r.Key, r.Hash, r.Body)
		if err != nil {
			return nil, err
		}
		source := agent.ContributionStory{Revision: r.Hash, Criteria: criteria, ExecutionRequired: required, Checks: map[string]agent.RetainedContributionCheck{}}
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
