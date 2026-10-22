package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

var errConversationInstructions = errors.New("conversation instruction validation failed")

// Migration uses only the pinned compatibility files, never current owner text.
func (s *Service) migrateConversationInstructions(ctx context.Context, at time.Time) error {
	legacy, err := s.db.ListLegacyConversations(ctx)
	if err != nil || len(legacy) == 0 {
		return err
	}
	baseline, err := instructionpackage.LegacyConversation(ctx)
	if err != nil {
		return err
	}
	raw, err := baseline.Encode()
	if err != nil {
		return err
	}
	expected := store.ConversationLegacyInstructions{Native: baseline.Native(), Factory: baseline.Factory(), Correction: baseline.Correction(), ActionOutcome: baseline.ActionOutcome()}
	var failures []error
	for _, c := range legacy {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.db.BackfillLegacyConversationCapture(ctx, c.ID, raw, expected, at); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Service) restoreConversationInstructions(ctx context.Context, id string, at time.Time) (*instructionpackage.ConversationCapture, error) {
	raw, legacy, err := s.db.ReadConversationCapture(ctx, id)
	var capture *instructionpackage.ConversationCapture
	if err == nil {
		capture, err = instructionpackage.RestoreConversation(raw)
		if err == nil && capture.Legacy() != legacy {
			err = errors.New("conversation instruction provenance mismatch")
		}
	}
	if err == nil {
		return capture, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Diagnostics deliberately omit retained instruction bodies and parser errors.
	reason := "retained conversation instructions unavailable or invalid; model calls paused"
	healthErr := s.db.SetConversationHealth(ctx, id, reason, at)
	return nil, errors.Join(fmt.Errorf("%w: %s", errConversationInstructions, reason), healthErr)
}

func (s *Service) logConversationInstructionDiagnostics(ctx context.Context, diagnostics []instructionpackage.Diagnostic) {
	if s.Logger == nil {
		return
	}
	for _, d := range diagnostics {
		s.Logger.WarnContext(ctx, "conversation instruction source fallback", "role", d.Role, "file", d.File, "category", d.Category, "selected_source", d.SelectedSource)
	}
}

// Saved prompts remain authoritative. A differing retained instruction identity
// pauses work instead of rewriting a journaled request from current sources.
func (s *Service) validateConversationPrompt(ctx context.Context, id, prompt string, capture *instructionpackage.ConversationCapture, factory bool, at time.Time) error {
	var fields struct{ Instructions, Correction string }
	err := json.Unmarshal([]byte(prompt), &fields)
	valid := err == nil && fields.Instructions == capture.Native()
	if factory {
		valid = err == nil && (fields.Instructions == capture.Factory() || fields.Instructions == capture.Factory()+" "+capture.ActionOutcome()) && (fields.Correction == "" || fields.Correction == capture.Correction())
	}
	if valid {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	reason := "saved conversation prompt conflicts with retained instructions; model calls paused"
	return errors.Join(fmt.Errorf("%w: %s", errConversationInstructions, reason), s.db.SetConversationHealth(ctx, id, reason, at))
}

// refreshConversationInstructions brings a retained binding onto the
// instructions this daemon started with. Bindings capture their instructions
// once, so without it a deploy with new role text never reached an existing
// conversation. It runs only at a turn boundary (the store refuses while a
// turn is pending or running, while paused, or before legacy migration), so
// no journaled prompt of an open turn ever meets a different capture, and the
// native session continues. Any failure keeps the retained capture.
func (s *Service) refreshConversationInstructions(ctx context.Context, r *conversationRuntime, id string, at time.Time) {
	if r.current == nil {
		return
	}
	raw, _, err := s.db.ReadConversationCapture(ctx, id)
	if err != nil {
		return
	}
	retained, err := instructionpackage.RestoreConversation(raw)
	if err != nil || !conversationInstructionsDiffer(retained, r.current) {
		return
	}
	next, err := r.current.Encode()
	if err != nil {
		return
	}
	refreshed, err := s.db.RefreshConversationCapture(ctx, id, next, at)
	if s.Logger == nil {
		return
	}
	if err != nil {
		s.Logger.WarnContext(ctx, "conversation instruction refresh failed", "conversation_id", id)
	} else if refreshed {
		s.Logger.InfoContext(ctx, "conversation instructions refreshed", "conversation_id", id, "from", retained.Digest(), "to", r.current.Digest())
	}
}

// conversationInstructionsDiffer compares the instruction text the model sees,
// not provenance: equal text needs no refresh.
func conversationInstructionsDiffer(a, b *instructionpackage.ConversationCapture) bool {
	return a.Native() != b.Native() || a.Factory() != b.Factory() || a.Correction() != b.Correction() || a.ActionOutcome() != b.ActionOutcome() || a.Reasoner() != b.Reasoner()
}
