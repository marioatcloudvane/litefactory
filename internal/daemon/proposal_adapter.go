package daemon

import (
	"context"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/turn"
)

// scheduleAdvisor is intentionally narrower than agent.Runner: advice cannot
// allocate a workflow, select a worktree, or write a run log.
type scheduleAdvisor interface {
	ProposeSchedule(context.Context, agent.ScheduleProposalRequest) (agent.ScheduleProposal, error)
}

type proposalAdapter struct{ advisor scheduleAdvisor }

func newProposalGenerator(kind agent.Kind, runners turn.RunnerSet) ProposalGenerator {
	runner, ok := runners[kind]
	if !ok {
		return nil
	}
	advisor, ok := runner.(scheduleAdvisor)
	if !ok {
		return nil
	}
	return proposalAdapter{advisor: advisor}
}

func (a proposalAdapter) ProposeSchedule(ctx context.Context, request ScheduleProposalRequest) (ScheduleProposal, error) {
	if a.advisor == nil {
		return ScheduleProposal{}, fmt.Errorf("schedule advisor is not configured")
	}
	proposal, err := a.advisor.ProposeSchedule(ctx, agent.ScheduleProposalRequest{
		Description: request.Description,
		ReceivedAt:  request.ReceivedAt,
	})
	if err != nil {
		return ScheduleProposal{}, err
	}
	items := make([]ScheduleProposalItem, len(proposal.Items))
	for i, item := range proposal.Items {
		items[i] = ScheduleProposalItem{Payload: item.Payload, DueAt: item.DueAt}
	}
	return ScheduleProposal{Items: items}, nil
}
