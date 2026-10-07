package store

import (
	"errors"
	"testing"
	"time"
)

var rightsAt = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func historyKinds(t *testing.T, db *DB, repoID int64) []string {
	t.Helper()
	rows, err := db.ListRightsHistory(t.Context(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, r := range rows {
		if r.ActorID != 7 || r.At.IsZero() || r.Snapshot == "" {
			t.Fatalf("history row incomplete: %+v", r)
		}
		kinds = append(kinds, r.Kind)
	}
	return kinds
}

func TestRightsGrantRevokeKeepsRowAndWritesHistory(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	g := RightsGrant{RepoID: repo.ID, SubjectID: 42, SubjectLogin: "ann", Workflow: "plan", Stage: "review", Action: "approve", GrantedBy: 7, CreatedAt: rightsAt, ExpiresAt: rightsAt.Add(time.Hour)}
	stored, err := db.CreateRightsGrant(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == 0 || !stored.ExpiresAt.Equal(g.ExpiresAt) || !stored.RevokedAt.IsZero() {
		t.Fatalf("stored grant: %+v", stored)
	}
	if _, err := db.CreateRightsGrant(t.Context(), g); !errors.Is(err, ErrRightsDuplicate) {
		t.Fatalf("duplicate active grant: %v", err)
	}
	if got := historyKinds(t, db, repo.ID); len(got) != 1 {
		t.Fatalf("a refused grant left history: %v", got)
	}
	revoked, err := db.RevokeRightsGrant(t.Context(), repo.ID, stored.ID, 7, rightsAt.Add(time.Minute))
	if err != nil || revoked.RevokedBy != 7 || revoked.RevokedAt.IsZero() {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	if _, err := db.RevokeRightsGrant(t.Context(), repo.ID, stored.ID, 7, rightsAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if got := historyKinds(t, db, repo.ID); len(got) != 2 || got[0] != "grant" || got[1] != "revoke" {
		t.Fatalf("history: %v", got)
	}
	all, _ := db.ListRightsGrants(t.Context(), repo.ID, false)
	active, _ := db.ListRightsGrants(t.Context(), repo.ID, true)
	if len(all) != 1 || len(active) != 0 {
		t.Fatalf("rows all=%d active=%d", len(all), len(active))
	}
	// A revoked grant frees the scope for a new one.
	if _, err := db.CreateRightsGrant(t.Context(), g); err != nil {
		t.Fatalf("regrant: %v", err)
	}
}

func TestRightsRefusals(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	if _, err := db.CreateRightsGrant(t.Context(), RightsGrant{RepoID: 999, SubjectID: 1, SubjectLogin: "a", Workflow: "plan", Stage: "s", Action: "approve", GrantedBy: 7, CreatedAt: rightsAt}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown repository: %v", err)
	}
	if _, err := db.CreateRightsGrant(t.Context(), RightsGrant{RepoID: repo.ID, SubjectID: 1, SubjectLogin: "a", Workflow: "plan", Stage: "s", Action: "approve", GrantedBy: 7, CreatedAt: rightsAt, ExpiresAt: rightsAt}); err == nil {
		t.Fatal("expiry not after creation was stored")
	}
	if got := historyKinds(t, db, repo.ID); len(got) != 0 {
		t.Fatalf("failed changes left history: %v", got)
	}
}

func TestRightsTablesRefuseEdits(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	g, _ := db.CreateRightsGrant(t.Context(), RightsGrant{RepoID: repo.ID, SubjectID: 1, SubjectLogin: "a", Workflow: "plan", Stage: "s", Action: "approve", GrantedBy: 7, CreatedAt: rightsAt})
	a, err := db.CreateReviewerAssignment(t.Context(), ReviewerAssignment{RepoID: repo.ID, TaskRef: "github:acme/api#1", Workflow: "plan", Stage: "s", ReviewerID: 2, ReviewerLogin: "bo", AssignedBy: 7, CreatedAt: rightsAt})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE rights_history SET actor_id=9`,
		`DELETE FROM rights_history`,
		`DELETE FROM rights_grants`,
		`DELETE FROM reviewer_assignments`,
		`UPDATE rights_grants SET subject_id=5`,
		`UPDATE reviewer_assignments SET reviewer_id=5`,
		`UPDATE rights_grants SET revoked_at=NULL,revoked_by=NULL`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), q); err == nil {
			t.Fatalf("%s was accepted", q)
		}
	}
	if _, err := db.RevokeRightsGrant(t.Context(), repo.ID, g.ID, 7, rightsAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE rights_grants SET revoked_at='2030-01-01T00:00:00.000Z'`); err == nil {
		t.Fatal("a revoked grant was changed again")
	}
	if _, err := db.RemoveReviewerAssignment(t.Context(), repo.ID, a.ID, 7, rightsAt); err != nil {
		t.Fatal(err)
	}
}

func TestReviewerAssignRemoveReplace(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	base := ReviewerAssignment{RepoID: repo.ID, TaskRef: "github:acme/api#1", Workflow: "plan", Stage: "s", ReviewerID: 2, ReviewerLogin: "bo", AssignedBy: 7, CreatedAt: rightsAt}
	first, err := db.CreateReviewerAssignment(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateReviewerAssignment(t.Context(), base); !errors.Is(err, ErrRightsDuplicate) {
		t.Fatalf("duplicate open assignment: %v", err)
	}
	next := base
	next.ReviewerID, next.ReviewerLogin = 3, "cy"
	second, err := db.ReplaceReviewerAssignment(t.Context(), next, true)
	if err != nil {
		t.Fatal(err)
	}
	open, _ := db.ListReviewerAssignments(t.Context(), repo.ID, base.TaskRef, true)
	all, _ := db.ListReviewerAssignments(t.Context(), repo.ID, "", false)
	if len(open) != 1 || open[0].ID != second.ID || len(all) != 2 {
		t.Fatalf("open=%v all=%v", open, all)
	}
	if _, err := db.RemoveReviewerAssignment(t.Context(), repo.ID, first.ID, 7, rightsAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing a closed assignment: %v", err)
	}
	if _, err := db.RemoveReviewerAssignment(t.Context(), repo.ID, second.ID, 7, rightsAt); err != nil {
		t.Fatal(err)
	}
	want := []string{"assign", "remove", "assign", "remove"}
	got := historyKinds(t, db, repo.ID)
	if len(got) != len(want) {
		t.Fatalf("history %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history %v want %v", got, want)
		}
	}
}
