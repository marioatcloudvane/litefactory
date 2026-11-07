package planbrief

import (
	"strings"
	"testing"
)

func TestValidateStorySizing(t *testing.T) {
	for _, tc := range []struct {
		name, size, reason string
		want               bool
	}{
		{"small", "S", "small", true}, {"medium", "M", "several parts", true}, {"large", "L", "many parts", true},
		{"wrong case", "s", "reason", false}, {"padded size", " S", "reason", false}, {"missing size", "", "reason", false},
		{"blank reason", "M", " \t ", false}, {"invalid UTF8", "M", string([]byte{0xff}), false},
		{"280 Unicode", "M", "  " + strings.Repeat("界", 280) + " \t", true}, {"281 Unicode", "M", strings.Repeat("界", 281), false},
		{"leading newline", "S", "\nreason", false}, {"trailing CR", "S", "reason\r", false}, {"inner newline", "S", "a\nb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, why := ValidateStorySizing(tc.size, tc.reason)
			if got != tc.want || (!got && why == "") {
				t.Fatalf("valid=%v reason=%q", got, why)
			}
		})
	}
}
