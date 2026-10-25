package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func graphProposalFixture(t *testing.T) (*ResultService, store.GraphInstance, ResultInput) {
	t.Helper()
	s := doctorIOService(t)
	ctx := t.Context()
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	def, err := s.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "validate", ProfileVersion: 1, Path: "core/validate.dot", Digest: strings.Repeat("a", 64), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := s.DB.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "validate", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.BeginDoctorAttempt(ctx, session.TaskRef, store.DoctorAttempt{ID: "graph-proposer", SourceID: "graph-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := s.DB.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "graph-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(raw)}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, "graph-proposer", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("graph:%d", instance.ID)
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "The graph is stale", Evidence: []string{target}, Uncertainty: "No execution is active"}}, CoverageGaps: evidence.Gaps, Proposal: []RepairStep{{Number: 1, Target: target, Action: &store.DoctorRepairAction{Kind: "graph_fail", Before: "running", After: "failed"}, Precondition: "No related work is active", Change: "Remove the stale blocker", Reason: "Owner requested bounded recovery", Reversibility: "Exact backup and numbered journal", Rollback: "Requires separate exact approval before related progress"}}}}
	return results, instance, input
}

func TestGraphProposalCaptureAndLostReceiptRetainExactManifest(t *testing.T) {
	results, instance, input := graphProposalFixture(t)
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := results.Preflight(t.Context(), raw)
	if err != nil || !pre.Valid {
		t.Fatal(pre, err)
	}
	receipt, err := results.Submit(t.Context(), "graph-result", raw)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := results.db.GetResultRevision(t.Context(), results.binding, receipt.AppliedRevision)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := DecodeDiagnosis(revision.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	action := captured.Proposal[0].Action
	if !store.DoctorGraphRepairTarget(action.Kind, input.Diagnosis.Proposal[0].Target, action.Before, action.After) {
		t.Fatalf("not an exact manifest: %+v", action)
	}
	current, err := results.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || current.Status != "running" {
		t.Fatal("diagnosis changed workflow", current, err)
	}
	// A lost response must recover the original immutable request after the graph
	// changes, without recapturing a timestamp or asking the owner to approve again.
	if err = results.db.SetGraphInstanceStatus(t.Context(), instance.ID, "failed", time.Time{}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	replay, err := results.Submit(t.Context(), "graph-result", raw)
	if err != nil || replay.AppliedRevision != receipt.AppliedRevision || !bytes.Equal(replay.Payload, receipt.Payload) {
		t.Fatal("receipt changed", replay, err)
	}
	input.Diagnosis.Proposal[0].Reason = "different proposal"
	changed, _ := json.Marshal(input)
	if _, err = results.Submit(t.Context(), "graph-result", changed); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatal("changed input reused receipt", err)
	}
}

func TestGraphProposalRejectsMixedToolsAndRetiredCascade(t *testing.T) {
	results, _, input := graphProposalFixture(t)
	input.Diagnosis.Proposal[0].Action.Kind = "release_instance"
	raw, _ := json.Marshal(input)
	if pre, err := results.Preflight(t.Context(), raw); err != nil || pre.Valid {
		t.Fatal("retired cascade admitted", pre, err)
	}
	input.Diagnosis.Proposal[0].Action.Kind = "graph_fail"
	input.Diagnosis.Proposal = append(input.Diagnosis.Proposal, RepairStep{Number: 2, Action: &store.DoctorRepairAction{Kind: "consume_comment", Before: "0", After: "1"}})
	raw, _ = json.Marshal(input)
	if pre, err := results.Preflight(t.Context(), raw); err != nil || pre.Valid {
		t.Fatal("mixed repair admitted", pre, err)
	}
}

func TestGraphRollbackUsesSeparatelyPresentedExactProposal(t *testing.T) {
	results, instance, input := graphProposalFixture(t)
	ctx := t.Context()
	raw, _ := json.Marshal(input)
	if _, err := results.Submit(ctx, "forward-result", raw); err != nil {
		t.Fatal(err)
	}
	if err := results.Adopt(ctx, "forward-result"); err != nil {
		t.Fatal(err)
	}
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(id string) {
		t.Helper()
		c, err := results.db.ReadDoctorConversation(ctx, results.binding.Task)
		if err != nil {
			t.Fatal(err)
		}
		pid, _, err := results.db.DoctorProposalReference(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = results.db.PresentDoctorProposal(ctx, results.binding.Task, id+"-presentation", pid); err != nil {
			t.Fatal(err)
		}
		op, err := results.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: id, SourceID: id + "-approval", Principal: "local-owner", TaskRef: results.binding.Task, PresentationID: id + "-presentation"})
		if err != nil {
			t.Fatal(err)
		}
		if err = results.db.AcknowledgeDoctorExclusion(ctx, op.ID, "fixture-exclusion"); err != nil {
			t.Fatal(err)
		}
		tools := &RepairTools{service: &Service{DB: results.db, Paths: config.NewPaths(t.TempDir())}, target: ref, operation: op.ID}
		if _, err = tools.ApplyTyped(ctx, 1, "graph_fail"); err != nil {
			t.Fatal(err)
		}
		if err = tools.verifyAll(ctx); err != nil {
			t.Fatal(err)
		}
		if id == "forward" {
			// Simulate a crash after the atomic commit and before the agent's
			// final summary. Reconciliation observes the whole journal once.
			settled, recoverErr := tools.service.ReconcileRepair(ctx, ref, op.ID)
			if recoverErr != nil || settled.State != "completed" {
				t.Fatal(settled, recoverErr)
			}
		} else if err = results.db.SettleDoctorTypedRepair(ctx, op.ID, true, ""); err != nil {
			t.Fatal(err)
		}
	}
	apply("forward")
	_, err = results.db.BeginDoctorAttempt(ctx, results.binding.Task, store.DoctorAttempt{ID: "rollback-proposer", SourceID: "inverse-source", OwnerText: "Propose a rollback", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := results.db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	evidenceBytes, _ := json.Marshal(evidence)
	if err = results.db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "rollback-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(evidenceBytes)}); err != nil {
		t.Fatal(err)
	}
	inverse, err := BindResults(ctx, results.db, "rollback-proposer", evidence, results.redactor)
	if err != nil {
		t.Fatal(err)
	}
	input.Diagnosis.Proposal = nil
	input.Diagnosis.RollbackRepairID = "forward"
	input.Diagnosis.RollbackReason = "Restore the verified stopped state before continuing"
	input.Diagnosis.CoverageGaps = evidence.Gaps
	raw, _ = json.Marshal(input)
	pre, err := inverse.Preflight(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = pre.CurrentRevision
	raw, _ = json.Marshal(input)
	pre, err = inverse.Preflight(ctx, raw)
	if err != nil || !pre.Valid {
		t.Fatal(pre, err)
	}
	receipt, err := inverse.Submit(ctx, "inverse-result", raw)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := results.db.GetResultRevision(ctx, inverse.binding, receipt.AppliedRevision)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := DecodeDiagnosis(revision.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Proposal) != 1 || !strings.Contains(proposal.Proposal[0].Action.Before, `"reversal_of":"forward"`) {
		t.Fatalf("inverse is not journal-bound: %+v", proposal)
	}
	if err = inverse.Adopt(ctx, "inverse-result"); err != nil {
		t.Fatal(err)
	}
	current, err := results.db.GetGraphInstance(ctx, instance.ID)
	if err != nil || current.Status != "failed" {
		t.Fatal("diagnosis performed rollback", current, err)
	}
	apply("inverse")
	current, err = results.db.GetGraphInstance(ctx, instance.ID)
	if err != nil || current.Status != "running" {
		t.Fatal("approved inverse did not restore row", current, err)
	}
}

func TestGraphProposalRefusesManifestThatCannotBeDisplayedExactly(t *testing.T) {
	results, _, input := graphProposalFixture(t)
	raw, _ := json.Marshal(input)
	prepared, _, err := results.prepare(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	action := prepared.Diagnosis.Proposal[0].Action
	action.Before = strings.ReplaceAll(action.Before, "Owner requested bounded recovery", "doctor-secret-canary-001")
	action.After = strings.ReplaceAll(action.After, "Owner requested bounded recovery", "doctor-secret-canary-001")
	raw, _ = json.Marshal(prepared)
	pre, err := results.Preflight(t.Context(), raw)
	if err != nil || pre.Valid || len(pre.Errors) != 1 || !strings.Contains(pre.Errors[0], "cannot be displayed unchanged") {
		t.Fatal("protected exact values were admitted", pre, err)
	}
	if _, err = results.Submit(t.Context(), "protected-graph-result", raw); err == nil {
		t.Fatal("protected manifest persisted")
	}
	if _, err = results.Receipt(t.Context(), "protected-graph-result"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("refused manifest retained a receipt", err)
	}
}
