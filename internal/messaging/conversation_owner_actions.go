package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

var errAmbiguousOwnerAction = errors.New("more than one proposal is pending; describe which action you mean before confirming or cancelling")

// EnableOwnerActions installs the one daemon-owned policy service before intake.
func (s *Service) EnableOwnerActions(actions *owneraction.Service, principal func(context.Context, string) (owneraction.Principal, error)) {
	s.ownerActions = actions
	s.ownerPrincipal = principal
	s.registerOwnerActionReads()
}

// OwnerActions returns the shared service for trusted daemon composition.
func (s *Service) OwnerActions() *owneraction.Service { return s.ownerActions }

// OwnerActionText renders exact factory facts; narration cannot alter this block.
func OwnerActionText(p owneraction.Proposal) string {
	args, _ := json.Marshal(p.Facts.Invocation.Arguments)
	scope := p.Facts.RepositoryRef
	if scope == "" {
		scope = "installation-wide"
	}
	origin := p.Principal.Transport
	if p.Principal.ClientID != "" {
		origin += " client " + p.Principal.ClientID + "; grant " + p.Principal.GrantID
	} else {
		origin += " owner " + p.Principal.ActorID() + "; channel " + p.Principal.ChannelID
	}
	text := fmt.Sprintf("Action: %s\nArguments: %s\nScope: %s\nExact target: %s %s — %s\nImpact: %s\nOwner: %s\nOrigin: %s\nCreated: %s\nExpires: %s\nEstimate: %s\nState: %s", p.Facts.Invocation.Command, args, scope, p.Facts.TargetKind, p.Facts.TargetID, p.Facts.TargetRevision, p.Tier, p.Principal.OwnerID, origin, p.CreatedAt.UTC().Format(time.RFC3339), p.ExpiresAt.UTC().Format(time.RFC3339), p.Facts.Estimate, p.State)
	if p.Reason != "" {
		text += "\nReason: " + p.Reason
	}
	if p.Result.Summary != "" {
		text += "\nResult: " + p.Result.Summary
	}
	if p.State == "awaiting_confirmation" {
		text += "\nConfirm separately at least five seconds after this proposal is delivered."
		if p.Tier == owneraction.Binding {
			text += " Then tap ✅ on the factory's separate Discord approval request before expiry."
		}
	}
	return text + "\nHistory reference: " + p.ID
}

func (r *factoryConversationRun) sharedPrincipal(ctx context.Context) (owneraction.Principal, error) {
	if r.s.ownerPrincipal == nil {
		return owneraction.Principal{}, owneraction.ErrUnavailable
	}
	if err := conversationAccess(ctx, r.s.conversations.Load(), r.c.ChannelRef, r.at); err != nil {
		return owneraction.Principal{}, err
	}
	return r.s.ownerPrincipal(ctx, r.c.ChannelRef)
}

func (r *factoryConversationRun) proposeShared(ctx context.Context, inv Invocation) error {
	principal, err := r.sharedPrincipal(ctx)
	if err != nil {
		return err
	}
	p, err := r.s.ownerActions.Propose(ctx, principal, owneraction.Invocation{Command: inv.Command, Arguments: inv.Arguments}, "conversation:"+r.t.ID)
	if err != nil {
		return r.complete(ctx, "No action was authorized: "+err.Error())
	}
	text := OwnerActionText(p)
	if _, err = presentation.RenderConversation(strings.Repeat("0", 32), nil, nil, text); err != nil {
		_, _ = r.s.ownerActions.Cancel(ctx, principal, p.ID)
		return r.complete(ctx, "The exact proposal is too large to show safely here. Nothing was authorized; shorten the request or review it through MCP.")
	}
	if err = r.s.db.SaveConversationActionLink(ctx, r.t, store.ConversationActionLink{TurnID: r.t.ID, DialogueID: p.ID, DialogueRevision: 1}); err != nil {
		return err
	}
	return r.s.saveConversationPresentation(ctx, r.t, "completed", nil, text, r.at)
}

