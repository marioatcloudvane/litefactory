package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/store"
)

type standardsReviewDelivery struct {
	ReviewID string `json:"standards_review"`
	Code     string `json:"code"`
	Offset   int    `json:"offset"`
	Count    int    `json:"count"`
	Prompt   bool   `json:"prompt"`
}

func standardsNonce() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}

func standardsReviewParts(text string) []string {
	var parts []string
	start, units := 0, 0
	for i, r := range text {
		n := utf16.RuneLen(r)
		if units+n > 1900 {
			parts = append(parts, text[start:i])
			start = i
			units = 0
		}
		units += n
	}
	if start < len(text) {
		parts = append(parts, text[start:])
	}
	return parts
}

func (r *factoryConversationRun) reviewStandards(ctx context.Context, inv Invocation) error {
	if r.s.standards == nil {
		return r.s.conversationOutcome(ctx, r.t, "completed", "Standards review unavailable.", r.at)
	}
	if err := conversationAccess(ctx, r.s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return err
	}
	request, err := r.s.standardsRequest(inv, r.c.ChannelRef, r.t.SourceID, r.at)
	if err != nil {
		return err
	}
	var manifest store.StandardsConversationReview
	if request.Operation == "review.next" {
		manifest, err = r.s.db.GetStandardsConversationReview(ctx, inv.Arguments["id"])
		if err == nil && (manifest.OwnerID != r.s.owner || manifest.ChannelID != r.c.ChannelRef || manifest.Transport != r.s.transport) {
			err = errors.New("review belongs to another conversation")
		}
	} else {
		var review StandardsReview
		review, err = r.s.standards.Review(ctx, request)
		if err == nil {
			var protected string
			protected, err = r.s.Protect(ctx, review.Text)
			if err == nil && protected != review.Text {
				return r.s.conversationOutcome(ctx, r.t, "completed", "Protection changed the exact review. Approval is blocked; revise the proposal safely and request a fresh review.", r.at)
			}
			if err == nil {
				var id, code string
				id, err = standardsNonce()
				if err != nil {
					return err
				}
				code, err = standardsNonce()
				if err != nil {
					return err
				}
				manifest, err = r.s.db.SaveStandardsConversationReview(ctx, store.StandardsConversationReview{ID: id, Transport: r.s.transport, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, Kind: review.Kind, TargetID: review.TargetID, TargetVersion: review.TargetVersion, BaseRevision: review.BaseRevision, ChangeDigest: review.ChangeDigest, ResultDigest: review.ResultDigest, Text: review.Text, Parts: standardsReviewParts(review.Text), Code: code})
			}
		}
	}
	if err != nil {
		return r.s.conversationOutcome(ctx, r.t, "completed", standardsError(err), r.at)
	}
	if !manifest.ConsumedAt.IsZero() {
		return r.s.conversationOutcome(ctx, r.t, "completed", "This review already has a saved approval. Inspect that outcome; publication remains separate.", r.at)
	}
	if manifest.PromptRemoteID != "" && !r.at.Before(manifest.ExpiresAt) {
		code, e := standardsNonce()
		if e != nil {
			return e
		}
		manifest, err = r.s.db.RefreshStandardsConversationReviewChallenge(ctx, manifest.ID, r.s.owner, r.c.ChannelRef, code, r.at)
		if err != nil {
			return err
		}
	}
	return r.queueStandardsReview(ctx, manifest)
}

