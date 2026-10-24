package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

// answerDetails reads saved factory-visible results, never native transcripts or
// provider requests. Selection has already been fixed at command admission.
func (s *Service) answerDetails(ctx context.Context, r store.ChatReply, id string, at time.Time) error {
	a, err := s.db.ReadConversationAnswer(ctx, s.transport, s.owner, r.ChannelRef, id)
	if errors.Is(err, store.ErrNotFound) {
		return s.saveDetailsNotice(ctx, r, "Answer details are unavailable or access is denied.", at)
	}
	if err != nil {
		return err
	}
	if !a.ExpiredAt.IsZero() || !a.CompletedAt.IsZero() && !at.Before(a.CompletedAt.Add(24*time.Hour)) {
		return s.saveDetailsNotice(ctx, r, "Details for this answer have expired.", at)
	}
	results, err := s.authorizedAnswerResults(ctx, a, at)
	if err != nil {
		if errors.Is(err, errAnswerDetailsDenied) {
			return s.saveDetailsNotice(ctx, r, "Answer details are unavailable or access is denied.", at)
		}
		return err
	}
	if len(results) == 0 {
		return s.saveDetailsNotice(ctx, r, "No tool results were obtained for this answer.", at)
	}
	export := struct {
		AnswerID string                   `json:"answer_id"`
		Results  []conversationReadResult `json:"results"`
	}{a.ID, results}
	raw, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return err
	}
	protected, err := s.Protect(ctx, string(raw))
	if err != nil {
		return err
	}
	chunks := detailsChunks(protected, 60000)
	parts := make([]store.ConversationAnswerPart, len(chunks))
	identities := make([]string, len(results))
	for i, result := range results {
		identities[i] = result.FactID
	}
	// Locate each result's exact JSON range so every export manifest names the
	// particular result continuations carried by its byte segment.
	type resultSpan struct {
		id         string
		start, end int
	}
	spans := make([]resultSpan, 0, len(results))
	for _, result := range results {
		encoded, e := json.MarshalIndent(result, "    ", "  ")
		if e != nil {
			return e
		}
		start := strings.Index(protected, string(encoded))
		if start < 0 {
			return errors.New("saved result changed during export protection")
		}
		spans = append(spans, resultSpan{result.FactID, start, start + len(encoded)})
	}
	offset := 0
	for i, chunk := range chunks {
		// Byte ranges identify result continuations in the one saved JSON document.
		manifest := fmt.Sprintf("Answer %s; export part %d of %d; JSON bytes %d..%d of %d; result identities: %s\nConcatenate JSON byte segments in order to reconstruct the saved export.\n\n", a.ID, i+1, len(chunks), offset, offset+len(chunk), len(protected), strings.Join(identities, ", "))
		for _, span := range spans {
			begin, end := max(offset, span.start), min(offset+len(chunk), span.end)
			if begin < end {
				continued := ""
				if begin > span.start || end < span.end {
					continued = " (continued)"
				}
				manifest = strings.TrimSuffix(manifest, "\n\n") + fmt.Sprintf("\nResult %s: bytes %d..%d of %d%s.\n\n", span.id, begin-span.start, end-span.start, span.end-span.start, continued)
			}
		}
		parts[i] = store.ConversationAnswerPart{Text: fmt.Sprintf("Answer %s · details export part %d of %d", a.ID, i+1, len(chunks)), Attachment: manifest + chunk}
		offset += len(chunk)
	}
	return s.db.SaveConversationDetails(ctx, r, parts, at)
}

var errAnswerDetailsDenied = errors.New("answer details access denied")

func (s *Service) authorizedAnswerResults(ctx context.Context, a store.ConversationAnswer, at time.Time) ([]conversationReadResult, error) {
	runtime := s.conversations.Load()
	if runtime == nil || !s.channels[a.ChannelRef] || a.OwnerID != s.owner {
		return nil, errAnswerDetailsDenied
	}
	if err := conversationAccess(ctx, runtime, a.ChannelRef, at); err != nil {
		if accessRetry(err) > 0 {
			return nil, err
		}
		return nil, errAnswerDetailsDenied
	}
	if err := s.authorizeInboxAnswer(ctx, a, at); err != nil {
		return nil, errAnswerDetailsDenied
	}
	exchanges, err := s.db.ListConversationExchanges(ctx, a.TurnID)
	if err != nil {
		return nil, err
	}
	var results []conversationReadResult
	for _, x := range exchanges {
		if x.Kind != "evidence" {
			continue
		}
		result, err := decodeConversationEvidence(x)
		if err != nil {
			return nil, err
		}
		if project := result.Arguments["project"]; project != "" && result.Command != "inbox" {
			resolution, e := ask.ResolveProject(ctx, s.db, project, nil)
			if e != nil {
				return nil, e
			}
			if resolution.Refusal != "" || resolution.Repository.Slug() != project || result.ResolvedRepoID > 0 && resolution.Repository.ID != result.ResolvedRepoID {
				return nil, errAnswerDetailsDenied
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) saveDetailsNotice(ctx context.Context, r store.ChatReply, text string, at time.Time) error {
	return s.db.SaveConversationDetails(ctx, r, []store.ConversationAnswerPart{{Text: text}}, at)
}

func detailsChunks(raw string, limit int) []string {
	var chunks []string
	for len(raw) > limit {
		end := limit
		for !utf8.ValidString(raw[:end]) {
			end--
		}
		chunks = append(chunks, raw[:end])
		raw = raw[end:]
	}
	if raw != "" {
		chunks = append(chunks, raw)
	}
	return chunks
}

func (s *Service) inlineConversationDetails(ctx context.Context, r store.ChatReply, at time.Time) error {
	selected, err := s.db.ConversationDetailsReplySelection(ctx, r)
	if err != nil {
		return err
	}
	remaining, err := s.db.ReadConversationDetailsParts(ctx, r)
	if err != nil {
		return err
	}
	var blocks []presentation.AnswerBlock
	for _, part := range remaining {
		raw := part.Attachment
		if raw == "" {
			raw = part.Text
		}
		blocks = append(blocks, presentation.AnswerBlock{Text: raw})
	}
	rendered, err := presentation.RenderConversation(selected, blocks, nil, "")
	if err != nil {
		return err
	}
	parts := make([]store.ConversationAnswerPart, len(rendered))
	for i, p := range rendered {
		parts[i].Text = p.Content
	}
	return s.db.ReplaceConversationDetailsParts(ctx, r, parts, at)
}