// sharedSelection chooses only one delivered proposal. An ambiguous yes never
// guesses; the agent may resolve a specific history reference from owner words.
func (r *factoryConversationRun) sharedSelection(ctx context.Context, pending bool) (*owneraction.Proposal, error) {
	principal, err := r.sharedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	page, err := r.s.ownerActions.History(ctx, principal, "", 100)
	if err != nil {
		return nil, err
	}
	var candidates []owneraction.Proposal
	for _, p := range page.Proposals {
		visible := p.Principal.Transport == "discord" && p.Principal.ChannelID == r.c.ChannelRef || p.ApprovalChannel == r.c.ChannelRef
		if !visible {
			continue
		}
		if pending {
			if p.State != "awaiting_confirmation" && p.State != "awaiting_discord" {
				continue
			}
			if p.Principal.Transport == "discord" && p.AvailableAt.IsZero() {
				continue
			}
			if !r.at.Before(p.ExpiresAt) {
				continue
			}
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if pending && len(candidates) > 1 {
		return nil, errAmbiguousOwnerAction
	}
	return &candidates[0], nil
}

func (r *factoryConversationRun) sharedControl(ctx context.Context) (bool, error) {
	text := strings.Trim(strings.ToLower(strings.TrimSpace(r.t.Source)), ".!? ")
	inspect := naturalDetailsRequest(text) || text == "show that" || text == "show the result" || text == "status of that" || text == "inspect that"
	history := text == "history" || text == "show history" || text == "show my action history"
	yes, no := naturalYes(r.t.Source), naturalCancel(r.t.Source)
	if !inspect && !history && !yes && !no {
		return false, nil
	}
	if history {
		principal, err := r.sharedPrincipal(ctx)
		if err != nil {
			return true, err
		}
		page, err := r.s.ownerActions.History(ctx, principal, "", 10)
		if err != nil {
			return true, err
		}
		var lines []string
		for i, p := range page.Proposals {
			if _, e := r.saveOwnerActionReadReference(ctx, "owner-action", map[string]string{"proposal_id": p.ID}, i); e != nil {
				return true, e
			}
			lines = append(lines, p.Facts.Summary+": "+p.State+"; history reference "+p.ID)
		}
		if page.NextCursor != "" {
			if _, e := r.saveOwnerActionReadReference(ctx, "owner-action-page", map[string]string{"cursor": page.NextCursor}, conversationListedLimit); e != nil {
				return true, e
			}
			lines = append(lines, "More history is available; say more.")
		}
		if len(lines) == 0 {
			lines = []string{"No authorized action history is recorded."}
		}
		return true, r.complete(ctx, strings.Join(lines, "\n"))
	}
	p, err := r.sharedSelection(ctx, !inspect)
	if err != nil {
		return true, r.complete(ctx, err.Error())
	}
	if p == nil {
		if invalidated, e := r.s.db.LegacyOwnerActionInvalidation(ctx, r.s.owner, r.c.ChannelRef); e != nil {
			return true, e
		} else if invalidated {
			return true, r.complete(ctx, "The confirmation policy changed. Old pending proposals cannot run; request a fresh proposal.")
		}
		if inspect || yes || no {
			return true, r.complete(ctx, "No single delivered proposal is available. Ask for a fresh proposal, or describe the action to inspect its history.")
		}
		return false, nil
	}
	if inspect {
		principal, e := r.sharedPrincipal(ctx)
		if e != nil {
			return true, e
		}
		saved, e := r.s.ownerActions.Inspect(ctx, principal, p.ID)
		if e != nil {
			return true, e
		}
		return true, r.complete(ctx, OwnerActionText(saved))
	}
	if yes && (p.Principal.Transport != "discord" || r.t.ReplyTo != "" && r.t.ReplyTo != p.ProposalMessage) {
		return true, r.complete(ctx, "That yes does not select this proposal. Confirm it in its originating client or reply to its exact factory proposal.")
	}
	if yes && r.t.ReplyTo == "" {
		sources, e := r.s.db.ListConversationSources(ctx, r.c.ID, r.at, 32)
		if e != nil {
			return true, e
		}
		// The source that proposed this action is recorded by its turn link.
		for _, source := range sources {
			if source.SourceID == r.t.SourceID {
				continue
			}
			action, e := r.s.db.GetConversationActionLink(ctx, source.ID)
			if e == nil && action.DialogueID == p.ID {
				break
			}
			if naturalLater(source.SourceID, "0") {
				return true, r.complete(ctx, "Please reply to the factory proposal so this yes selects it unambiguously.")
			}
		}
	}
	principal, e := r.sharedPrincipal(ctx)
	if e != nil {
		return true, e
	}
	var changed owneraction.Proposal
	if no {
		changed, e = r.s.ownerActions.Cancel(ctx, principal, p.ID)
	} else {
		changed, e = r.s.ownerActions.Confirm(ctx, principal, p.ID)
	}
	if e != nil {
		return true, r.complete(ctx, "The action was not authorized: "+e.Error())
	}
	return true, r.complete(ctx, sharedAcknowledgment(changed))
}

func sharedAcknowledgment(p owneraction.Proposal) string {
	text := "Recorded state: " + p.State + "."
	switch p.State {
	case "awaiting_confirmation":
		text += " Earliest confirmation: " + p.ConfirmNotBefore.UTC().Format(time.RFC3339) + ". Send a separate confirmation after that time; no retry was scheduled."
	case "awaiting_discord":
		text += " Tap ✅ on the factory's separate approval request before expiry; no work is admitted yet."
	case "admitted", "executing":
		text += " Cancellation cannot undo admitted work. Inspect the recorded result before requesting it again."
	case "cancelled":
		text += " It was cancelled before admission."
	case "uncertain":
		text += " Inspect the retained result before proposing again."
	}
	if p.Reason != "" {
		text += " Reason: " + p.Reason + "."
	}
	return text + "\nHistory reference: " + p.ID
}

func (s *Service) bindSharedProposal(ctx context.Context, r store.ChatReply, message string) error {
	answer, ordinal, err := s.db.ConversationReplyAnswer(ctx, r)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if answer.ProposalPart != ordinal {
		return nil
	}
	link, err := s.db.GetConversationActionLink(ctx, answer.TurnID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if link.DialogueID == "" {
		return nil
	}
	return s.ownerActions.Available(ctx, link.DialogueID, message)
}

func (r *factoryConversationRun) judgedShared(ctx context.Context, envelope ConversationEnvelope, cancel bool) error {
	principal, err := r.sharedPrincipal(ctx)
	if err != nil {
		return err
	}
	saved, err := r.s.ownerActions.Inspect(ctx, principal, envelope.Proposal)
	if err != nil {
		return r.complete(ctx, naturalUnavailable)
	}
	p := &saved
	visible := p.Principal.Transport == "discord" && p.Principal.ChannelID == r.c.ChannelRef || p.ApprovalChannel == r.c.ChannelRef
	if p.State != "awaiting_confirmation" && p.State != "awaiting_discord" || !cancel && (!visible || p.Principal.Transport == "discord" && p.AvailableAt.IsZero()) {
		return r.complete(ctx, naturalUnavailable)
	}
	if !cancel && (p.Principal.Transport != "discord" || envelope.Confirmation == nil || envelope.Confirmation.SourceID != r.t.SourceID || !validOwnerConfirmationSpan(r.t.Source, *envelope.Confirmation)) {
		return r.complete(ctx, naturalUnavailable)
	}
	var changed owneraction.Proposal
	if cancel {
		changed, err = r.s.ownerActions.Cancel(ctx, principal, p.ID)
	} else {
		changed, err = r.s.ownerActions.Confirm(ctx, principal, p.ID)
	}
	if err != nil {
		return r.complete(ctx, "The action was not authorized: "+err.Error())
	}
	return r.complete(ctx, sharedAcknowledgment(changed))
}

// OwnerActionNotifications queues factual outcomes with their history references.
func (s *Service) OwnerActionNotifications(ctx context.Context, at time.Time) error {
	if s.ownerActions == nil || s.ownerPrincipal == nil {
		return nil
	}
	due, err := s.db.OwnerActionNoticesDue(ctx)
	if err != nil {
		return err
	}
	for _, record := range due {
		if !s.channels[record.ApprovalChannel] {
			continue
		}
		if err = conversationAccess(ctx, s.conversations.Load(), record.ApprovalChannel, at); err != nil {
			continue
		}
		principal, e := s.ownerPrincipal(ctx, record.ApprovalChannel)
		if e != nil {
			continue
		}
		p, e := s.ownerActions.Inspect(ctx, principal, record.ID)
		if e != nil {
			continue
		}
		text := p.Facts.Summary + ": " + p.State + ".\n" + p.Result.Summary + "\nHistory reference: " + p.ID
		notices, e := s.announcement(ctx, record.ApprovalChannel, "owner-outcome:"+p.ID+":"+p.State, "", text, "", at)
		if e != nil {
			return e
		}
		if e = s.db.QueueOwnerActionOutcome(ctx, p.ID, p.State, notices); e != nil && !errors.Is(e, store.ErrNotFound) {
			return e
		}
	}
	return nil
}
