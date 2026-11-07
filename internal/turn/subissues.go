package turn

import (
	"context"

	"github.com/cloudvane/litefactory/internal/github"
)

// SubIssueCreator retains the shared forge dependency used by managed scope
// admission and publication. Production supplies *github.Client; consumers
// require their narrower managed interfaces before accessing the forge.
type SubIssueCreator interface {
	CreateSubIssues(ctx context.Context, repo github.Repo, parent int, specs []github.SubIssueSpec) ([]github.SubIssueResult, error)
}

// planbriefFileMode is the mode of the factory's link-only Plan index.
const planbriefFileMode = 0o644
