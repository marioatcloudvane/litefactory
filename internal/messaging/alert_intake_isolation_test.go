package messaging_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestAlertControlScanFailurePreservesOrdinaryConversationIntake(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateAlertControlScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	// A large incomplete control interval must remain durable on capacity failure.
	// Its unavailable state must not prevent an ordinary bound-channel message.
	for offset := 0; offset < 4096; offset += 400 {
		scan, _, err := db.ReadAlertControlScan(ctx, "2")
		if err != nil {
			t.Fatal(err)
		}
		next := scan
		next.HighWater = "99999"
		next.Before = "201"
		var messages []store.NaturalMessage
		for index := 0; index < min(400, 4096-offset); index++ {
			messages = append(messages, store.NaturalMessage{Transport: "discord", ChannelRef: "2", OwnerID: "3", SourceID: fmt.Sprint(1000 + offset + index), Text: "alerts on", CreatedAt: at})
		}
		if err = db.StageAlertControlScan(ctx, "2", scan, next, messages, false); err != nil {
			t.Fatal(err)
		}
	}
	page := messaging.IntakePage{HighID: "102", LowID: "101", Count: 2, Messages: []store.NaturalMessage{
		{ChannelRef: "2", OwnerID: "3", SourceID: "102", Text: "mute disk 2h", CreatedAt: at},
		{ChannelRef: "2", OwnerID: "3", SourceID: "101", Text: "ordinary conversation input", CreatedAt: at},
	}}
	reader := &intakeReader{pages: map[string]messaging.IntakePage{"": page, "201": page}}
	var logs bytes.Buffer
	intake := alertControlIntake(t, db, reader)
	intake.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	intake.OrdinaryChannels = []string{"2"}
	err := intake.Poll(ctx, at)
	if err == nil || !strings.Contains(err.Error(), "alert control scan capacity unavailable") {
		t.Fatalf("missing independent alert failure: %v", err)
	}
	for _, want := range []string{"stage=control_scan reason=persistence_failed", "stage=ordinary_scan reason=released"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing %s: %s", want, logs.String())
		}
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 1 || pending[0].SourceID != "101" || pending[0].Text != "ordinary conversation input" {
		t.Fatalf("alert failure blocked ordinary intake or leaked reserved control: %+v %v", pending, err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 0 {
		t.Fatalf("incomplete reserved control executed: %+v %v", ledger.Receipts, err)
	}
	scan, _, err := db.ReadAlertControlScan(ctx, "2")
	if err != nil || len(scan.Messages) != 4096 || scan.Complete || scan.Cursor != "100" {
		t.Fatalf("capacity failure changed durable interval: count=%d complete=%t cursor=%s error=%v", len(scan.Messages), scan.Complete, scan.Cursor, err)
	}
}
