package store

import (
	"errors"
	"testing"
	"time"
)

func TestDoctorDiscoveryReopenReplaysPartiallyAdmittedPage(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "api")
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := db.DoctorScanWindow(ctx, repo.ID, at); err != nil {
		t.Fatal(err)
	}
	window, err := db.DoctorScanWindow(ctx, repo.ID, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	input := doctorInboxInput(repo, 406, 101, "diagnose")
	input.CreatedAt = at.Add(30 * time.Second)
	admitted, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	// Process loss after admission, before advancing the page: its saved command
	// and unfinished scan must survive reopening the actual SQLite file.
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	saved, err := reopened.DoctorScanWindow(ctx, repo.ID, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Page != window.Page || !saved.WindowEnd.Equal(window.WindowEnd) || !saved.CompletedAt.Equal(at) {
		t.Fatalf("lost unfinished window: %+v", saved)
	}
	input.Instructions = "edited after initial admission"
	replay, err := reopened.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != admitted.ID || replay.Instructions != admitted.Instructions {
		t.Fatalf("replay changed immutable command: %+v", replay)
	}
	input.CommentID = 102
	input.IssueNumber = 407
	input.IssueID = 10407
	input.TaskRef = "github:acme/api#407"
	second, err := reopened.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.AdvanceDoctorScan(ctx, saved, 2); err != nil {
		t.Fatal(err)
	}
	// A delayed completion for the old page cannot skip the unfinished next page.
	if err := reopened.AdvanceDoctorScan(ctx, saved, 0); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("stale page changed cursor: %v", err)
	}
	next, err := reopened.DoctorScanWindow(ctx, repo.ID, at.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if next.Page != 2 || !next.WindowEnd.Equal(window.WindowEnd) || !next.CompletedAt.Equal(at) {
		t.Fatalf("page persistence: %+v", next)
	}
	for _, want := range []int64{admitted.ID, second.ID} {
		claimed, err := reopened.ClaimDoctorCommand(ctx, "", false, "reopened-daemon")
		if err != nil || claimed.ID != want {
			t.Fatalf("durable queue: %+v %v want %d", claimed, err, want)
		}
		if err := reopened.CompleteDoctorCommand(ctx, claimed.ID, "fixture result", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reopened.ClaimDoctorCommand(ctx, "", false, "reopened-daemon"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replay duplicated work: %v", err)
	}
}
