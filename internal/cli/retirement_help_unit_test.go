package cli

import (
	"strings"
	"testing"
)

// US-003 AC-004: help is available without opening repository/runtime state.
func TestRetirementHelp_ExplainsAutomaticAndPreviewBehavior(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{"clean/active daily retention", []string{"task", "clean", "--help"}, []string{"closed issues daily", "worktree_retain_days", "last completed run", "creation", "no runs", "task retire", "--force"}},
		{"retire/advisory only", []string{"task", "retire", "--help"}, []string{"advisory", "fresh remote refs", "each issue directly", "Nothing is deleted", "ownership is retained", "schedule is unchanged", "verified again"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			router, stdout, stderr := newTestRouter(t)
			if code := router.Run(t.Context(), tt.args); code != ExitOK {
				t.Fatalf("exit = %d; stderr: %s", code, stderr)
			}
			out := stdout.String() + stderr.String()
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q:\n%s", want, out)
				}
			}
		})
	}
}
