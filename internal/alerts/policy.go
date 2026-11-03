// Package alerts evaluates local evidence without executing factory work.
package alerts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// Observation is a source-owned fact. Unknown observations break recovery.
// Recovery must name affirmative evidence newer than the failure.
type Observation struct {
	Rule, Kind, Key, TaskRef, Summary, Action, ID, Source string
	Condition, Severity, RecoveryEvidence                 string
	At, EvidenceAt                                        time.Time
	Approximate                                           bool
}

// Engine owns policy; callers own the clock and bounded observation scheduling.
type Engine struct {
	DB       *store.DB
	Settings config.AlertsSettings
	Location *time.Location
}

// SubjectID forms the stable rule and source identity.
func SubjectID(rule, kind, key string) string { return rule + ":" + kind + ":" + key }
func (e *Engine) location() *time.Location {
	if e.Location != nil {
		return e.Location
	}
	return time.Local
}

// Enabled reports the combined global and rule configuration switch.
func (e *Engine) Enabled(rule string) bool { return e.Settings.Enabled && e.Settings.Rules[rule] }
func keyID(v string) string                { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:12]) }

// Observe commits source evidence and lifecycle together. Repeated event IDs
// never recount failures; successful snapshot observations may share evidence.
func (e *Engine) Observe(ctx context.Context, o Observation) error {
	o.At = o.At.UTC()
	o.EvidenceAt = o.EvidenceAt.UTC()
	if o.At.IsZero() || o.Rule == "" || o.Key == "" {
		return fmt.Errorf("invalid alert observation")
	}
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error { return e.observe(l, o) })
}
func (e *Engine) observe(l *store.AlertLedger, o Observation) error {
	obs := l.Observers[o.Rule]
	obs.LastAttempt = o.At
	obs.Ready = o.Condition != "unknown"
	obs.Reason = ""
	if obs.Ready {
		obs.LastSuccess = o.At
	} else {
		obs.Reason = "observation_unavailable"
	}
	l.Observers[o.Rule] = obs
	id := SubjectID(o.Rule, o.Kind, o.Key)
	s := l.Subjects[id]
	if s == nil {
		s = &store.AlertSubject{Rule: o.Rule, Kind: o.Kind, Key: o.Key, TaskRef: o.TaskRef, Episodes: map[int64]*store.AlertEpisode{}}
		l.Subjects[id] = s
	}
	if !s.LastObserved.IsZero() && o.At.Before(s.LastObserved) {
		return nil
	}
	if o.Rule == "no_progress" && !o.EvidenceAt.IsZero() {
		s.LastProgress = o.EvidenceAt
	}
	previousObservation := s.LastObserved
	s.LastObserved = o.At
	if o.ID != "" && (strings.HasPrefix(o.ID, "disk:") || strings.HasPrefix(o.ID, "pool:") || strings.HasPrefix(o.ID, "progress:") || strings.HasPrefix(o.ID, "report:")) {
		o.ID = "snapshot:" + id + ":" + o.At.UTC().Format(time.RFC3339Nano)
	}
	if o.ID != "" {
		evidenceKey := o.ID
		if strings.HasPrefix(o.ID, "snapshot:") {
			evidenceKey = "snapshot:" + id
		}
		if previous, exists := l.Evidence[evidenceKey]; exists && previous.ID == o.ID && o.Condition == "failing" {
			return nil
		}
		l.Evidence[evidenceKey] = store.AlertEvidence{ID: o.ID, Rule: o.Rule, Subject: id, Source: o.Source, Condition: o.Condition, At: o.At, RecoveryEvidence: o.RecoveryEvidence, Approximate: o.Approximate}
	}
	switch o.Condition {
	case "failing":
		s.CleanCount = 0
		s.RecoveryEvidence = ""
		s.FailureEvidence = o.ID
		l.Evidence["last-failure:"+id] = store.AlertEvidence{At: o.At}
		if !s.Active {
			s.Active = true
			s.Episode++
			for _, pending := range l.Notifications {
				if pending.Subject == id && pending.State != "confirmed" && pending.State != "uncertain" && pending.State != "sending" && pending.Attempt == 0 {
					pending.State = "suppressed"
				}
			}
			s.FirstObserved = o.At
			s.Severity = o.Severity
			s.Episodes[s.Episode] = &store.AlertEpisode{Number: s.Episode, Severity: o.Severity, OpenedAt: o.At}
			s.Summary = o.Summary
			s.Action = o.Action
			kind := "opening"
			if previous := s.Episodes[s.Episode-1]; previous != nil && rank(s.Severity) > rank(previous.Severity) {
				kind = "escalation"
			}
			queue(l, id, s, kind, o.At)
		} else if rank(o.Severity) > rank(s.Severity) {
			s.Severity = o.Severity
			s.Episodes[s.Episode].Severity = o.Severity
			queue(l, id, s, "escalation", o.At)
		}
	case "clean":
		if !o.At.After(previousObservation) {
			return nil
		}
		if !s.Active {
			return nil
		}
		if o.RecoveryEvidence == "" || !o.EvidenceAt.After(l.Evidence["last-failure:"+id].At) {
			s.CleanCount = 0
			return nil
		}
		s.RecoveryEvidence = o.RecoveryEvidence
		s.LastProgress = o.EvidenceAt
		s.CleanCount++
		if s.CleanCount >= 2 {
			s.Active = false
			s.RecoveredAt = o.At
			s.CooldownUntil = o.At.Add(time.Duration(e.Settings.CooldownMinutes) * time.Minute)
			s.Episodes[s.Episode].RecoveredAt = o.At
			queue(l, id, s, "clear", o.At)
		}
	default:
		s.CleanCount = 0
	}
	return nil
}
func rank(s string) int {
	if s == "critical" {
		return 2
	}
	return 1
}
func queue(l *store.AlertLedger, id string, s *store.AlertSubject, kind string, at time.Time) {
	nid := keyID(fmt.Sprintf("%s:%d:%s:%s", id, s.Episode, kind, s.Severity))
	if l.Notifications[nid] != nil {
		return
	}
	l.Notifications[nid] = &store.AlertNotification{ID: nid, Subject: id, Rule: s.Rule, Episode: s.Episode, Kind: kind, Severity: s.Severity, State: "detected", CreatedAt: at, Marker: "[lf:alert:" + nid + "]", Nonce: nid}
}

