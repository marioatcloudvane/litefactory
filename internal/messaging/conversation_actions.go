package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

func (r *factoryConversationRun) propose(ctx context.Context, inv Invocation, envelope ConversationEnvelope, sources []DialogueTurn) error {
	s := r.s
	if err := conversationAccess(ctx, s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return err
	}
	validated, effect, err := s.ValidateInvocation(inv)
	if err != nil || effect != ChangeEffect {
		return ErrConversationEnvelope
	}
	p := naturalPayload{Invocation: validated, Channel: r.c.ChannelRef}
	clarification, err := s.resolveNatural(ctx, &p, r.at)
	if err != nil {
		return err
	}
	if clarification != "" {
		return s.conversationOutcome(ctx, r.t, "completed", clarification, r.at)
	}
	if p.Invocation.Command == "draft" && strings.TrimSpace(p.Invocation.Arguments["note"]) == "" {
		return s.conversationOutcome(ctx, r.t, "completed", "Please provide the exact feature idea for the proposed draft.", r.at)
	}
	d, err := s.db.ReadNaturalDialogue(ctx, s.transport, s.owner, r.c.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if d.State == "authorized" || d.State == "executing" {
		return s.conversationOutcome(ctx, r.t, "completed", "The confirmed action is already in progress. Please check its saved outcome before requesting another change.", r.at)
	}
	old := d.Revision
	d.Transport = s.transport
	d.OwnerID = s.owner
	d.ChannelRef = r.c.ChannelRef
	d.SourceID = r.t.SourceID
	d.Revision++
	d.State = "awaiting_confirmation"
	d.UpdatedAt = r.at
	d.ExpiresAt = r.at.Add(15 * time.Minute)
	d.PromptID = ""
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	d.Code = hex.EncodeToString(nonce[:])
	p.Prompt = s.naturalProposal(p, d.Code)
	if replySize(p.Prompt) > s.ReplyLimit()-32 {
		return s.conversationOutcome(ctx, r.t, "completed", "The exact proposal is too large to show safely. Please shorten the request; nothing was authorized.", r.at)
	}
	protected, err := s.Protect(ctx, p.Prompt)
	if err != nil {
		return err
	}
	if protected != p.Prompt {
		return s.conversationOutcome(ctx, r.t, "completed", "Protection changed the exact proposal. Nothing was authorized; please shorten the request.", r.at)
	}
	d.Payload, d.Digest, err = naturalDigest(p)
	if err != nil {
		return err
	}
	// Pin the exact owner source needed by this live proposal, including spans
	// from a prior turn nearing its ordinary retention deadline.
	seen := map[string]bool{}
	for _, arg := range envelope.Arguments {
		if arg.SourceID == "" || seen[arg.SourceID] {
			continue
		}
		seen[arg.SourceID] = true
		for _, source := range sources {
			if source.SourceID != arg.SourceID {
				continue
			}
			reference := store.ConversationReference{ID: r.t.ID + "-source-" + arg.SourceID, TurnID: r.t.ID, Kind: "source", Target: arg.SourceID, Content: source.Text, ExpiresAt: d.ExpiresAt}
			if err = s.db.SaveConversationReference(ctx, r.t, reference); err != nil {
				return err
			}
		}
	}
	if err = s.db.SaveNaturalDialogue(ctx, d, old); err != nil {
		return err
	}
	if err = s.db.SaveConversationActionLink(ctx, r.t, store.ConversationActionLink{TurnID: r.t.ID, DialogueID: d.Code, DialogueRevision: d.Revision, PreviewID: p.PreviewID, PayloadDigest: d.Digest}); err != nil {
		return err
	}
	return s.conversationOutcome(ctx, r.t, "completed", p.Prompt, r.at)
}

func (r *factoryConversationRun) confirm(ctx context.Context) (bool, error) {
	s := r.s
	d, err := s.db.ReadNaturalDialogue(ctx, s.transport, s.owner, r.c.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return true, err
	}
	complete := func(text string) (bool, error) {
		if r.savedOutcomes != "" {
			text += "\n\nSaved action outcome:\n" + r.savedOutcomes
		}
		return true, s.conversationHandled(ctx, r.t, text, r.at)
	}
	if naturalCancel(r.t.Source) {
		if d.State == "clarifying" || d.State == "awaiting_confirmation" {
			if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at); err != nil {
				return true, err
			}
		}
		return complete("Cancelled the pending request.")
	}
	if d.State == "awaiting_confirmation" && naturalConfirmation(r.t.Source, d, r.t.ReplyTo) {
		if d.PromptID == "" || !r.at.Before(d.ExpiresAt) || !naturalLater(r.t.SourceID, d.SourceID) {
			return complete("This proposal is not available for confirmation. Please request a fresh proposal.")
		}
		if err = conversationAccess(ctx, s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
			return true, err
		}
		var p naturalPayload
		if err = json.Unmarshal([]byte(d.Payload), &p); err != nil {
			return true, err
		}
		_, digest, e := naturalDigest(p)
		if e != nil {
			return true, e
		}
		if digest != d.Digest {
			return complete("The proposal changed; please request it again.")
		}
		refusal, e := s.recheckNatural(ctx, p, r.at)
		if e != nil {
			return true, e
		}
		if refusal != "" {
			if e = s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at); e != nil {
				return true, e
			}
			return complete(refusal)
		}
		invocation, effect, e := s.ValidateInvocation(p.Invocation)
		if e != nil || effect != ChangeEffect {
			return complete("This action is no longer enabled; request a fresh proposal.")
		}
		reply := store.ChatReply{Transport: s.transport, ChannelRef: r.c.ChannelRef, MessageID: "nl-exec:" + d.Code, Command: invocation.Command, Arguments: invocation.Arguments, CreatedAt: r.at, ResolvedRepoID: p.RepoID, ConversationTurnID: r.t.ID}
		if err = s.db.AuthorizeConversationDialogue(ctx, r.t, d, r.t.SourceID, reply, r.at); err != nil {
			return true, err
		}
		if p.PreviewID != "" {
			link, e := s.db.GetConversationActionLink(ctx, r.t.ID)
			if e != nil {
				return true, e
			}
			link.PreviewID = p.PreviewID
			if e = s.db.SaveConversationActionLink(ctx, r.t, link); e != nil {
				return true, e
			}
		}

		return true, r.execute(ctx, reply, p)
	}
	lower := strings.ToLower(strings.TrimSpace(r.t.Source))
	if naturalConfirmationCode(r.t.Source) != "" || lower == "confirm" || strings.HasPrefix(lower, "confirm ") || (lower == "yes" && d.State == "awaiting_confirmation") || r.t.ReplyTo != "" && r.t.ReplyTo == d.PromptID && (lower == "yes" || lower == "do it") {
		return complete("This confirmation is unknown, expired or already consumed. Request a fresh proposal; nothing further was authorized.")
	}
	if d.State == "authorized" || d.State == "executing" {
		return complete("The confirmed action is already in progress. Its saved outcome will be reported when available.")
	}
	return false, nil
}

