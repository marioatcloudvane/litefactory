package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func (s *Service) registerOwnerActionReads() {
	s.RegisterCommand(CommandDefinition{Name: "actions", Description: "Read owner action history, inspect a selected proposal, or read more history", NaturalLanguage: true, Effect: ReadEffect, Arguments: []Argument{{Name: "operation", Type: TextArgument, Description: "history, inspect or more"}, {Name: "proposal_id", Type: TextArgument, Description: "Factory reference for the selected proposal; the client carries this"}, {Name: "cursor", Type: TextArgument, Description: "Factory reference for the next authorized history page"}, {Name: "limit", Type: IntegerArgument, Description: "Maximum history records, from 1 to 50"}}}, nil)
}

func (r *factoryConversationRun) lookupOwnerActions(ctx context.Context, inv Invocation) (conversationReadResult, *ConversationReference, error) {
	result := conversationReadResult{Command: "actions", Arguments: inv.Arguments, Source: "factory:owner-actions", CapturedAt: r.at, Complete: true, Gaps: []string{}}
	if r.s.ownerActions == nil {
		return result, nil, owneraction.ErrUnavailable
	}
	actor, err := r.sharedPrincipal(ctx)
	if err != nil {
		return result, nil, err
	}
	validated, effect, err := r.s.ValidateInvocation(inv)
	if err != nil || effect != ReadEffect {
		return result, nil, ErrConversationEnvelope
	}
	operation := validated.Arguments["operation"]
	if operation == "" {
		operation = "history"
	}
	limit := 10
	if raw := validated.Arguments["limit"]; raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			return result, nil, ErrConversationEnvelope
		}
	}
	result.Status = "recorded"
	var next *ConversationReference
	switch operation {
	case "inspect":
		if validated.Arguments["cursor"] != "" {
			return result, nil, ErrConversationEnvelope
		}
		id := validated.Arguments["proposal_id"]
		if id == "" {
			selected, e := r.sharedSelection(ctx, false)
			if e != nil {
				return result, nil, e
			}
			if selected == nil {
				result.Status = "empty"
				result.Result = "No authorized proposal is recorded."
				return result, nil, nil
			}
			id = selected.ID
		}
		p, e := r.s.ownerActions.Inspect(ctx, actor, id)
		if e != nil {
			return result, nil, e
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return result, nil, e
		}
		result.Result = string(raw)
		result.Target = p.Facts.TargetID
		result.ResolvedRepoID = p.Facts.RepositoryID
		next, err = r.saveOwnerActionReadReference(ctx, "owner-action", map[string]string{"proposal_id": p.ID}, 0)
	case "history", "more":
		if validated.Arguments["proposal_id"] != "" {
			return result, nil, ErrConversationEnvelope
		}
		cursor := validated.Arguments["cursor"]
		if operation == "more" && cursor == "" {
			result.Status = "clarification"
			result.Clarification = "Read action history first, then use its factory next-page reference."
			return result, nil, nil
		}
		page, e := r.s.ownerActions.History(ctx, actor, cursor, limit)
		if e != nil {
			return result, nil, e
		}
		raw, e := json.Marshal(page)
		if e != nil {
			return result, nil, e
		}
		result.Result = string(raw)
		for i, p := range page.Proposals {
			if i >= conversationListedLimit {
				break
			}
			ref, e := r.saveOwnerActionReadReference(ctx, "owner-action", map[string]string{"proposal_id": p.ID}, i)
			if e != nil {
				return result, nil, e
			}
			r.listed = append(r.listed, *ref)
		}
		if page.NextCursor != "" {
			next, err = r.saveOwnerActionReadReference(ctx, "owner-action-page", map[string]string{"cursor": page.NextCursor}, conversationListedLimit)
		}
	default:
		return result, nil, ErrConversationEnvelope
	}
	if err != nil {
		return result, nil, err
	}
	if bounded, cut := boundedConversationText(result.Result, 16384); cut {
		result.Result = bounded
		result.Complete = false
		result.Gaps = append(result.Gaps, "The history detail exceeded the conversation evidence limit; inspect one selected proposal for its details.")
	}
	return result, next, nil
}

func (r *factoryConversationRun) saveOwnerActionReadReference(ctx context.Context, kind string, args map[string]string, index int) (*ConversationReference, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	saved := store.ConversationReference{ID: fmt.Sprintf("%s-action-%d-%d%s", r.t.ID, r.ordinal, index, r.refSuffix), TurnID: r.t.ID, Kind: kind, Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
	if err = r.s.db.SaveConversationReference(ctx, r.t, saved); err != nil {
		return nil, err
	}
	return &ConversationReference{ID: saved.ID, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, ExpiresAt: saved.ExpiresAt, Arguments: args}, nil
}

func (r *factoryConversationRun) ownerActionReferenceCurrent(ctx context.Context, ref store.ConversationReference, args map[string]string) (bool, error) {
	if r.s.ownerActions == nil || !r.at.Before(ref.ExpiresAt) {
		return false, nil
	}
	p, err := r.sharedPrincipal(ctx)
	if err != nil {
		return false, err
	}
	switch ref.Kind {
	case "owner-action":
		if args["proposal_id"] == "" {
			return false, nil
		}
		_, err = r.s.ownerActions.Inspect(ctx, p, args["proposal_id"])
	case "owner-action-page":
		if args["cursor"] == "" {
			return false, nil
		}
		_, err = r.s.ownerActions.History(ctx, p, args["cursor"], 1)
	default:
		return false, nil
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConnectorInvalid) || errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorScope) {
		return false, nil
	}
	return err == nil, err
}
