package alerts

import (
	"sort"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Projection contains only safe rule names, timestamps and state. Subject keys,
// provider/account identities, raw output and delivery bodies are never exposed.
type Projection struct {
	Enabled             *bool            `json:"enabled"`
	Rules               []RuleStatus     `json:"rules"`
	Active              []IncidentStatus `json:"active"`
	HasMore             bool             `json:"has_more"`
	GlobalMuteUntil     *time.Time       `json:"global_mute_until"`
	DeliveryReady       bool             `json:"delivery_ready"`
	DeliveryReason      string           `json:"delivery_reason"`
	DeliveryLastSuccess *time.Time       `json:"delivery_last_success"`
	DeliveryStates      map[string]int   `json:"delivery_states"`
}

// RuleStatus describes rule coverage and effective controls.
type RuleStatus struct {
	Rule        string     `json:"rule"`
	Enabled     *bool      `json:"enabled"`
	Ready       bool       `json:"ready"`
	LastSuccess *time.Time `json:"last_success"`
	Reason      string     `json:"reason"`
	MuteUntil   *time.Time `json:"mute_until"`
}

// IncidentStatus is a sanitized active incident summary.
type IncidentStatus struct {
	Rule          string     `json:"rule"`
	Severity      string     `json:"severity"`
	Task          string     `json:"task,omitempty"`
	FirstObserved *time.Time `json:"first_observed"`
	CooldownUntil *time.Time `json:"cooldown_until"`
	Delivery      []string   `json:"delivery"`
	QuietHeld     bool       `json:"quiet_held"`
}

func projectionTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}
func futureTime(t, now time.Time) *time.Time {
	if !t.After(now) {
		return nil
	}
	return projectionTime(t)
}

// Project performs no observation or mutation. allowTask must reject subjects
// outside the caller's repository grants; nil omits every task-specific entry.
func Project(l store.AlertLedger, now time.Time, allowTask func(string) bool) Projection {
	p := Projection{Rules: []RuleStatus{}, Active: []IncidentStatus{}, GlobalMuteUntil: futureTime(l.Controls.OffUntil, now), DeliveryReady: l.Delivery.Ready, DeliveryReason: projectionReason(l.Delivery.Reason), DeliveryLastSuccess: projectionTime(l.Delivery.LastSuccess), DeliveryStates: map[string]int{}}
	if l.Configuration.Configured {
		v := l.Configuration.Enabled
		p.Enabled = &v
	}
	for _, name := range []string{"disk_low", "pool_blocked", "no_progress", "provider_failure", "task_stuck", "delivery_stuck"} {
		o := l.Observers[name]
		r := RuleStatus{Rule: name, Ready: o.Ready, LastSuccess: projectionTime(o.LastSuccess), Reason: projectionReason(o.Reason), MuteUntil: futureTime(l.Controls.RuleMutes[name], now)}
		if l.Configuration.Configured {
			v := l.Configuration.Enabled && l.Configuration.Rules[name]
			r.Enabled = &v
		}
		if o.LastSuccess.IsZero() {
			r.Ready = false
			if r.Reason == "" {
				r.Reason = "not_observed"
			}
		} else if now.Sub(o.LastSuccess) > 5*time.Minute {
			r.Ready = false
			r.Reason = "observation_stale"
		}
		p.Rules = append(p.Rules, r)
	}
	keys := make([]string, 0, len(l.Subjects))
	for k := range l.Subjects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s := l.Subjects[key]
		if s == nil || !s.Active {
			continue
		}
		if s.TaskRef != "" && (allowTask == nil || !allowTask(s.TaskRef)) {
			continue
		}
		if len(p.Active) >= 50 {
			p.HasMore = true
			continue
		}
		entry := IncidentStatus{Rule: s.Rule, Severity: s.Severity, Task: s.TaskRef, FirstObserved: projectionTime(s.FirstObserved), CooldownUntil: futureTime(s.CooldownUntil, now), Delivery: []string{}}
		states := map[string]bool{}
		for _, n := range l.Notifications {
			if n != nil && n.Subject == key && n.Episode == s.Episode {
				state := projectionState(n.State)
				states[state] = true
				if n.State == "held" {
					entry.QuietHeld = true
				}
			}
		}
		for state := range states {
			entry.Delivery = append(entry.Delivery, state)
		}
		sort.Strings(entry.Delivery)
		p.Active = append(p.Active, entry)
	}
	for _, n := range l.Notifications {
		if n == nil {
			continue
		}
		if n.Kind == "digest" {
			hidden := false
			for _, m := range n.Members {
				s := l.Subjects[m.Subject]
				if s != nil && s.TaskRef != "" && (allowTask == nil || !allowTask(s.TaskRef)) {
					hidden = true
					break
				}
			}
			if hidden {
				continue
			}
		}
		s := l.Subjects[n.Subject]
		if s != nil && s.TaskRef != "" && (allowTask == nil || !allowTask(s.TaskRef)) {
			continue
		}
		p.DeliveryStates[projectionState(n.State)]++
	}
	return p
}
func projectionState(s string) string {
	switch s {
	case "grouped":
		return "queued"
	case "confirmed", "uncertain", "queued", "held", "sending", "detected", "suppressed", "failed":
		return s
	default:
		return "unknown"
	}
}
func projectionReason(s string) string {
	switch s {
	case "":
		return ""
	case "discord_destination_unavailable", "Discord alert destination unavailable", "invalid alert destination or identity":
		return "destination_unavailable"
	case "Discord authentication unavailable":
		return "authentication_failed"
	case "Discord alert send permission unavailable":
		return "permission_denied"
	case "configured alert owner unavailable":
		return "owner_configuration_unavailable"
	case "alert text unavailable":
		return "rendering_unavailable"
	case "rate_limited", "send_unconfirmed", "delivery_unavailable":
		return s
	case "disk_guard_disabled", "observation_unavailable", "destination_unavailable", "observation_stale", "not_observed", "store_unavailable", "permission_denied", "authentication_failed", "history_unavailable", "channel_unavailable":
		return s
	default:
		return "unavailable"
	}
}