func (r *factoryConversationRun) execute(ctx context.Context, reply store.ChatReply, p naturalPayload) error {
	s := r.s
	if err := conversationAccess(ctx, s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return err
	}
	var err error
	reply, err = s.db.StartConversationActionReply(ctx, r.t, reply, r.at)
	if err != nil {
		return err
	}
	// Reuse the existing immutable execution and target/content revision checks.
	if _, err := s.prepareNaturalExecution(ctx, &reply, r.at); err != nil {
		return err
	}
	text := reply.ReplyText
	executionCtx, source := presentation.WithAssessment(ctx)
	if text == "" {
		switch reply.Command {
		case "draft":
			var err error
			text, err = s.conversationDraft(executionCtx, r.c, r.b, r.t, p.Invocation, r.at, r.invoke)
			if err == nil {
				preview, e := s.db.ChatIssuePreviewForMessage(ctx, s.transport, "conversation:"+r.t.ID)
				if e != nil && !errors.Is(e, store.ErrNotFound) {
					return e
				}
				if e == nil {
					link, e := s.db.GetConversationActionLink(ctx, r.t.ID)
					if e != nil {
						return e
					}
					link.PreviewID = preview.ID
					if e = s.db.SaveConversationActionLink(ctx, r.t, link); e != nil {
						return e
					}
					if e = r.savePreviewReference(ctx, preview); e != nil {
						text = "The preview changed before it could be bound. Request a fresh draft; no issue was created."
						reply.OutcomeStatus = string(presentation.Refusal)
					}

				}
			}

			if err != nil {
				text = "Feature generation was interrupted or unavailable. No issue was created; please check the saved preview before requesting another draft."
				reply.OutcomeStatus = string(presentation.Uncertain)
			}
		case "confirm":
			if s.confirmer == nil {
				text = "Feature confirmation unavailable."
			} else {
				var err error
				text, err = s.confirmer.Confirm(executionCtx, s.transport, r.c.ChannelRef, reply.MessageID, p.Invocation.Arguments["code"], r.at)
				if err != nil {
					text = "Feature creation outcome is uncertain. Check the saved preview and issue before requesting another confirmation."
					reply.OutcomeStatus = string(presentation.Uncertain)
				}
			}
		default:
			s.mu.RLock()
			handler := s.commands[reply.Command]
			s.mu.RUnlock()
			if handler == nil {
				text = "This action is no longer enabled. Nothing was applied."
			} else {
				execution, assessment := presentation.WithAssessment(ctx)
				var err error
				text, err = handler(execution, Message{Transport: s.transport, ID: reply.MessageID, Channel: r.c.ChannelRef, Sender: s.owner, Command: reply.Command, Arguments: p.Invocation.Arguments}, r.at)
				reply.OutcomeStatus = string(assessment.Status)
				if errors.Is(err, errDeferredDiagnosis) {
					// Only the existing diagnosis worker performs this queued operation. Its
					// saved completion is supplied at a later ordinary session step.
					return s.conversationOutcome(ctx, r.t, "completed", "The confirmed diagnosis was queued. Its saved result will be supplied at the next eligible conversation step; any repair remains subject to issue review.", r.at)
				}
				if err != nil {
					text = "The confirmed action was interrupted after execution began. Its effect is uncertain; check the target before requesting it again."
					reply.OutcomeStatus = string(presentation.Uncertain)
				}
			}
		}
	}
	if reply.OutcomeStatus == "" {
		reply.OutcomeStatus = string(source.Status)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	protected, err := s.Protect(cleanup, text)
	if err != nil || reply.Command == "draft" && protected != text {
		protected = "The action finished, but its outcome could not be protected for display. Check the target before requesting it again."
		reply.OutcomeStatus = string(presentation.Uncertain)
	}
	reply.ReplyText = protected
	if err = s.finishNaturalOutcome(cleanup, reply, protected, r.at); err != nil {
		return err
	}
	if err = s.db.FinishConversationActionReply(cleanup, r.t, reply, r.at); err != nil {
		return err
	}
	// The saved factual outcome survives every narration failure. Preview bytes
	// already have a separate exact confirmation gate and are delivered verbatim.
	if reply.Command == "draft" {
		return s.conversationOutcome(cleanup, r.t, "completed", protected, r.at)
	}
	return r.narrate(ctx, protected)
}

func (r *factoryConversationRun) narrate(ctx context.Context, outcome string) error {
	prompt := factoryConversationPrompt{Instructions: factoryConversationInstructions + " Explain only the saved action outcome below. Return an answer envelope. Do not propose, request lookup or infer unsaved effects.", SavedOutcome: outcome, OwnerSources: []DialogueTurn{{SourceID: r.t.SourceID, Text: r.t.Source}}, Commands: r.s.ConversationCommands()}
	raw, err := json.Marshal(prompt)
	if err != nil {
		return err
	}
	response, runErr := r.invoke(ctx, string(raw))
	text := "Saved action outcome:\n" + outcome
	if runErr == nil {
		envelope, _, decodeErr := DecodeConversationEnvelope(response, ConversationDecodeRequest{Turns: prompt.OwnerSources, Commands: prompt.Commands})
		if decodeErr == nil && envelope.Kind == "answer" {
			// Always retain the actual saved status beside its explanation.
			explained, err := r.s.Protect(ctx, envelope.Text)
			if err == nil && replySize(explained+"\n\n"+text) <= r.s.ReplyLimit() {
				text = explained + "\n\n" + text
			}
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.s.conversationOutcome(cleanup, r.t, "completed", text, r.at)
}

// consumeOutcomes hands an asynchronous diagnosis's saved result to the next
// ordinary native-session turn, without concurrent provider use or re-execution.
func (r *factoryConversationRun) consumeOutcomes(ctx context.Context) (string, error) {
	replies, err := r.s.db.ListPendingConversationActionOutcomes(ctx, r.c.ID)
	if err != nil {
		return "", err
	}
	var outcomes []string
	if len(replies) > 1 {
		replies = replies[:1]
	}
	for _, reply := range replies {
		protected, err := r.s.Protect(ctx, reply.ReplyText)
		if err != nil {
			return "", err
		}
		r.ordinal++
		if err = r.s.db.SaveConversationExchange(ctx, r.t, store.ConversationExchange{TurnID: r.t.ID, Ordinal: r.ordinal, Kind: "saved_outcome", Request: reply.MessageID, Result: protected, CapturedAt: r.at, Complete: true}); err != nil {
			return "", err
		}
		reply.ReplyText = protected
		if err = r.s.finishNaturalOutcome(ctx, reply, protected, r.at); err != nil {
			return "", err
		}
		if err = r.s.db.FinishConversationActionReply(ctx, r.t, reply, r.at); err != nil {
			return "", err
		}
		outcomes = append(outcomes, protected)
	}
	text := strings.Join(outcomes, "\n\n")
	bounded, cut := boundedConversationText(text, 16384)
	if cut {
		bounded += "\nAdditional saved outcome content was omitted; the original result remains retained."
	}
	return bounded, nil
}
