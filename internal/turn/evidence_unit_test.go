package turn

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// The inert handle keeps construction and permission checks independent of I/O.
// Package-local construction avoids starting a real SubmissionSession broker.
func evidenceUnitOwners(t *testing.T) (*SubmissionSession, *ReceiptService) {
	t.Helper()
	db := &store.DB{}
	b := store.ResultBinding{Task: "github:owner/repo#273", RunID: 7, Attempt: "attempt", Mode: "implement", SubmissionVersion: 2, WorkflowVersion: 2}
	r, err := NewReceiptService(db, b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &SubmissionSession{
		options: SubmissionSessionOptions{DB: db, Progress: &StoryProgress{}},
		binding: store.SubmissionAttempt{RunID: b.RunID, Attempt: b.Attempt, Parent: b.Task, Stage: "engineering", Generation: 1,
			Stories: []store.SubmissionStoryBinding{{Story: "US-006", Revision: strings.Repeat("a", 64), IssueID: 286}}},
		scope: agent.ContributionScope{ContractVersion: 2, WorkflowVersion: 2, Stage: "engineering"},
	}
	return s, r
}

func TestEvidenceUnit_StageBinding(t *testing.T) {
	for _, tc := range []struct {
		mode, stage string
		workflow    int
		allowed     bool
	}{
		{"implement", "engineering", 2, true},
		{"implement", "legacy", 1, true},
		{"validate", "validate", 2, true},
		{"validate", "engineering", 2, false},
		{"validate", "legacy", 1, false},
		{"implement", "validate", 2, false},
		{"research", "engineering", 2, false},
		{"plan", "plan", 2, false},
		{"spike", "engineering", 2, false},
	} {
		t.Run(tc.mode+"/"+tc.stage, func(t *testing.T) {
			t.Parallel()
			s, r := evidenceUnitOwners(t)
			s.binding.Stage, s.scope.Stage = tc.stage, tc.stage
			s.scope.WorkflowVersion, r.binding.WorkflowVersion = tc.workflow, tc.workflow
			r.binding.Mode = tc.mode
			got, err := NewEvidenceService(s, r)
			if tc.allowed {
				if err != nil || got == nil {
					t.Fatalf("authorized stage refused: service=%v err=%v", got, err)
				}
				if got.Binding() != r.Binding() {
					t.Fatal("service changed immutable result scope")
				}
			} else if err == nil || got != nil {
				t.Fatalf("forbidden stage accepted: service=%v err=%v", got, err)
			}
		})
	}
}

func TestEvidenceUnit_RefusesMismatchedOwners(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(**SubmissionSession, **ReceiptService)
	}{
		{"missing session", func(s **SubmissionSession, _ **ReceiptService) { *s = nil }},
		{"missing receipts", func(_ **SubmissionSession, r **ReceiptService) { *r = nil }},
		{"different database", func(s **SubmissionSession, _ **ReceiptService) { (*s).options.DB = &store.DB{} }},
		{"missing progress owner", func(s **SubmissionSession, _ **ReceiptService) { (*s).options.Progress = nil }},
		{"different run", func(s **SubmissionSession, _ **ReceiptService) { (*s).binding.RunID++ }},
		{"different attempt", func(s **SubmissionSession, _ **ReceiptService) { (*s).binding.Attempt = "old" }},
		{"different parent", func(s **SubmissionSession, _ **ReceiptService) { (*s).binding.Parent = "github:owner/repo#999" }},
		{"different workflow", func(s **SubmissionSession, _ **ReceiptService) { (*s).scope.WorkflowVersion = 1 }},
		{"old submission contract", func(_ **SubmissionSession, r **ReceiptService) { (*r).binding.SubmissionVersion = 1 }},
	} {
		t.Run(tc.name+"/refused", func(t *testing.T) {
			t.Parallel()
			s, r := evidenceUnitOwners(t)
			tc.change(&s, &r)
			got, err := NewEvidenceService(s, r)
			if err == nil || got != nil {
				t.Fatalf("incompatible owners admitted: service=%v err=%v", got, err)
			}
		})
	}
}

func TestEvidenceUnit_FrozenPermissionMaterial(t *testing.T) {
	t.Parallel()
	s, r := evidenceUnitOwners(t)
	svc, err := NewEvidenceService(s, r)
	if err != nil {
		t.Fatal(err)
	}
	want := s.CheckpointBinding()
	before, err := svc.PermissionMaterial()
	if err != nil {
		t.Fatal(err)
	}
	var decoded store.SubmissionAttempt
	if err := json.Unmarshal(before, &decoded); err != nil || !reflect.DeepEqual(decoded, want) {
		t.Fatalf("permission omits frozen binding: %s err=%v", before, err)
	}
	copyBinding := s.CheckpointBinding()
	copyBinding.Stories[0].Revision = strings.Repeat("b", 64)
	copyBinding.Stories[0].Story = "foreign"
	copyBinding.Generation++
	if !reflect.DeepEqual(s.CheckpointBinding(), want) {
		t.Fatal("caller changed the session's frozen selection through returned binding")
	}
	after, err := svc.PermissionMaterial()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("caller changed permission bytes: %s err=%v", after, err)
	}
	before[0] = '!'
	fresh, err := svc.PermissionMaterial()
	if err != nil || !bytes.Equal(fresh, after) {
		t.Fatalf("returned permission bytes alias retained authority: %s err=%v", fresh, err)
	}
}
