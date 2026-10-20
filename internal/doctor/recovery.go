package doctor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// RecoverSource adopts a durable accepted result after lost process/output
// without another provider invocation. It never reopens stopped authority.
func (s *Service) RecoverSource(ctx context.Context, ref naming.Ref, source string) (out Conversation, err error) {
	if err := s.validate(ctx, ref); err != nil {
		return out, err
	}
	unlock, err := s.lock(ref)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	a, request, data, err := s.DB.DoctorSourceReceipt(ctx, source)
	if err != nil {
		return out, err
	}
	if err := s.checkRetainedInstructions(ctx, a.ID); err != nil {
		return out, err
	}
	owner, err := s.DB.DoctorConversationByID(ctx, a.ConversationID)
	if err != nil {
		return out, err
	}
	if owner.TaskRef != ref.String() {
		return out, store.ErrDoctorFenced
	}
	if a.State != "completed" {
		if a.State != "running" {
			return out, store.ErrDoctorFenced
		}
		var evidence store.DoctorEvidence
		if err := json.Unmarshal([]byte(a.Evidence), &evidence); err != nil {
			return out, err
		}
		results, err := BindResults(ctx, s.DB, a.ID, evidence, s.Redactor)
		if err != nil {
			return out, err
		}
		if err := results.Adopt(ctx, request); err != nil {
			return out, err
		}
	}
	out, err = s.ReadConversation(ctx, ref)
	if err != nil {
		return out, err
	}
	// ReadConversation may include a later CLI diagnosis; publication of this
	// source must retain exactly its own accepted findings and proposal.
	safe, err := s.Redactor.Apply(ctx, string(data), 131072)
	if err != nil {
		return out, err
	}
	if safe.Truncated() {
		return out, errors.New("doctor recovery result exceeds redaction bound")
	}
	diagnosis, err := DecodeDiagnosis([]byte(safe.String()))
	if err != nil {
		return out, err
	}
	out.Conversation = owner
	out.Diagnosis = &diagnosis
	out.CurrentProposal = diagnosis.Proposal
	return out, nil
}
