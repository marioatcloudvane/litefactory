package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func fileRepairFixture(t *testing.T, reserve bool) (*Service, naming.Ref, string, string) {
	t.Helper()
	s := doctorIOService(t)
	ctx := t.Context()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Paths = config.NewPaths(filepath.Join(base, "factory"))
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: filepath.Join(base, "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(base, "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	branch := "lf/406-case"
	if _, err = s.DB.FreezeSessionSlug(ctx, session.ID, "case", branch); err != nil {
		t.Fatal(err)
	}
	worktree, err := naming.WorktreePath(repo.WorktreeRoot, 406)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.SetSessionWorktree(ctx, session.ID, worktree); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(worktree, ".aiteam", "lf-406-case")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "notes.md")
	if err = os.WriteFile(path, []byte("original issue notes\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	evidence := store.DoctorEvidence{TaskRef: ref.String()}
	s.collectInventory(store.DoctorLocalSources{WorktreePath: worktree, Branch: branch}, &evidence)
	if len(evidence.Records) != 1 || evidence.Records[0].Identity == "" {
		t.Fatalf("missing exact file identity: %+v", evidence)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "file-proposer", SourceID: "file-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	captured, _ := json.Marshal(evidence)
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "file-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(captured)}); err != nil {
		t.Fatal(err)
	}
	result, err := BindResults(ctx, s.DB, "file-proposer", evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Notes need an approved correction.", Uncertainty: "Only these bytes were inspected.", Evidence: []string{"session_file:notes.md"}}}, Proposal: []RepairStep{{Number: 1, Target: "session_file:notes.md", Precondition: "Exact captured digest", Change: "Replace notes with reviewed text", Reason: "Correct issue notes", Reversibility: "Restore retained original", Rollback: "Copy original bytes and mode", Action: &store.DoctorRepairAction{Kind: "session_file", Before: evidence.Records[0].State, After: "corrected issue notes\n"}}}}}
	retainLegacyRepairDiagnosis(t, result, "file-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, ref.String(), "file-presentation", pid); err != nil {
		t.Fatal(err)
	}
	if !reserve {
		return s, ref, "file-presentation", path
	}
	operation, err := s.DB.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "file-repair", SourceID: "file-approval", Principal: "local-owner", TaskRef: ref.String(), PresentationID: "file-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.AcknowledgeDoctorExclusion(ctx, operation.ID, "controlled-exclusion"); err != nil {
		t.Fatal(err)
	}
	return s, ref, operation.ID, path
}
func TestDoctorRepairFileCopyReplacementVerificationAndRetainedStop(t *testing.T) {
	s, ref, id, path := fileRepairFixture(t, true)
	ctx := t.Context()
	tools := &RepairTools{service: s, target: ref, operation: id}
	if err := tools.Write(ctx, 1); err == nil {
		t.Fatal("file write before backup accepted")
	}
	if err := tools.Backup(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := tools.Write(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := tools.verifyAll(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "corrected issue notes\n" {
		t.Fatalf("wrong bytes %q %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("mode not retained", err)
	}
	steps, err := s.DB.DoctorRepairSteps(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(steps[0].BackupReference)
	if err != nil || string(backup) != "original issue notes\n" {
		t.Fatal("backup unusable", err)
	}
	if err = tools.Write(ctx, 1); err == nil {
		t.Fatal("file replacement replayed")
	}
	if err = s.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	held, err := s.DB.DoctorRepairHeld(ctx, ref.String())
	if err != nil || !held {
		t.Fatal("partial repair hold lost", err)
	}
	if err = tools.Backup(ctx, 1); err == nil {
		t.Fatal("stopped repair accepted tools")
	}
	if _, err = os.Stat(steps[0].BackupReference); err != nil {
		t.Fatal("stop lost backup", err)
	}
}
func TestDoctorRepairFileRefusesAliasAndMissingBackup(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "missing-backup"} {
		t.Run(kind, func(t *testing.T) {
			s, ref, id, path := fileRepairFixture(t, true)
			tools := &RepairTools{service: s, target: ref, operation: id}
			ctx := t.Context()
			switch kind {
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".alias"); err != nil {
					t.Fatal(err)
				}
			case "missing-backup":
				if err := tools.Backup(ctx, 1); err != nil {
					t.Fatal(err)
				}
				steps, err := s.DB.DoctorRepairSteps(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(steps[0].BackupReference); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if kind == "missing-backup" {
				err = tools.Write(ctx, 1)
			} else {
				err = tools.Backup(ctx, 1)
			}
			if err == nil {
				t.Fatal("unsafe file accepted")
			}
			current, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if strings.Contains(string(current), "corrected") {
				t.Fatal("unexpected repair effect")
			}
		})
	}
}

type repairFixtureEnvironment struct{}

func (repairFixtureEnvironment) Preflight(context.Context, naming.Ref) error { return nil }
func (repairFixtureEnvironment) Exclude(context.Context, naming.Ref, string) (string, error) {
	return "controlled-old-instance", nil
}

type repairFixtureRunner struct{}

func (repairFixtureRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled-only", nil
}
func (repairFixtureRunner) RunRepair(ctx context.Context, in Invocation, tools *RepairTools, results *ResultService) (InvocationOutcome, error) {
	if err := tools.Backup(ctx, 1); err != nil {
		return InvocationOutcome{}, err
	}
	if err := tools.Write(ctx, 1); err != nil {
		return InvocationOutcome{}, err
	}
	if err := tools.Verify(ctx, 1); err != nil {
		return InvocationOutcome{}, err
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "The approved issue artifact was corrected.", Uncertainty: "Factory recovery is still pending.", Evidence: []string{"session_file:notes.md"}}}, CoverageGaps: results.evidence.Gaps, Repair: &RepairSummary{OperationID: tools.operation, CompletedSteps: []int{1}, Observations: []string{"Exact replacement reread by the engine"}}}}
	data, err := json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	preflight, err := results.Preflight(ctx, data)
	if err != nil {
		return InvocationOutcome{}, err
	}
	input.ExpectedRevision = preflight.CurrentRevision
	data, err = json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if _, err = results.Submit(ctx, "repair-final", data); err != nil {
		return InvocationOutcome{}, err
	}
	return InvocationOutcome{RequestID: "repair-final", ObservedModel: in.ConfiguredModel, ProviderVersion: "controlled"}, nil
}

type repairFixtureRestart struct {
	dispatches int
	confirmed  bool
}

func (r *repairFixtureRestart) Dispatch(context.Context, store.DoctorRestart) error {
	r.dispatches++
	return errors.New("simulated lost restart response")
}
func (r *repairFixtureRestart) Observe(context.Context, store.DoctorRestart) (string, bool, error) {
	return "controlled-new-instance", r.confirmed, nil
}
func TestDoctorApplyRetainsRestartUncertaintyWithoutReplay(t *testing.T) {
	s, ref, presentation, path := fileRepairFixture(t, false)
	ctx := t.Context()
	request := ApplyRequest{Target: ref, SourceID: "apply-source", Principal: "local-owner", PresentationID: presentation}
	if _, err := s.Apply(ctx, request); err == nil {
		t.Fatal("uninstalled capabilities accepted")
	}
	if held, err := s.DB.DoctorRepairHeld(ctx, ref.String()); err != nil || held {
		t.Fatal("unavailable route created hold", err)
	}
	route := &repairFixtureRestart{}
	s.RepairRunner = repairFixtureRunner{}
	s.RepairEnvironment = repairFixtureEnvironment{}
	s.RestartRoute = route
	operation, err := s.Apply(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != "restart_pending" {
		t.Fatalf("wrong handoff %+v", operation)
	}
	if _, err = s.DispatchRepairRestart(ctx, ref); err == nil {
		t.Fatal("lost response claimed successful restart")
	}
	if _, err = s.DispatchRepairRestart(ctx, ref); err == nil || route.dispatches != 1 {
		t.Fatal("uncertain restart was repeated")
	}
	if _, err = s.RecoverRepair(ctx, ref); err == nil {
		t.Fatal("unconfirmed restart released hold")
	}
	route.confirmed = true
	operation, err = s.RecoverRepair(ctx, ref)
	if err != nil || operation.State != "recovered" {
		t.Fatalf("recovery %+v %v", operation, err)
	}
	if held, err := s.DB.DoctorRepairHeld(ctx, ref.String()); err != nil || held {
		t.Fatal("verified recovery retained hold", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "corrected issue notes\n" {
		t.Fatal("repair data lost", err)
	}
}

// refusingConsumeEffects refuses consume_comment with the given error.
type refusingConsumeEffects struct{ err error }

func (e refusingConsumeEffects) Execute(_ context.Context, _ naming.Ref, _ store.DoctorRepair, step store.DoctorRepairStep, _ string) (store.DoctorRepairStep, error) {
	return step, e.err
}
func (refusingConsumeEffects) Verify(context.Context, naming.Ref, store.DoctorRepairStep) error {
	return nil
}

type cancelledConsumeRunner struct{}

func (cancelledConsumeRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "scoped-only", nil
}
func (cancelledConsumeRunner) RunRepair(ctx context.Context, _ Invocation, tools *RepairTools, _ *ResultService) (InvocationOutcome, error) {
	_, err := tools.ApplyTyped(ctx, 1, "consume_comment")
	if ctx.Err() != nil {
		return InvocationOutcome{}, ctx.Err()
	}
	return InvocationOutcome{}, err
}

// C4, C5: a refused consume_comment step keeps a fixed, step-specific reason
// with the next action; the attempt records the same reason, and only the log
// carries the redacted cause.
func TestConsumeRefusalRetainsStepCategoryAndLogsCause(t *testing.T) {
	cases := []struct {
		name, category string
		err            error
	}{
		{"reply not posted", "doctor reply could not be posted", fmt.Errorf("%w: %w", ErrRepairReplyNotPosted, errors.New("POST 503 doctor-secret-canary-001"))},
		{"fenced claim", "repair state changed since approval", store.ErrDoctorFenced},
		{"watermark drift", "inputs drifted: comment watermark changed", fmt.Errorf("%w: %w", ErrRepairCommentDrifted, errors.New("drift"))},
		{"uncertain replay", "doctor reply outcome uncertain; refusing replay", ErrRepairReplyUncertain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := doctorIOService(t)
			var logs strings.Builder
			s.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			ctx := t.Context()
			repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			session, err := s.DB.GetOrCreateSession(ctx, repo, 686)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.DB.BootstrapWatermark(ctx, session.ID, 10); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 7, Body: "repair", CreatedAt: time.Now(), Classification: store.ClassificationInstruction}); err != nil {
				t.Fatal(err)
			}
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := s.DB.ReadDoctorEvidence(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "consume-proposer", SourceID: "consume-source", OwnerText: "Diagnose", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
			if err != nil {
				t.Fatal(err)
			}
			captured, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "consume-proposer", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(captured)}); err != nil {
				t.Fatal(err)
			}
			results, err := BindResults(ctx, s.DB, "consume-proposer", evidence, s.Redactor)
			if err != nil {
				t.Fatal(err)
			}
			input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "Comment is stuck.", Uncertainty: "Local rows only.", Evidence: []string{"comment:11"}}}, Proposal: []RepairStep{{Number: 1, Target: "comment:11", Precondition: "Watermark 10", Change: "Consume comment 11", Reason: "Unblock the task", Reversibility: "Backed up watermark", Rollback: "Restore watermark", Action: &store.DoctorRepairAction{Kind: "consume_comment", Before: "10", After: "11"}}}}}
			retainLegacyRepairDiagnosis(t, results, "consume-proposal", input)
			pid, _, err := s.DB.DoctorProposalReference(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.DB.PresentDoctorProposal(ctx, ref.String(), "consume-presentation", pid); err != nil {
				t.Fatal(err)
			}
			s.RepairEffects = refusingConsumeEffects{err: c.err}
			s.RepairRunner = cancelledConsumeRunner{}
			s.RepairEnvironment = repairFixtureEnvironment{}
			want := "step 1 consume_comment: " + c.category + ". Run /doctor check to re-diagnose against the current state before approving another repair."
			if _, err = s.Apply(ctx, ApplyRequest{Target: ref, SourceID: "consume-apply", Principal: "local-owner", PresentationID: "consume-presentation"}); err == nil {
				t.Fatal("refused consume accepted")
			}
			saved, err := s.DB.ReadDoctorRepair(ctx, ref.String())
			if err != nil {
				t.Fatal(err)
			}
			if saved.State != "failed" || saved.Failure != want {
				t.Fatalf("repair %s failure %q, want %q", saved.State, saved.Failure, want)
			}
			if body := RenderConversation(Conversation{Repair: &saved}); !strings.Contains(body, "Unresolved: "+want) {
				t.Fatalf("owner reply lacks the reason: %s", body)
			}
			attempts, err := s.DB.DoctorAttempts(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, attempt := range attempts {
				if attempt.SourceID == "repair:"+saved.ID {
					found = true
					if attempt.Failure != want {
						t.Fatalf("attempt reason %q, want %q", attempt.Failure, want)
					}
				}
			}
			if !found {
				t.Fatal("repair attempt missing")
			}
			logged := logs.String()
			if !strings.Contains(logged, "doctor repair step refused") || !strings.Contains(logged, "repair_id="+saved.ID) || !strings.Contains(logged, "step=1") || !strings.Contains(logged, "kind=consume_comment") || strings.Contains(logged, "doctor-secret-canary-001") {
				t.Fatalf("refusal log: %s", logged)
			}
			if c.name == "reply not posted" && !strings.Contains(logged, "POST 503") {
				t.Fatalf("refusal log lacks the redacted cause: %s", logged)
			}
		})
	}
}
