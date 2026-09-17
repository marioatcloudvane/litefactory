package turn

import (
	"context"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// SubIssueCreator retains the shared forge dependency used by managed scope
// admission and publication. Production supplies *github.Client; consumers
// require their narrower managed interfaces before accessing the forge.
type SubIssueCreator interface {
	CreateSubIssues(ctx context.Context, repo github.Repo, parent int, specs []github.SubIssueSpec) ([]github.SubIssueResult, error)
}

// planbriefFileMode is the mode of the factory's link-only Plan index.
const planbriefFileMode = 0o644

// locateSessionDir locates the retained Plan brief for managed publication.
// Its date may precede the current attempt, so retain the shared session lookup.
func locateSessionDir(prep *preparation, log *slog.Logger) (string, bool) {
	return findSessionDir(
		prep.worktree.Path(),
		prep.session.Branch,
		prep.ref.Issue,
		prep.run.StartedAt,
		planbrief.FileName,
		log,
	)
}
