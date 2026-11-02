package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
)

func TestRenderAlertsKeepsUnavailableAndUncertainVisible(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	enabled := true
	p := alerts.Projection{Enabled: &enabled, DeliveryReason: "destination_unavailable", GlobalMuteUntil: &at, Rules: []alerts.RuleStatus{{Rule: "disk_low", Reason: "observation_stale", LastSuccess: &at, MuteUntil: &at, Enabled: &enabled}}, Active: []alerts.IncidentStatus{{Rule: "disk_low", Severity: "critical", Delivery: []string{"uncertain"}}}, DeliveryStates: map[string]int{"uncertain": 1}}
	text := RenderAlerts(&p)
	for _, fact := range []string{"delivery ready=false", "destination_unavailable", "observation_stale", "2026-11-02T12:00:00Z", "delivery=uncertain", "Alert delivery uncertain: 1"} {
		if !strings.Contains(text, fact) {
			t.Fatalf("missing status fact %q: %s", fact, text)
		}
	}
}
