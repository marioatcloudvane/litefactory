package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// A suite whose coverage inventory is larger than the saved-output bound must
// still hand its result to the next step: the saved result stays within the
// bound it is read with, and a result saved before that bound existed is still
// read, with the full assessment reloaded from its evidence (#1268).
func TestValidateSuiteLargeAssessmentStaysReadableByNextStep(t *testing.T) {
	f := newFixture(t, nil)
	b, _ := coverageGuardFixture(t)
	b.DB = f.db
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "bound", SourceKey: "request", Policy: "full", Commit: suiteCommit})
	mustProgress(t, err)
	b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "bound", Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	files := make([]string, 0, 60000)
	for i := range cap(files) {
		files = append(files, fmt.Sprintf("internal/pkg%03d/file_%05d.go", i%500, i))
	}
	discover := b.Coverage.Discover
	b.Coverage.Discover = func(ctx context.Context, s ValidateSuiteScratch, c []config.BugCheckCommand, r coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		inv, facts, err := discover(ctx, s, c, r)
		inv.Units[0].Files = files
		return inv, facts, err
	}

	profile := captureValidate(t).Profile
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "validate", ProfileVersion: profile.ProfileVersion, Path: "validate/validate.dot", Digest: profile.Digest})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: "suite", StartedAt: suiteClock})
	mustProgress(t, err)

	res, out, err := guardExecute(t, b, instance.ID)
	mustProgress(t, err)
	if len(res.Output) > validateSuiteOutputMaxBytes || out.AssessmentID == "" || out.AssessmentDigest == "" {
		t.Fatalf("saved suite result: bytes=%d assessment=%q digest=%q", len(res.Output), out.AssessmentID, out.AssessmentDigest)
	}
	evidence, err := f.db.ValidationEvidence(t.Context(), out.AssessmentID)
	mustProgress(t, err)
	var full coverage.Manifest
	mustProgress(t, json.Unmarshal(evidence.Payload, &full))
	if len(full.Inventory.Units) != 1 || len(full.Inventory.Units[0].Files) != len(files) {
		t.Fatalf("evidence lost inventory: %+v", full.Inventory.Reason)
	}
	legacy := out
	legacy.Assessment = &full
	oversized, err := json.Marshal(legacy)
	mustProgress(t, err)
	if len(oversized) <= validateSuiteOutputMaxBytes {
		t.Fatalf("fixture too small to exceed the bound: %d bytes", len(oversized))
	}

	for _, saved := range [][]byte{res.Output, oversized} {
		attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "suite", Limit: 16, StartedAt: suiteClock})
		mustProgress(t, err)
		mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: res.Outcome, FinishedAt: suiteClock, InstanceStatus: store.GraphRunning,
			Outputs: []store.GraphOutput{{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: saved, CreatedAt: suiteClock}}}))
		view, err := latestValidateSuite(t.Context(), Options{DB: f.db}, instance.ID, "suite")
		if err != nil {
			t.Fatalf("next step cannot read a %d-byte saved suite result: %v", len(saved), err)
		}
		if view.Attempt.ID != attempt.ID || view.Output.Assessment == nil || coverage.Digest(*view.Output.Assessment) != out.AssessmentDigest || len(view.Output.Assessment.Inventory.Units[0].Files) != len(files) {
			t.Fatalf("read view of %d-byte result lost the assessment", len(saved))
		}
	}
}
