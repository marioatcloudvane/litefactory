package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func TestResultDiagnosticError_PreservesCorrection(t *testing.T) {
	for _, tc := range []struct{ name, path, code, message string }{
		{"malformed field/actionable rejection", "$.result.comment", "type", "Supply a string comment."},
		{"missing evidence/actionable rejection", "$.result.payload.implement.aggregate_checks", "missing_evidence", "Reference retained aggregate checks."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := ar.Diagnostics{Complete: true, Diagnostics: []agent.ContributionDiagnostic{{Path: tc.path, Code: tc.code, Message: tc.message}}}
			got := resultDiagnosticError(d)
			if got == nil || !strings.Contains(got.Error(), tc.path) || !strings.Contains(got.Error(), tc.message) {
				t.Fatalf("rejection lost field or correction: %v", got)
			}
		})
	}
}
