package store

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestNaturalScanAtomicOrderedRecovery(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	initial, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	partial := NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "300", Before: "200"}
	msg := func(id string) NaturalMessage {
		return NaturalMessage{ChannelRef: "2", SourceID: id, OwnerID: "1", Text: "request", CreatedAt: at}
	}
	if err = db.StageNaturalScan(ctx, initial, partial, []NaturalMessage{msg("300"), msg("200")}, false); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("partial released: %v %v", pending, err)
	}
	resumed, err := db.ReadNaturalScan(ctx, "2")
	if err != nil || resumed != partial {
		t.Fatalf("lost continuation: %+v %v", resumed, err)
	}
	if err = db.StageNaturalScan(ctx, initial, partial, nil, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale collector: %v", err)
	}
	if err = db.StageNaturalScan(ctx, resumed, partial, []NaturalMessage{msg("150")}, true); err != nil {
		t.Fatal(err)
	}
	pending, err = db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 3 {
		t.Fatalf("missing interval: %v %v", pending, err)
	}
	for i, id := range []string{"150", "200", "300"} {
		if pending[i].SourceID != id {
			t.Fatal("unordered admission")
		}
	}
	if err = db.FinishNaturalMessage(ctx, "2", "150", at); err != nil {
		t.Fatal(err)
	}
	var content string
	if err = db.sql.QueryRowContext(ctx, `SELECT content FROM chat_natural_messages WHERE source_id='150'`).Scan(&content); err != nil || content != "" {
		t.Fatalf("terminal text: %q %v", content, err)
	}
}
func TestNaturalAuthorizationFencesAndCleanup(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: `{"command":"diagnose"}`, Digest: "digest", Code: "code", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err := db.SaveNaturalDialogue(ctx, d, 0); err != nil {
		t.Fatal(err)
	}
	r := ChatReply{Transport: "discord", MessageID: "natural-code", ChannelRef: "2", Command: "diagnose", Arguments: map[string]string{"issue": "3", "note": "exact"}}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "101", r, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("undelivered authorized: %v", err)
	}
	if err := db.BindNaturalPrompt(ctx, d, "500"); err != nil {
		t.Fatal(err)
	}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "101", r, at.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired authorized: %v", err)
	}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "101", r, at); err != nil {
		t.Fatal(err)
	}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "102", r, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate authorized: %v", err)
	}
	if err := db.EndNaturalDialogue(ctx, d, "cancelled", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel beat authorized: %v", err)
	}
	inbox, err := db.ReadChatReply(ctx, "discord", r.MessageID)
	if err != nil || inbox.Arguments["note"] != "exact" {
		t.Fatalf("canonical payload lost: %+v %v", inbox, err)
	}
	if err = db.StartNaturalExecution(ctx, r.MessageID, at); err != nil {
		t.Fatal(err)
	}
	if err = db.StartNaturalExecution(ctx, r.MessageID, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("execution repeated: %v", err)
	}
	if err = db.FinishNaturalExecution(ctx, r.MessageID, "saved outcome", false, at); err != nil {
		t.Fatal(err)
	}
	execution, err := db.ReadNaturalExecution(ctx, r.MessageID)
	if err != nil || execution.Payload != "{}" || execution.Outcome != "saved outcome" {
		t.Fatalf("execution cleanup: %+v %v", execution, err)
	}
	terminal, err := db.ReadNaturalDialogue(ctx, "discord", "1", "2")
	if err != nil || terminal.Payload != "{}" || terminal.Code != "" {
		t.Fatalf("dialogue cleanup: %+v %v", terminal, err)
	}
}

