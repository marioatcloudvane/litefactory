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
	if standardsDirect(p.Invocation) {
		return r.direct(ctx, p)
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
	p.Prompt = naturalProposal(p, d.Code, r.at)
	if _, renderErr := presentation.RenderConversation(strings.Repeat("0", 32), nil, nil, p.Prompt); renderErr != nil {
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
	return s.saveConversationPresentation(ctx, r.t, "completed", nil, p.Prompt, r.at)
}

// confirm handles the plain confirmations and cancellations that need no
// interpretation: the proposal's code, a yes in reply to the proposal, and a
// whole-message yes, no or details while the proposal is the last thing
// said. Anything else goes to the conversation agent, which judges whether
// the owner confirms (confirmJudged), cancels or says something else.
func (r *factoryConversationRun) confirm(ctx context.Context) (bool, error) {
	s := r.s
	d, err := s.db.ReadNaturalDialogue(ctx, s.transport, s.owner, r.c.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return true, err
	}
	live := d.State == "awaiting_confirmation" && r.at.Before(d.ExpiresAt)
	last := true
	if live {
		if last, err = r.proposalIsLast(ctx, d); err != nil {
			return true, err
		}
	}
	if naturalCancel(r.t.Source) && (!live || last) {
		if d.State == "clarifying" || d.State == "awaiting_confirmation" {
			if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at); err != nil {
				return true, err
			}
		}
		return true, r.complete(ctx, "Okay, I dropped it.")
	}
	if live && naturalDetailsRequest(r.t.Source) {
		var p naturalPayload
		if err = json.Unmarshal([]byte(d.Payload), &p); err != nil {
			return true, err
		}
		return true, r.complete(ctx, naturalDetails(p, d))
	}
	lower := strings.ToLower(strings.TrimSpace(r.t.Source))
	yes := naturalYes(r.t.Source)
	// A plain yes in reply to another message never confirms the proposal.
	stale := d.State == "awaiting_confirmation" && r.t.ReplyTo != "" && r.t.ReplyTo != d.PromptID && yes
	code := d.Code != "" && (lower == "confirm "+strings.ToLower(d.Code) || naturalConfirmationCode(r.t.Source) == strings.ToLower(d.Code))
	replied := yes && r.t.ReplyTo != "" && r.t.ReplyTo == d.PromptID
	plain := yes && r.t.ReplyTo == "" && last
	if d.State == "awaiting_confirmation" && !stale && (code || replied || plain) {
		return true, r.authorize(ctx, d)
	}
	if naturalConfirmationCode(r.t.Source) != "" || strings.HasPrefix(lower, "confirm ") || yes && (d.State == "awaiting_confirmation" && (!live || stale) || replied) {
		if stale && live {
			return true, r.complete(ctx, "You replied to an earlier message, so I didn't do anything. Say yes to go ahead with the latest proposal.")
		}
		return true, r.complete(ctx, naturalUnavailable)
	}
	if d.State == "authorized" || d.State == "executing" {
		return true, r.complete(ctx, "That's already running; I'll tell you here when it's done.")
	}
	return false, nil
}

// proposalIsLast reports whether no other owner message came between the
// proposal and this one, so a bare yes or no can only answer the proposal.
func (r *factoryConversationRun) proposalIsLast(ctx context.Context, d store.NaturalDialogue) (bool, error) {
	sources, err := r.s.db.ListConversationSources(ctx, r.c.ID, r.at, 8)
	if err != nil {
		return false, err
	}
	for _, source := range sources {
		if source.SourceID != r.t.SourceID && naturalLater(source.SourceID, d.SourceID) {
			return false, nil
		}
	}
	return true, nil
}

// complete answers the owner with a fixed factory text, after any saved
// outcome; a turn that asked the agent is not marked as handled without it.
func (r *factoryConversationRun) complete(ctx context.Context, text string) error {
	if r.savedOutcomes != "" {
		text += "\n\n" + r.savedOutcomes
	}
	if r.calls > 0 {
		return r.s.conversationOutcome(ctx, r.t, "completed", text, r.at)
	}
	return r.s.conversationHandled(ctx, r.t, text, r.at)
}

