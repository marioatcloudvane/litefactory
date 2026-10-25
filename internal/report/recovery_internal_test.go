package report

import (
	"strings"
	"testing"
	"time"
)

func TestRecoveryUsesCurrentActionDisposition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status, want string
		scheduled    bool
	}{
		{"pending", "Please wait", true}, {"reserved", "Please wait", true}, {"running", "completion is not yet confirmed", false}, {"succeeded", "retained result", false}, {"failed", "Inspect the retained result", false}, {"cancelled", "changed context", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			text := recoverySection(&Recovery{ActionID: 42, Status: tc.status, DueAt: time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC), ManualReason: "changed context"})
			if !strings.Contains(text, tc.want) || strings.Contains(text, "/schedule") {
				t.Fatalf("incorrect guidance: %s", text)
			}
			if strings.Contains(text, "eligible at") != tc.scheduled {
				t.Fatalf("incorrect scheduling claim: %s", text)
			}
		})
	}
}
