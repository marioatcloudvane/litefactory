package turn

import (
	"strings"
	"testing"
)

func TestReconciledDependencyGraph(t *testing.T) {
	for _, tt := range []struct {
		name   string
		graph  map[string][]string
		reason string
	}{
		{"attached acyclic", map[string][]string{"US-001": {}, "US-002": {"US-001"}, "US-003": {"US-001", "US-002"}}, ""},
		{"self cycle", map[string][]string{"US-001": {"US-001"}}, "cycle"},
		{"mutual contradiction", map[string][]string{"US-001": {"US-002"}, "US-002": {"US-001"}}, "cycle"},
		{"unattached follow-up", map[string][]string{"US-001": {"US-099"}}, "US-099 is not an attached story"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := reconciledDependencyGraph(tt.graph)
			if tt.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "pending:") || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("got %v want pending %s", err, tt.reason)
			}
		})
	}
}
