package doctor

import "testing"

func TestDoctorCommandEnvelope(t *testing.T) {
	for _, tt := range []struct {
		body, operation, text string
		want                  bool
	}{
		{" /DoCtOr Explain it", "diagnose", "Explain it", true}, {"/doctor", "usage", "", true}, {"/doctor stop", "stop", "", true}, {"/doctor STOP", "stop", "", true}, {"/doctor stop now", "usage", "", true}, {"/doctor apply", "apply", "", true}, {"/doctor apply 42", "usage", "", true}, {"/doctor-like explain", "", "", false}, {"quoted /doctor x", "", "", false}, {"> /doctor x", "", "", false}, {"```\n/doctor x\n```", "", "", false}, {"plain followup", "", "", false},
	} {
		t.Run(tt.body, func(t *testing.T) {
			c, ok := ParseCommand(tt.body)
			if ok != tt.want || c.Operation != tt.operation || c.Text != tt.text {
				t.Fatalf("got %+v,%v", c, ok)
			}
		})
	}
}
