package planbrief

import (
	"strings"
	"testing"
)

func TestValidateStorySizing(t *testing.T) {
	supported, unsupported := PlanParallelVersion, PlanParallelVersion+1
	for _, tc := range []struct {
		name, size, reason string
		route              *int
		want               bool
	}{
		{"small", "S", "small", nil, true}, {"medium", "M", "several parts", &supported, true}, {"large", "L", "many parts", nil, true},
		{"wrong case", "s", "reason", nil, false}, {"padded size", " S", "reason", nil, false}, {"missing size", "", "reason", nil, false},
		{"blank reason", "M", " \t ", nil, false}, {"invalid UTF8", "M", string([]byte{0xff}), nil, false},
		{"280 Unicode", "M", "  " + strings.Repeat("界", 280) + " \t", nil, true}, {"281 Unicode", "M", strings.Repeat("界", 281), nil, false},
		{"leading newline", "S", "\nreason", nil, false}, {"trailing CR", "S", "reason\r", nil, false}, {"inner newline", "S", "a\nb", nil, false},
		{"unsupported route", "S", "reason", &unsupported, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, why := ValidateStorySizing(tc.size, tc.reason, tc.route)
			if got != tc.want || (!got && why == "") {
				t.Fatalf("valid=%v reason=%q", got, why)
			}
		})
	}
}
