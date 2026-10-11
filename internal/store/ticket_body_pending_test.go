package store

import "testing"

func TestTicketBootstrapRetainsBodyAndPreservesInitializedHistory(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "body-recovery")
	ctx := t.Context()
	legacy, err := db.GetOrCreateSession(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.BootstrapWatermark(ctx, legacy.ID, 100); err != nil || !created {
		t.Fatalf("legacy bootstrap %v %v", created, err)
	}
	watermark, created, err := db.BootstrapTicketWatermark(ctx, legacy.ID, 200)
	if err != nil || created || watermark.LastCommentID != 100 {
		t.Fatalf("legacy boundary changed %+v %v %v", watermark, created, err)
	}
	pending, err := db.TicketBodyPending(ctx, legacy.ID)
	if err != nil || pending {
		t.Fatalf("initialized history gained body work %v %v", pending, err)
	}
	fresh, err := db.GetOrCreateSession(ctx, repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	watermark, created, err = db.BootstrapTicketWatermark(ctx, fresh.ID, 7)
	if err != nil || !created || watermark.LastCommentID != 7 {
		t.Fatalf("enrolled bootstrap %+v %v %v", watermark, created, err)
	}
	pending, err = db.TicketBodyPending(ctx, fresh.ID)
	if err != nil || !pending {
		t.Fatalf("body signal not atomic with bootstrap %v %v", pending, err)
	}
	if _, err := db.AdvanceWatermark(ctx, fresh.ID, 9); err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.BootstrapWatermark(ctx, fresh.ID, 100); err != nil || created {
		t.Fatalf("interrupted bootstrap changed %v %v", created, err)
	}
	pending, err = db.TicketBodyPending(ctx, fresh.ID)
	if err != nil || !pending {
		t.Fatalf("reply advance or bootstrap erased body %v %v", pending, err)
	}
	if err := db.CompleteTicketBody(ctx, fresh.ID); err != nil {
		t.Fatal(err)
	}
	if _, created, err := db.BootstrapTicketWatermark(ctx, fresh.ID, 100); err != nil || created {
		t.Fatalf("repeat bootstrap %v %v", created, err)
	}
	pending, err = db.TicketBodyPending(ctx, fresh.ID)
	if err != nil || pending {
		t.Fatalf("consumed body replayed %v %v", pending, err)
	}
	watermark, exists, err := db.GetWatermark(ctx, fresh.ID)
	if err != nil || !exists || watermark.LastCommentID != 9 {
		t.Fatalf("body completion moved reply boundary %+v %v %v", watermark, exists, err)
	}
}
