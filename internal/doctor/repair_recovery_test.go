package doctor

import (
	"os"
	"testing"
)

func TestDoctorRepairCrashObservationNeverReplaysFileWrite(t *testing.T) {
	for _, state := range []string{"before", "after", "third"} {
		t.Run(state, func(t *testing.T) {
			s, ref, id, path := fileRepairFixture(t, true)
			ctx := t.Context()
			tools := &RepairTools{service: s, target: ref, operation: id}
			if err := tools.Backup(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.BeginDoctorFileWrite(ctx, id, 1); err != nil {
				t.Fatal(err)
			}
			steps, err := s.DB.DoctorRepairSteps(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if state != "before" {
				data := []byte(steps[0].After)
				if state == "third" {
					data = []byte("unexpected external content")
				}
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			op, err := s.ReconcileRepair(ctx, ref)
			if err != nil || op.State != "needs_review" {
				t.Fatal(op, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("recovery wrote file", err)
			}
			steps, err = s.DB.DoctorRepairSteps(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			want := "uncertain"
			if state == "after" {
				want = "verified"
			}
			if steps[0].State != want {
				t.Fatal(steps)
			}
			if held, err := s.DB.DoctorRepairHeld(ctx, ref.String()); err != nil || !held {
				t.Fatal("partial hold released", held, err)
			}
			if err = tools.Write(ctx, 1); err == nil {
				t.Fatal("recovery restored writing authority")
			}
		})
	}
}
