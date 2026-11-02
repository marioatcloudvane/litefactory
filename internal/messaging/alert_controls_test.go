package messaging_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestAlertControlsWithoutConversationProvider(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	service := messaging.New(db, "discord", "3", []string{"2"}, &sender{}, nil)
	service.SetAlertControls(engine.Command)
	m := store.NaturalMessage{Transport: "discord", ChannelRef: "2", OwnerID: "3", SourceID: "101", Text: "mute disk for 2h", CreatedAt: at}
	handled, err := service.HandleAlertControl(ctx, m, at)
	if !handled || err != nil {
		t.Fatalf("control=%t %v", handled, err)
	}
	handled, err = service.HandleAlertControl(ctx, m, at.Add(time.Hour))
	if !handled || err != nil {
		t.Fatal(err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Receipts) != 1 || !ledger.Controls.RuleMutes["disk_low"].Equal(at.Add(2*time.Hour)) {
		t.Fatalf("replay extended control: %+v", ledger.Controls)
	}
	for _, invalid := range []store.NaturalMessage{
		{Transport: "discord", ChannelRef: "2", OwnerID: "4", SourceID: "102", Text: "alerts on"},
		{Transport: "discord", ChannelRef: "5", OwnerID: "3", SourceID: "103", Text: "alerts on"},
		{Transport: "other", ChannelRef: "2", OwnerID: "3", SourceID: "104", Text: "alerts on"},
	} {
		handled, err = service.HandleAlertControl(ctx, invalid, at)
		if !handled || err != nil {
			t.Fatalf("unauthorized=%t %v", handled, err)
		}
	}
	ledger, err = db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 1 {
		t.Fatalf("unauthorized receipt: %v %+v", err, ledger.Receipts)
	}
	m.SourceID = "105"
	m.Text = "mute disk nonsense"
	handled, err = service.HandleAlertControl(ctx, m, at)
	if !handled || err != nil {
		t.Fatalf("malformed=%t %v", handled, err)
	}
	m.Text = "new conversation"
	handled, err = service.HandleAlertControl(ctx, m, at)
	if handled || err != nil {
		t.Fatalf("ordinary path=%t %v", handled, err)
	}
}
func TestAlertControlsHistoryBypassesConversationStaging(t *testing.T) {
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
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	service := messaging.New(db, "discord", "3", []string{"2"}, &sender{}, nil)
	service.SetAlertControls(engine.Command)
	reader := &intakeReader{pages: map[string]messaging.IntakePage{"": {HighID: "103", LowID: "101", Count: 3, Messages: []store.NaturalMessage{
		{ChannelRef: "2", OwnerID: "3", SourceID: "101", Text: "mute disk 2h", CreatedAt: at},
		{ChannelRef: "2", OwnerID: "3", SourceID: "102", Text: "hello", CreatedAt: at},
		{ChannelRef: "2", OwnerID: "4", SourceID: "103", Text: "alerts on", CreatedAt: at},
	}}}}
	intake := &messaging.Intake{DB: db, Reader: reader, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }, Controls: service.HandleAlertControl}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 1 || pending[0].Text != "hello" {
		t.Fatalf("pending=%+v error=%v", pending, err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 1 {
		t.Fatalf("ledger receipts=%+v error=%v", ledger.Receipts, err)
	}
}

func TestAlertControlsWaitForCompleteHistoryAndRunOldestFirst(t *testing.T) {
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
	engine := &alerts.Engine{DB: db, Settings: config.DefaultAlerts(), Location: time.UTC}
	service := messaging.New(db, "discord", "3", []string{"2"}, &sender{}, nil)
	service.SetAlertControls(engine.Command)
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
		reader.pages[before] = messaging.IntakePage{HighID: fmt.Sprint(high), LowID: fmt.Sprint(high - 99), Count: 100, Messages: []store.NaturalMessage{{ChannelRef: "2", OwnerID: "3", SourceID: fmt.Sprint(high), Text: text, CreatedAt: at}}}
	}
	intake := &messaging.Intake{DB: db, Reader: reader, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }, Controls: service.HandleAlertControl}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	ledger, err := db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 0 {
		t.Fatalf("partial commands executed: %+v %v", ledger.Receipts, err)
	}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	ledger, err = db.ReadAlerts(ctx)
	if err != nil || len(ledger.Receipts) != 5 || !ledger.Controls.OffUntil.IsZero() {
		t.Fatalf("command order or completeness: %+v %v", ledger, err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("control escaped to provider: %+v %v", pending, err)
	}
}