func (r *factoryConversationRun) queueStandardsReview(ctx context.Context, m store.StandardsConversationReview) error {
	start := 0
	for start < len(m.RemoteIDs) && m.RemoteIDs[start] != "" {
		start++
	}
	var parts []store.ConversationAnswerPart
	size := 0
	for i := start; i < len(m.Parts); i++ {
		if size+len(m.Parts[i]) > 64<<10 {
			break
		}
		size += len(m.Parts[i])
		parts = append(parts, store.ConversationAnswerPart{Text: m.Parts[i]})
	}
	meta := standardsReviewDelivery{ReviewID: m.ID, Code: m.Code, Offset: start, Count: len(parts)}
	if start+len(parts) == len(m.Parts) {
		meta.Prompt = true
		parts = append(parts, store.ConversationAnswerPart{Text: fmt.Sprintf("Complete exact %s review %s: %s version %d, base %d, change digest %s, result digest %s. To approve only these terms, send approve standards %s. Approval expires 15 minutes after this prompt is delivered. Publication requires a separate confirmed decision.", m.Kind, m.ID, m.TargetID, m.TargetVersion, m.BaseRevision, m.ChangeDigest, m.ResultDigest, m.Code)})
	} else {
		parts = append(parts, store.ConversationAnswerPart{Text: "Review batch saved. Request the next review batch for exact manifest " + m.ID + ". Approval is unavailable until every part has been delivered."})
	}
	for _, p := range parts {
		protected, err := r.s.Protect(ctx, p.Text)
		if err != nil {
			return err
		}
		if protected != p.Text {
			return errors.New("exact review delivery changed by protection")
		}
	}
	raw, _ := json.Marshal(meta)
	id, err := standardsNonce()
	if err != nil {
		return err
	}
	return r.s.db.CompleteConversationAnswer(ctx, r.t, "completed", store.ConversationAnswer{ID: id, Blocks: string(raw), Version: 1}, parts, r.at)
}

func (s *Service) acknowledgeStandardsReview(ctx context.Context, reply store.ChatReply, remote string, at time.Time) error {
	a, ordinal, err := s.db.ConversationReplyAnswer(ctx, reply)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var meta standardsReviewDelivery
	if json.Unmarshal([]byte(a.Blocks), &meta) != nil || meta.ReviewID == "" {
		return nil //nolint:nilerr // Ordinary answer block arrays are not review metadata.
	}
	if ordinal <= meta.Count {
		_, err = s.db.AcknowledgeStandardsConversationReviewPart(ctx, meta.ReviewID, s.owner, reply.ChannelRef, meta.Offset+ordinal-1, remote)
		return err
	}
	if meta.Prompt {
		current, e := s.db.GetStandardsConversationReview(ctx, meta.ReviewID)
		if e != nil {
			return e
		}
		if current.Code != meta.Code {
			return nil
		}
		_, err = s.db.ActivateStandardsConversationReview(ctx, meta.ReviewID, s.owner, reply.ChannelRef, remote, at)
		return err
	}
	return nil
}

