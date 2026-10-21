package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// savePreviewReference exposes only a code from a saved factory preview. Native
// history and owner/model-authored values cannot manufacture this reference.
func (r *factoryConversationRun) savePreviewReference(ctx context.Context, preview store.ChatIssuePreview) error {
	if preview.Transport != r.c.Transport || preview.ChannelRef != r.c.ChannelRef || preview.State != "previewed" || !r.at.Before(preview.ExpiresAt) {
		return errors.New("conversation preview is not current")
	}
	current, err := r.s.db.CurrentChatIssuePreview(ctx, r.c.Transport, r.c.ChannelRef)
	if err != nil {
		return err
	}
	if current.ID != preview.ID || current.ContentHash != preview.ContentHash || current.Code != preview.Code || current.RepoID != preview.RepoID {
		return errors.New("conversation preview changed")
	}
	repo, err := r.s.db.GetRepo(ctx, preview.RepoID)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]string{"project": repo.Slug(), "code": preview.Code})
	if err != nil {
		return err
	}
	return r.s.db.SaveConversationReference(ctx, r.t, store.ConversationReference{ID: r.t.ID + "-preview", TurnID: r.t.ID, Kind: "preview", Repository: strconv.FormatInt(preview.RepoID, 10), Target: preview.ID, Revision: preview.ContentHash, Content: string(content), ExpiresAt: preview.ExpiresAt})
}

// previousFactoryReply is explanation context only, never an executable source.
// Its selection is based on persisted turn sequence, independent of slice order.
func (r *factoryConversationRun) previousFactoryReply(ctx context.Context, sources []store.ConversationTurn) (string, error) {
	var previous *store.ConversationTurn
	for i := range sources {
		source := &sources[i]
		if source.ID == r.t.ID || source.ConversationID != r.c.ID || source.Sequence >= r.t.Sequence || source.ReplyID == "" {
			continue
		}
		if previous == nil || source.Sequence > previous.Sequence {
			previous = source
		}
	}
	if previous == nil {
		return "", nil
	}
	reply, err := r.s.db.ReadChatReply(ctx, r.c.Transport, previous.ReplyID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if reply.ChannelRef != r.c.ChannelRef || reply.Command != "conversation" {
		return "", nil
	}
	text := reply.ReplyText
	if answer, _, lookupErr := r.s.db.ConversationReplyAnswer(ctx, reply); lookupErr == nil {
		parts, partErr := r.s.db.ReadConversationAnswerParts(ctx, answer.ID)
		if partErr != nil {
			return "", partErr
		}
		var content []string
		for _, part := range parts {
			content = append(content, part.Text)
		}
		text = strings.Join(content, "\n\n")
	} else if !errors.Is(lookupErr, store.ErrNotFound) {
		return "", lookupErr
	}
	protected, err := r.s.Protect(ctx, text)
	if err != nil {
		return "", err
	}
	bounded, _ := boundedConversationText(protected, 4096)
	return bounded, nil
}

// referenceCurrent refreshes revision-sensitive factory references. It does not
// grant permission: the coordinator still resolves the registered repository,
// and proposal/confirmation/execution independently recheck target permissions.
func (r *factoryConversationRun) referenceCurrent(ctx context.Context, reference store.ConversationReference, args map[string]string) (bool, error) {
	if !r.at.Before(reference.ExpiresAt) {
		return false, nil
	}
	switch reference.Kind {
	case "standards":
		// Exact saved identities remain references even when stale. The domain
		// adapter checks their versions; never silently advance them to latest.
		return r.s.standards != nil, nil
	case "preview":
		preview, err := r.s.db.CurrentChatIssuePreview(ctx, r.c.Transport, r.c.ChannelRef)
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return preview.State == "previewed" && r.at.Before(preview.ExpiresAt) && preview.ID == reference.Target && preview.ContentHash == reference.Revision && preview.Code == args["code"] && strconv.FormatInt(preview.RepoID, 10) == reference.Repository, nil
	case "target":
		if reference.Revision == "" {
			return true, nil
		}
		if r.s.research == nil || args["revision"] != reference.Revision {
			return false, nil
		}
		target, refusal := r.s.researchTarget(ctx, Message{Arguments: args}, true)
		if refusal != "" {
			return false, nil
		}
		question, err := r.s.research.Current(ctx, target)
		if err != nil {
			return false, err
		}
		return question.Text != "" && question.Token == reference.Revision, nil
	default:
		return false, nil
	}
}

// Replies are paired by retained source identity, never admitted as owner input.
type conversationPriorReply struct {
	SourceID string `json:"source_id"`
	Reply    string `json:"factory_reply"`
}

func (r *factoryConversationRun) recentFactoryReplies(ctx context.Context, sources []store.ConversationTurn, turns []DialogueTurn) ([]conversationPriorReply, error) {
	var out []conversationPriorReply
	remaining := 4096
	for _, turn := range turns {
		for _, source := range sources {
			if source.SourceID != turn.SourceID || source.ID == r.t.ID {
				continue
			}
			reply, err := r.previousFactoryReply(ctx, []store.ConversationTurn{source})
			if err != nil {
				return nil, err
			}
			if reply == "" {
				reply = "No completed factory reply retained; request may remain unanswered."
			}
			// Divide the existing history budget across all retained owner sources.
			reply, cut := boundedConversationText(reply, min(remaining, 4096/max(1, len(turns))))
			if cut {
				reply += " [reply excerpt]"
			}
			remaining -= len(reply)
			out = append(out, conversationPriorReply{SourceID: source.SourceID, Reply: reply})
		}
	}
	return out, nil
}
