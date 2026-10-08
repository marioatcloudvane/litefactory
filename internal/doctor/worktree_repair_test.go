package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func worktreeRepairFixture(t *testing.T, absent bool) (*Service, naming.Ref, string, string) {
	t.Helper()
	s, ref, _, _ := fileRepairFixture(t, false)
	ctx := t.Context()
	source, err := s.DB.DoctorSources(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source.WorktreePath, "data", "payload.txt")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if !absent {
		if err = os.WriteFile(path, []byte("original worktree bytes"), 0640); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(path, 0640); err != nil {
			t.Fatal(err)
		}
	}
	evidence := store.DoctorEvidence{TaskRef: ref.String()}
	status := "?? data/payload.txt\x00"
	if absent {
		status = " D data/payload.txt\x00"
	}
	s.collectWorktreeFiles(ctx, ref, source, status, &evidence)
	if len(evidence.Records) != 1 {
		t.Fatalf("missing worktree handle: %+v", evidence)
	}
	attempt := "worktree-proposer"
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: attempt, SourceID: attempt, OwnerText: "Repair changed issue file", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	captured, _ := json.Marshal(evidence)
	if err = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: attempt, Provider: "claude", ConfiguredModel: "fixture", Evidence: string(captured)}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(ctx, s.DB, attempt, evidence, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	handle := EvidenceHandle(evidence.Records[0])
	input := ResultInput{ExpectedRevision: 1, Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "The issue-owned changed file needs correction", Uncertainty: "Only selected local file observed", Evidence: []string{handle}}}, Proposal: []RepairStep{{Number: 1, Target: handle, Precondition: evidence.Records[0].State, Change: "Use reviewed replacement bytes", Reason: "Recover issue file", Reversibility: "Restore exact recorded original existence", Rollback: "Use retained manifest bytes/mode/existence", Action: &store.DoctorRepairAction{Kind: "worktree_file", Before: evidence.Records[0].State, After: "approved new worktree bytes"}}}}}
	retainLegacyRepairDiagnosis(t, results, "worktree-proposal", input)
	pid, _, err := s.DB.DoctorProposalReference(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PresentDoctorProposal(ctx, ref.String(), "worktree-presentation", pid); err != nil {
		t.Fatal(err)
	}
	operation, err := s.DB.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "worktree-repair", SourceID: "worktree-approval", TaskRef: ref.String(), Principal: "local-owner", PresentationID: "worktree-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.AcknowledgeDoctorExclusion(ctx, operation.ID, "controlled-exclusive"); err != nil {
		t.Fatal(err)
	}
	return s, ref, operation.ID, path
}

func TestDoctorWorktreeRepairRetainsOriginalExistenceAndMode(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "absent"}[absent], func(t *testing.T) {
			s, ref, id, path := worktreeRepairFixture(t, absent)
			ctx := t.Context()
			tools := &RepairTools{service: s, target: ref, operation: id}
			if err := tools.Backup(ctx, 1); err != nil {
				t.Fatal(err)
			}
			steps, err := s.DB.DoctorRepairSteps(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(steps[0].BackupReference)
			if err != nil {
				t.Fatal(err)
			}
			manifestBytes, err := os.ReadFile(strings.TrimSuffix(steps[0].BackupReference, ".data") + ".manifest.json")
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				Existed                  bool
				Mode                     uint32
				Restoration, AfterDigest string
			}
			if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Existed == absent || manifest.Restoration == "" || manifest.AfterDigest == "" {
				t.Fatal("unrestorable original manifest", manifest)
			}
			if absent && len(data) != 0 {
				t.Fatal("absent backup invented bytes")
			}
			if !absent && (string(data) != "original worktree bytes" || manifest.Mode != 0640) {
				t.Fatal("original bytes/mode lost", manifest)
			}
			if err = tools.Write(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err = tools.Verify(ctx, 1); err != nil {
				t.Fatal(err)
			}
			current, err := os.ReadFile(path)
			if err != nil || string(current) != "approved new worktree bytes" {
				t.Fatal(string(current), err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0640)
			if absent {
				want = 0600
			}
			if info.Mode().Perm() != want {
				t.Fatal("mode changed", info.Mode())
			}
			// Exercise retained restoration information in this isolated fixture only.
			if absent {
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = os.WriteFile(path, data, os.FileMode(manifest.Mode)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = os.Stat(steps[0].BackupReference); err != nil {
				t.Fatal("backup deleted", err)
			}
		})
	}
}

func TestDoctorAbsentFileRefusesLateCreationAndParentDrift(t *testing.T) {
	for _, change := range []string{"late-file", "parent-drift"} {
		t.Run(change, func(t *testing.T) {
			s, ref, id, path := worktreeRepairFixture(t, true)
			tools := &RepairTools{service: s, target: ref, operation: id}
			ctx := t.Context()
			if err := tools.Backup(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if change == "late-file" {
				if err := os.WriteFile(path, []byte("concurrent bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				parent := filepath.Dir(path)
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := tools.Write(ctx, 1); err == nil {
				t.Fatal("changed absence precondition accepted")
			}
			if change == "late-file" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "concurrent bytes" {
					t.Fatal("overwrote unrelated late file", err)
				}
			}
		})
	}
}

func TestDoctorWorktreeEvidenceRefusesFactoryAuthorityAndAmbiguousPaths(t *testing.T) {
	s, ref, _, _ := fileRepairFixture(t, false)
	source, err := s.DB.DoctorSources(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "config.yaml", ".git/config", "migrations/0001.sql", ".codex/skill.md", "../escape", "clean.txt"} {
		evidence := store.DoctorEvidence{TaskRef: ref.String()}
		status := "?? " + name + "\x00"
		if name == "clean.txt" {
			status = ""
		}
		s.collectWorktreeFiles(t.Context(), ref, source, status, &evidence)
		if len(evidence.Records) != 0 {
			t.Fatal("forbidden or clean file disclosed", name)
		}
	}
	path := filepath.Join(source.WorktreePath, "changed.go")
	if err = os.WriteFile(path, []byte("package example"), 0600); err != nil {
		t.Fatal(err)
	}
	evidence := store.DoctorEvidence{TaskRef: ref.String()}
	s.collectWorktreeFiles(t.Context(), ref, source, " M changed.go\x00", &evidence)
	if len(evidence.Records) != 1 {
		t.Fatal("ordinary nonfactory issue-owned source not supported", evidence)
	}
	if err = os.WriteFile(filepath.Join(source.WorktreePath, "go.mod"), []byte("module github.com/cloudvane/litefactory\n"), 0600); err != nil {
		t.Fatal(err)
	}
	evidence.Records = nil
	s.collectWorktreeFiles(t.Context(), ref, source, " M changed.go\x00", &evidence)
	if len(evidence.Records) != 0 {
		t.Fatal("factory source exposed as repair handle")
	}
}

func TestDoctorDeletedSessionArtifactHasAbsentOriginalHandle(t *testing.T) {
	s, ref, _, path := fileRepairFixture(t, false)
	source, err := s.DB.DoctorSources(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	evidence := store.DoctorEvidence{TaskRef: ref.String()}
	status := " D .aiteam/" + strings.ReplaceAll(source.Branch, "/", "-") + "/notes.md\x00"
	s.collectWorktreeFiles(t.Context(), ref, source, status, &evidence)
	if len(evidence.Records) != 1 || evidence.Records[0].Kind != "session_file" || evidence.Records[0].ID != "notes.md" || evidence.Records[0].State != absentFile {
		t.Fatal(evidence)
	}
}
