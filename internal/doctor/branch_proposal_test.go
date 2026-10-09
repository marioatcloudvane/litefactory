package doctor

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorBranchProposalBindsCapturedHeads(t *testing.T) {
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	evidence := store.DoctorEvidence{Records: []store.DoctorEvidenceRecord{{Kind: "branch", ID: "lf/406", State: head, Identity: base}}}
	d := Diagnosis{Findings: []Finding{{Explanation: "Branch needs synchronization", Uncertainty: "Local refs only", Evidence: []string{"branch:lf/406"}}}, Proposal: []RepairStep{{Number: 1, Target: "branch:lf/406", Action: &store.DoctorRepairAction{Kind: "sync_branch", Before: head, After: base}, Precondition: "Captured head", Change: "Merge captured base", Reason: "Restore synchronization", Reversibility: "Backed up head", Rollback: "Restore head"}}}
	if err := ValidateDiagnosis(d, evidence); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"before", "after", "target", "invalid-sha"} {
		t.Run(mode, func(t *testing.T) {
			candidate := d
			candidate.Proposal = append([]RepairStep(nil), d.Proposal...)
			action := *d.Proposal[0].Action
			candidate.Proposal[0].Action = &action
			switch mode {
			case "before":
				action.Before = strings.Repeat("c", 40)
			case "after":
				action.After = strings.Repeat("c", 40)
			case "target":
				candidate.Proposal[0].Target = "branch:other"
			case "invalid-sha":
				action.Before = "HEAD"
			}
			if err := ValidateDiagnosis(candidate, evidence); err == nil {
				t.Fatal("unobserved branch input accepted")
			}
		})
	}
}
