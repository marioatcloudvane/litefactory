package submission_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

// Story-003 AC-010: the two self-check tools install only through
// NewBugFixerToolProfile, never through the plain bug workflow catalog the
// bug-analyst profile uses.
func TestNewWorkflowToolProfile_BugCatalogExcludesSelfCheckTools(t *testing.T) {
	t.Parallel()
	profile, err := submission.NewWorkflowToolProfile("bug", "")
	if err != nil {
		t.Fatal(err)
	}
	got := profile.List()
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"communication_preflight", "communication_receipt", "communication_submit"}
	if len(names) != len(want) {
		t.Fatalf("bug workflow catalog=%v want=%v", names, want)
	}
	for _, n := range names {
		found := false
		for _, w := range want {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected tool %q in plain bug catalog", n)
		}
	}
	for _, forbidden := range []string{"bug_self_check_start", "bug_self_check_status"} {
		if profile.Allows(forbidden) {
			t.Fatalf("plain bug workflow profile allows %q", forbidden)
		}
	}
}
