package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// aggregateResultInput adapts the existing aggregate reference and capture
// types to the shared preflight/submit/receipt transport. It carries no factory
// identity or story event, and cannot itself satisfy required final output.
type aggregateResultInput struct {
	Engineering agent.EngineeringContribution `json:"engineering"`
	Capture     agent.AggregateCapture        `json:"capture"`
}

func aggregateInput(data []byte) bool {
	var routing struct {
		Capture json.RawMessage `json:"capture"`
	}
	return json.Unmarshal(data, &routing) == nil && len(routing.Capture) > 0
}

func (s *AgentResultService) prepareAggregateInput(ctx context.Context, data []byte) (*store.ResultAggregate, agentresult.UpdateDiagnostics, error) {
	d := agentresult.UpdateDiagnostics{Diagnostics: aggregateDiagnostics()}
	if _, err := s.trustedContext(ctx); err != nil {
		return nil, d, err
	}
	var input aggregateResultInput
	if len(data) > agentresult.MaxBytes {
		aggregateFault(&d.Diagnostics, "$", "limit", "Use bounded aggregate capture input.")
	} else if err := decodeMCPArguments(data, &input); err != nil {
		aggregateFault(&d.Diagnostics, "$", "structure", "Use only engineering and the typed aggregate capture, without factory identities.")
	} else if s.binding.Mode != "implement" || s.prepareAggregate == nil {
		aggregateFault(&d.Diagnostics, "$.capture", "authorization", "Aggregate capture requires the authorized Implement capture route.")
	} else {
		if _, err := agent.AggregateReference(input.Engineering.SnapshotID, input.Engineering.CheckIDs); err != nil {
			aggregateFault(&d.Diagnostics, "$.engineering", "reference", err.Error())
		}
		_, err := input.Capture.SnapshotFiles()
		if err != nil {
			aggregateFault(&d.Diagnostics, "$.capture", "capture", err.Error())
		}
		ids := make([]string, 0, len(input.Capture.Checks))
		for _, check := range input.Capture.Checks {
			ids = append(ids, check.ID)
		}
		expected := slices.Clone(input.Engineering.CheckIDs)
		slices.Sort(ids)
		slices.Sort(expected)
		if !slices.Equal(ids, expected) {
			aggregateFault(&d.Diagnostics, "$.engineering.check_ids", "coverage", "Reference every supplied aggregate check exactly once.")
		}
		if d.Valid {
			aggregate, err := s.prepareAggregate(ctx, s.binding, input.Engineering, input.Capture)
			if err != nil {
				aggregateFault(&d.Diagnostics, "$.capture", "evidence", err.Error())
			} else {
				return &aggregate, d, nil
			}
		}
	}
	return nil, d, nil
}

func (s *AgentResultService) submitAggregateInput(ctx context.Context, requestID string, data []byte) (AgentResultSubmission, error) {
	aggregate, d, err := s.prepareAggregateInput(ctx, data)
	if err != nil {
		return AgentResultSubmission{}, err
	}
	diagnostics, err := json.Marshal(d)
	if err != nil {
		return AgentResultSubmission{}, fmt.Errorf("encode aggregate diagnostics: %w", err)
	}
	baseline, err := s.Baseline(ctx)
	if err != nil {
		return AgentResultSubmission{}, err
	}
	receipt, err := s.db.RecordResult(ctx, store.ResultRecord{Binding: s.binding, RequestID: requestID, Payload: slices.Clone(data), Diagnostics: diagnostics, ExpectedRevision: baseline.Projection.Revision, Accepted: d.Valid, Aggregate: aggregate})
	if err != nil {
		return AgentResultSubmission{}, err
	}
	return s.response(ctx, receipt)
}
