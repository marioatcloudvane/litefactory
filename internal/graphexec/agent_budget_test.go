package graphexec

import "testing"

func TestAgentBudgetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"1", true}, {"60", true}, {"120", true}, {"1440", true}, {"0", false}, {"1441", false}, {"1.5", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			_, present, err := AgentBudget(map[string]string{BudgetAttribute: tc.value})
			if (err == nil) != tc.valid || (tc.valid && !present) {
				t.Fatalf("present=%v error=%v", present, err)
			}
		})
	}
	_, present, err := AgentBudget(nil)
	if present || err != nil {
		t.Fatalf("missing budget present=%v error=%v", present, err)
	}
	values := AgentAttributes()[BudgetAttribute].Values
	if len(values) != 1440 || values[0] != "1" || values[1439] != "1440" {
		t.Fatalf("budget metadata length = %d", len(values))
	}
}