func (r *factoryConversationRun) approveStandards(ctx context.Context) (bool, error) {
	text := strings.TrimSpace(r.t.Source)
	words := strings.Fields(strings.ToLower(text))
	if len(words) < 2 || words[0] != "approve" || words[1] != "standards" {
		return false, nil
	}
	finish := func(text string) (bool, error) { return true, r.s.conversationHandled(ctx, r.t, text, r.at) }
	if (len(words) != 3 && (len(words) != 2 || r.t.ReplyTo == "")) || quotedSpan(text, 0, len(text)) {
		return finish("Explicit approval needs the exact delivered review code: approve standards CODE.")
	}
	if err := conversationAccess(ctx, r.s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return true, err
	}
	var m store.StandardsConversationReview
	var err error
	if len(words) == 2 {
		m, err = r.s.db.FindStandardsConversationReviewByPrompt(ctx, r.s.transport, r.s.owner, r.c.ChannelRef, r.t.ReplyTo)
	} else {
		m, err = r.s.db.FindStandardsConversationReviewByCode(ctx, r.s.transport, r.s.owner, r.c.ChannelRef, words[2])
	}
	if err != nil {
		return finish("That standards review is unknown, expired or unavailable. Request a complete fresh review.")
	}
	args := map[string]string{"id": m.TargetID, "version": strconv.FormatInt(m.TargetVersion, 10), "base": strconv.FormatInt(m.BaseRevision, 10), "digest": m.ChangeDigest}
	request := StandardsRequest{Operation: m.Kind + ".review", Arguments: args, Transport: r.s.transport, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, MessageID: r.t.SourceID, OperationKey: r.s.transport + ":" + r.c.ChannelRef + ":review:" + m.ID, At: r.at}
	if recovery, ok := r.s.standards.(StandardsRecovery); ok {
		prior := request
		prior.Operation = m.Kind + ".approve"
		result, found, e := recovery.Recover(ctx, prior)
		if e != nil {
			return finish(standardsError(e))
		}
		if found {
			return finish("Saved standards approval outcome:\n" + standardsDisplay(result, prior.OperationKey) + "\nPublication remains a separate confirmed action.")
		}
	}
	if m.PromptRemoteID == "" || !r.at.Before(m.ExpiresAt) {
		return finish("The complete review prompt is unavailable or expired. Request a fresh review.")
	}
	current, err := r.s.standards.Review(ctx, request)
	if err != nil {
		return finish(standardsError(err))
	}
	if current.TargetID != m.TargetID || current.TargetVersion != m.TargetVersion || current.BaseRevision != m.BaseRevision || current.ChangeDigest != m.ChangeDigest || current.ResultDigest != m.ResultDigest || current.Text != m.Text {
		return finish("The reviewed terms changed. Request a complete fresh review before approval.")
	}
	request.Operation = m.Kind + ".approve"
	approval := store.WithStandardsConversationReviewConsumption(ctx, m.ID, r.s.owner, r.c.ChannelRef, m.Code, r.at)
	result, err := r.s.standards.Execute(approval, request)
	if err != nil {
		return finish(standardsError(err))
	}
	if _, err = r.saveStandardsReference(ctx, result.References); err != nil {
		return true, err
	}
	return finish("Saved standards approval outcome:\n" + standardsDisplay(result, request.OperationKey) + "\nPublication remains a separate confirmed action.")
}

// Specialized approvals have no generic yes-dialogue. Recover their own receipt
// before the ordinary interrupted-turn path, without interpreting owner text.
func (s *Service) recoverStandardsApproval(ctx context.Context, c store.Conversation, t store.ConversationTurn, at time.Time) error {
	recovery, ok := s.standards.(StandardsRecovery)
	if !ok {
		return nil
	}
	words := strings.Fields(strings.ToLower(strings.TrimSpace(t.Source)))
	if len(words) < 2 || words[0] != "approve" || words[1] != "standards" {
		return nil
	}
	var m store.StandardsConversationReview
	var err error
	switch {
	case len(words) == 3:
		m, err = s.db.FindStandardsConversationReviewByCode(ctx, s.transport, s.owner, c.ChannelRef, words[2])
	case len(words) == 2 && t.ReplyTo != "":
		m, err = s.db.FindStandardsConversationReviewByPrompt(ctx, s.transport, s.owner, c.ChannelRef, t.ReplyTo)
	default:
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	request := StandardsRequest{Operation: m.Kind + ".approve", Arguments: map[string]string{"id": m.TargetID, "version": strconv.FormatInt(m.TargetVersion, 10), "base": strconv.FormatInt(m.BaseRevision, 10), "digest": m.ChangeDigest}, Transport: s.transport, OwnerID: s.owner, ChannelID: c.ChannelRef, MessageID: t.SourceID, OperationKey: s.transport + ":" + c.ChannelRef + ":review:" + m.ID, At: at}
	result, found, err := recovery.Recover(ctx, request)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	text, err := s.Protect(ctx, standardsDisplay(result, request.OperationKey))
	if err != nil {
		return err
	}
	return s.db.SaveConversationExchange(ctx, t, store.ConversationExchange{TurnID: t.ID, Ordinal: 1, Kind: "saved_outcome", Result: text, CapturedAt: at, Complete: true})
}
