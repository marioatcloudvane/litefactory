package agent_test

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"testing"
)

func TestContributionInheritedCoverageRequiresTrustedAssessment(t *testing.T) {
	for _, fault := range []string{"", "unverified", "missing origin", "missing assessment", "pre-existing", "fresh criterion"} {
		t.Run(fault, func(t *testing.T) {
			scope, c := contributionFixture("validate")
			s := scope.Stories["US-001"]
			check := s.Checks["check-1"]
			check.Executed = false
			check.ProofKind = "inherited-pass"
			check.OriginID = "original-execution"
			check.AssessmentID = "assessment"
			check.AssessmentVerified = true
			switch fault {
			case "unverified":
				check.AssessmentVerified = false
			case "missing origin":
				check.OriginID = ""
			case "missing assessment":
				check.AssessmentID = ""
			case "pre-existing":
				check.ProofKind = "pre-existing-failure"
			case "fresh criterion":
				s.FreshRequired = []string{"AC-001"}
			}
			s.Checks["check-1"] = check
			scope.Stories["US-001"] = s
			_, d := agent.ParseContribution(contributionJSON(t, c), scope)
			if d.Valid != (fault == "") {
				t.Fatalf("valid=%v diagnostics=%+v", d.Valid, d.Diagnostics)
			}
			if s.Checks["check-1"].Executed {
				t.Fatal("inherited proof became fresh execution")
			}
		})
	}
}
