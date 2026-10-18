package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// This controlled provider observes the actual launch boundary, with no live
// provider or remote service, while keeping the real result and approval gates.
type capturePathRunner struct {
	t        *testing.T
	service  *Service
	schedule store.Schedule
	edited   bool
	captures []*instructionpackage.RoleCapture
	repair   typedApplyRunner
}

func (*capturePathRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled scoped-only provider", nil
}
func (r *capturePathRunner) observe(ctx context.Context, in Invocation, role string) *instructionpackage.RoleCapture {
	r.t.Helper()
	raw, err := r.service.DB.DoctorInstructionCapture(ctx, in.ID)
	if err != nil {
		r.t.Fatal("capture must be durable before launch", err)
	}
	captured, err := instructionpackage.Restore(raw)
	if err != nil {
		r.t.Fatal(err)
	}
	if captured.Snapshot().Role != role {
		r.t.Fatalf("capture role %q, want %q", captured.Snapshot().Role, role)
	}
	for _, body := range [][]byte{captured.Instructions(), captured.SkillInstructions("doctor-field-guide")} {
		if len(body) == 0 || !strings.Contains(in.Prompt, string(body)) {
			r.t.Fatal("dispatched prompt differs from retained instructions")
		}
	}
	authority := captured.SkillInstructions("doctor-repair-authority")
	if role == "doctor-repair" {
		if len(authority) == 0 || !strings.Contains(in.Prompt, string(authority)) {
			r.t.Fatal("repair authority missing")
		}
		if strings.Index(in.Prompt, string(captured.Instructions())) > strings.Index(in.Prompt, string(captured.SkillInstructions("doctor-field-guide"))) || strings.Index(in.Prompt, string(captured.SkillInstructions("doctor-field-guide"))) > strings.Index(in.Prompt, string(authority)) {
			r.t.Fatal("repair instruction order changed")
		}
	} else if len(authority) != 0 {
		r.t.Fatal("diagnosis received repair authority skill")
	}
	return captured
}
func (r *capturePathRunner) Run(ctx context.Context, in Invocation, results *ResultService) (InvocationOutcome, error) {
	captured := r.observe(ctx, in, "doctor-diagnosis")
	r.captures = append(r.captures, captured)
	if !r.edited {
		// Mutating the source after dispatch cannot change this invocation's capture.
		r.edited = true
		path := filepath.Join(r.service.Paths.InternalInstructionsDir(), "agents", "doctor-diagnosis.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("Owner diagnosis instructions updated after capture.\n"), 0600); err != nil {
			r.t.Fatal(err)
		}
		again, err := r.service.DB.DoctorInstructionCapture(ctx, in.ID)
		if err != nil {
			r.t.Fatal(err)
		}
		restored, err := instructionpackage.Restore(again)
		if err != nil || !bytes.Equal(restored.Instructions(), captured.Instructions()) || strings.Contains(in.Prompt, "Owner diagnosis instructions updated after capture.") {
			r.t.Fatal("active invocation changed with owner file", err)
		}
	}
	diagnosis := Diagnosis{Findings: []Finding{{Explanation: "The pending schedule should be cancelled.", Uncertainty: "Only retained task evidence was inspected.", Evidence: []string{fmt.Sprintf("schedule:%d", r.schedule.ID)}}}, CoverageGaps: results.evidence.Gaps}
	if len(r.captures) == 1 {
		diagnosis.Proposal = []RepairStep{{Number: 1, Target: fmt.Sprintf("schedule:%d", r.schedule.ID), Precondition: "The schedule remains pending.", Change: "Cancel the pending schedule.", Reason: "Restore the owner's intended schedule state.", Reversibility: "The original row is backed up.", Rollback: "Review the retained backup.", Action: &store.DoctorRepairAction{Kind: "cancel_schedule", Before: "pending", After: "failed"}}}
	}
	input := ResultInput{Diagnosis: diagnosis}
	raw, err := json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	pre, err := results.Preflight(ctx, raw)
	if err != nil {
		return InvocationOutcome{}, err
	}
	input.ExpectedRevision = pre.CurrentRevision
	raw, err = json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	pre, err = results.Preflight(ctx, raw)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if !pre.Valid {
		r.t.Fatalf("invalid main-path diagnosis: %+v", pre)
	}
	id := "capture-result-" + in.ID
	if _, err = results.Submit(ctx, id, raw); err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Receipt(ctx, id); err != nil {
		return InvocationOutcome{}, err
	}
	return InvocationOutcome{RequestID: id, ObservedModel: in.ConfiguredModel, ProviderVersion: "controlled"}, nil
}
func (r *capturePathRunner) RunRepair(ctx context.Context, in Invocation, tools *RepairTools, results *ResultService) (InvocationOutcome, error) {
	r.observe(ctx, in, "doctor-repair")
	return r.repair.RunRepair(ctx, in, tools, results)
}

func TestDoctorCapturedPackageDiagnosisAndApprovedRepair(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 955)
	if err != nil {
		t.Fatal(err)
	}
	schedule, _, err := s.DB.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 7, OwnerHandle: "owner", Payload: "work", DueAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	target, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := instructionpackage.New(ctx, s.Paths)
	if err != nil {
		t.Fatal(err)
	}
	// Populate a complete valid owner closure before exercising owner edits.
	for _, role := range []string{"doctor-diagnosis", "doctor-repair"} {
		captured, _, captureErr := pkg.Capture(ctx, role)
		if captureErr != nil {
			t.Fatal(captureErr)
		}
		for _, file := range captured.Snapshot().Files {
			path := filepath.Join(s.Paths.InternalInstructionsDir(), filepath.FromSlash(file.Path))
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, file.Content, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.Instructions = pkg
	runner := &capturePathRunner{t: t, service: s, schedule: schedule}
	s.Runner = runner
	s.RepairRunner = runner
	s.RepairEnvironment = repairFixtureEnvironment{}
	conversation, err := s.Diagnose(ctx, DiagnoseRequest{Target: target, Instructions: "Diagnose the pending schedule.", SourceID: "capture-diagnosis"})
	if err != nil {
		t.Fatal(err)
	}
	pid, _, err := s.DB.DoctorProposalReference(ctx, conversation.Conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, target.String(), "capture-owner-presentation", pid); err != nil {
		t.Fatal(err)
	}
	operation, err := s.Apply(ctx, ApplyRequest{Target: target, SourceID: "capture-owner-approval", Principal: "local-owner", PresentationID: "capture-owner-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != "completed" || runner.repair.runs != 1 || !runner.repair.receipt {
		t.Fatal("approved repair did not complete", operation)
	}
	current, err := s.DB.GetSchedule(ctx, schedule.ID)
	if err != nil || current.Status != "failed" {
		t.Fatal("repair effect not verified", current, err)
	}
	if _, err = s.Diagnose(ctx, DiagnoseRequest{Target: target, Instructions: "Review the repaired schedule.", SourceID: "capture-fresh-diagnosis"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.captures) != 2 || !strings.Contains(string(runner.captures[1].Instructions()), "Owner diagnosis instructions updated after capture.") || runner.captures[0].Snapshot().EffectiveDigest == runner.captures[1].Snapshot().EffectiveDigest {
		t.Fatal("fresh invocation did not capture owner edit")
	}
}
