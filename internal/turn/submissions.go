package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// PlanSubmissionCallbacks adapts the Plan owner to the shared broker without
// importing publication logic into the neutral contribution package.
type PlanSubmissionCallbacks struct {
	Parent     string
	Generation int
	Existing   []store.SubmissionStoryBinding
	Validate   func(context.Context, json.RawMessage) agent.ContributionDiagnostics
	Submit     func(context.Context, store.SubmissionReceipt, agent.Contribution) error
	Prompt     string
}

// SubmissionSessionOptions are factory-bound inputs for either runner adapter.
type SubmissionSessionOptions struct {
	Plan       *PlanSubmissionCallbacks
	DB         *store.DB
	Progress   *StoryProgress
	Paths      config.Paths
	RunID      int64
	Worktree   string
	Bundle     *agent.ApprovedBundle
	Validation *agent.ValidationContext
}

// SubmissionSession owns one attempt's broker and immutable authorizations.
type SubmissionSession struct {
	options SubmissionSessionOptions
	binding store.SubmissionAttempt
	broker  *submission.Broker
	scope   agent.ContributionScope
	stories map[string]agent.ApprovedStory
	mu      sync.Mutex
}

// StartSubmissionSession selects only the persisted new contract. Both adapter
// paths call this at each actual launch, including resumed and fallback attempts.
func StartSubmissionSession(ctx context.Context, o SubmissionSessionOptions) (*SubmissionSession, error) {
	if o.DB == nil {
		return nil, fmt.Errorf("submission database missing")
	}
	parent := ""
	generation := 0
	if o.Bundle != nil {
		parent = o.Bundle.Parent
		generation = o.Bundle.Generation
	}
	if o.Validation != nil {
		parent = o.Validation.Parent
		generation = o.Validation.Generation
	}
	if o.Plan != nil {
		parent = o.Plan.Parent
		generation = o.Plan.Generation
	}
	if parent == "" {
		return nil, nil
	}
	contract, err := o.DB.GetGenerationContract(ctx, parent, generation)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if contract.SubmissionVersion != 2 {
		return nil, nil
	}
	feature, err := o.DB.GetManagedFeature(ctx, parent)
	if err != nil {
		return nil, err
	}
	if o.Progress == nil && o.Plan == nil {
		return nil, fmt.Errorf("submission progress service missing")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	stage := "engineering"
	if feature.WorkflowVersion == 1 {
		stage = "legacy"
	}
	if o.Validation != nil {
		stage = "validate"
	}
	if o.Plan != nil {
		stage = "plan"
		if o.Plan.Validate == nil || o.Plan.Submit == nil {
			return nil, fmt.Errorf("plan submission callbacks missing")
		}
	}
	s := &SubmissionSession{options: o, binding: store.SubmissionAttempt{RunID: o.RunID, Attempt: hex.EncodeToString(random[:]), Parent: parent, Generation: generation, Stage: stage}, scope: agent.ContributionScope{ContractVersion: 2, WorkflowVersion: feature.WorkflowVersion, Stage: stage, Stories: map[string]agent.ContributionStory{}}, stories: map[string]agent.ApprovedStory{}}
	var selected []agent.ApprovedStory
	if o.Bundle != nil {
		for _, story := range o.Bundle.Stories {
			if !story.EngineeringComplete && story.State != "completed" {
				selected = append(selected, story)
			}
		}
	}
	if o.Validation != nil {
		for _, v := range o.Validation.Stories {
			selected = append(selected, v.Story)
		}
	}
	if o.Plan != nil {
		s.binding.Stories = slices.Clone(o.Plan.Existing)
	}
	if len(selected) == 0 && o.Plan == nil && (o.Bundle == nil || len(o.Bundle.Stories) == 0) {
		return nil, nil
	}
	for _, story := range selected {
		criteria, err := storyCriterionIDs(ctx, o.DB, parent, generation, story.Key, story.Revision, story.Body)
		if err != nil {
			return nil, err
		}
		s.stories[story.Key] = story
		s.binding.Stories = append(s.binding.Stories, store.SubmissionStoryBinding{Story: story.Key, IssueID: story.IssueID, Revision: story.Revision})
		s.scope.Stories[story.Key] = agent.ContributionStory{Revision: story.Revision, Criteria: criteria, ExecutionRequired: slices.Clone(criteria), Checks: map[string]agent.RetainedContributionCheck{}}
	}
	if o.Validation != nil {
		for _, v := range o.Validation.Stories {
			source := s.scope.Stories[v.Story.Key]
			source.SnapshotID = v.Snapshot
			if v.Evidence != nil {
				source.SnapshotRetained = true
				source.ImplementationDigest = v.Evidence.ImplementationDigest
				for _, check := range v.Evidence.Tests {
					source.Checks[check.Name] = agent.RetainedContributionCheck{Story: v.Story.Key, Revision: v.Story.Revision, SnapshotID: v.Snapshot, ImplementationDigest: check.ImplementationDigest, Executed: true, Passed: check.Passed, Retained: true}
				}
			}
			s.scope.Stories[v.Story.Key] = source
		}
	}
	if err := o.DB.BindSubmissionAttempt(ctx, s.binding); err != nil {
		return nil, err
	}
	dir := o.Paths.SubmissionDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(s.stories))
	for key := range s.stories {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	s.broker, err = submission.Start(ctx, submission.Options{Directory: dir, AuthorizedStories: keys, AllowPlan: o.Plan != nil, AllowResult: len(keys) == 0, MaxWorkers: 4, RequestTimeout: 30 * time.Second, Handler: s.handle})
	if err != nil {
		endErr := o.DB.EndSubmissionAttempt(context.WithoutCancel(ctx), s.binding.RunID, s.binding.Attempt)
		return nil, errors.Join(err, endErr)
	}
	return s, nil
}

// DescriptorPath names the fresh broker for this actual runner invocation.
func (s *SubmissionSession) DescriptorPath() string { return s.broker.DescriptorPath() }

// Close joins transport before revoking authority; pending receipts are retained.
func (s *SubmissionSession) Close(ctx context.Context) error {
	closeErr := s.broker.Close()
	var recoveryErr error
	if s.options.Progress != nil {
		recoveryErr = s.options.Progress.RecoverSubmissions(ctx)
	}
	return errors.Join(closeErr, recoveryErr, s.options.DB.EndSubmissionAttempt(ctx, s.binding.RunID, s.binding.Attempt))
}

func (s *SubmissionSession) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	return s.checkpoint(ctx, q)
}

