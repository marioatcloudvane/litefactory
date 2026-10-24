package messaging

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// validateSavedContinuation checks current authority without changing the saved
// prompt. A missing source or changed target stops continuity instead of silently
// giving old source bytes or references new authority.
func (r *factoryConversationRun) validateSavedContinuation(ctx context.Context, raw string, prompt *factoryConversationPrompt) error {
	if err := r.s.validateConversationPrompt(ctx, r.c.ID, raw, r.instructions, true, r.at); err != nil {
		return err
	}
	if err := r.validateContinuationContext(ctx, prompt); err != nil {
		return err
	}
	r.savedPrompt = raw
	return nil
}

func (r *factoryConversationRun) validateContinuationContext(ctx context.Context, prompt *factoryConversationPrompt) error {
	if r.c.SessionHandle == "" && r.calls > 0 {
		return agent.ErrConversationSessionMissing
	}
	if prompt.OwnerInput != r.t.Source || len(prompt.OwnerSources) == 0 {
		return errors.New("saved continuation owner source unavailable")
	}
	sources, err := r.s.db.ListConversationSources(ctx, r.c.ID, r.at, 8)
	if err != nil {
		return err
	}
	for _, saved := range prompt.OwnerSources {
		found := false
		for _, current := range sources {
			if saved.SourceID == current.SourceID && saved.Text == current.Source {
				found = true
				break
			}
		}
		if !found {
			return errors.New("saved continuation source expired or changed")
		}
	}
	refs, err := r.references(ctx)
	if err != nil {
		return err
	}
	for _, saved := range prompt.References {
		found := false
		for _, current := range refs {
			if saved.ID == current.ID && saved.Revision == current.Revision && reflect.DeepEqual(saved.Arguments, current.Arguments) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("saved continuation target expired or changed")
		}
	}
	return nil
}

// recoverFactoryBoundary is called only after process-exit proof. It checks the
// journal before yielding the expired worker's fence; claiming the pending turn
// then gives the next worker a new generation. Action links never take this path.
func (s *Service) recoverFactoryBoundary(ctx context.Context, c store.Conversation, b ConversationBinding, t store.ConversationTurn, at time.Time) (bool, error) {
	if _, err := s.db.GetConversationActionLink(ctx, t.ID); err == nil {
		return false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	capture, err := s.restoreConversationInstructions(ctx, c.ID, at)
	if err != nil {
		return false, err
	}
	run := &factoryConversationRun{s: s, c: c, b: b, t: t, at: at, instructions: capture, safe: true}
	sources, err := s.db.ListConversationSources(ctx, c.ID, at, 8)
	if err != nil {
		return false, err
	}
	_, err = run.loadAuthor(ctx)
	if err != nil {
		return false, err
	}
	refs, err := run.references(ctx)
	if err != nil {
		return false, err
	}
	prompt := factoryConversationPrompt{References: refs}
	for _, source := range sources {
		prompt.OwnerSources = append(prompt.OwnerSources, DialogueTurn{SourceID: source.SourceID, Text: source.Source})
	}
	slices.Reverse(prompt.OwnerSources)
	_, _, _, err = run.resumeBoundary(ctx, &prompt)
	if err != nil {
		if errors.Is(err, errConversationInstructions) || errors.Is(err, errConversationBudget) {
			return false, err
		}
		return false, nil
	}
	exchanges, err := s.db.ListConversationExchanges(ctx, t.ID)
	if err != nil {
		return false, err
	}
	if len(exchanges) == 0 {
		return false, nil
	}
	last := exchanges[len(exchanges)-1]
	switch last.Kind {
	case "correction", "lookup", "consult", "evidence":
		if c.SessionHandle == "" {
			return false, nil
		}
	case "provider":
		if !last.Complete || last.Gaps != "provider_not_started" {
			return false, nil
		}
	default:
		return false, nil
	}
	if run.calls >= run.maxCalls() || run.ordinal > 14 {
		return false, fmt.Errorf("%w: no invocation or exchange slots remain for recovery", errConversationBudget)
	}
	return true, s.db.DeferConversationTurn(ctx, t)
}

// The journal is an alternating sequence of launches and their factory-owned
// boundaries. Evidence belongs to a saved read; terminal actions are never
// continuation boundaries.
func validConversationTransition(rows []store.ConversationExchange, i int) bool {
	x := rows[i]
	previous := ""
	if i > 0 {
		previous = rows[i-1].Kind
	}
	switch x.Kind {
	case "saved_outcome":
		return i == 0 && x.Complete
	case "provider":
		return previous == "" || previous == "saved_outcome" || previous == "correction" || previous == "evidence"
	case "correction", "lookup", "consult", "answer", "clarify", "propose", "confirm", "cancel":
		return previous == "provider" && x.Complete
	case "evidence":
		return (previous == "lookup" || previous == "consult") && x.Request == rows[i-1].Request
	}
	return false
}
