package doctor

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

// RenderAlerts displays only the same safe local status used by overview.
func RenderAlerts(p *alerts.Projection) string {
	if p == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Alerts: active=%d; delivery ready=%t", len(p.Active), p.DeliveryReady)
	if p.DeliveryReason != "" {
		fmt.Fprintf(&out, " (%s)", p.DeliveryReason)
	}
	out.WriteByte('\n')
	if p.CI != nil {
		c := p.CI
		fmt.Fprintf(&out, "CI %s %s %s: conclusion=%s pending=%t monitoring ready=%t (%s).\n", c.Repository, c.Branch, c.Workflow, c.Conclusion, c.Pending, c.Ready, c.Reason)
		if c.NextRetry != nil {
			fmt.Fprintf(&out, "CI next retry: %s.\n", c.NextRetry.Format(time.RFC3339))
		}
	}
	if p.GlobalMuteUntil != nil {
		fmt.Fprintf(&out, "Alerts paused until %s.\n", p.GlobalMuteUntil.Format("2006-01-02T15:04:05Z07:00"))
	}
	for _, rule := range p.Rules {
		fmt.Fprintf(&out, "%s: ready=%t", rule.Rule, rule.Ready)
		if rule.Enabled != nil {
			fmt.Fprintf(&out, " enabled=%t", *rule.Enabled)
		}
		if rule.LastSuccess != nil {
			fmt.Fprintf(&out, " last observation=%s", rule.LastSuccess.Format("2006-01-02T15:04:05Z07:00"))
		}
		if rule.Reason != "" {
			fmt.Fprintf(&out, " reason=%s", rule.Reason)
		}
		if rule.MuteUntil != nil {
			fmt.Fprintf(&out, " muted until=%s", rule.MuteUntil.Format("2006-01-02T15:04:05Z07:00"))
		}
		out.WriteByte('\n')
	}
	for _, incident := range p.Active {
		fmt.Fprintf(&out, "Active alert: %s severity=%s delivery=%s\n", incident.Rule, incident.Severity, strings.Join(incident.Delivery, ","))
	}
	if p.DeliveryStates["uncertain"] > 0 {
		fmt.Fprintf(&out, "Alert delivery uncertain: %d.\n", p.DeliveryStates["uncertain"])
	}
	return out.String()
}