// checkpoint is shared by the helper and family facade. It owns the session
// lock, followed by the existing progress and cross-process parent ownership.
func (s *SubmissionSession) checkpoint(ctx context.Context, q submission.Request) (output json.RawMessage, err error) {
	return s.checkpointWithTerminal(ctx, q, false)
}

func (s *SubmissionSession) checkpointWithTerminal(ctx context.Context, q submission.Request, terminal bool) (output json.RawMessage, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpointLocked(ctx, q, terminal)
}

// checkpointLocked requires the session lock, including whole terminal batches.
func (s *SubmissionSession) checkpointLocked(ctx context.Context, q submission.Request, terminal bool) (output json.RawMessage, err error) {
	if q.Method == "receipt" {
		receipt, err := s.options.DB.GetSubmissionReceipt(ctx, s.binding.RunID, q.RequestID)
		if err != nil {
			return nil, err
		}
		if receipt.Attempt != s.binding.Attempt {
			return nil, fmt.Errorf("receipt belongs to an earlier attempt")
		}
		return json.Marshal(receipt)
	}
	scope := s.scopeForContribution(ctx, q.Payload)
	contribution, diagnostics := agent.ParseContribution(q.Payload, scope)
	if diagnostics.Valid && contribution.Engineering != nil {
		if err := s.verifyEngineeringSnapshot(ctx, contribution); err != nil {
			diagnostics.Valid = false
			diagnostics.Diagnostics = append(diagnostics.Diagnostics, agent.ContributionDiagnostic{Path: "$.engineering.snapshot_id", Code: "snapshot", Expected: "current stable worktree bytes", Actual: "mismatch", Message: "Capture the exact current files after executed checks; no evidence was retained."})
		}
	}
	if diagnostics.Valid && s.binding.Stage != "validate" && s.binding.Stage != "plan" && contribution.Outcome != "invalidated" {
		current, readErr := s.options.DB.ListManagedStories(ctx, s.binding.Parent, s.binding.Generation)
		if readErr != nil {
			return nil, readErr
		}
		for _, story := range current {
			if story.Key == contribution.Story && story.EngineeringComplete {
				diagnostics.Valid = false
				diagnostics.Diagnostics = append(diagnostics.Diagnostics, agent.ContributionDiagnostic{Path: "$.outcome", Code: "engineering_complete", Expected: "invalidation before rework", Actual: "already engineered", Message: "Retain the previous completion or explicitly invalidate affected unaccepted engineering before rework."})
			}
		}
	}
	if q.Method == "preflight" {
		return json.Marshal(diagnostics)
	}
	if q.Method != "submit" {
		return nil, fmt.Errorf("unsupported submission method")
	}
	if s.options.Plan != nil {
		return s.submitPlan(ctx, q, contribution, diagnostics)
	}
	// Scope comes from the broker selector and frozen factory map, never a copied
	// parent/run/issue/revision field inside contributed JSON.
	var selector struct {
		Story string `json:"story"`
	}
	if err := json.Unmarshal(q.Payload, &selector); err != nil {
		return json.Marshal(diagnostics)
	}
	story, ok := s.stories[selector.Story]
	if !ok {
		return json.Marshal(diagnostics)
	}
	encodedDiagnostics, err := json.Marshal(diagnostics)
	if err != nil {
		return nil, err
	}
	inputState := "valid"
	if !diagnostics.Valid {
		inputState = "rejected"
	}
	p := s.options.Progress
	if err := p.RecoverSubmissions(ctx); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	release, err := p.db.LockStoryProgress(ctx, s.binding.Parent, p.lockDir)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()
	if terminal && diagnostics.Valid && s.binding.Stage == "validate" {
		receipt, found, err := s.reconcileValidationCheckpoint(ctx, contribution)
		if err != nil {
			return nil, err
		}
		if found {
			return json.Marshal(receipt)
		}
	}
	receipt, err := s.options.DB.AllocateSubmission(ctx, store.SubmissionRequest{RunID: s.binding.RunID, Attempt: s.binding.Attempt, RequestID: q.RequestID, Story: story.Key, Revision: story.Revision, InputState: inputState, Payload: q.Payload, Diagnostics: encodedDiagnostics})
	if err != nil {
		return nil, err
	}
	if receipt.EngineeringState != "pending" || receipt.AcceptanceState != "pending" {
		return json.Marshal(receipt)
	}
	switch {
	case !diagnostics.Valid:
		if s.binding.Stage != "validate" {
			err = s.options.DB.RejectSubmissionProgress(ctx, receipt.ID)
		} else {
			err = s.options.DB.DecideSubmission(ctx, receipt.ID, store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "rejected", Effects: "not_applicable", Plan: "not_applicable"})
		}
	case s.binding.Stage == "validate":
		err = s.recordAssessment(ctx, receipt, contribution, story)
	default:
		err = s.recordEngineering(ctx, receipt, contribution, story)
	}
	if err != nil {
		return nil, err
	}
	receipt, err = s.options.DB.GetSubmissionReceipt(ctx, s.binding.RunID, q.RequestID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(receipt)
}

