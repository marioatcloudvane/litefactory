package store

import (
	"errors"
	"testing"
)

func TestDoctorConsumeBackupPrecedesRemoteReply(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 645)
	if _, _, e := db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RecordComment(ctx, Comment{SessionID: session.ID, ForgeCommentID: 11, Body: "consume", AuthorHandle: "owner", AuthorUserID: 10, CreatedAt: graphTestTime(), Classification: ClassificationInstruction}); e != nil {
		t.Fatal(e)
	}
	op := typedRowApproval(t, db, session, "consume_comment", "comment:11", "10", "11")
	id, e := db.PrepareDoctorConsumeComment(ctx, op.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if id != session.ID {
		t.Fatal("foreign session")
	}
	steps, e := db.DoctorRepairSteps(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	if steps[0].State != "backed_up" || steps[0].BackupReference == "" {
		t.Fatal("backup not recorded", steps)
	}
	watermark, _, e := db.GetWatermark(ctx, session.ID)
	if e != nil || watermark.LastCommentID != 10 {
		t.Fatal("watermark changed before delivery")
	}
	if _, e = db.PrepareDoctorConsumeComment(ctx, op.ID, 1); e == nil {
		t.Fatal("uncertain effect replay accepted")
	}
}

// consumeDeliveryFixture claims a consume reply, so its row is possibly sent.
func consumeDeliveryFixture(t *testing.T) (*DB, DoctorRepair, ReportDelivery) {
	t.Helper()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 646)
	if _, _, e := db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RecordComment(ctx, Comment{SessionID: session.ID, ForgeCommentID: 11, Body: "consume", AuthorHandle: "owner", AuthorUserID: 10, CreatedAt: graphTestTime(), Classification: ClassificationInstruction}); e != nil {
		t.Fatal(e)
	}
	op := typedRowApproval(t, db, session, "consume_comment", "comment:11", "10", "11")
	if _, e := db.PrepareDoctorConsumeComment(ctx, op.ID, 1); e != nil {
		t.Fatal(e)
	}
	key := "doctor-consume:" + op.ID + ":1"
	marker := "<!-- lf:doctor-consume operation=" + op.ID + " step=1 -->"
	d, claimed, e := db.ClaimDoctorConsumeDelivery(ctx, op.ID, 1, ReportDelivery{Key: key, Version: 1, SessionID: session.ID, Task: session.TaskRef, Destination: session.TaskRef, TriggerID: 11, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "consumed\n\n" + marker, Marker: marker})
	if e != nil || !claimed || !d.PossiblySent {
		t.Fatal(d, claimed, e)
	}
	return db, op, d
}

// C9, C12, C13: only a stopped repair's unverified, receipt-less reply is
// dropped, and only once it is not possibly sent or a lookup found no trace.
func TestSettleFencedDoctorConsumeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name        string
		prepare     func(*testing.T, *DB, DoctorRepair, ReportDelivery) string
		lookupEmpty bool
		settled     bool
	}{
		{"live repair", func(*testing.T, *DB, DoctorRepair, ReportDelivery) string { return "" }, true, false},
		{"possibly sent without lookup", stopConsumeRepair, false, false},
		{"possibly sent with empty lookup", stopConsumeRepair, true, true},
		{"rate-limit unmarked", func(t *testing.T, db *DB, op DoctorRepair, d ReportDelivery) string {
			if e := db.UnmarkDoctorConsumeDelivery(t.Context(), op.ID, 1, d.Key); e != nil {
				t.Fatal(e)
			}
			if saved, e := db.GetReportDelivery(t.Context(), d.Key); e != nil || saved.PossiblySent || saved.CommentID != 0 {
				t.Fatal(saved, e)
			}
			return stopConsumeRepair(t, db, op, d)
		}, false, true},
		{"receipt recorded", func(t *testing.T, db *DB, op DoctorRepair, d ReportDelivery) string {
			if e := db.ConfirmDoctorConsumeDelivery(t.Context(), op.ID, 1, d.Key, 500, "https://github.com/acme/api/issues/123#issuecomment-500"); e != nil {
				t.Fatal(e)
			}
			saved, e := db.GetReportDelivery(t.Context(), d.Key)
			if e != nil || saved.ConfirmedAt.IsZero() || saved.ConfirmedURL == "" {
				t.Fatalf("doctor confirmation metadata: %+v %v", saved, e)
			}
			return stopConsumeRepair(t, db, op, d)
		}, true, false},
		{"verified step", func(t *testing.T, db *DB, op DoctorRepair, d ReportDelivery) string {
			if _, e := db.sql.ExecContext(t.Context(), `UPDATE doctor_repair_steps SET state='verified' WHERE repair_id=?`, op.ID); e != nil {
				t.Fatal(e)
			}
			return stopConsumeRepair(t, db, op, d)
		}, true, false},
		{"non-doctor key", func(t *testing.T, db *DB, op DoctorRepair, d ReportDelivery) string {
			stopConsumeRepair(t, db, op, d)
			return "report:" + d.Key
		}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, op, d := consumeDeliveryFixture(t)
			key := tc.prepare(t, db, op, d)
			if key == "" {
				key = d.Key
			}
			before, e := db.ReadDoctorRepair(t.Context(), op.TaskRef)
			if e != nil {
				t.Fatal(e)
			}
			settlement, settled, e := db.SettleFencedDoctorConsumeDelivery(t.Context(), key, tc.lookupEmpty)
			if e != nil || settled != tc.settled {
				t.Fatalf("settled=%v err=%v, want %v", settled, e, tc.settled)
			}
			after, e := db.ReadDoctorRepair(t.Context(), op.TaskRef)
			if e != nil {
				t.Fatal(e)
			}
			_, rowErr := db.GetReportDelivery(t.Context(), d.Key)
			if !tc.settled {
				if rowErr != nil || after != before {
					t.Fatalf("refused settlement changed state: row=%v repair=%+v", rowErr, after)
				}
				return
			}
			if !errors.Is(rowErr, ErrNotFound) || settlement.RepairID != op.ID || settlement.Step != 1 {
				t.Fatalf("row=%v settlement=%+v", rowErr, settlement)
			}
			if after.State != before.State || after.Failure != before.Failure+DoctorConsumeDroppedNote {
				t.Fatalf("repair %s failure %q", after.State, after.Failure)
			}
			if _, again, e := db.SettleFencedDoctorConsumeDelivery(t.Context(), key, true); e != nil || again {
				t.Fatalf("second settlement: %v %v", again, e)
			}
		})
	}
}

func stopConsumeRepair(t *testing.T, db *DB, op DoctorRepair, _ ReportDelivery) string {
	t.Helper()
	if e := db.StopDoctorRepair(t.Context(), op.TaskRef, "step 1 consume_comment: doctor reply could not be posted."); e != nil {
		t.Fatal(e)
	}
	return ""
}
