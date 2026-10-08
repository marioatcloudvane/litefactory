package store

import "testing"

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
