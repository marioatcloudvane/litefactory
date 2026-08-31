package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
)

// TestRunInputCarriesBothShas pins the plumbing a permalink depends on.
//
// internal/report renders links from sha_after, not from the commit sha, and
// the two are not interchangeable: a run holding the Bash tool may commit for
// itself, which leaves [Result.CommitSHA] empty while the branch has moved and
// [Result.SHAAfter] says where to. That is a supported path, not an edge — so a
// render input carrying only CommitSHA would render a blank link on exactly the
// runs whose work is real and published.
//
// Both shas are asserted on every case, including the ones where they agree,
// because the failure this guards against is one of them quietly not being
// copied at all.
func TestRunInputCarriesBothShas(t *testing.T) {
	t.Parallel()

	const head = "0badc0ffee1234567890abcdef1234567890abcd"

	tests := []struct {
		name          string
		commitSHA     string
		shaAfter      string
		wantCommitSHA string
		wantSHAAfter  string
	}{
		{
			name:          "litefactory made the commit, so the two agree",
			commitSHA:     head,
			shaAfter:      head,
			wantCommitSHA: head,
			wantSHAAfter:  head,
		},
		{
			name: "claude committed for itself: no commit sha, a moved head",
			// git.CommitResult reports Committed false and an empty SHA here,
			// with Head ahead of sha_before. AC-3.24.
			commitSHA:     "",
			shaAfter:      head,
			wantCommitSHA: "",
			wantSHAAfter:  head,
		},
		{
			name:      "nothing moved, so there is nothing to pin to",
			commitSHA: "",
			shaAfter:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Ordinal 2 keeps this off the nextOrdinal path, which is the only
			// branch of runInput that reads the database.
			result := Result{
				Disposition: DispositionRan,
				Ref:         naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123},
				Branch:      "lf/123-add-a-widget",
				CommitSHA:   tt.commitSHA,
				SHAAfter:    tt.shaAfter,
				Pushed:      tt.shaAfter != "",
			}
			result.Run.Ordinal = 2

			in, err := (&Reporter{}).runInput(t.Context(), result)
			if err != nil {
				t.Fatalf("runInput: %v", err)
			}
			if in.CommitSHA != tt.wantCommitSHA {
				t.Errorf("report.Run.CommitSHA = %q, want %q", in.CommitSHA, tt.wantCommitSHA)
			}
			if in.SHAAfter != tt.wantSHAAfter {
				t.Errorf("report.Run.SHAAfter = %q, want %q", in.SHAAfter, tt.wantSHAAfter)
			}
			if in.Pushed != result.Pushed {
				t.Errorf("report.Run.Pushed = %v, want %v", in.Pushed, result.Pushed)
			}
		})
	}
}
