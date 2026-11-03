package alerts

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func (e *Engine) digest(l *store.AlertLedger, held []*store.AlertNotification, at time.Time, channel string, out *[]*store.AlertNotification) {
	if channel == "" || len(held) == 0 {
		return
	}
	body := "Quiet-hours factory alert digest\n"
	ids := []string{}
	members := []store.AlertNotificationMember{}
	flush := func() {
		if len(ids) == 0 {
			return
		}
		id := keyID("digest:" + strings.Join(ids, ":"))
		if existing := l.Notifications[id]; existing == nil || (existing.Attempt == 0 && existing.State == "suppressed") {
			n := &store.AlertNotification{ID: id, Kind: "digest", Severity: "warning", State: "queued", Body: body, Destination: channel, Members: members, CreatedAt: at, Marker: "[lf:alert:" + id + "]", Nonce: id}
			l.Notifications[id] = n
			*out = append(*out, n)
		}
		body = "Quiet-hours factory alert digest\n"
		ids = nil
		members = nil
	}
	for _, n := range held {
		s := l.Subjects[n.Subject]
		ep := s.Episodes[n.Episode]
		resolved := !ep.RecoveredAt.IsZero()
		state := "active"
		if resolved {
			state = "already resolved"
		}
		entry := withoutAlertMarkers(n.Body)
		if _, rest, ok := strings.Cut(entry, "\n"); ok {
			entry = rest
		}
		entry = safeText(entry, 600)
		entry = fmt.Sprintf("%s: %s\n", state, entry)
		if textUnits(body+entry) > 1500 {
			flush()
		}
		body += entry
		ids = append(ids, fmt.Sprintf("%s:%t", n.ID, resolved))
		members = append(members, store.AlertNotificationMember{Subject: n.Subject, Episode: n.Episode, Kind: n.Kind, Resolved: resolved})
		n.State = "grouped"
	}
	flush()
}

// Rebuild only unattempted parts when controls or current recovery changed.
// Attempted parts retain their exact bytes and fixed membership forever.
func (e *Engine) reconcileDigests(l *store.AlertLedger, at time.Time, channel string) {
	for _, n := range l.Notifications {
		if n.Kind != "digest" || n.Attempt > 0 || n.State == "confirmed" || n.State == "suppressed" {
			continue
		}
		rebuild := n.Destination != channel
		for _, m := range n.Members {
			s := l.Subjects[m.Subject]
			if s == nil {
				rebuild = true
				break
			}
			ep := s.Episodes[m.Episode]
			if ep == nil || m.Episode != s.Episode || e.muted(l, s.Rule, at) || m.Resolved != !ep.RecoveredAt.IsZero() {
				rebuild = true
				break
			}
		}
		if !rebuild {
			continue
		}
		n.State = "suppressed"
		for _, m := range n.Members {
			for _, original := range l.Notifications {
				if original.Kind != "digest" && original.Subject == m.Subject && original.Episode == m.Episode && original.Kind == m.Kind && original.State == "grouped" {
					original.State = "held"
				}
			}
		}
	}
}
