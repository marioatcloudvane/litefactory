package github_test

import (
	"net/http"
	"testing"
	"time"
)

func TestDoctorRepositoryPageKeepsExactIssueAndActor(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, `[{"id":10,"body":"/doctor explain","issue_url":"https://api.github.com/repos/acme/acme-api/issues/406","user":{"id":7,"login":"owner"},"created_at":"2026-09-29T12:00:00Z","updated_at":"2026-09-29T12:00:00Z"}]`))
	page, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC), 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 1 || page.Comments[0].IssueNumber != 406 || page.Comments[0].Comment.Author.ID != 7 || page.Comments[0].Comment.Body != "/doctor explain" {
		t.Fatalf("routing lost: %+v", page)
	}
}

func TestDoctorRepositoryPageRejectsForeignRouting(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, `[{"id":10,"body":"/doctor explain","issue_url":"https://api.github.com/repos/other/api/issues/406"}]`))
	if _, err := f.Client.ListRepositoryCommentsPage(t.Context(), testRepo, time.Now(), 1, 12); err == nil {
		t.Fatal("foreign routing accepted")
	}
}
