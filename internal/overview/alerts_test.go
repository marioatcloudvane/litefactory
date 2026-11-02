package overview

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type alertOverviewReader struct {
	*fakeReader
	ledger store.AlertLedger
}

func (r *alertOverviewReader) ReadAlerts(context.Context) (store.AlertLedger, error) {
	return r.ledger, nil
}
func TestOverviewAlertsRestrictedRegistryAndReadiness(t *testing.T) {
	t.Parallel()
	now := testNow()
	reader := &alertOverviewReader{fakeReader: &fakeReader{repos: []store.OverviewRepository{{ID: 1, Name: "allowed/repo", ObservedAt: now}}}, ledger: store.AlertLedger{
		Configuration: store.AlertConfiguration{Configured: true, Enabled: true, Rules: map[string]bool{"disk_low": true}},
		Observers:     map[string]store.AlertObserver{"disk_low": {Ready: true, LastSuccess: now.Add(-6 * time.Minute)}},
		Subjects: map[string]*store.AlertSubject{
			"visible": {Rule: "no_progress", TaskRef: "github:allowed/repo#1", Active: true, Severity: "critical"},
			"hidden":  {Rule: "no_progress", TaskRef: "github:secret/repo#2", Active: true, Severity: "critical", Key: "private-key"},
		},
		Delivery: store.AlertObserver{Ready: false, Reason: "destination_unavailable"},
	}}
	service := New(reader, redact.New(redact.Options{}), func() time.Time { return now })
	result, err := service.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Alerts.Active) != 1 || result.Alerts.Active[0].Task != "github:allowed/repo#1" || result.Alerts.DeliveryReady || result.Alerts.DeliveryReason != "destination_unavailable" || result.Coverage.Sources["alerts"].Status != "unavailable" {
		t.Fatalf("alerts=%+v coverage=%+v", result.Alerts, result.Coverage.Sources["alerts"])
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret/repo") || strings.Contains(string(raw), "private-key") {
		t.Fatalf("restricted alert leaked: %s", raw)
	}
	if !reader.ledger.Subjects["visible"].Active {
		t.Fatal("read repaired incident")
	}
}
