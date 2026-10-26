package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

func conversationEvidenceTarget(args map[string]string) string {
	target := args["project"]
	issue := args["issue"]
	if issue == "" {
		issue = args["task"]
	}
	if issue != "" {
		target += "#" + issue
	}
	if target == "" {
		target = "factory"
	}
	return target
}

// decodeConversationEvidence adds factory presentation metadata to retained
// prerequisite records. Their result bytes and capture identity remain saved data.
func decodeConversationEvidence(x store.ConversationExchange) (conversationReadResult, error) {
	var result conversationReadResult
	if err := json.Unmarshal([]byte(x.Result), &result); err != nil {
		return result, fmt.Errorf("invalid saved evidence: %w", err)
	}
	if result.FactID == "" {
		result.FactID = fmt.Sprintf("%s-fact-%d", x.TurnID, x.Ordinal)
	}
	if result.Target == "" {
		result.Target = conversationEvidenceTarget(result.Arguments)
	}
	if result.CapturedAt.IsZero() {
		result.CapturedAt = x.CapturedAt
	}
	if result.Status == "" {
		result.Status = "incomplete"
		if x.Complete && result.Complete {
			result.Status = "complete"
		}
	}
	return result, nil
}

// currentConversationFacts uses only this turn's saved admitted lookup records.
// Provider inputs, old turns and action targets never become evidence.
func (s *Service) currentConversationFacts(ctx context.Context, turnID string) ([]presentation.CurrentFact, error) {
	exchanges, err := s.db.ListConversationExchanges(ctx, turnID)
	if err != nil {
		return nil, err
	}
	var facts []presentation.CurrentFact
	for _, x := range exchanges {
		if x.Kind != "evidence" {
			continue
		}
		result, err := decodeConversationEvidence(x)
		if err != nil {
			return nil, err
		}
		text := result.Result
		if result.Command == "ask" {
			var envelope struct {
				Bundle json.RawMessage `json:"bundle"`
			}
			if json.Unmarshal([]byte(text), &envelope) == nil && len(envelope.Bundle) > 0 {
				text = string(envelope.Bundle)
			}
		}
		facts = append(facts, presentation.CurrentFact{ID: result.FactID, Text: text, Source: result.Source, Target: result.Target, Caveats: strings.Join(result.Gaps, "; "), Complete: x.Complete && result.Complete && len(result.Gaps) == 0})
	}
	return facts, nil
}

func (s *Service) saveConversationPresentation(ctx context.Context, t store.ConversationTurn, state string, blocks []presentation.AnswerBlock, protected string, at time.Time, intent ...string) error {
	return s.saveConversationAnswer(ctx, t, state, blocks, protected, at, false, intent...)
}

func (s *Service) saveConversationAnswer(ctx context.Context, t store.ConversationTurn, state string, blocks []presentation.AnswerBlock, protected string, at time.Time, handled bool, intent ...string) error {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(identity[:])
	// Redact before deriving classifications or saving any delivery bytes.
	blocks = append([]presentation.AnswerBlock(nil), blocks...)
	for i := range blocks {
		text, err := s.Protect(ctx, blocks[i].Text)
		if err != nil {
			return err
		}
		blocks[i].Text = text
		blocks[i].FactRefs = append([]string(nil), blocks[i].FactRefs...)
		for j, ref := range blocks[i].FactRefs {
			value, err := s.Protect(ctx, ref)
			if err != nil {
				return err
			}
			blocks[i].FactRefs[j] = value
		}
	}
	if protected != "" {
		text, err := s.Protect(ctx, protected)
		if err != nil {
			return err
		}
		if text != protected {
			return fmt.Errorf("protected proposal changed during redaction")
		}
	}
	facts, err := s.currentConversationFacts(ctx, t.ID)
	if err != nil {
		return err
	}
	for i := range facts {
		caveats, err := s.Protect(ctx, facts[i].Caveats)
		if err != nil {
			return err
		}
		facts[i].Caveats = caveats
	}
	rendered, err := presentation.RenderConversation(id, blocks, facts, protected, intent...)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	answer := store.ConversationAnswer{ID: id, TurnID: t.ID, Blocks: string(encoded), Version: 1}
	parts := make([]store.ConversationAnswerPart, len(rendered))
	for i, part := range rendered {
		parts[i] = store.ConversationAnswerPart{Text: part.Content}
		if part.Protected {
			answer.ProposalPart = i + 1
		}
	}
	if handled {
		return s.db.CompleteConversationHandledAnswer(ctx, t, answer, parts, at)
	}
	return s.db.CompleteConversationAnswer(ctx, t, state, answer, parts, at)
}
