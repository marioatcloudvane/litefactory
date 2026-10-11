package messaging_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

type intakeReader struct {
	pages map[string]messaging.IntakePage
	calls int
	fail  bool
}

func (r *intakeReader) CheckIntakeCapability(context.Context, string) error { return nil }
func (r *intakeReader) ReadIntakePage(_ context.Context, _, _, before string) (messaging.IntakePage, error) {
	r.calls++
	if r.fail {
		return messaging.IntakePage{}, errors.New("read failed")
	}
	return r.pages[before], nil
}
func TestNaturalIntakeOrderedRestartAndFailure(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	r := &intakeReader{pages: map[string]messaging.IntakePage{}}
	// Four complete pages require another tick; all retained messages remain staged.
	for page := 0; page < 5; page++ {
		high := 600 - page*100
		low := high - 99
		before := ""
		if page > 0 {
			before = fmt.Sprint(high + 1)
		}
		count := 100
		if page == 4 {
			count = 99
			low = 101
		}
		r.pages[before] = messaging.IntakePage{HighID: fmt.Sprint(high), LowID: fmt.Sprint(low), Count: count, Messages: []store.NaturalMessage{{ChannelRef: "2", OwnerID: "3", SourceID: fmt.Sprint(high), Text: "hello", CreatedAt: at}}}
	}
	intake := &messaging.Intake{DB: db, Reader: r, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("partial release: %+v %v", pending, err)
	}
	scan, err := db.ReadNaturalScan(ctx, "2")
	if err != nil || scan.Cursor != "100" || scan.Before != "201" {
		t.Fatalf("progress %+v %v", scan, err)
	}
	r.fail = true
	if intake.Poll(ctx, at) == nil {
		t.Error("failed read accepted")
	}
	after, _ := db.ReadNaturalScan(ctx, "2")
	if after != scan {
		t.Error("failed scan advanced")
	}
	r.fail = false
	restarted := *intake
	if err := restarted.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	pending, err = db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 5 {
		t.Fatalf("release %+v %v", pending, err)
	}
	for index, m := range pending {
		if m.SourceID != fmt.Sprint(200+index*100) {
			t.Fatalf("unordered release %+v", pending)
		}
	}
}
func TestNaturalIntakeActivationDiscardsHistory(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Now()
	r := &intakeReader{pages: map[string]messaging.IntakePage{"": {HighID: "42", LowID: "42", Count: 1, Messages: []store.NaturalMessage{{ChannelRef: "2", SourceID: "42", OwnerID: "3", Text: "old", CreatedAt: at}}}}}
	i := &messaging.Intake{DB: db, Reader: r, Owner: "3", Channels: []string{"2"}}
	if err := i.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("old requests admitted: %+v %v", pending, err)
	}
}

func TestNaturalIntakeFutureTimestampDoesNotAdvance(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	r := &intakeReader{pages: map[string]messaging.IntakePage{"": {HighID: "101", LowID: "101", Count: 1, Messages: []store.NaturalMessage{{ChannelRef: "2", OwnerID: "3", SourceID: "101", Text: "request", CreatedAt: at.Add(time.Second)}}}}}
	intake := &messaging.Intake{DB: db, Reader: r, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(ctx, at); err == nil {
		t.Fatal("future message accepted")
	}
	scan, err := db.ReadNaturalScan(ctx, "2")
	if err != nil || scan.Cursor != "100" || scan.HighWater != "" {
		t.Fatalf("future message advanced cursor: %+v %v", scan, err)
	}
	if err := intake.Poll(ctx, at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 1 || pending[0].SourceID != "101" {
		t.Fatalf("future request lost: %+v %v", pending, err)
	}
}

func TestNaturalIntakeLargeIntervalCompletes(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	// Twelve full pages of owner messages (IDs 101..1300) exceed the 1000 limit.
	r := &intakeReader{pages: map[string]messaging.IntakePage{}}
	for page := 0; page < 12; page++ {
		high := 1300 - page*100
		before := ""
		if page > 0 {
			before = fmt.Sprint(high + 1)
		}
		var messages []store.NaturalMessage
		for id := high; id > high-100; id-- {
			messages = append(messages, store.NaturalMessage{ChannelRef: "2", OwnerID: "3", SourceID: fmt.Sprint(id), Text: "hello", CreatedAt: at})
		}
		r.pages[before] = messaging.IntakePage{HighID: fmt.Sprint(high), LowID: fmt.Sprint(high - 99), Count: 100, Messages: messages}
	}
	intake := &messaging.Intake{DB: db, Reader: r, Owner: "3", Transport: "discord", Channels: []string{"2"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	for tick := 0; tick < 4; tick++ {
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	scan, err := db.ReadNaturalScan(ctx, "2")
	if err != nil || scan.Cursor != "1300" || scan.HighWater != "" || scan.Before != "" {
		t.Fatalf("scan not completed: %+v %v", scan, err)
	}
	want := 101
	for {
		pending, e := db.PendingNaturalMessages(ctx, "2", 100)
		if e != nil {
			t.Fatal(e)
		}
		if len(pending) == 0 {
			break
		}
		for _, m := range pending {
			if m.SourceID != fmt.Sprint(want) {
				t.Fatalf("release order: got %s want %d", m.SourceID, want)
			}
			if e = db.FinishNaturalMessage(ctx, "2", m.SourceID, at); e != nil {
				t.Fatal(e)
			}
			want++
		}
	}
	if want != 1301 {
		t.Fatalf("released %d messages, want 1200", want-101)
	}
}
