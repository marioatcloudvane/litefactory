package github_test

import (
	"net/http"
	"testing"
)

func TestSearchIssuesIsOneBoundedPageOfIssues(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/search/issues" || q.Get("q") != "repo:acme/acme-api is:issue routing timed out" || q.Get("per_page") != "2" {
			t.Errorf("unexpected search: %s %s", r.Method, r.URL)
		}
		respondJSON(http.StatusOK, `{"total_count":3,"items":[{"number":9,"title":"Routing timeout","state":"closed","labels":[{"name":"bug"}]},{"number":8,"state":"open","pull_request":{"url":"ignored"}},{"number":7,"state":"open"}]}`)(w, r)
	})
	issues, err := f.Client.SearchIssues(t.Context(), testRepo, "  routing \n timed out ", 2)
	if err != nil || len(issues) != 2 || issues[0].Number != 9 || issues[0].State != "closed" || issues[0].Labels[0] != "bug" || issues[1].Number != 7 {
		t.Fatalf("search = %+v, %v", issues, err)
	}
	f.Only(t)
	if _, err = f.Client.SearchIssues(t.Context(), testRepo, " ", 2); err == nil {
		t.Fatal("empty search accepted")
	}
}
