package store

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateReporterResultIsStoredOnce(t *testing.T) {
	db := newTestDB(t)
	at := graphTestTime()
	commit := strings.Repeat("a", 40)
	row, created, err := db.BeginValidateReporter(t.Context(), ValidateReporterResult{InstanceID: 1, Commit: commit, Mode: ReporterModeReport, Attempt: "a1", CreatedAt: at})
	if err != nil || !created || row.State != ReporterPending {
		t.Fatalf("begin: %+v %v %v", row, created, err)
	}
	again, created, err := db.BeginValidateReporter(t.Context(), ValidateReporterResult{InstanceID: 1, Commit: commit, Mode: ReporterModeReport, Attempt: "a2", CreatedAt: at})
	if err != nil || created || again.ID != row.ID || again.Attempt != "a1" {
		t.Fatalf("a second report attempt was started: %+v %v %v", again, created, err)
	}
	if err := db.FinishValidateReporter(t.Context(), row.ID, ReporterAccepted, `{"mode":"report"}`, "", at); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishValidateReporter(t.Context(), row.ID, ReporterAccepted, `{"mode":"report"}`, "", at); err != nil {
		t.Fatalf("idempotent finish: %v", err)
	}
	if err := db.FinishValidateReporter(t.Context(), row.ID, ReporterAccepted, `{"mode":"other"}`, "", at); !errors.Is(err, ErrReporterSettled) {
		t.Fatalf("a settled result changed: %v", err)
	}
	if err := db.FinishValidateReporter(t.Context(), row.ID, ReporterFallback, "", "late", at); !errors.Is(err, ErrReporterSettled) {
		t.Fatalf("a settled result fell back: %v", err)
	}
	if _, _, err := db.BeginValidateReporter(t.Context(), ValidateReporterResult{InstanceID: 1, Commit: commit, Mode: ReporterModeFollowUp, Attempt: "f1", CreatedAt: at}); err == nil {
		t.Fatal("a follow-up without an owner comment was accepted")
	}
	follow, created, err := db.BeginValidateReporter(t.Context(), ValidateReporterResult{InstanceID: 1, Commit: commit, Mode: ReporterModeFollowUp, Attempt: "f1", CommentID: 9, Question: "why?", CreatedAt: at})
	if err != nil || !created {
		t.Fatalf("follow-up: %v", err)
	}
	if err := db.FinishValidateReporter(t.Context(), follow.ID, ReporterFallback, "", "timeout", at); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListValidateReporter(t.Context(), 1)
	if err != nil || len(rows) != 2 || rows[0].Result != `{"mode":"report"}` || rows[1].Question != "why?" || rows[1].State != ReporterFallback {
		t.Fatalf("rows %+v %v", rows, err)
	}
}

func TestValidateFactoryBugIntentAndRetry(t *testing.T) {
	db := newTestDB(t)
	at := graphTestTime()
	commit := strings.Repeat("b", 40)
	in := ValidateFactoryBug{InstanceID: 1, Commit: commit, Item: "failure-1", CommentID: 5, AuthorID: 2, Marker: "m", CreatedAt: at}
	row, err := db.PutValidateFactoryBugIntent(t.Context(), in)
	if err != nil || row.State != FactoryBugIntent {
		t.Fatalf("intent %+v %v", row, err)
	}
	if err := db.FinishValidateFactoryBug(t.Context(), row.ID, FactoryBugFailed, "", "", "rate limited", at); err != nil {
		t.Fatal(err)
	}
	in.CommentID = 6
	retry, err := db.PutValidateFactoryBugIntent(t.Context(), in)
	if err != nil || retry.ID != row.ID || retry.State != FactoryBugIntent || retry.CommentID != 6 {
		t.Fatalf("retry %+v %v", retry, err)
	}
	if err := db.FinishValidateFactoryBug(t.Context(), row.ID, FactoryBugFiled, "o/r#7", "https://x/7", "", at); err != nil {
		t.Fatal(err)
	}
	in.CommentID = 7
	same, err := db.PutValidateFactoryBugIntent(t.Context(), in)
	if err != nil || same.State != FactoryBugFiled || same.Reference != "o/r#7" {
		t.Fatalf("a filed Bug changed: %+v %v", same, err)
	}
}
