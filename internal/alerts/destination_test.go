package alerts

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestAttemptedAlertDestinationCannotBeRevokedAndRetried(t *testing.T) {
	for _, kind := range []string{"opening", "digest"} {
		t.Run(kind, func(t *testing.T) {
			e := policyEngine(t)
			at := policyAt
			rule, severity := "disk_low", "critical"
			if kind == "digest" {
				at = time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
				rule, severity = "task_stuck", "warning"
			}
			policyObserve(t, e, rule, "failing", severity, "failure", at)
			if kind == "digest" {
				policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error { e.Ready(l, at, "3"); return nil }))
				at = at.Add(8 * time.Hour)
			}
			var original Message
			transport := &policyScriptTransport{send: func(_ context.Context, m Message) Delivery { original = m; return Delivery{RetryAfter: time.Minute} }}
			more, err := e.SendStep(t.Context(), at, "3", "2", transport)
			policyMust(t, err)
			if !more || original.Kind != kind || original.Channel != "3" {
				t.Fatalf("original attempt %+v more=%v", original, more)
			}
			more, err = e.SendStep(t.Context(), at.Add(2*time.Minute), "4", "2", transport)
			policyMust(t, err)
			if more || transport.sends != 1 || transport.reconciles != 0 {
				t.Fatalf("revoked destination retried: more=%v sends=%d reads=%d", more, transport.sends, transport.reconciles)
			}
			ledger := policyLedger(t, e)
			notification := ledger.Notifications[original.ID]
			if notification.State != "queued" || notification.RemoteID != "" || notification.Attempt != 1 || notification.Destination != "3" {
				t.Fatalf("attempted identity changed: %+v", notification)
			}
			if policySubject(t, e, rule, "task", "one").Episodes[1].OpeningConfirmed {
				t.Fatal("revoked destination established opening confirmation")
			}
		})
	}
}

func TestUncertainAlertReconcilesOriginalDestinationAfterChange(t *testing.T) {
	e := policyEngine(t)
	at := policyAt
	policyObserve(t, e, "disk_low", "failing", "critical", "failure", at)
	var original Message
	transport := &policyScriptTransport{send: func(_ context.Context, m Message) Delivery { original = m; return Delivery{} }, reconcile: func(_ context.Context, m Message, _ string) (string, error) {
		if m.Channel != "3" || m.ID != original.ID || m.Body != original.Body {
			t.Errorf("uncertain receipt identity changed: %+v", m)
		}
		return "remote-original", nil
	}}
	_, err := e.SendStep(t.Context(), at, "3", "2", transport)
	policyMust(t, err)
	more, err := e.SendStep(t.Context(), at.Add(6*time.Minute), "4", "2", transport)
	policyMust(t, err)
	if !more || transport.sends != 1 || transport.reconciles != 1 {
		t.Fatalf("uncertainty replayed instead of reading receipt: more=%v sends=%d reads=%d", more, transport.sends, transport.reconciles)
	}
	notification := policyLedger(t, e).Notifications[original.ID]
	if notification.State != "confirmed" || notification.RemoteID != "remote-original" || notification.Destination != "3" || notification.Attempt != 1 {
		t.Fatalf("original receipt not retained: %+v", notification)
	}
}

func TestUnattemptedDigestRebuildsAtCurrentDestination(t *testing.T) {
	e := policyEngine(t)
	at := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	policyObserve(t, e, "task_stuck", "failing", "warning", "failure", at)
	var before *store.AlertNotification
	policyMust(t, e.DB.UpdateAlerts(t.Context(), func(l *store.AlertLedger) error {
		e.Ready(l, at, "3")
		due := e.Ready(l, at.Add(8*time.Hour), "3")
		if len(due) != 1 || due[0].Kind != "digest" {
			t.Fatalf("expected one quiet digest: %+v", due)
		}
		snapshot := *due[0]
		snapshot.Members = append([]store.AlertNotificationMember(nil), due[0].Members...)
		before = &snapshot
		return nil
	}))
	transport := &policyTransport{}
	more, err := e.SendStep(t.Context(), at.Add(8*time.Hour+time.Minute), "4", "2", transport)
	policyMust(t, err)
	if !more || len(transport.sent) != 1 {
		t.Fatalf("rebuilt digest not sent: more=%v sent=%d", more, len(transport.sent))
	}
	sent := transport.sent[0]
	if sent.Channel != "4" || sent.Kind != "digest" || sent.Body != before.Body {
		t.Fatalf("digest body or routing changed: %+v", sent)
	}
	ledger := policyLedger(t, e)
	after := ledger.Notifications[sent.ID]
	if !reflect.DeepEqual(before.Members, after.Members) || after.RemoteID == "" || after.State != "confirmed" {
		t.Fatalf("digest membership or confirmation changed: before=%+v after=%+v", before, after)
	}
	for _, n := range ledger.Notifications {
		if n.Kind == "digest" && n.Destination == "3" && n.Attempt != 0 {
			t.Fatal("old unattempted destination was sent")
		}
	}
}
