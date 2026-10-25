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
		{"restart_pending", "", "not settled"},
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

func TestRepairPresentationUsesRetainedDisposition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		state, restart string
		wait           bool
	}{
		{"reserved", "", false},
		{"applying", "", false},
		{"restart_pending", "uncertain", true},
		{"restart_pending", "dispatched", true},
		{"restart_pending", "pending", false},
		{"restart_pending", "", false},
		{"completed", "acknowledged", false},
		{"failed", "", false},
		{"needs_review", "", false},
		{"unknown", "", false},
	} {
		t.Run(tc.state+"/"+tc.restart, func(t *testing.T) {
			t.Parallel()
			c := doctor.Conversation{Repair: &store.DoctorRepair{ID: "retained-7", TaskRef: "github:owner/repo#47", State: tc.state}}
			if tc.restart != "" {
				c.Restart = &store.DoctorRestart{State: tc.restart}
			}
			body := doctor.RenderConversation(c)
			if !strings.Contains(body, "Repair retained-7: "+tc.state) || strings.Contains(body, "fresh diagnosis") || strings.Contains(body, "Continue with /doctor") {
				t.Fatalf("conflicting repair guidance: %s", body)
			}
			if tc.wait {
				if !strings.Contains(body, "Wait; no owner action is needed") || strings.Contains(body, "litefactory logs") {
					t.Fatalf("active repair lost wait disposition: %s", body)
				}
			} else if strings.Count(body, "litefactory logs --all --grep 'github:owner/repo#47'") != 1 || strings.Contains(body, "Wait;") {
				t.Fatalf("unconfirmed or settled repair lost single inspection: %s", body)
			}
		})
	}
}

// A failed command may leave an intermediate phase retained without a live
// worker. That phase alone cannot establish automatic progress.
func TestRepairCommandFailureDoesNotPromiseAutomaticProgress(t *testing.T) {
	t.Parallel()
	c := doctor.Conversation{
		RepairInspectionRequired: true,
		Repair:                   &store.DoctorRepair{ID: "retained-7", TaskRef: "github:owner/repo#47", State: "quiescing"},
	}
	body := doctor.RenderConversation(c)
	for _, fact := range []string{"Repair retained-7: quiescing", "Overall: not settled", "repair command did not complete", "further progress is not confirmed"} {
		if !strings.Contains(body, fact) {
			t.Errorf("missing failed-command fact %q: %s", fact, body)
		}
	}
	if strings.Count(body, "litefactory logs --all --grep 'github:owner/repo#47'") != 1 || strings.Contains(body, "Wait;") || strings.Contains(body, "fresh diagnosis") {
		t.Fatalf("failed command must offer one inspection rather than automatic-progress advice: %s", body)
	}
	if c.Repair.State != "quiescing" {
		t.Fatal("presentation changed retained repair phase")
	}
}

func TestRepairFailurePresentationKeepsOneNextStep(t *testing.T) {
	t.Parallel()
	reason := "step 1 consume_comment: forge reply failed: reply outcome uncertain. Run /doctor check to re-diagnose against the current state before approving another repair."
	c := doctor.Conversation{Repair: &store.DoctorRepair{ID: "retained-7", TaskRef: "github:owner/repo#47", State: "failed", Failure: reason}}
	body := doctor.RenderConversation(c)
	if !strings.Contains(body, "forge reply failed: reply outcome uncertain") || strings.Count(body, "litefactory logs --all --grep 'github:owner/repo#47'") != 1 || strings.Contains(body, "/doctor check") {
		t.Fatalf("failed repair lost uncertainty or offered competing next steps: %s", body)
	}
	if c.Repair.Failure != reason {
		t.Fatal("display changed retained failure")
	}
	// Similar text supplied as part of an explanation is preserved; only the
	// exact factory-owned trailing suffix is removed.
	other := "Owner requested /doctor check; current effect is uncertain"
	if got := doctor.RepairFailureExplanation(other); got != other {
		t.Fatalf("changed non-suffix explanation: %s", got)
	}
}