func TestNaturalConfirmationCancellationRace(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: `{"command":"diagnose"}`, Digest: "digest", Code: "code", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err := db.SaveNaturalDialogue(ctx, d, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.BindNaturalPrompt(ctx, d, "500"); err != nil {
		t.Fatal(err)
	}
	r := ChatReply{Transport: "discord", MessageID: "natural-race", ChannelRef: "2", Command: "diagnose", Arguments: map[string]string{"issue": "3"}}
	result := make(chan error, 2)
	start := make(chan struct{})
	go func() { <-start; result <- db.AuthorizeNaturalDialogue(ctx, d, "101", r, at) }()
	go func() { <-start; result <- db.EndNaturalDialogue(ctx, d, "cancelled", at) }()
	close(start)
	success := 0
	for range 2 {
		err := <-result
		if err == nil {
			success++
		} else if !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("race had %d winners", success)
	}
	terminal, err := db.ReadNaturalDialogue(ctx, "discord", "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	_, executionErr := db.ReadNaturalExecution(ctx, r.MessageID)
	if terminal.State == "cancelled" && !errors.Is(executionErr, ErrNotFound) {
		t.Fatal("cancelled action executed")
	}
	if terminal.State == "authorized" && executionErr != nil {
		t.Fatal("authorization lost execution")
	}
}
func TestNaturalCorrectionAndExpiryCleanup(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	d := NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: `{"note":"original"}`, Digest: "digest", Code: "code", ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err := db.SaveNaturalDialogue(ctx, d, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.BindNaturalPrompt(ctx, d, "500"); err != nil {
		t.Fatal(err)
	}
	corrected := d
	corrected.Revision = 2
	corrected.Code = "new"
	corrected.Digest = "newdigest"
	corrected.Payload = `{"note":"changed"}`
	if err := db.SaveNaturalDialogue(ctx, corrected, 1); err != nil {
		t.Fatal(err)
	}
	r := ChatReply{Transport: "discord", MessageID: "natural-old", ChannelRef: "2", Command: "diagnose"}
	if err := db.AuthorizeNaturalDialogue(ctx, d, "101", r, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale correction authorized: %v", err)
	}
	if err := db.PurgeNaturalChat(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	terminal, err := db.ReadNaturalDialogue(ctx, "discord", "1", "2")
	if err != nil || terminal.State != "expired" || terminal.Payload != "{}" {
		t.Fatalf("expiry cleanup: %+v %v", terminal, err)
	}
	if err = db.PurgeNaturalChat(ctx, at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadNaturalDialogue(ctx, "discord", "1", "2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("terminal retention: %v", err)
	}
}

func TestNaturalExpiredQueueClearsTextRetainsIdentity(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	initial, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	partial := NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "300", Before: "200"}
	messages := []NaturalMessage{{ChannelRef: "2", SourceID: "300", OwnerID: "1", ReplyTo: "50", Text: "private text", CreatedAt: at.Add(-25 * time.Hour)}}
	if err = db.StageNaturalScan(ctx, initial, partial, messages, false); err != nil {
		t.Fatal(err)
	}
	if err = db.PurgeNaturalChat(ctx, at); err != nil {
		t.Fatal(err)
	}
	var state, content, reply string
	if err = db.sql.QueryRowContext(ctx, `SELECT state,content,reply_to FROM chat_natural_messages WHERE channel_ref='2' AND source_id='300'`).Scan(&state, &content, &reply); err != nil || state != "finished" || content != "" || reply != "" {
		t.Fatalf("expired queued content retained: %q %q %q %v", state, content, reply, err)
	}
	if err = db.StageNaturalScan(ctx, partial, partial, messages, true); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "2", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expired replay released: %+v %v", pending, err)
	}
	if err = db.PurgeNaturalChat(ctx, at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM chat_natural_messages WHERE channel_ref='2' AND source_id='300'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("identity retention: %d %v", count, err)
	}
}

func TestNaturalScanLargeIntervalCompletes(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	expected, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	// Three partial commits of 400 newest-first messages stage 1200 rows (IDs 101..1300).
	high := 1300
	for batch := 0; batch < 3; batch++ {
		var messages []NaturalMessage
		top := high - batch*400
		for id := top; id > top-400; id-- {
			messages = append(messages, NaturalMessage{ChannelRef: "2", SourceID: strconv.Itoa(id), OwnerID: "1", Text: "request", CreatedAt: at})
		}
		next := NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "1300", Before: strconv.Itoa(top - 399)}
		if err = db.StageNaturalScan(ctx, expected, next, messages, false); err != nil {
			t.Fatalf("batch %d: %v", batch, err)
		}
		expected = next
	}
	if err = db.StageNaturalScan(ctx, expected, expected, nil, true); err != nil {
		t.Fatal(err)
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
			if m.SourceID != strconv.Itoa(want) {
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

func TestNaturalScanPendingBackpressureRecovers(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	if err := db.ActivateNaturalScan(ctx, "2", "100"); err != nil {
		t.Fatal(err)
	}
	expected, err := db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	// Release exactly 1000 pending messages (IDs 101..1100).
	for _, size := range []int{400, 400, 200} {
		top := 1100
		if expected.Before != "" {
			top, _ = strconv.Atoi(expected.Before)
			top--
		}
		var messages []NaturalMessage
		for id := top; id > top-size; id-- {
			messages = append(messages, NaturalMessage{ChannelRef: "2", SourceID: strconv.Itoa(id), OwnerID: "1", Text: "request", CreatedAt: at})
		}
		next := NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "1100", Before: strconv.Itoa(top - size + 1)}
		if err = db.StageNaturalScan(ctx, expected, next, messages, size == 200); err != nil {
			t.Fatal(err)
		}
		expected = next
	}
	expected, err = db.ReadNaturalScan(ctx, "2")
	if err != nil || expected.Cursor != "1100" {
		t.Fatalf("release incomplete: %+v %v", expected, err)
	}
	next := NaturalScan{ChannelRef: "2", Cursor: "1100", HighWater: "1101", Before: "1101"}
	extra := []NaturalMessage{{ChannelRef: "2", SourceID: "1101", OwnerID: "1", Text: "request", CreatedAt: at}}
	if err = db.StageNaturalScan(ctx, expected, next, extra, false); err == nil || err.Error() != "natural intake backpressure" {
		t.Fatalf("pending limit not enforced: %v", err)
	}
	if err = db.FinishNaturalMessage(ctx, "2", "101", at); err != nil {
		t.Fatal(err)
	}
	if err = db.StageNaturalScan(ctx, expected, next, extra, false); err != nil {
		t.Fatalf("backpressure did not recover: %v", err)
	}
}
