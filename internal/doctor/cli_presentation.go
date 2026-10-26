package doctor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
)

func (s *Service) proposalForPresentation(ctx context.Context, cid int64) (int64, []RepairStep, error) {
	id, data, err := s.DB.DoctorProposalReference(ctx, cid)
	if err != nil || len(data) == 0 {
		return id, nil, err
	}
	body, err := s.Redactor.Apply(ctx, string(data), 131072)
	if err != nil {
		return 0, nil, err
	}
	if body.Truncated() {
		return 0, nil, errors.New("doctor proposal exceeds presentation budget")
	}
	var proposal []RepairStep
	if err = json.Unmarshal([]byte(body.String()), &proposal); err != nil {
		return 0, nil, err
	}
	return id, proposal, nil
}

// RecordPresentation is called only after the complete CLI output was written.
// It binds the displayed immutable revision, refusing a proposal changed meanwhile.
func (s *Service) RecordPresentation(ctx context.Context, ref naming.Ref, displayed Conversation) (string, error) {
	if displayed.Review != nil || displayed.ProposalID == 0 || len(displayed.CurrentProposal) == 0 {
		return "", nil
	}
	ref = canonicalTarget(ref)
	if displayed.Conversation.TaskRef != ref.String() {
		return "", errors.New("doctor presentation target mismatch")
	}
	id := randomID()
	if err := s.DB.PresentDoctorProposal(ctx, ref.String(), id, displayed.ProposalID); err != nil {
		return "", err
	}
	return id, nil
}
