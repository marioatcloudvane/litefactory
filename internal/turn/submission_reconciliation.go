package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// reconcileValidationCheckpoint runs under the session and progress locks after
// recovery. Terminal findings reuse live checkpoint decisions so a lost reply
// or different terminal request ID cannot create a second assessment or effect.
func (s *SubmissionSession) reconcileValidationCheckpoint(ctx context.Context, contribution agent.Contribution) (store.SubmissionReceipt, bool, error) {
	var matched store.SubmissionReceipt
	story := s.stories[contribution.Story]
	receipts, err := s.options.DB.ValidationCheckpoints(ctx, s.binding.RunID, s.binding.Attempt, story.Key, story.Revision)
	if err != nil {
		return matched, false, fmt.Errorf("read validation checkpoints: %w", err)
	}
	canonical, err := json.Marshal(contribution)
	if err != nil {
		return matched, false, err
	}
	for _, receipt := range receipts {
		previous, diagnostics := agent.ParseContribution(receipt.Payload, s.scope)
		if !diagnostics.Valid {
			return matched, false, fmt.Errorf("recorded validation checkpoint is invalid for the bound snapshot")
		}
		encoded, err := json.Marshal(previous)
		if err != nil {
			return matched, false, err
		}
		if !bytes.Equal(canonical, encoded) {
			return matched, false, fmt.Errorf("validation findings conflict with recorded checkpoint for %s", story.Key)
		}
		if receipt.EngineeringState != "not_applicable" || (receipt.AcceptanceState != "accepted" && receipt.AcceptanceState != "unverified") {
			return matched, false, fmt.Errorf("validation checkpoint lacks a recorded authoritative decision for %s", story.Key)
		}
		if matched.ID == 0 {
			matched = receipt
		}
	}
	return matched, matched.ID != 0, nil
}
