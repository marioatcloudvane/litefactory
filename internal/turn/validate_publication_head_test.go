package turn

import (
	"testing"
)

func TestValidatePublicationHeadObservesLocalBranchWithoutFetching(t *testing.T) {
	f, _, _ := assembledImplementFixture(t, nil)
	// Make the origin unreachable: this read must still observe the local branch.
	f.repo.Git("branch", "lf/123-feature")
	f.repo.SetOriginURL("/not-an-available-remote")
	f.session.Branch = "lf/123-feature"
	want := f.repo.Git("rev-parse", "refs/heads/lf/123-feature")
	got, err := ValidatePublicationHead(t.Context(), f.opts, f.session)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("head=%q, want local %q", got, want)
	}
	f.session.Branch = "missing-branch"
	if _, err := ValidatePublicationHead(t.Context(), f.opts, f.session); err == nil {
		t.Fatal("missing branch became a current publication head")
	}
}
