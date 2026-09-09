package store

import "testing"

func TestAcceptanceFindingsDistinguishBlockingAndAdvisoryAndWaived(t *testing.T) {
	for _, tc := range []struct {
		name, status, classification, reason string
		criterion                            bool
		want                                 bool
	}{
		{"satisfied", "satisfied", "blocking", "", true, true},
		{"unmet criterion", "unmet", "blocking", "", true, false},
		{"unverified criterion", "unverified", "blocking", "", true, false},
		{"criterion cannot become advisory", "unmet", "advisory", "", true, false},
		{"blocking review", "unmet", "blocking", "", false, false},
		{"advisory review", "unmet", "advisory", "", false, true},
		{"waived unmet criterion", "unmet", "blocking", "Owner accepts documented risk", true, true},
		{"waived missing proof", "unverified", "blocking", "Owner accepts missing evidence", true, true},
		{"waived review", "unmet", "blocking", "Owner accepts review defect", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := AcceptanceFindings{Criteria: []AcceptanceFinding{{Name: "AC-1", Status: "satisfied", Classification: "blocking", Evidence: "retained proof"}}, Reviews: []AcceptanceFinding{{Name: "code", Status: "satisfied", Classification: "blocking", Evidence: "review"}, {Name: "architecture", Status: "satisfied", Classification: "blocking", Evidence: "review"}, {Name: "plan", Status: "satisfied", Classification: "blocking", Evidence: "review"}}}
			target := &f.Reviews[0]
			if tc.criterion {
				target = &f.Criteria[0]
			}
			target.Status = tc.status
			target.Classification = tc.classification
			target.WaiverReason = tc.reason
			if got := f.Accepted(); got != tc.want {
				t.Fatalf("Accepted=%v want %v", got, tc.want)
			}
			if target.Status != tc.status || target.WaiverReason != tc.reason {
				t.Fatal("original finding rewritten")
			}
		})
	}
}
