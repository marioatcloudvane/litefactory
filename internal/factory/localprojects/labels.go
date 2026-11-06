package localprojects

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// LabelSetup is a read-only plan for one project. For GitHub nothing is
// stored; for Linear, Linear holds the plan and Apply saves the found IDs.
type LabelSetup struct {
	Linear *LinearLabelSetup
	Found  []string
	Plan   agent.LabelPlan
	repo   github.Repo
	client *github.Client
}

// LabelSetupResult reports an apply. Empty Created and Failed, no Refused and
// no SavedRevision means nothing was written.
type LabelSetupResult struct {
	Created   []string
	Failed    string
	FailedWhy string
	NotTried  []string
	// Linear only.
	Refused         string // refused before any write
	AppearedAdopted []string
	Replanned       *projectconnection.LinearLabelPlan
	LinearCreated   []LinearCreated
	NothingToSave   bool
	SaveRefused     string
	SavedRevision   int64
}

// PlanLabelSetup reads every label and plans, with zero writes. Missing
// or disabled connections, an identity mismatch and an incomplete read refuse.
func (s *Service) PlanLabelSetup(ctx context.Context) (*LabelSetup, error) {
	v, err := s.db.CurrentConnection(ctx, s.repo.ID, store.TicketConnection)
	if err != nil {
		return nil, unavailableText("no ticket connection for this project; run `litefactory repo connection set` first")
	}
	if !v.Enabled {
		return nil, unavailableText("the ticket connection is disabled; run `litefactory repo connection set` first")
	}
	if v.Provider == ticket.Linear {
		return s.planLinearLabelSetup(ctx, v)
	}
	identities, err := turn.LoadIdentities(ctx, s.db, store.ForgeGitHub)
	if err != nil {
		return nil, unavailable(err)
	}
	clients, err := s.factory(identities.BotUserID).Clients(ctx, v, 0)
	if err != nil {
		return nil, unavailable(err)
	}
	repo, err := projectconnection.VerifyDestination(ctx, clients.Code, v)
	if err != nil {
		if errors.Is(err, projectconnection.ErrDestinationChanged) {
			return nil, unavailableText("the saved repository does not match the repository found; nothing was changed")
		}
		return nil, unavailable(fmt.Errorf("repository could not be inspected: %w", err))
	}
	names, err := clients.Code.ListLabelNames(ctx, repo)
	if err != nil {
		return nil, unavailable(fmt.Errorf("labels could not be read in full; nothing was changed: %w", err))
	}
	return &LabelSetup{Found: names, Plan: agent.PlanLabels(agent.WantedLabels(s.options.LabelAgents), names), repo: repo, client: clients.Code}, nil
}

// Apply creates the missing labels one at a time and stops at the first failure.
func (l *LabelSetup) Apply(ctx context.Context) LabelSetupResult {
	if l.Linear != nil {
		return l.Linear.applyLinear(ctx)
	}
	var out LabelSetupResult
	for i, w := range l.Plan.Create {
		if err := l.client.CreateLabel(ctx, l.repo, w.Name, w.Color, w.Description); err != nil {
			out.Failed, out.FailedWhy = w.Name, labelFailureReason(err)
			for _, rest := range l.Plan.Create[i+1:] {
				out.NotTried = append(out.NotTried, rest.Name)
			}
			return out
		}
		out.Created = append(out.Created, w.Name)
	}
	return out
}

func labelFailureReason(err error) string {
	switch {
	case errors.Is(err, github.ErrForbidden), errors.Is(err, github.ErrUnauthorized):
		return "no write access to issues"
	case errors.Is(err, github.ErrRateLimited):
		return "rate limited by GitHub"
	case errors.Is(err, github.ErrUnprocessable):
		return "GitHub refused the label"
	case errors.Is(err, github.ErrNotFound):
		return "repository not found"
	default:
		return "request failed"
	}
}
