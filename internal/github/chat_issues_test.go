package github_test

import (
	"net/http"
	"testing"
)

func TestChatIssueReadIsBoundedAndCurrent(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || q.Get("state") != "open" || q.Get("sort") != "updated" || q.Get("direction") != "desc" || q.Get("per_page") != "3" {
			t.Errorf("unexpected chat read: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Link", `<https://api.github.com/repos/acme/acme-api/issues?page=2>; rel="next"`)
		respondJSON(http.StatusOK, `[{"number":9,"state":"open"},{"number":8,"state":"open","pull_request":{"url":"ignored"}},{"number":7,"state":"open"}]`)(w, r)
	})
	issues, more, err := f.Client.ReadOpenIssues(t.Context(), testRepo, 2)
	if err != nil || !more || len(issues) != 2 || issues[0].Number != 9 || issues[1].Number != 7 {
		t.Fatalf("bounded issues: %v %t %v", issues, more, err)
	}
	f.Only(t)
}
