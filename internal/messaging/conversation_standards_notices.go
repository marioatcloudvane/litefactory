package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// StandardsNotice is an immutable retained scheduler notice, never owner input.
type StandardsNotice struct {
	ID      int64
	Payload string
}

// StandardsNoticeSource supplies bounded notices using the scheduler's retained IDs.
type StandardsNoticeSource interface {
	Notices(context.Context, StandardsRequest) ([]StandardsNotice, error)
}

// EnableStandardsNotices selects only the configured, authorized owner destination.
func (s *Service) EnableStandardsNotices(destination string) { s.standardsNoticeChannel = destination }

// StandardsNoticeStep projects at most 25 retained notices and attempts one
// immutable part. The existing conversation sender calls it only when idle.
func (s *Service) StandardsNoticeStep(ctx context.Context, at time.Time) (bool, error) {
	channel := s.standardsNoticeChannel
	source, ok := s.standards.(StandardsNoticeSource)
	if !ok || channel == "" || s.conversations.Load() == nil {
		return false, nil
	}
	if !s.channels[channel] || s.conversations.Load().bindings[channel].Provider == "" {
		return false, nil
	}
	if err := conversationAccess(ctx, s.conversations.Load(), channel, at); err != nil {
		return false, err
	}
	backlog, err := s.db.CountStandardsConversationNoticeBacklog(ctx, s.transport, s.owner, channel)
	if err != nil {
		return false, err
	}
	var notices []StandardsNotice
	if backlog == 0 {
		after, err := s.db.LastStandardsConversationNoticeID(ctx, s.transport, s.owner, channel)
		if err != nil {
			return false, err
		}
		request := StandardsRequest{Operation: "notice.list", Transport: s.transport, OwnerID: s.owner, ChannelID: channel, MessageID: "standards-notice-worker", Arguments: map[string]string{"after": strconv.FormatInt(after, 10)}, At: at}
		notices, err = source.Notices(ctx, request)
		if err != nil {
			return false, err
		}
		if len(notices) > 25 {
			return false, errors.New("standards notice source exceeded batch limit")
		}
		for _, notice := range notices {
			payload, err := s.Protect(ctx, notice.Payload)
			if err != nil {
				return false, err
			}
			if payload != notice.Payload {
				return false, errors.New("standards notice exact references changed by protection")
			}
			if _, err = s.db.SaveStandardsConversationNoticeDelivery(ctx, store.StandardsConversationNoticeDelivery{NoticeID: notice.ID, Transport: s.transport, OwnerID: s.owner, ChannelID: channel, Payload: payload}); err != nil {
				return false, err
			}
		}
	}
	rows, err := s.db.ListDueStandardsConversationNoticeDeliveries(ctx, s.transport, s.owner, channel, at)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return len(notices) > 0, nil
	}
	d, err := s.db.ClaimStandardsConversationNoticeDelivery(ctx, rows[0].NoticeID, s.transport, s.owner, channel, at, time.Minute)
	if err != nil {
		return false, err
	}
	parts := standardsReviewParts(d.Payload)
	if d.PartsAcknowledged >= len(parts) {
		return false, errors.New("invalid saved standards notice part progress")
	}
	// Every retry rechecks access; the transport disables mentions for notices.
	if err = conversationAccess(ctx, s.conversations.Load(), channel, at); err != nil {
		d.Status = "paused"
		d.LastError = "Owner conversation authorization unavailable"
		if delay := accessRetry(err); delay > 0 {
			d.Status = "pending"
			d.NextAttemptAt = at.Add(delay)
		}
		_, saveErr := s.db.FinishStandardsConversationNoticeDelivery(ctx, d, at)
		return true, saveErr
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	delivery := s.sender.Deliver(work, Target{Transport: s.transport, Channel: channel, MessageID: fmt.Sprintf("standards-notice:%d:%d", d.NoticeID, d.PartsAcknowledged), Protected: true, Notice: true}, parts[d.PartsAcknowledged])
	switch {
	case delivery.Confirmed && delivery.PromptID != "":
		d.PartsAcknowledged++
		d.RemoteID = delivery.PromptID
		d.Status = "pending"
		d.NextAttemptAt = at
		if d.PartsAcknowledged == len(parts) {
			d.Status = "delivered"
		}
	case delivery.Permanent:
		d.Status = "paused"
		d.LastError = "Transport permanently refused retained standards notice"
	default:
		d.Status = "pending"
		d.LastError = "Delivery unconfirmed; immutable notice retained"
		d.NextAttemptAt = at.Add(max(delivery.RetryAfter, min(time.Second<<min(d.Attempts-1, 10), 15*time.Minute)))
	}
	_, err = s.db.FinishStandardsConversationNoticeDelivery(ctx, d, at)
	return true, err
}

func (s *Service) standardsNoticeStatus(ctx context.Context) string {
	if s.standardsNoticeChannel == "" {
		return "Standards notice delivery is unconfigured; original notices remain retained and inspectable."
	}
	if s.conversations.Load() == nil || !s.channels[s.standardsNoticeChannel] || s.conversations.Load().bindings[s.standardsNoticeChannel].Provider == "" {
		return "Standards notice delivery is paused: the configured destination is not an authorized owner conversation. Original notices remain retained and inspectable."
	}
	count, err := s.db.CountStandardsConversationNoticeBacklog(ctx, s.transport, s.owner, s.standardsNoticeChannel)
	if err != nil {
		return "Standards notice delivery backlog unavailable."
	}
	rows, err := s.db.ListStandardsConversationNoticeDeliveries(ctx, s.transport, s.owner, s.standardsNoticeChannel, 25)
	if err != nil {
		return "Standards notice delivery details unavailable."
	}
	raw, _ := json.Marshal(rows)
	return fmt.Sprintf("Standards notice delivery backlog: %d. Retained delivery states: %s", count, raw)
}
