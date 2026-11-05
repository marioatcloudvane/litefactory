package github_test

import (
	"net/http"
	"testing"
)

func TestCreatedIssuesPageAsksForCreatorAllStatesNewestFirst(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/acme-api/issues" || q.Get("creator") != "lf-bot[bot]" || q.Get("state") != "all" || q.Get("sort") != "created" || q.Get("direction") != "desc" || q.Get("page") != "2" || q.Get("per_page") != "100" {
			t.Errorf("unexpected request %s %s %v", r.Method, r.URL.Path, q)
		}
		w.Header().Set("Link", `<https://api.github.com/repos/acme/acme-api/issues?page=3>; rel="next"`)
		respondJSON(http.StatusOK, issuesAndPullRequestsJSON)(w, r)
	})
	page, err := f.Client.ListCreatedIssuesPage(t.Context(), testRepo, "lf-bot[bot]", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextPage != 3 {
		t.Fatalf("page=%+v", page)
	}
	if _, err := f.Client.ListCreatedIssuesPage(t.Context(), testRepo, "", 1, 100); err == nil {
		t.Fatal("empty creator accepted")
	}
}
