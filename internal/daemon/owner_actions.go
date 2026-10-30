package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// configureOwnerActions composes the shared service over existing domain adapters.
func (d *Daemon) configureOwnerActions(s *messaging.Service, discordOwner string, channels []string, adapter *discord.Adapter, clock func() time.Time) {
	var actions *owneraction.Service
	if d.remoteConnector != nil && d.remoteConnector.actions != nil {
		actions = d.remoteConnector.actions.actions
		d.remoteConnector.actions.notices.Store(s)
	} else {
		actions = owneraction.New(d.db, messaging.OwnerActionDomain{Service: s}, clock)
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
	text := fmt.Sprintf("Approval request\nAction: %s %s\nScope: %s\nExact target: %s %s — %s\nImpact: %s\nEstimate: %s\nOwner: %s\nExpires: %s\nTap ✅ on this message to authorize the exact reviewed action.\nHistory reference: %s", p.Facts.Invocation.Command, p.Facts.Invocation.Arguments["action"], p.Facts.RepositoryRef, p.Facts.TargetKind, p.Facts.TargetID, p.Facts.TargetRevision, p.Tier, p.Facts.Estimate, p.Principal.OwnerID, p.ExpiresAt.UTC().Format(time.RFC3339), p.ID)
	if p.Facts.RepositoryRef == "" {
		text = fmt.Sprintf("Installation-wide %s", text)
	}
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
	if d.remoteConnector != nil && d.remoteConnector.actions != nil {
		d.remoteConnector.actions.start(ctx, wg, d.log)
		return
	}
	actions := d.chat.service.OwnerActions()
	if actions == nil {
		return
	}
	runtime := &ownerActionRuntime{actions: actions}
	runtime.notices.Store(d.chat.service)
	runtime.start(ctx, wg, d.log)
}

func (d *Daemon) ownerReaction(ctx context.Context, r discord.OwnerReaction) {
	actions := d.chat.service.OwnerActions()
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