// openProposal is the one proposal the owner can still confirm, if any: the
// conversation agent sees it and may judge that the owner said yes to it.
func (r *factoryConversationRun) openProposal(ctx context.Context) (*conversationOpenProposal, error) {
	d, err := r.s.db.ReadNaturalDialogue(ctx, r.s.transport, r.s.owner, r.c.ChannelRef)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.State == "awaiting_confirmation" && d.PromptID != "" && r.at.Before(d.ExpiresAt) && naturalLater(r.t.SourceID, d.SourceID) {
		var p naturalPayload
		if err = json.Unmarshal([]byte(d.Payload), &p); err != nil {
			return nil, err
		}
		return &conversationOpenProposal{Reference: d.Code, Text: naturalProposalText(p, r.at), ExpiresAt: d.ExpiresAt}, nil
	}
	// An expired proposal or an unanswered clarification ends with this message.
	if d.State == "awaiting_confirmation" || d.State == "clarifying" {
		return nil, r.s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at)
	}
	return nil, nil
}

// confirmJudged runs the open proposal when the conversation agent judged
// that the owner's current message confirms it. The factory checks what the
// agent cannot vouch for: one live proposal, the agent named exactly that
// one, and the cited words stand verbatim in the owner's current message.
func (r *factoryConversationRun) confirmJudged(ctx context.Context, envelope ConversationEnvelope) error {
	d, err := r.s.db.ReadNaturalDialogue(ctx, r.s.transport, r.s.owner, r.c.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if envelope.Confirmation == nil || envelope.Confirmation.SourceID != r.t.SourceID || d.State != "awaiting_confirmation" || d.Code == "" || envelope.Proposal != d.Code {
		return r.complete(ctx, naturalUnavailable)
	}
	if _, ok := ownerSpan(r.t.Source, *envelope.Confirmation); !ok {
		return r.complete(ctx, naturalUnavailable)
	}
	return r.authorize(ctx, d)
}

// cancelJudged drops the open proposal when the agent judged that the owner
// declined it.
func (r *factoryConversationRun) cancelJudged(ctx context.Context, envelope ConversationEnvelope) error {
	d, err := r.s.db.ReadNaturalDialogue(ctx, r.s.transport, r.s.owner, r.c.ChannelRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if d.State == "awaiting_confirmation" && d.Code != "" && envelope.Proposal == d.Code {
		if err = r.s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at); err != nil {
			return err
		}
	}
	return r.complete(ctx, "Okay, I dropped it.")
}

// authorize consumes the confirmed proposal d once and executes exactly its
// stored request, after rechecking its digest and current targets.
func (r *factoryConversationRun) authorize(ctx context.Context, d store.NaturalDialogue) error {
	s := r.s
	if d.PromptID == "" || !r.at.Before(d.ExpiresAt) || !naturalLater(r.t.SourceID, d.SourceID) {
		return r.complete(ctx, naturalUnavailable)
	}
	if err := conversationAccess(ctx, s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return err
	}
	var p naturalPayload
	if err := json.Unmarshal([]byte(d.Payload), &p); err != nil {
		return err
	}
	_, digest, err := naturalDigest(p)
	if err != nil {
		return err
	}
	if digest != d.Digest {
		return r.complete(ctx, "The proposal changed; please request it again.")
	}
	refusal, err := s.recheckNatural(ctx, p, r.at)
	if err != nil {
		return err
	}
	if refusal != "" {
		if err = s.db.EndNaturalDialogue(ctx, d, "cancelled", r.at); err != nil {
			return err
		}
		return r.complete(ctx, refusal)
	}
	invocation, effect, err := s.ValidateInvocation(p.Invocation)
	if err != nil || effect != ChangeEffect {
		return r.complete(ctx, "This action is no longer enabled; request a fresh proposal.")
	}
	reply := store.ChatReply{Transport: s.transport, ChannelRef: r.c.ChannelRef, MessageID: "nl-exec:" + d.Code, Command: invocation.Command, Arguments: invocation.Arguments, CreatedAt: r.at, ResolvedRepoID: p.RepoID, ConversationTurnID: r.t.ID}
	if err = s.db.AuthorizeConversationDialogue(ctx, r.t, d, r.t.SourceID, reply, r.at); err != nil {
		return err
	}
	if p.PreviewID != "" {
		link, err := s.db.GetConversationActionLink(ctx, r.t.ID)
		if err != nil {
			return err
		}
		link.PreviewID = p.PreviewID
		if err = s.db.SaveConversationActionLink(ctx, r.t, link); err != nil {
			return err
		}
	}
	return r.execute(ctx, reply, p)
}

// standardsDirect reports the author steps that run without a confirmation:
// they record the owner's own words or ask the author for its next draft.
// Starting a session (it costs money), turning a draft into a proposal,
// approval and publication stay confirmed.
func standardsDirect(inv Invocation) bool {
	if inv.Command != "standards-author" {
		return false
	}
	switch inv.Arguments["action"] {
	case "answer", "continue", "edit", "validate", "rebase":
		return true
	}
	return false
}

// direct runs an author step without a proposal. Its outcome is told like a
// confirmed action's, and a draft it starts is posted after that reply.
func (r *factoryConversationRun) direct(ctx context.Context, p naturalPayload) error {
	s := r.s
	execution := "nl-direct:" + r.t.ID
	if err := s.db.SaveConversationActionLink(ctx, r.t, store.ConversationActionLink{TurnID: r.t.ID, ExecutionID: execution}); err != nil {
		return err
	}
	request, err := s.standardsRequest(p.Invocation, r.c.ChannelRef, r.t.SourceID, r.at)
	if err != nil {
		return err
	}
	request.OperationKey = s.transport + ":" + r.c.ChannelRef + ":" + execution
	result, err := s.standards.Execute(ctx, request)
	text := result.Text
	if err != nil {
		text = standardsError(err)
	} else if _, err = r.saveStandardsReference(ctx, result.References); err != nil {
		return err
	}
	if text, err = s.Protect(ctx, text); err != nil {
		return err
	}
	return r.narrate(ctx, text)
}

// naturalUnavailable answers a confirmation with nothing left to confirm.
const naturalUnavailable = "That proposal has expired or was already used, so I didn't do anything. Ask me again and I'll make a fresh one."

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
	// plain is the owner's view of the outcome when it differs from the saved
	// text, which keeps exact operation references for storage and details.
	plain := ""
	executionCtx, source := presentation.WithAssessment(ctx)
	if text == "" {
		switch reply.Command {
		case "standards-author", "standards-proposal", "standards-exception", "standards-analysis":
			request, e := s.standardsRequest(p.Invocation, r.c.ChannelRef, r.t.SourceID, r.at)
			request.OperationKey = s.transport + ":" + r.c.ChannelRef + ":" + reply.MessageID
			if e != nil {
				return e
			}
			result, e := s.standards.Execute(executionCtx, request)
			if e != nil {
				text = standardsError(e)
			} else {
				text, plain = standardsDisplay(result, request.OperationKey), result.Text
				if result.Interrupted {
					reply.OutcomeStatus = string(presentation.Uncertain)
				}
				if _, e = r.saveStandardsReference(ctx, result.References); e != nil {
					return e
				}
			}
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
					// saved completion is posted to the channel when ready.
					return s.conversationOutcome(ctx, r.t, "completed", "I've queued the diagnosis. I'll post what it finds here as soon as it's ready; any repair still goes through review on the issue.", r.at)
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
	if plain == "" || protected != text {
		return r.narrate(ctx, protected)
	}
	if plain, err = s.Protect(cleanup, plain); err != nil {
		return err
	}
	return r.narrate(ctx, plain)
}

// narrate tells the owner the outcome in the conversation's voice: one short
// message. If the narration fails, the outcome itself is the message.
func (r *factoryConversationRun) narrate(ctx context.Context, outcome string) error {
	instructions, runErr := r.s.restoreConversationInstructions(ctx, r.c.ID, r.at)
	var prompt factoryConversationPrompt
	var response []byte
	if runErr == nil {
		prompt = factoryConversationPrompt{Instructions: instructions.Factory() + " " + instructions.ActionOutcome(), SavedOutcome: outcome, OwnerSources: []DialogueTurn{{SourceID: r.t.SourceID, Text: r.t.Source}}, Commands: r.s.ConversationCommands()}
		raw, err := json.Marshal(prompt)
		if err != nil {
			return err
		}
		response, runErr = r.invoke(ctx, string(raw))
	}
	blocks := []presentation.AnswerBlock{{Text: outcome}}
	if runErr == nil {
		envelope, _, decodeErr := DecodeConversationEnvelope(response, ConversationDecodeRequest{Turns: prompt.OwnerSources, Commands: prompt.Commands})
		if decodeErr == nil && envelope.Kind == "answer" {
			blocks = envelope.Blocks
			if len(blocks) == 0 {
				blocks = []presentation.AnswerBlock{{Text: envelope.Text}}
			}
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.s.saveConversationPresentation(cleanup, r.t, "completed", blocks, "", r.at)
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
		if reply.State == "confirmed" {
			// The diagnosis worker already posted this result to the channel.
			protected = "Already posted to the owner in this channel; context only, do not repeat it unasked:\n" + protected
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
