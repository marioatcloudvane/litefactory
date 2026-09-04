package agent_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestModeVocabularyAndLabels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ value, label string }{
		{"research", "Mode: Research"}, {"plan", "Mode: Plan"},
		{"implement", "Mode: Implement"}, {"validate", "Mode: Validate"}, {"spike", "Mode: Spike"},
	} {
		mode, ok := agent.ParseMode(tc.value)
		if !ok || mode.Label() != tc.label {
			t.Errorf("ParseMode(%q) = %q, %t", tc.value, mode, ok)
		}
		if got, ok := agent.ParseModeLabel(tc.label); !ok || got != mode {
			t.Errorf("ParseModeLabel(%q) = %q, %t", tc.label, got, ok)
		}
	}
	for _, bad := range []string{"", "infer", "Research", "implementation"} {
		if _, ok := agent.ParseMode(bad); ok {
			t.Errorf("ParseMode(%q) succeeded", bad)
		}
	}
}
