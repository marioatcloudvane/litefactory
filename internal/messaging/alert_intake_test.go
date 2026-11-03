package messaging_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func alertControlIntake(t *testing.T, db *store.DB, reader messaging.IntakeReader) *messaging.Intake {
	t.Helper()
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	service := messaging.New(db, "discord", "3", []string{"2"}, &sender{}, nil)
	service.SetAlertControls(engine.Command)
	return &messaging.Intake{DB: db, Reader: reader, Owner: "3", Transport: "discord", Channels: []string{"2"}, OrdinaryChannels: []string{}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }, Controls: service.HandleAlertControl}
}
func TestAlertControlIntakeBypassesFullConversationBacklog(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < 1000; offset += 400 {
		scan, err := db.ReadNaturalScan(ctx, "2")
		if err != nil {
			t.Fatal(err)
		}
		count := min(400, 1000-offset)
		var messages []store.NaturalMessage
		for index := 0; index < count; index++ {
			messages = append(messages, store.NaturalMessage{ChannelRef: "2", SourceID: fmt.Sprint(101 + offset + index), OwnerID: "3", Text: "ordinary retained work", CreatedAt: at})
		}
		next := store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: messages[len(messages)-1].SourceID}
		if err = db.StageNaturalScan(ctx, scan, next, messages, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ActivateAlertControlScan(ctx, "2", "1100"); err != nil {
		t.Fatal(err)
	}
	reader := &intakeReader{pages: map[string]messaging.IntakePage{"": {HighID: "1101", LowID: "1101", Count: 1, Messages: []store.NaturalMessage{{ChannelRef: "2", SourceID: "1101", OwnerID: "3", Text: "mute disk 2h", CreatedAt: at}}}}}
	intake := alertControlIntake(t, db, reader)
	intake.OrdinaryChannels = nil
	// Ordinary backpressure remains visible after the independent control ran.
	if err := intake.Poll(ctx, at); err == nil {
		t.Fatal("ordinary backpressure disappeared")
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || !ledger.Controls.RuleMutes["disk_low"].Equal(at.Add(2*time.Hour)) || len(ledger.Receipts) != 1 {
		t.Fatalf("backlog blocked controls: %+v %v", ledger, err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 100 || pending[0].Text != "ordinary retained work" {
		t.Fatalf("control consumed conversation work: %+v %v", pending, err)
	}
	// Replay after the ordinary scan fails must retain the first expiry.
	if err = intake.Poll(ctx, at.Add(time.Hour)); err == nil {
		t.Fatal("backpressure disappeared on replay")
	}
	ledger, err = db.ReadAlerts(ctx)
	if err != nil || !ledger.Controls.RuleMutes["disk_low"].Equal(at.Add(2*time.Hour)) || len(ledger.Receipts) != 1 {
		t.Fatalf("backlog replay changed control: %+v %v", ledger, err)
	}
}
func TestAlertControlIntakeUnboundChannelsAndRestartedCompleteInterval(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.ActivateAlertControlScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	reader := &intakeReader{pages: map[string]messaging.IntakePage{}}
	for index := 0; index < 5; index++ {
		high := 600 - index*100
		before := ""
		if index > 0 {
			before = fmt.Sprint(high + 1)
		}
		text := "alerts on"
		if index == 4 {
			text = "alerts off until 07:00"
		}
		reader.pages[before] = messaging.IntakePage{HighID: fmt.Sprint(high), LowID: fmt.Sprint(high - 99), Count: 100, Messages: []store.NaturalMessage{
			{ChannelRef: "2", SourceID: fmt.Sprint(high), OwnerID: "3", Text: text, CreatedAt: at},
			{ChannelRef: "2", SourceID: fmt.Sprint(high - 1), OwnerID: "3", Text: "ordinary unbound text", CreatedAt: at},
		}}
	}
	var logs bytes.Buffer
	intake := alertControlIntake(t, db, reader)
	intake.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 0 {
		t.Fatalf("incomplete control interval executed: %+v %v", ledger.Receipts, err)
	}
	scan, ok, err := db.ReadAlertControlScan(ctx, "2")
	if err != nil || !ok || scan.Before != "201" || scan.Complete || len(scan.Messages) != 4 {
		t.Fatalf("control scan not durable: %+v %t %v", scan, ok, err)
	}
	if !strings.Contains(logs.String(), "reason=partial_staged") || strings.Contains(logs.String(), "reason=released") {
		t.Fatal(logs.String())
	}
	restarted := alertControlIntake(t, db, reader)
	restarted.Logger = intake.Logger
	if err = restarted.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "reason=released") || strings.Count(logs.String(), "reason=completed") != 5 {
		t.Fatal(logs.String())
	}
	ledger, err = db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 5 || !ledger.Controls.OffUntil.IsZero() {
		t.Fatalf("restarted command order wrong: %+v %v", ledger, err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("unbound texts accumulated: %+v %v", pending, err)
	}
	if _, err = db.ReadNaturalScan(ctx, "2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unbound ordinary scan activated: %v", err)
	}
}

func TestControlDiagnosticsAfterEffectReplyFailure(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateAlertControlScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	port := &diagnosticFailStore{Port: db, failReply: true}
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	service := messaging.NewWithServices(port, factory.NewMessaging(db), "discord", "3", []string{"2"}, &sender{}, nil)
	service.SetAlertControls(engine.Command)
	reader := &intakeReader{pages: map[string]messaging.IntakePage{"": {HighID: "101", LowID: "101", Count: 1, Messages: []store.NaturalMessage{{ChannelRef: "2", OwnerID: "3", SourceID: "101", Text: "mute disk 2h", CreatedAt: at}}}}}
	intake := alertControlIntake(t, db, reader)
	intake.Controls = service.HandleAlertControl
	var logs bytes.Buffer
	intake.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := intake.Poll(ctx, at); err == nil {
		t.Fatal("queue failure disappeared")
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 1 || !ledger.Controls.RuleMutes["disk_low"].Equal(at.Add(2*time.Hour)) {
		t.Fatalf("effect %+v %v", ledger, err)
	}
	scan, _, err := db.ReadAlertControlScan(ctx, "2")
	if err != nil || !scan.Complete || scan.Cursor != "101" || len(scan.Messages) != 1 {
		t.Fatalf("retry state %+v %v", scan, err)
	}
	if !strings.Contains(logs.String(), "reason=execution_or_reply_failed") || strings.Contains(logs.String(), "reason=completed") || strings.Contains(logs.String(), "private") {
		t.Fatal(logs.String())
	}
	port.failReply = false
	for range 2 {
		if err = intake.Poll(ctx, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err = db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 1 || !ledger.Controls.RuleMutes["disk_low"].Equal(at.Add(2*time.Hour)) {
		t.Fatalf("repeated effect %+v %v", ledger, err)
	}
	scan, _, err = db.ReadAlertControlScan(ctx, "2")
	if err != nil || scan.Cursor != "101" || len(scan.Messages) != 0 {
		t.Fatalf("unfinished retry %+v %v", scan, err)
	}
	if !strings.Contains(logs.String(), "reason=completed") {
		t.Fatal(logs.String())
	}
}
