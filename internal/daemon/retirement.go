package daemon

import (
	"context"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/maintenance"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// RetirementIssues retains the narrow factory-owned closure reader contract.
type RetirementIssues = maintenance.RetirementIssues

// RetirementOptions retains the factory-owned retirement value for daemon callers.
type RetirementOptions = maintenance.RetirementOptions

// RetirementDecision retains the factory-owned retirement value for daemon callers.
type RetirementDecision = maintenance.RetirementDecision

// RetirementEvaluation retains the factory-owned retirement value for daemon callers.
type RetirementEvaluation = maintenance.RetirementEvaluation

// ObserveRetirementGit delegates read-only resource observation to its factory owner.
func ObserveRetirementGit(ctx context.Context, owner store.RetirementOwner) (git.RetirementObservation, error) {
	return maintenance.ObserveRetirementGit(ctx, owner)
}

// RetirementIssueClosed delegates closure identity validation to its factory owner.
func RetirementIssueClosed(repo store.Repo, number int, issue github.Issue) bool {
	return maintenance.RetirementIssueClosed(repo, number, issue)
}

// EvaluateRetirement delegates advisory retirement decisions to their factory owner.
func EvaluateRetirement(ctx context.Context, options RetirementOptions, owner store.RetirementOwner) RetirementEvaluation {
	return maintenance.EvaluateRetirement(ctx, options, owner)
}

// retirementTicketIssues reads closure through the repository's ticket
// connection (Linear or GitHub), falling back to the forge without one.
type retirementTicketIssues struct{ d *Daemon }

// A session-scoped read with project connections addresses the session's
// ticket container, as readScheduledIssue does, not the code repository: a
// Linear destination never equals the GitHub owner/name.
func (r retirementTicketIssues) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope); r.d.connections != nil && scope.sessionID > 0 {
		binding, err := r.d.db.GetSessionTicket(ctx, scope.sessionID)
		if err != nil {
			return github.Issue{}, err
		}
		owner, name, _ := strings.Cut(binding.Locator.Container, "/")
		repo = github.Repo{Owner: owner, Name: name}
	}
	return r.d.readIssue(ctx, repo, number)
}