func (s *SubmissionSession) scopeForContribution(ctx context.Context, payload json.RawMessage) agent.ContributionScope {
	scope := s.scope
	if s.options.Plan != nil {
		scope.ValidatePlan = func(raw json.RawMessage) agent.ContributionDiagnostics { return s.options.Plan.Validate(ctx, raw) }
	}
	scope.Stories = make(map[string]agent.ContributionStory, len(s.scope.Stories))
	for key, value := range s.scope.Stories {
		scope.Stories[key] = value
	}
	var input agent.Contribution
	// Full strict validation below owns errors; this initial decode only locates
	// previously retained references without acquiring authority.
	if err := json.Unmarshal(payload, &input); err == nil && input.Engineering != nil && input.Capture == nil {
		if source, ok := scope.Stories[input.Story]; ok {
			snapshot, err := s.options.DB.GetSubmissionSnapshot(ctx, s.binding.RunID, input.Engineering.SnapshotID)
			if err == nil && snapshot.Story == input.Story && snapshot.Revision == source.Revision {
				source.SnapshotID = snapshot.Reference
				source.ImplementationDigest = snapshot.Digest
				source.SnapshotRetained = true
				source.Checks = map[string]agent.RetainedContributionCheck{}
				for _, id := range input.Engineering.CheckIDs {
					check, err := s.options.DB.GetSubmissionCheck(ctx, s.binding.RunID, id)
					if err == nil {
						source.Checks[id] = agent.RetainedContributionCheck{Story: input.Story, Revision: snapshot.Revision, SnapshotID: check.SnapshotRef, ImplementationDigest: snapshot.Digest, Executed: check.Executed, Passed: check.Passed, Retained: true}
					}
				}
				scope.Stories[input.Story] = source
			}
		}
	}

	return scope
}

func (s *SubmissionSession) submitPlan(ctx context.Context, q submission.Request, c agent.Contribution, d agent.ContributionDiagnostics) (json.RawMessage, error) {
	encoded, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	state := "valid"
	if !d.Valid {
		state = "rejected"
	}
	receipt, err := s.options.DB.AllocateSubmission(ctx, store.SubmissionRequest{RunID: s.binding.RunID, Attempt: s.binding.Attempt, RequestID: q.RequestID, InputState: state, Payload: q.Payload, Diagnostics: encoded})
	if err != nil {
		return nil, err
	}
	if receipt.EngineeringState != "pending" {
		return json.Marshal(receipt)
	}
	decision := store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "not_applicable", Effects: "not_applicable", Plan: "rejected"}
	if d.Valid {
		if err := s.options.Plan.Submit(ctx, receipt, c); err != nil {
			return nil, err
		}
		decision.Plan = "pending"
		if c.Outcome == "discussion" {
			decision.Plan = "discussion"
		}
	}
	if err := s.options.DB.DecideSubmission(ctx, receipt.ID, decision); err != nil {
		return nil, err
	}
	receipt, err = s.options.DB.GetSubmissionReceipt(ctx, s.binding.RunID, q.RequestID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(receipt)
}
