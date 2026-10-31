package store

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestDoctorDiagnosisBySourceKeepsExactSupersededReceipt(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	const target = "github:acme/app#42"
	attempt := doctorIOAttempt("original")
	attempt.SourceID = "owner-action:original"
	if _, err := db.BeginDoctorAttempt(ctx, target, attempt); err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := resultRecord(binding, "accepted", 0)
	if _, err = db.RecordResult(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err = db.AdoptDoctorResult(ctx, binding, request.RequestID, nil); err != nil {
		t.Fatal(err)
	}
	if err = db.StopDoctorConversation(ctx, target); err != nil {
		t.Fatal(err)
	}
	// Move the original source beyond the bounded latest-30 history window.
	for i := range 32 {
		if _, err = db.BeginDoctorAttempt(ctx, target, doctorIOAttempt(fmt.Sprintf("later-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := db.DoctorDiagnosisBySource(ctx, target, attempt.SourceID)
	if err != nil || receipt.AttemptID != attempt.ID || receipt.State != "completed" || !bytes.Equal(receipt.Snapshot, request.Snapshot) {
		t.Fatalf("source receipt replaced by latest: %+v %v", receipt, err)
	}
	for _, input := range []struct{ target, source string }{{"github:acme/app#43", attempt.SourceID}, {target, "missing"}} {
		if _, err := db.DoctorDiagnosisBySource(ctx, input.target, input.source); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign/missing source exposed: %v", err)
		}
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE application_result_revisions SET snapshot='{}' WHERE doctor_attempt_id=?`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DoctorDiagnosisBySource(ctx, target, attempt.SourceID); err == nil {
		t.Fatal("corrupted accepted snapshot was trusted")
	}

}

func TestDoctorDiagnosisBySourceDoesNotClaimUnacceptedResult(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	const target = "github:acme/app#42"
	attempt := doctorIOAttempt("failed")
	if _, err := db.BeginDoctorAttempt(ctx, target, attempt); err != nil {
		t.Fatal(err)
	}
	if err := db.FailDoctorAttempt(ctx, attempt.ID, "failed", "provider refused"); err != nil {
		t.Fatal(err)
	}
	receipt, err := db.DoctorDiagnosisBySource(ctx, target, attempt.SourceID)
	if err != nil || receipt.State != "failed" || len(receipt.Snapshot) != 0 {
		t.Fatalf("unaccepted result invented: %+v %v", receipt, err)
	}
}
