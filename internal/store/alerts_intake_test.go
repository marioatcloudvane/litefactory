package store

import (
	"fmt"
	"testing"
	"time"
)

func TestAlertControlScanCapacityRollsBackWithoutLosingReservedCommands(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := db.ActivateAlertControlScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 10; batch++ {
		scan, _, err := db.ReadAlertControlScan(ctx, "2")
		if err != nil {
			t.Fatal(err)
		}
		next := scan
		next.HighWater = "99999"
		next.Before = fmt.Sprint(99999 - batch*400)
		var messages []NaturalMessage
		for index := 0; index < 400; index++ {
			messages = append(messages, NaturalMessage{ChannelRef: "2", SourceID: fmt.Sprint(101 + batch*400 + index), OwnerID: "3", Text: "mute disk 2h", CreatedAt: at})
		}
		if err = db.StageAlertControlScan(ctx, "2", scan, next, messages, false); err != nil {
			t.Fatal(err)
		}
	}
	scan, _, err := db.ReadAlertControlScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	next := scan
	next.Before = "2"
	messages := make([]NaturalMessage, 100)
	for index := range messages {
		messages[index] = NaturalMessage{ChannelRef: "2", SourceID: fmt.Sprint(5000 + index), OwnerID: "3", Text: "alerts on", CreatedAt: at}
	}
	if err = db.StageAlertControlScan(ctx, "2", scan, next, messages, true); err == nil {
		t.Fatal("control capacity silently exceeded")
	}
	retained, _, err := db.ReadAlertControlScan(ctx, "2")
	if err != nil || len(retained.Messages) != 4000 || retained.Before != scan.Before || retained.Complete || retained.Cursor != "100" {
		t.Fatalf("capacity failure lost commands or advanced cursor: count=%d before=%s complete=%t cursor=%s error=%v", len(retained.Messages), retained.Before, retained.Complete, retained.Cursor, err)
	}
}
