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
		{"recovered", "", "worked"},
		{"stopped", "Tool refused drift", "failed"},
		{"needs_review", "Interrupted effect", "failed"},
		{"restart_pending", "", "rechecked"},
	} {
		t.Run(tt.state, func(t *testing.T) {
			c := doctor.Conversation{ApplyAvailable: true, Repair: &store.DoctorRepair{ID: "approved", State: tt.state, Failure: tt.failure}, RepairSteps: []store.DoctorRepairStep{{Number: 1, State: "verified", BackupReference: "database:retained:1", ObservedAfter: "failed"}, {Number: 2, State: "pending"}}}
			body := doctor.RenderConversation(c)
			for _, want := range []string{"Overall: " + tt.overall, "Step 1: verified", "backup: database:retained:1", "recheck: passed: failed", "Step 2: pending", "recheck: not completed"} {
				if !strings.Contains(body, want) {
					t.Error("outcome omits", want)
				}
			}
			if strings.Contains(body, "Apply is unavailable") {
				t.Fatal("available repair presented as unavailable")
			}
		})
	}
}