// Quiet reports warning hold state in host local calendar time.
func (e *Engine) Quiet(at time.Time) bool {
	q := e.Settings.QuietHours
	if !q.Enabled {
		return false
	}
	t := at.In(e.location())
	current := t.Hour()*60 + t.Minute()
	var sh, sm, eh, em int
	_, _ = fmt.Sscanf(q.Start, "%d:%d", &sh, &sm)
	_, _ = fmt.Sscanf(q.End, "%d:%d", &eh, &em)
	start, end := sh*60+sm, eh*60+em
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}
func (e *Engine) muted(l *store.AlertLedger, rule string, at time.Time) bool {
	return !e.Enabled(rule) || at.Before(l.Controls.OffUntil) || at.Before(l.Controls.RuleMutes[rule])
}

// Ready reconciles unattempted transitions against current state. Attempted
// bytes and membership stay immutable, including uncertain notifications.
func (e *Engine) Ready(l *store.AlertLedger, at time.Time, channel string) []*store.AlertNotification {
	// Only unattempted bodies may adopt the current composition. Attempted
	// bytes must remain available for exact legacy receipt reconciliation.
	for _, n := range l.Notifications {
		if n.Attempt == 0 && (n.State == "queued" || n.State == "held") {
			n.Body = withoutAlertMarkers(n.Body)
		}
	}
	e.reconcileDigests(l, at, channel)
	var out []*store.AlertNotification
	var held []*store.AlertNotification
	ids := make([]string, 0, len(l.Notifications))
	for id := range l.Notifications {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := l.Notifications[id]
		if n.State == "grouped" || n.State == "confirmed" || n.State == "suppressed" || n.State == "uncertain" || n.State == "sending" || at.Before(n.RetryAt) {
			continue
		}
		if n.Kind == "digest" {
			allowed := true
			for _, m := range n.Members {
				member := l.Subjects[m.Subject]
				if member == nil || e.muted(l, member.Rule, at) {
					allowed = false
					break
				}
			}
			if allowed && !e.Quiet(at) {
				out = append(out, n)
			}
			continue
		}
		s := l.Subjects[n.Subject]
		if s == nil {
			continue
		}
		ep := s.Episodes[n.Episode]
		if ep == nil {
			continue
		}
		if e.muted(l, n.Rule, at) {
			continue
		}
		if n.Kind == "opening" && ep.OpeningConfirmed {
			n.State = "suppressed"
			continue
		}
		if n.Kind == "clear" {
			if n.Episode != s.Episode {
				n.State = "suppressed"
				continue
			}
			if s.Active || !ep.OpeningConfirmed {
				continue
			}
			if ep.ClearConfirmed {
				n.State = "suppressed"
				continue
			}
		}
		if n.Kind != "clear" && n.Episode != s.Episode && n.State != "held" {
			n.State = "suppressed"
			continue
		}
		if n.Kind != "clear" && !s.Active {
			if n.State != "held" {
				n.State = "suppressed"
				continue
			}
		}
		if n.Kind != "clear" && n.Kind != "escalation" && at.Before(s.CooldownUntil) && s.Active {
			continue
		}
		if n.Severity != "critical" && e.Quiet(at) {
			n.State = "held"
			if n.Body == "" {
				n.Body = render(s, n, false)
			}
			continue
		}
		if n.State == "held" {
			held = append(held, n)
			continue
		}
		if n.Attempt == 0 {
			n.Destination = channel
			n.MentionOwner = n.Severity == "critical" && (n.Kind == "opening" || n.Kind == "escalation") && e.Settings.MentionOwner == "critical"
		}
		if n.Attempt > 0 && ((n.MentionOwner && e.Settings.MentionOwner != "critical") || n.Destination != channel) {
			n.LastError = "configuration_changed_after_attempt"
			l.Delivery.Ready = false
			l.Delivery.LastAttempt = at
			l.Delivery.Reason = "destination_unavailable"
			continue
		}
		if n.Destination == "" {
			n.Destination = channel
		}
		if n.Destination == "" {
			continue
		}
		if n.Body == "" {
			n.Body = render(s, n, !s.Active)
			n.MentionOwner = n.Severity == "critical" && n.Kind != "clear" && e.Settings.MentionOwner == "critical"
		}
		n.State = "queued"
		out = append(out, n)
	}
	e.digest(l, held, at, channel, &out)
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i].Severity) != rank(out[j].Severity) {
			return rank(out[i].Severity) > rank(out[j].Severity)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
func render(s *store.AlertSubject, n *store.AlertNotification, resolved bool) string {
	heading := strings.ToUpper(n.Severity) + " factory alert"
	switch {
	case n.Kind == "clear":
		heading = "All clear"
	case resolved:
		heading = "Quiet-hours digest — already resolved"
	case n.State == "held":
		heading = "Quiet-hours digest — active"
	}
	subject := s.Kind
	if s.TaskRef != "" {
		subject = s.TaskRef
	}
	if subject == "" {
		subject = "factory"
	}
	summary := s.Summary
	if summary == "" {
		summary = "Factory work needs attention."
	}
	action := s.Action
	if action == "" {
		action = "Check factory overview and doctor for current evidence."
	}
	if n.Kind == "clear" {
		summary = "Two observations confirmed recovery."
	}
	return safeText(heading+"\n"+summary+"\nAffected: "+subject+"\n"+action, 1600)
}

// SafeText neutralizes every mention, including an injected configured owner.
func SafeText(s string) string { return safeText(s, 1600) }

// NeutralizeMentions makes mention neutralization idempotent across boundaries.
func NeutralizeMentions(s string) string {
	s = strings.ReplaceAll(s, "@", "@\u200b")
	for strings.Contains(s, "@\u200b\u200b") {
		s = strings.ReplaceAll(s, "@\u200b\u200b", "@\u200b")
	}
	return s
}
func textUnits(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func safeText(s string, limit int) string {
	s = NeutralizeMentions(s)
	if textUnits(s) <= limit {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if n+size > limit-1 {
			break
		}
		b.WriteRune(r)
		n += size
	}
	return b.String() + "…"
}

// withoutAlertMarkers removes legacy engine markers before new transport
// composition, including member markers embedded in queued digests.
func withoutAlertMarkers(body string) string {
	for {
		start := strings.Index(body, "[lf:alert:")
		if start < 0 {
			return strings.TrimSpace(body)
		}
		end := strings.IndexByte(body[start:], ']')
		if end < 0 {
			return strings.TrimSpace(body)
		}
		body = body[:start] + body[start+end+1:]
	}
}
