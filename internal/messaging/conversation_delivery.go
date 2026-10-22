package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationDeliveryStep retries only the immutable stored head reply. It
// never invokes a model and never applies the ordinary request expiry window.
func (s *Service) ConversationDeliveryStep(ctx context.Context, at time.Time) (bool, error) {
	runtime := s.conversations.Load()
	if runtime == nil {
		return false, nil
	}
	r, err := s.db.ClaimConversationReply(ctx, s.transport, at)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.Command == "conversation_details" {
		return true, s.deliverConversationDetails(ctx, r, at)
	}

	c, err := s.db.ReadConversation(ctx, s.transport, r.ChannelRef)
	if err != nil {
		return true, err
	}
	b := runtime.bindings[r.ChannelRef]
	if !s.channels[r.ChannelRef] || c.OwnerID != s.owner || b.Provider == "" {
		return true, s.db.PauseConversationReply(ctx, r, "conversation disabled or authorization removed; saved reply retained", at)
	}
	if err = conversationAccess(ctx, runtime, r.ChannelRef, at); err != nil {
		if delay := accessRetry(err); delay > 0 {
			return true, s.db.RetryChatReply(ctx, r, at.Add(delay), at)
		}
		return true, s.db.PauseConversationReply(ctx, r, "conversation delivery authorization unavailable; saved reply retained", at)
	}
	// Protected delivery prevents the transport from truncating or replacing the
	// saved bytes. Oversized answers remain retained with a visible health reason.
	deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d := s.sender.Deliver(deliveryCtx, Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Protected: true}, r.ReplyText)
	switch {
	case d.Confirmed:
		if err := s.acknowledgeStandardsReview(ctx, r, d.PromptID, at); err != nil {
			return true, err
		}
		if runtime.factory && d.PromptID != "" {
			proposal, e := s.db.ReadNaturalDialogue(ctx, s.transport, s.owner, r.ChannelRef)
			if e != nil && !errors.Is(e, store.ErrNotFound) {
				return true, e
			}
			if e == nil && proposal.State == "awaiting_confirmation" {
				var payload naturalPayload
				if e = json.Unmarshal([]byte(proposal.Payload), &payload); e != nil {
					return true, e
				}
				answer, ordinal, lookupErr := s.db.ConversationReplyAnswer(ctx, r)
				if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
					return true, lookupErr
				}
				isProposal := false
				if lookupErr == nil && answer.ProposalPart == ordinal && strings.HasSuffix(r.ReplyText, payload.Prompt) {
					link, linkErr := s.db.GetConversationActionLink(ctx, answer.TurnID)
					if linkErr != nil {
						return true, linkErr
					}
					isProposal = link.DialogueID == proposal.Code && link.DialogueRevision == proposal.Revision && link.PayloadDigest == proposal.Digest
				}
				if (isProposal || payload.Prompt == r.ReplyText) && proposal.PromptID == "" {
					if e = s.db.BindNaturalPrompt(ctx, proposal, d.PromptID); e != nil {
						return true, e
					}
				}
			}
		}
		return true, s.db.FinishConversationReply(ctx, r, d.PromptID, at)
	case d.Permanent:
		return true, s.db.PauseConversationReply(ctx, r, "Discord permanently refused the saved conversation reply; retained work is paused", at)
	default:
		delay := min(time.Second<<min(max(r.Attempts-1, 0), 10), 15*time.Minute)
		// Honor transport retry guidance in full; unlike ordinary requests, pending
		// conversations have no expiry deadline that can shorten Retry-After.
		delay = max(delay, d.RetryAfter)
		return true, s.db.RetryChatReply(ctx, r, at.Add(delay), at)
	}
}

func (s *Service) deliverConversationDetails(ctx context.Context, r store.ChatReply, at time.Time) error {
	if !s.channels[r.ChannelRef] || s.conversations.Load().bindings[r.ChannelRef].Provider == "" {
		return s.db.PauseConversationReply(ctx, r, "Answer details channel authorization removed; saved export retained", at)
	}
	id, err := s.db.ConversationDetailsReplySelection(ctx, r)
	if err != nil {
		return err
	}
	a, err := s.db.ReadConversationAnswer(ctx, s.transport, s.owner, r.ChannelRef, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	// A notice can be delivered without exporting data. Evidence exports always
	// recheck the currently registered target and channel before each send.
	if r.ReplyAttachment != "" || strings.HasPrefix(r.ReplyText, "Answer ") && strings.Contains(r.ReplyText, "Unverified") {
		if errors.Is(err, store.ErrNotFound) {
			return s.db.PauseConversationReply(ctx, r, "Answer details access lost; saved export retained", at)
		}
		if _, err = s.authorizedAnswerResults(ctx, a, at); err != nil {
			if delay := accessRetry(err); delay > 0 {
				return s.db.RetryChatReply(ctx, r, at.Add(delay), at)
			}
			return s.db.PauseConversationReply(ctx, r, "Answer details access lost; saved export retained", at)
		}
	} else if err = conversationAccess(ctx, s.conversations.Load(), r.ChannelRef, at); err != nil {
		if delay := accessRetry(err); delay > 0 {
			return s.db.RetryChatReply(ctx, r, at.Add(delay), at)
		}
		return s.db.PauseConversationReply(ctx, r, "Answer details channel access lost; saved export retained", at)
	}
	target := Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Protected: true}
	parts, err := s.db.ReadConversationDetailsParts(ctx, r)
	if err != nil {
		return err
	}
	// The original interaction handle belongs only to the first export part.
	if len(parts) > 0 && parts[0].Ordinal == 1 {
		target.Handle = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(r.MessageID, "details:"), ":1"), ":inline")
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var delivery Delivery
	if r.ReplyAttachment != "" {
		rich, ok := s.sender.(ReplyTransport)
		if !ok {
			return s.inlineConversationDetails(ctx, r, at)
		}
		delivery = rich.DeliverReply(work, target, presentation.Payload{Content: r.ReplyText, Attachment: &presentation.Attachment{Name: presentation.RawFilename, Content: r.ReplyAttachment}})
	} else {
		delivery = s.sender.Deliver(work, target, r.ReplyText)
	}
	if delivery.AttachmentRejected && r.ReplyAttachment != "" {
		return s.inlineConversationDetails(ctx, r, at)
	}
	if delivery.Confirmed {
		return s.db.FinishConversationReply(ctx, r, delivery.PromptID, at)
	}
	if delivery.Permanent {
		return s.db.PauseConversationReply(ctx, r, "Discord permanently refused answer details; saved export retained", at)
	}
	delay := max(min(time.Second<<min(max(r.Attempts-1, 0), 10), 15*time.Minute), delivery.RetryAfter)
	return s.db.RetryChatReply(ctx, r, at.Add(delay), at)
}
