package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/turn"
)

type recordingScheduleAdvisor struct {
	request agent.ScheduleProposalRequest
}

func (r *recordingScheduleAdvisor) Run(context.Context, agent.Request) (*agent.RunResult, error) {
	return nil, nil
}

func (r *recordingScheduleAdvisor) ProposeSchedule(_ context.Context, request agent.ScheduleProposalRequest) (agent.ScheduleProposal, error) {
	r.request = request
	return agent.ScheduleProposal{Items: []agent.ScheduleProposalItem{{
		Payload: "/implement T-1", DueAt: time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC),
	}}}, nil
}

func TestNewProposalGeneratorUsesOnlyTheConfiguredDefaultAdvisor(t *testing.T) {
	selected := &recordingScheduleAdvisor{}
	other := &recordingScheduleAdvisor{}
	generator := newProposalGenerator("selected", turn.RunnerSet{"selected": selected, "other": other})
	if generator == nil {
		t.Fatal("newProposalGenerator returned nil for a schedule-capable configured runner")
	}
	received := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	proposal, err := generator.ProposeSchedule(t.Context(), ScheduleProposalRequest{Description: "split it", ReceivedAt: received})
	if err != nil {
		t.Fatalf("ProposeSchedule: %v", err)
	}
	if selected.request.Description != "split it" || !selected.request.ReceivedAt.Equal(received) {
		t.Errorf("selected request = %+v", selected.request)
	}
	if other.request.Description != "" {
		t.Errorf("non-default advisor was called: %+v", other.request)
	}
	if len(proposal.Items) != 1 || proposal.Items[0].Payload != "/implement T-1" {
		t.Fatalf("proposal = %+v", proposal)
	}
}
