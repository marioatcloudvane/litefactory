package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

// prepareNaturalExecution fences effects before the existing command dispatcher.
// An interrupted effect is reported honestly rather than replayed blindly.
func (s *Service) prepareNaturalExecution(ctx context.Context, r *store.ChatReply, at time.Time) (bool, error) {
	execution, err := s.db.ReadNaturalExecution(ctx, r.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.ReplyText != "" {
		// Saved outcomes are immutable on delivery retry; no effect remains
		// to authorize and current permissions cannot rewrite historical facts.
		return true, nil
	}
	if execution.Transport != s.transport || execution.Transport != r.Transport || execution.OwnerID != s.owner || execution.ChannelRef != r.ChannelRef || !s.channels[r.ChannelRef] {
		r.ReplyText = "This confirmed action is no longer permitted in this channel. Nothing further was applied."
		r.OutcomeStatus = string(presentation.Refusal)
		if execution.State != "authorized" {
			r.ReplyText = "This action is no longer permitted here, and its prior effect is uncertain. Check the target before requesting it again."
			r.OutcomeStatus = string(presentation.Uncertain)
		}
		return true, nil
	}
	switch execution.State {
	case "finished", "uncertain":
		r.ReplyText = execution.Outcome
		if execution.State == "uncertain" {
			r.OutcomeStatus = string(presentation.Uncertain)
		}
		if r.ReplyText == "" {
			r.ReplyText = "The saved action has completed, but its outcome is unavailable. Please check the target before sending it again."
			r.OutcomeStatus = string(presentation.Uncertain)
		}
		return true, nil
	case "executing":
		if r.ReplyText == "" {
			r.ReplyText = "The confirmed action was interrupted after execution began. Its effect is uncertain; check the target or use /status before requesting it again."
			r.OutcomeStatus = string(presentation.Uncertain)
		}
		return true, nil
	case "authorized":
	default:
		r.ReplyText = "This action's execution record is unavailable. Check the target before requesting it again."
		r.OutcomeStatus = string(presentation.Uncertain)
		return true, nil
	}
	// A saved handler outcome is authoritative even if the process stopped before
	// the execution record was finalized.
	if r.ReplyText != "" {
		return true, nil
	}
	var p naturalPayload
	if err = json.Unmarshal([]byte(execution.Payload), &p); err != nil {
		return true, err
	}
	if p.Invocation.Command != r.Command || !maps.Equal(p.Invocation.Arguments, r.Arguments) {
		r.ReplyText = "The protected invocation changed. Nothing was applied; request a fresh proposal."
		r.OutcomeStatus = string(presentation.Refusal)
		return true, nil
	}
	if refusal, e := s.recheckNatural(ctx, p, at); e != nil {
		return true, e
	} else if refusal != "" {
		r.ReplyText = refusal
		r.OutcomeStatus = string(presentation.Refusal)
		return true, nil
	}
	validated, effect, e := s.ValidateInvocation(p.Invocation)
	if e != nil || effect != ChangeEffect {
		r.ReplyText = "This action is no longer enabled. Nothing was applied; try /help."
		r.OutcomeStatus = string(presentation.Refusal)
		return true, nil //nolint:nilerr // Validation failure becomes a saved refusal, not an execution retry.
	}
	r.Arguments = validated.Arguments
	r.ResolvedRepoID = p.RepoID
	if err = s.db.StartNaturalExecution(ctx, r.MessageID, at); err != nil {
		return true, err
	}
	return true, nil
}

// finishNaturalOutcome saves the factual result before presentation or delivery.
func (s *Service) finishNaturalOutcome(ctx context.Context, r store.ChatReply, text string, at time.Time) error {
	execution, err := s.db.ReadNaturalExecution(ctx, r.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if execution.State != "authorized" && execution.State != "executing" {
		return nil
	}
	return s.db.FinishNaturalExecution(ctx, r.MessageID, text, r.OutcomeStatus == string(presentation.Uncertain), at)
}
