package alerts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

var aliases = map[string]string{"main": "main_red", "main_red": "main_red", "ci_monitoring_unavailable": "ci_monitoring_unavailable", "disk": "disk_low", "pool": "pool_blocked", "progress": "no_progress", "provider": "provider_failure", "delivery": "delivery_stuck", "disk_low": "disk_low", "pool_blocked": "pool_blocked", "no_progress": "no_progress", "provider_failure": "provider_failure", "task_stuck": "task_stuck", "delivery_stuck": "delivery_stuck"}

const usage = "Use alerts; mute disk 2h (or 30m, up to 7d); unmute disk; alerts off until 07:00; alerts off until tomorrow; alerts on."

// Reserved prevents malformed controls from falling through to a model.
func Reserved(text string) bool {
	f := strings.Fields(strings.ToLower(text))
	return len(f) > 0 && (f[0] == "alerts" || f[0] == "mute" || f[0] == "unmute")
}

// Command changes only notification controls. The caller authenticates owner
// and allowed channel before invoking it. Source identity makes replay inert.
func (e *Engine) Command(ctx context.Context, source, text string, at time.Time) (string, error) {
	at = at.UTC()
	if source == "" {
		return "", fmt.Errorf("missing command identity")
	}
	var reply string
	var transport store.CITransportState
	var transportErr error
	if strings.EqualFold(strings.TrimSpace(text), "alerts") {
		snapshot, readErr := e.DB.ReadAlerts(ctx)
		if readErr != nil {
			return "", readErr
		}
		if snapshot.CI.Origin != "" {
			transport, transportErr = e.DB.CITransport(ctx, "ci:"+snapshot.CI.Origin)
		}
	}
	err := e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		if r, ok := l.Receipts[source]; ok {
			reply = r.Reply
			return nil
		}
		fields := strings.Fields(strings.ToLower(text))
		expiry := time.Time{}
		reply = usage
		switch {
		case len(fields) == 1 && fields[0] == "alerts":
			reply = e.status(l, at, transport, transportErr)
		case len(fields) == 2 && fields[0] == "alerts" && fields[1] == "on":
			l.Controls.OffUntil = time.Time{}
			reply = "Global alert pause cleared. Rule mutes and configuration still apply."
		case len(fields) == 4 && fields[0] == "alerts" && fields[1] == "off" && fields[2] == "until":
			var err error
			expiry, err = e.until(fields[3], at)
			if err != nil {
				reply = err.Error() + " " + usage
				break
			}
			l.Controls.OffUntil = expiry
			reply = "Alerts paused until " + formatExpiry(expiry, e.location()) + ". Observation continues."
		case len(fields) == 2 && fields[0] == "unmute":
			if rule := aliases[fields[1]]; rule != "" {
				delete(l.Controls.RuleMutes, rule)
				reply = rule + " unmuted. Global pause and configuration still apply."
			}
		case (len(fields) == 3 || len(fields) == 4) && fields[0] == "mute":
			rule := aliases[fields[1]]
			duration := fields[len(fields)-1]
			if rule == "" || (len(fields) == 4 && fields[2] != "for") {
				break
			}
			d, err := parseDuration(duration)
			if err != nil {
				reply = err.Error() + " " + usage
				break
			}
			expiry = at.Add(d)
			l.Controls.RuleMutes[rule] = expiry
			reply = rule + " muted until " + formatExpiry(expiry, e.location()) + ". Observation continues."
		}
		l.Receipts[source] = store.AlertCommandReceipt{Source: source, Command: text, Reply: reply, At: at, Expiry: expiry}
		return nil
	})
	return reply, err
}
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration must be a positive whole number with m, h or d, at most seven days")
	}
	for _, r := range s[:len(s)-1] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("duration must contain only digits followed by m, h or d")
		}
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	unit := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[s[len(s)-1]]
	if err != nil || n <= 0 || unit == 0 || n > int((7*24*time.Hour)/unit) {
		return 0, fmt.Errorf("duration must be a positive whole number with m, h or d, at most seven days")
	}
	return time.Duration(n) * unit, nil
}
func formatExpiry(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04:05 -07:00 MST") + " (" + loc.String() + ")"
}
func (e *Engine) until(s string, at time.Time) (time.Time, error) {
	loc := e.location()
	local := at.In(loc)
	h, m := 7, 0
	day := 0
	if s == "tomorrow" {
		day = 1
		if e.Settings.QuietHours.Enabled {
			_, _ = fmt.Sscanf(e.Settings.QuietHours.End, "%d:%d", &h, &m)
		}
	} else {
		if len(s) != 5 || s[2] != ':' {
			return time.Time{}, fmt.Errorf("use a 24-hour clock time HH:MM or tomorrow")
		}
		var err error
		for _, i := range []int{0, 1, 3, 4} {
			if s[i] < '0' || s[i] > '9' {
				return time.Time{}, fmt.Errorf("use a 24-hour clock time HH:MM or tomorrow")
			}
		}
		h, err = strconv.Atoi(s[:2])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid clock time")
		}
		m, err = strconv.Atoi(s[3:])
		if err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return time.Time{}, fmt.Errorf("invalid clock time")
		}
	}
	// Search actual instants in a local calendar day. This chooses the earliest
	// future repeated time, and the first valid minute after a DST gap.
	for ; day < 3; day++ {
		date := local.AddDate(0, 0, day)
		start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		exists := false
		for t := start; t.Before(start.Add(27 * time.Hour)); t = t.Add(time.Minute) {
			v := t.In(loc)
			if v.Year() == date.Year() && v.YearDay() == date.YearDay() && v.Hour() == h && v.Minute() == m {
				exists = true
				break
			}
		}
		for t := start; t.Before(start.Add(27 * time.Hour)); t = t.Add(time.Minute) {
			v := t.In(loc)
			if v.Year() != date.Year() || v.YearDay() != date.YearDay() {
				continue
			}
			if !exists && v.Hour()*60+v.Minute() > h*60+m {
				if t.After(at) {
					return t, nil
				}
				break
			}
			if v.Hour() == h && v.Minute() == m && t.After(at) {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no future clock occurrence available")
}
func (e *Engine) status(l *store.AlertLedger, at time.Time, transport store.CITransportState, transportErr error) string {
	snapshot := *l
	snapshot.Configuration = store.AlertConfiguration{Configured: true, Enabled: e.Settings.Enabled, Rules: e.Settings.Rules}
	p := Project(snapshot, at, func(string) bool { return true })
	applyCITransport(&p, transport, transportErr, at)
	var b strings.Builder
	fmt.Fprintf(&b, "Alerts enabled: %t. Delivery ready: %t (%s).\n", e.Settings.Enabled, p.DeliveryReady, p.DeliveryReason)
	if p.CI != nil {
		c := p.CI
		fmt.Fprintf(&b, "CI %s %s %s: conclusion=%s pending=%t monitoring ready=%t (%s).\n", c.Repository, c.Branch, c.Workflow, c.Conclusion, c.Pending, c.Ready, c.Reason)
		if c.NextRetry != nil {
			fmt.Fprintf(&b, "CI next retry: %s.\n", formatExpiry(*c.NextRetry, e.location()))
		}
	}
	if p.GlobalMuteUntil != nil {
		fmt.Fprintf(&b, "Global pause until %s.\n", formatExpiry(*p.GlobalMuteUntil, e.location()))
	}
	for _, r := range p.Rules {
		fmt.Fprintf(&b, "%s: enabled=%t ready=%t", r.Rule, e.Enabled(r.Rule), r.Ready)
		if r.LastSuccess != nil {
			fmt.Fprintf(&b, " last observation=%s", formatExpiry(*r.LastSuccess, e.location()))
		}
		if r.Reason != "" {
			fmt.Fprintf(&b, " reason=%s", r.Reason)
		}
		if r.MuteUntil != nil {
			fmt.Fprintf(&b, " muted until %s", formatExpiry(*r.MuteUntil, e.location()))
		}
		b.WriteByte('\n')
	}
	for _, s := range p.Active {
		fmt.Fprintf(&b, "Active: %s %s %s delivery=%s quiet-held=%t", s.Rule, s.Severity, s.Task, strings.Join(s.Delivery, ","), s.QuietHeld)
		if s.CooldownUntil != nil {
			fmt.Fprintf(&b, " cooldown until %s", formatExpiry(*s.CooldownUntil, e.location()))
		}
		b.WriteByte('\n')
	}
	if p.HasMore {
		b.WriteString("More active incidents omitted.\n")
	}
	fmt.Fprintf(&b, "Delivery totals: detected=%d queued=%d held=%d confirmed=%d uncertain=%d.\n", p.DeliveryStates["detected"], p.DeliveryStates["queued"], p.DeliveryStates["held"], p.DeliveryStates["confirmed"], p.DeliveryStates["uncertain"])
	return safeText(b.String(), 16000)
}
