package doctor_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestTypedRepairOutcomePresentation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ state, failure, overall string }{
		{"recovered", "", "repaired"},
		{"completed", "", "repaired"},
		{"failed", "Interrupted typed repair", "failed"},
		{"stopped", "Tool refused drift", "failed"},
		{"needs_review", "Interrupted effect", "failed"},
		{"restart_pending", "", "rechecked"},
	} {
		t.Run(tt.state, func(t *testing.T) {
			c := doctor.Conversation{ApplyAvailable: true, Repair: &store.DoctorRepair{ID: "approved", State: tt.state, Failure: tt.failure}, RepairSteps: []store.DoctorRepairStep{{Number: 1, Kind: "release_instance", Target: "graph:410", Before: "running", After: "failed", State: "verified", BackupReference: "database:retained:1", ObservedAfter: "failed"}, {Number: 2, Kind: "cancel_schedule", Target: "schedule:7", Before: "pending", After: "failed", State: "pending"}}}
			body := doctor.RenderConversation(c)
			for _, want := range []string{"Overall: " + tt.overall, "Step 1 release_instance graph:410: verified (running → failed), recheck passed, backup database:retained:1", "Step 2 cancel_schedule schedule:7: pending (pending → failed), recheck not completed, backup none"} {
				if !strings.Contains(body, want) {
					t.Error("outcome omits", want)
				}
			}
			if strings.Contains(body, "recheck: passed: failed") {
				t.Error("ambiguous recheck line retained")
			}
			if strings.Contains(body, "Apply is unavailable") {
				t.Fatal("available repair presented as unavailable")
			}
		})
	}
}

func TestRetainedTypedRepairOutcomeDoesNotAdvertiseUnavailableApply(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"completed", "failed"} {
		body := doctor.RenderConversation(doctor.Conversation{Repair: &store.DoctorRepair{State: state}})
		if strings.Contains(body, "Apply is unavailable") {
			t.Fatalf("retained %s outcome advertises unavailable apply", state)
		}
	}
}

func TestVerifiedTypedRepairWithAgentNotesRendersRepaired(t *testing.T) {
	t.Parallel()
	c := doctor.Conversation{
		ApplyAvailable: true,
		Diagnosis:      &doctor.Diagnosis{Questions: []string{"Does the next turn start?"}, Repair: &doctor.RepairSummary{CompletedSteps: []int{1}, Unresolved: []string{"post-apply state not observable"}}},
		Repair:         &store.DoctorRepair{ID: "approved", State: "completed"},
		RepairSteps:    []store.DoctorRepairStep{{Number: 1, Kind: "release_instance", Target: "graph:410", Before: "running", After: "failed", State: "verified", BackupReference: "database:retained:1", ObservedAfter: "failed"}},
	}
	body := doctor.RenderConversation(c)
	for _, want := range []string{"Overall: repaired", "Note: post-apply state not observable", "Does the next turn start?", "Step 1 release_instance graph:410: verified (running → failed), recheck passed, backup database:retained:1"} {
		if !strings.Contains(body, want) {
			t.Error("outcome omits", want)
		}
	}
	for _, unwanted := range []string{"Overall: failed", "Doctor request did not complete"} {
		if strings.Contains(body, unwanted) {
			t.Error("verified repair presented as failure:", unwanted)
		}
	}
}
