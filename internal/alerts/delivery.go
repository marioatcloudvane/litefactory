package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Message is immutable alert intent. OwnerID is configured composition data,
// never source text. Only the adapter constructs the visible mention token.
type Message struct {
	ID, Channel, Body, OwnerID string
	Severity, Kind             string
	MentionOwner               bool
	AttemptedAt                time.Time
}

// Delivery records remote acceptance or a safe delivery limitation.
type Delivery struct {
	Confirmed, Permanent bool
	RemoteID             string
	RetryAfter           time.Duration
	Unavailable          string
}

// Transport sends and reconciles only alert-owned messages.
type Transport interface {
	SendAlert(context.Context, Message) Delivery
	ReconcileAlert(context.Context, Message, string) (string, error)
}

func message(n *store.AlertNotification, owner string) Message {
	if n.OwnerID != "" {
		owner = n.OwnerID
	}
	return Message{ID: n.ID, Channel: n.Destination, Body: n.Body, OwnerID: owner, Severity: n.Severity, Kind: n.Kind, MentionOwner: n.MentionOwner, AttemptedAt: n.AttemptedAt}
}

// SendStep reconciles uncertain attempts before taking one new fenced claim.
// A failed/ambiguous request is never blindly retried. Known 429 rejections are.
func (e *Engine) SendStep(ctx context.Context, at time.Time, channel, owner string, t Transport) (bool, error) {
	at = at.UTC()
	if t == nil || channel == "" {
		return false, e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
			l.Delivery.Ready = false
			l.Delivery.LastAttempt = at
			l.Delivery.Reason = "discord_destination_unavailable"
			return nil
		})
	}
	var claim *store.AlertNotification
	err := e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		for _, n := range l.Notifications {
			if n.State == "sending" && !at.Before(n.LeaseUntil) {
				n.State = "uncertain"
			}
		}
		due := e.Ready(l, at, channel)
		claimSend := func(n *store.AlertNotification) {
			if n.OwnerID == "" {
				n.OwnerID = owner
			}
			n.State = "sending"
			n.Fence++
			n.Attempt++
			n.AttemptedAt = at
			n.LeaseUntil = at.Add(time.Minute)
			snapshot := *n
			claim = &snapshot
		}
		eligible := func(n *store.AlertNotification) bool {
			return n.Destination == channel && (n.Attempt == 0 || !n.MentionOwner || n.OwnerID == "" || n.OwnerID == owner)
		}
		for _, n := range due {
			if n.Severity == "critical" && eligible(n) {
				claimSend(n)
				return nil
			}
		}
		for _, n := range l.Notifications {
			if n.State == "uncertain" && !at.Before(n.RetryAt) {
				snapshot := *n
				claim = &snapshot
				n.RetryAt = at.Add(5 * time.Minute)
				return nil
			}
		}
		for _, n := range due {
			if eligible(n) {
				claimSend(n)
				break
			}
		}

		return nil
	})
	if err != nil || claim == nil {
		return false, err
	}
	if claim.State == "uncertain" {
		id, err := t.ReconcileAlert(ctx, message(claim, owner), claim.RemoteID)
		if err != nil {
			return true, e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
				n := l.Notifications[claim.ID]
				if n != nil && n.Fence == claim.Fence {
					n.LastError = "history_unavailable"
				}
				l.Delivery.Ready = false
				l.Delivery.LastAttempt = at
				l.Delivery.Reason = "history_unavailable"
				return nil
			})
		}
		if id == "" {
			return true, nil
		}
		return true, e.confirm(ctx, claim, id, at)
	}
	d := t.SendAlert(ctx, message(claim, owner))
	if d.Confirmed && d.RemoteID != "" {
		return true, e.confirm(ctx, claim, d.RemoteID, at)
	}
	return true, e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		n := l.Notifications[claim.ID]
		if n == nil || n.Fence != claim.Fence || n.State != "sending" {
			return fmt.Errorf("stale alert send attempt")
		}
		n.State = "uncertain"
		n.LastError = "send_unconfirmed"
		n.RetryAt = at.Add(5 * time.Minute)
		if d.RetryAfter > 0 {
			n.State = "queued"
			n.RetryAt = at.Add(d.RetryAfter)
			n.LastError = "rate_limited"
		}
		if d.Permanent {
			n.State = "queued"
			n.RetryAt = at.Add(5 * time.Minute)
			n.LastError = "delivery_unavailable"
		}
		l.Delivery.Ready = false
		l.Delivery.LastAttempt = at
		l.Delivery.Reason = d.Unavailable
		if l.Delivery.Reason == "" {
			l.Delivery.Reason = n.LastError
		}
		return nil
	})
}
func (e *Engine) confirm(ctx context.Context, claim *store.AlertNotification, id string, at time.Time) error {
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		n := l.Notifications[claim.ID]
		if n == nil || n.Fence != claim.Fence || (n.State != "sending" && n.State != "uncertain") {
			return fmt.Errorf("stale alert confirmation")
		}
		n.State = "confirmed"
		n.RemoteID = id
		n.ConfirmedAt = at
		n.LastError = ""
		l.Delivery = store.AlertObserver{Ready: true, LastSuccess: at, LastAttempt: at}
		if n.Kind == "digest" {
			for _, m := range n.Members {
				s := l.Subjects[m.Subject]
				if s == nil {
					continue
				}
				ep := s.Episodes[m.Episode]
				if ep == nil {
					continue
				}
				if m.Kind == "clear" {
					ep.ClearConfirmed = true
				} else {
					ep.OpeningConfirmed = true
					if m.Resolved {
						ep.ClearConfirmed = true
					}
				}
				for _, p := range l.Notifications {
					if p.Subject == m.Subject && p.Episode == m.Episode && ((p.State == "grouped") || (m.Resolved && p.Kind == "clear" && p.Attempt == 0)) {
						p.State = "confirmed"
						p.RemoteID = id
						p.ConfirmedAt = at
					}
				}
			}
			return nil
		}
		s := l.Subjects[n.Subject]
		if s != nil {
			ep := s.Episodes[n.Episode]
			if ep != nil {
				if n.Kind == "clear" {
					ep.ClearConfirmed = true
				} else {
					ep.OpeningConfirmed = true
					if !ep.RecoveredAt.IsZero() && strings.Contains(claim.Body, "already resolved") {
						ep.ClearConfirmed = true
						for _, pending := range l.Notifications {
							if pending.Subject == n.Subject && pending.Episode == n.Episode && pending.Kind == "clear" && pending.Attempt == 0 {
								pending.State = "suppressed"
							}
						}
					}
				}
			}
		}
		return nil
	})
}
