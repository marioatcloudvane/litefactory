package alerts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestProjectionMixedGrantDigestDoesNotExposeHiddenDelivery(t *testing.T) {
	l := store.AlertLedger{Subjects: map[string]*store.AlertSubject{
		"allowed": {Rule: "task_stuck", TaskRef: "github:allowed/repo#1", Active: true, Episode: 1},
		"private": {Rule: "task_stuck", TaskRef: "github:private/repo#9", Active: true, Episode: 1},
	}, Notifications: map[string]*store.AlertNotification{
		"allowed-member": {Subject: "allowed", Episode: 1, State: "grouped"},
		"private-member": {Subject: "private", Episode: 1, State: "grouped"},
		"mixed-digest":   {Kind: "digest", State: "uncertain", Body: "github:private/repo#9 secret", Members: []store.AlertNotificationMember{{Subject: "allowed", Episode: 1}, {Subject: "private", Episode: 1}}},
	}}
	p := Project(l, policyAt, func(task string) bool { return task == "github:allowed/repo#1" })
	if len(p.Active) != 1 || p.Active[0].Task != "github:allowed/repo#1" || p.DeliveryStates["uncertain"] != 0 || p.DeliveryStates["queued"] != 1 {
		t.Fatalf("mixed grant projection exposed private counts or identities: %+v", p)
	}
	raw, err := json.Marshal(p)
	policyMust(t, err)
	for _, hidden := range []string{"private", "mixed-digest", "secret"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatalf("projection leaked %q", hidden)
		}
	}
	if l.Notifications["mixed-digest"].State != "uncertain" {
		t.Fatal("projection mutated delivery journal")
	}
}
