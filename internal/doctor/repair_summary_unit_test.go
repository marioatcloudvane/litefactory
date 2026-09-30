package doctor_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
)

func TestDoctorRepairSummarySchema(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, payload string
		valid         bool
	}{
		{"exact result/retains operation and observations", `{"repair":{"operation_id":"repair-406","completed_steps":[1,2],"observations":["Rows reread","Backups retained"],"unresolved":["Restart remains pending"]}}`, true},
		{"invented approval/rejected", `{"repair":{"operation_id":"repair-406","approved":true}}`, false},
		{"invented verification/rejected", `{"repair":{"operation_id":"repair-406","verified":true}}`, false},
		{"invented restart/rejected", `{"repair":{"operation_id":"repair-406","restart_command":"kill daemon"}}`, false},
		{"numeric operation identity/rejected", `{"repair":{"operation_id":406}}`, false},
		{"string step identity/rejected", `{"repair":{"completed_steps":["1"]}}`, false},
		{"fractional step identity/rejected", `{"repair":{"completed_steps":[1.5]}}`, false},
		{"object observation/rejected", `{"repair":{"observations":[{"verified":true}]}}`, false},
		{"object unresolved finding/rejected", `{"repair":{"unresolved":[{"ignore":true}]}}`, false},
		{"second result/rejected", `{"repair":{"operation_id":"repair-406"}} {"repair":{"operation_id":"other"}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := doctor.DecodeDiagnosis([]byte(tt.payload))
			if (err == nil) != tt.valid {
				t.Fatalf("decode error=%v; valid=%t", err, tt.valid)
			}
			if !tt.valid {
				return
			}
			want := &doctor.RepairSummary{OperationID: "repair-406", CompletedSteps: []int{1, 2}, Observations: []string{"Rows reread", "Backups retained"}, Unresolved: []string{"Restart remains pending"}}
			if !reflect.DeepEqual(got.Repair, want) {
				t.Fatalf("summary=%+v; want %+v", got.Repair, want)
			}
		})
	}
}
