package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationDiagnosisInput rechecks a queued conversation action immediately
// before its existing diagnosis worker starts. Exact protected notes come from
// the saved authorized payload, including after restart, never provider memory.
func (s *Service) ConversationDiagnosisInput(ctx context.Context, reply store.ChatReply, at time.Time) (string, error) {
	runtime := s.conversations.Load()
	if runtime == nil || !runtime.factory || reply.ConversationTurnID == "" || reply.Command != "diagnose" || reply.Transport != s.transport || !s.channels[reply.ChannelRef] || runtime.bindings[reply.ChannelRef].Provider == "" {
		return "", errors.New("conversation diagnosis is no longer permitted")
	}
	if err := conversationAccess(ctx, runtime, reply.ChannelRef, at); err != nil {
		return "", err
	}
	conversation, err := s.db.ReadConversation(ctx, s.transport, reply.ChannelRef)
	if err != nil {
		return "", err
	}
	execution, err := s.db.ReadNaturalExecution(ctx, reply.MessageID)
	if err != nil {
		return "", err
	}
	link, err := s.db.GetConversationActionLink(ctx, reply.ConversationTurnID)
	if err != nil {
		return "", err
	}
	if conversation.OwnerID != s.owner || conversation.HealthReason != "" || execution.OwnerID != s.owner || execution.Transport != s.transport || execution.ChannelRef != reply.ChannelRef || execution.State != "executing" || link.ExecutionID != reply.MessageID {
		return "", errors.New("conversation diagnosis authorization unavailable")
	}
	var payload naturalPayload
	if err = json.Unmarshal([]byte(execution.Payload), &payload); err != nil {
		return "", err
	}
	if payload.Invocation.Command != "diagnose" {
		return "", ErrInvalidInvocation
	}
	if refusal, err := s.recheckNatural(ctx, payload, at); err != nil {
		return "", err
	} else if refusal != "" {
		return "", errors.New("conversation diagnosis target or protected content changed")
	}
	target, refusal := s.researchTarget(ctx, Message{Arguments: payload.Invocation.Arguments}, false)
	if refusal != "" || target != reply.TargetRef {
		return "", errors.New("conversation diagnosis target changed")
	}
	return payload.Invocation.Arguments["note"], nil
}

// PushConversationDiagnosis posts a finished conversation diagnosis to its
// channel without waiting for the owner to write again; link, when set, follows
// verbatim. It is delivered after the conversation's queued confirmation, and a
// repeated push changes nothing.
func (s *Service) PushConversationDiagnosis(ctx context.Context, reply store.ChatReply, link string, at time.Time) error {
	if reply.ConversationTurnID == "" || reply.Command != "diagnose" || reply.Transport != s.transport {
		return errors.New("not a conversation diagnosis")
	}
	notices, err := s.announcement(ctx, reply.ChannelRef, "diagnosis:"+reply.MessageID, reply.MessageID, reply.ReplyText, link, at)
	if err != nil {
		return err
	}
	return s.db.PushConversationDiagnosis(ctx, reply, notices, at)
}
