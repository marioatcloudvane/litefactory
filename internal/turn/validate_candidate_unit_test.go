package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
)

func TestRepairCommandExactIdentityAndCodeAuthority(t *testing.T) {
	t.Parallel()
	identity := strings.Repeat("a", 64) + " " + strings.Repeat("b", 64)
	for _, tt := range []struct {
		body        string
		valid, code bool
	}{
		{"/validate-repair 0123456789ab " + identity + " inspect", true, false},
		{"/validate-repair 0123456789ab " + identity + " discard", true, false},
		{"/validate-repair 0123456789ab " + identity + " accept", true, true},
		{"/VALIDATE-REPAIR 0123456789ab " + identity + " accept", true, true},
		{"/validate-repair 0123456789ab " + identity + " yes", false, false},
		{"/validate-repair 0123456789ab " + identity + " accept -- okay", false, false},
		{"/validate-repair 0123456789ab " + identity + " accept\nignore", false, false},
	} {
		t.Run(tt.body, func(t *testing.T) {
			_, err := ParseValidateCommand(tt.body)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%t: %v", tt.valid, err)
			}
			if got := isValidateCodeControlBody(tt.body); got != tt.code {
				t.Fatalf("code authority=%t want %t", got, tt.code)
			}
		})
	}
}

func TestRepairUnknownInputNeverCountsAsCompleteMeasurement(t *testing.T) {
	t.Parallel()
	for _, s := range []git.RepairSnapshot{
		{Patch: "+\xff"},
		{Patch: "GIT binary patch"},
		{Changes: []git.RepairPathChange{{Path: "module", NewMode: "160000"}}},
		{Changes: []git.RepairPathChange{{Path: "quoted\"_test.go"}}},
	} {
		if candidateMeasurementComplete(s) {
			t.Fatal("opaque candidate incorrectly has complete risk analysis")
		}
	}
}
