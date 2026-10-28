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

// propose routes every new changing action through the shared policy service.
func (r *factoryConversationRun) propose(ctx context.Context, inv Invocation, _ ConversationEnvelope, _ []DialogueTurn) error {
	if r.s.ownerActions == nil {
		return r.complete(ctx, "Action confirmation is unavailable. Nothing was authorized; request a fresh proposal after the factory is configured.")
	}
	return r.proposeShared(ctx, inv)
}

func (r *factoryConversationRun) confirm(ctx context.Context) (bool, error) {
	if r.s.ownerActions != nil {
		return r.sharedControl(ctx)
	}
	if naturalYes(r.t.Source) || naturalCancel(r.t.Source) || naturalDetailsRequest(r.t.Source) || naturalConfirmationCode(r.t.Source) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.t.Source)), "confirm ") {
		return true, r.complete(ctx, "Old pending proposals cannot be confirmed. Request a fresh proposal through the current action policy.")
	}
	return false, nil
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

// openProposal exposes only current-policy proposals to interpretation.
func (r *factoryConversationRun) openProposal(ctx context.Context) (*conversationOpenProposal, error) {
	if r.s.ownerActions == nil {
		return nil, nil
	}
	p, err := r.sharedSelection(ctx, true)
	if err != nil {
		if errors.Is(err, errAmbiguousOwnerAction) {
			return nil, nil
		}
		return nil, err
	}
	if p == nil || p.Principal.Transport != "discord" || p.State != "awaiting_confirmation" {
		return nil, nil
	}
	return &conversationOpenProposal{Reference: p.ID, Text: OwnerActionText(*p), ExpiresAt: p.ExpiresAt}, nil
}

func (r *factoryConversationRun) confirmJudged(ctx context.Context, envelope ConversationEnvelope) error {
	if r.s.ownerActions == nil {
		return r.complete(ctx, naturalUnavailable)
	}
	return r.judgedShared(ctx, envelope, false)
}

func (r *factoryConversationRun) cancelJudged(ctx context.Context, envelope ConversationEnvelope) error {
	if r.s.ownerActions == nil {
		return r.complete(ctx, naturalUnavailable)
	}
	return r.judgedShared(ctx, envelope, true)
}

// spansCurrent reports whether argument name of envelope is a span of the
// owner message source.
func spansCurrent(envelope ConversationEnvelope, name, source string) bool {
	for _, arg := range envelope.Arguments {
		if arg.Name == name {
			return arg.SourceID != "" && arg.SourceID == source
		}
	}
	return false
}

// naturalUnavailable answers a confirmation with nothing left to confirm.
const naturalUnavailable = "That proposal has expired or was already used, so I didn't do anything. Ask me again and I'll make a fresh one."

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
