package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func prepareValidationContext(ctx context.Context, opts Options, in *harness.PromptInput) error {
	f, err := opts.DB.GetManagedFeature(ctx, in.Ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.WorkflowVersion != store.WorkflowSeparated || f.State != "implementing" {
		return fmt.Errorf("managed Validate requires approved separated workflow")
	}
	revisions, err := opts.DB.ListApprovedStoryRevisions(ctx, f.ParentTask, f.Generation)
	if err != nil {
		return err
	}
	v := &agent.ValidationContext{Version: 1, Parent: f.ParentTask, Generation: f.Generation}
	for _, r := range revisions {
		if r.State == "completed" {
			v.Dependencies = append(v.Dependencies, agent.CompletedStory{Key: r.Key, IssueNumber: r.IssueNumber, IssueID: r.IssueID})
			continue
		}
		criteria, err := planbrief.AcceptanceCriteria(r.Body)
		if err != nil {
			return fmt.Errorf("story %s criteria: %w", r.Key, err)
		}
		s := agent.ValidationStory{Story: agent.ApprovedStory{EngineeringComplete: r.EngineeringComplete, State: r.State, Key: r.Key, IssueNumber: r.IssueNumber, IssueID: r.IssueID, ParentID: r.ParentID, Source: fmt.Sprintf("https://github.com/%s/%s/issues/%d", in.Ref.Owner, in.Ref.Repo, r.IssueNumber), Title: r.Title, Body: r.Body, Revision: r.Hash, FetchedAt: r.FetchedAt.UTC().Format(time.RFC3339Nano), Dependencies: slices.Clone(r.Dependencies)}, Criteria: criteria, EvidenceStatus: "unverified", EvidenceReason: "No retained initial engineering evidence"}
		retained, err := opts.DB.EngineeringEvidence(ctx, f.ParentTask, f.Generation, r.Key)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			attachValidationEvidence(&s, retained, f)
		}
		v.Stories = append(v.Stories, s)
	}
	if len(v.Stories) == 0 {
		return fmt.Errorf("managed Validate has no unaccepted frozen stories")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > agent.MaxStoryRunBytes {
		return fmt.Errorf("retained validation context exceeds limit")
	}
	in.Validation = v.Clone()
	return nil
}

func attachValidationEvidence(s *agent.ValidationStory, retained store.StoryProgressEvent, f store.ManagedFeature) {
	e, err := agent.ParseRetainedEngineering(retained.Payload)
	if err != nil {
		s.EvidenceReason = "Malformed or stale retained engineering evidence"
		return
	}
	if e.RunID != retained.RunID || e.Sequence != retained.Sequence || e.Parent != f.ParentTask || e.Generation != f.Generation || e.Story != s.Story.Key || e.IssueID != s.Story.IssueID || e.Revision != s.Story.Revision || e.Kind != "engineering_complete" || e.Evidence == nil || retained.Digest != agent.StoryDigest(retained.Payload) {
		s.EvidenceReason = "Incorrect retained evidence identity or revision"
		return
	}
	s.Snapshot = e.Evidence.ImplementationDigest
	s.Evidence = e.Evidence
	s.EvidenceStatus = "satisfied"
	s.EvidenceReason = ""
	for _, check := range e.Evidence.Tests {
		if !check.Passed {
			s.EvidenceStatus = "failed"
			s.EvidenceReason = check.Name + ": " + check.Evidence
			return
		}
	}
}

// harvestValidation is called before run success is persisted. No result file,
// fresh checks, GitHub reads or mutation occurs on this boundary.
func harvestValidation(ctx context.Context, opts Options, prep *preparation, result *agent.RunResult) *agent.RunResult {
	if result == nil || !result.Succeeded() || prep.promptInput.Validation == nil {
		return result
	}
	result = result.Clone()
	scope := prep.promptInput.Validation.Clone()
	output, err := agent.ParseValidationOutput([]byte(result.ResultText), scope)
	if err == nil {
		var input, body []byte
		input, err = json.Marshal(scope)
		if err == nil {
			body, err = json.Marshal(output)
		}
		if err == nil {
			assessment := store.ValidationAssessment{RunID: prep.run.ID, Parent: scope.Parent, Generation: scope.Generation, ContextJSON: string(input), ResultJSON: string(body)}
			err = opts.DB.RetainValidationAssessment(ctx, assessment)
			if err == nil {
				err = retainAcceptance(ctx, opts.DB, assessment)
			}
		}
	}
	if err != nil {
		result.Outcome = agent.OutcomeFailed
		result.Reason = "Validation output rejected: " + err.Error()
		// The reviewer's own text is kept rather than replaced by the
		// rejection. Overwriting it put the parser's one-line error where the
		// assessment should have been and left the report saying no output was
		// captured, so a rejected run read as a run that did nothing — with the
		// work it did do reachable only by opening the local NNNN.jsonl. Reason
		// carries the error; the callout above the result already shows it.
		result.Diagnostic = result.Reason
		return result
	}
	var report strings.Builder
	report.WriteString("Validation assessment and attributed decisions retained. The factory projects checks and closes accepted stories through its recoverable delivery path.\n")
	for _, s := range output.Stories {
		for _, group := range [][]agent.ValidationFinding{s.Criteria, s.Reviews} {
			for _, f := range group {
				fmt.Fprintf(&report, "\n%s %s: %s (%s) — %s", s.Key, f.Name, f.Status, f.Classification, f.Evidence)
			}
		}
	}
	if suggestion := output.ImplementSuggestion(); suggestion != "" {
		report.WriteString("\n\n" + suggestion)
	}
	result.ResultText = report.String()
	return result
}
