package daemon

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// configureOwnerActions attaches delivery and bounded consumer access to the existing coordinator.
func (d *Daemon) configureOwnerActions(s *messaging.Service, discordOwner string, channels []string, adapter *discord.Adapter) {
	if d.ownerActions == nil {
		return
	}
	runtime := d.ownerActions
	runtime.adapter.Store(adapter)
	actions := runtime.actions
	if workflows := runtime.workflows; workflows != nil {
		bindWorkflowNoticeAuthorizer(s, adapter, workflows)
	}
	runtime.notices.Store(s)
	if d.chat != nil {
		d.chat.actions = actions
	}
	actions.SetDiscord(discordOwner, channels)
	actions.SetDelivery(actionApprovalDelivery{adapter})
	s.EnableOwnerActions(actions, func(ctx context.Context, channel string) (owneraction.Principal, error) {
		pin := d.filter.Identities().OwnerUserID
		current, err := d.db.ConnectorOwner(ctx, pin)
		if err != nil {
			return owneraction.Principal{}, err
		}
		return owneraction.Principal{OwnerID: strconv.FormatInt(current.OwnerID, 10), OwnerGeneration: current.Generation, Transport: "discord", ChannelID: channel, DiscordOwnerID: discordOwner}, nil
	})
	if d.remoteConnector != nil {
		d.remoteConnector.server.SetActions(actions)
	}
}

type actionApprovalDelivery struct{ adapter *discord.Adapter }

func (a actionApprovalDelivery) Send(ctx context.Context, p owneraction.Proposal) (string, error) {
	text := owneraction.Render(p, owneraction.Options{Transport: owneraction.TransportDiscord, Mode: owneraction.Approval}).Text()
	delivery := a.adapter.Deliver(ctx, messaging.Target{Transport: "discord", Channel: p.ApprovalChannel, MessageID: "owner-approval:" + p.ID, Protected: true}, text)
	if delivery.Confirmed && delivery.PromptID != "" {
		return delivery.PromptID, nil
	}
	if delivery.Status == 429 && delivery.RetryAfter > 0 {
		return "", &owneraction.DeliveryRetryError{RetryAfter: delivery.RetryAfter}
	}
	return "", errors.New("discord approval delivery was not confirmed; no action was authorized")
}

func (d *Daemon) startOwnerActions(ctx context.Context, wg *sync.WaitGroup) {
	if d.ownerActions != nil {
		d.ownerActions.start(ctx, wg, d.log)
	}
}

func (d *Daemon) ownerReaction(ctx context.Context, r discord.OwnerReaction) {
	actions := d.chat.actions
	if actions == nil {
		return
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := actions.Reaction(work, owneraction.Reaction{ChannelID: r.ChannelID, MessageID: r.MessageID, UserID: r.UserID, Emoji: "✅", ReceivedAt: r.ReceivedAt})
	if err != nil {
		d.log.InfoContext(ctx, "owner reaction did not authorize an action")
	}
}

func bindWorkflowNoticeAuthorizer(s *messaging.Service, adapter *discord.Adapter, workflows *durableworkflow.Service) {
	s.SetWorkflowNoticeAuthorizer(func(ctx context.Context, eventKey, channel string) error {
		if err := workflows.AuthorizeNotice(ctx, eventKey, channel); err != nil {
			return err
		}
		return adapter.CheckIntakeCapability(ctx, channel)
	})
}
