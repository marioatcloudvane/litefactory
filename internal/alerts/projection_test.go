package alerts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestProjectionFiltersSubjectsAndKeepsUnavailableVisible(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	l := store.AlertLedger{Configuration: store.AlertConfiguration{Configured: true, Enabled: true, Rules: config.DefaultAlerts().Rules}, Subjects: map[string]*store.AlertSubject{
		"hidden-account":    {Rule: "provider_failure", TaskRef: "github:private/repo#1", Active: true, Severity: "critical", Key: "credential-secret"},
		"visible":           {Rule: "no_progress", TaskRef: "github:allowed/repo#2", Active: true, Severity: "critical", Episode: 1, FirstObserved: at},
		"disk-private-path": {Rule: "disk_low", Active: true, Severity: "critical", Key: "/Users/private/path", Summary: "secret"},
	}, Observers: map[string]store.AlertObserver{"disk_low": {Ready: true, LastSuccess: at.Add(-6 * time.Minute)}}, Notifications: map[string]*store.AlertNotification{
		"hidden":  {Subject: "hidden-account", State: "uncertain", Body: "provider raw secret"},
		"visible": {Subject: "visible", Episode: 1, State: "uncertain"},
	}, Controls: store.AlertControls{RuleMutes: map[string]time.Time{"disk_low": at.Add(time.Hour)}}}
	p := Project(l, at, func(task string) bool { return task == "github:allowed/repo#2" })
	if len(p.Active) != 2 || p.DeliveryStates["uncertain"] != 1 || p.Rules[0].Ready || p.Rules[0].Reason != "observation_stale" || p.Rules[0].MuteUntil == nil || p.Enabled == nil || !*p.Enabled {
		t.Fatalf("projection=%+v", p)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private/repo", "credential-secret", "/Users/private/path", "raw secret", "disk-private-path", "hidden-account"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("projection leaked %q", secret)
		}
	}
	if !l.Subjects["visible"].Active || l.Notifications["visible"].State != "uncertain" {
		t.Fatal("read changed state")
	}
}
