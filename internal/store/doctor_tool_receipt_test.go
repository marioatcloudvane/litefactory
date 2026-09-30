package store

import (
	"bytes"
	"testing"
)

func TestDoctorToolReceiptSharesLedgerWithoutAdoptingResult(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	if _, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("tool-attempt")); err != nil {
		t.Fatal(err)
	}
	if err := db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: "tool-attempt", Provider: "claude", ConfiguredModel: "model", Evidence: `{"task_ref":"github:acme/api#406"}`}); err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, "tool-attempt")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"name":"doctor_write","arguments":{"step":1}}`)
	reply := []byte(`{"operation_id":"repair","step":1,"state":"verified"}`)
	record := ResultRecord{Binding: binding, RequestID: "tool-request", Payload: payload, Diagnostics: reply, Accepted: true, DoctorToolReceipt: true}
	receipt, err := db.RecordResult(ctx, record)
	if err != nil || receipt.AppliedRevision != 0 {
		t.Fatal(receipt, err)
	}
	var revision, count int
	if err = db.sql.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=?`, binding.ResultID).Scan(&revision); err != nil || revision != 0 {
		t.Fatal("tool adopted application revision", revision, err)
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM application_result_revisions WHERE result_id=?`, binding.ResultID).Scan(&count); err != nil || count != 0 {
		t.Fatal("tool created result history", count, err)
	}
	if err = db.StopDoctorConversation(ctx, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	replay, err := db.ReplayResultRequest(ctx, binding, "tool-request", payload)
	if err != nil || replay.ID != receipt.ID || !bytes.Equal(replay.Diagnostics, reply) {
		t.Fatal("lost reply not recoverable after fence", replay, err)
	}
	if _, err = db.ReplayResultRequest(ctx, binding, "tool-request", []byte(`{"name":"doctor_write","arguments":{"step":2}}`)); err == nil {
		t.Fatal("correlation rebound")
	}
}
