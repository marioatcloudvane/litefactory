package doctor_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
)

func TestDoctorCommandBoundaries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body, operation, text string
		envelope                    bool
	}{
		{"unicode leading space/diagnosis", "\u2003/doctor\tExplain\nthis issue", "diagnose", "Explain\nthis issue", true},
		{"newline control/exact stop", "/doctor\nSTOP\r\n", "stop", "", true},
		{"mixed case control/exact apply", "/DOCTOR\tApPlY ", "apply", "", true},
		{"multiline stop argument/usage", "/doctor stop\nthis issue", "usage", "", true},
		{"multiline apply argument/usage", "/doctor apply\tcurrent", "usage", "", true},
		{"apply prefix in diagnostic text/diagnosis", "/doctor applying this would fail", "diagnose", "applying this would fail", true},
		{"stop prefix in diagnostic text/diagnosis", "/doctor stopped during diagnosis", "diagnose", "stopped during diagnosis", true},
		{"instruction limit/retained", "/doctor " + strings.Repeat("x", 32768), "diagnose", strings.Repeat("x", 32768), true},
		{"instruction overflow/usage", "/doctor " + strings.Repeat("x", 32769), "usage", "", true},
		{"unicode byte overflow/usage", "/doctor " + strings.Repeat("界", 10923), "usage", "", true},
		{"token punctuation/normal input", "/doctor: explain", "", "", false},
		{"token slash/normal input", "/doctor/stop", "", "", false},
		{"inline code/stop", "`/doctor stop`", "stop", "", true},
		{"quoted token/normal input", "\"/doctor\" stop", "", "", false},
		{"nonleading second line/normal input", "Please investigate\n/doctor stop", "", "", false},
		{"only whitespace/normal input", " \n\t", "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.envelope && !strings.ContainsAny(tt.body, "\r\n`") {
				wrapped, ok := doctor.ParseCommand("`" + strings.TrimSpace(tt.body) + "`")
				if !ok || wrapped.Operation != tt.operation || wrapped.Text != tt.text {
					t.Errorf("wrapped command changed semantics: %+v", wrapped)
				}
			}
			got, ok := doctor.ParseCommand(tt.body)
			if ok != tt.envelope || got.Operation != tt.operation || got.Text != tt.text {
				t.Fatalf("got operation=%q text length=%d envelope=%t; want operation=%q text length=%d envelope=%t", got.Operation, len(got.Text), ok, tt.operation, len(tt.text), tt.envelope)
			}
		})
	}
}
