package github_test

import (
	"net/http"
	"testing"
)

func TestListFollowupIssuesIncludesClosedAcrossPages(t *testing.T) {
	t.Parallel()
	f := newFixture(t, pagedHandler([]string{`[{"id":8001,"number":41,"state":"open","body":"marker","user":{"id":9}}]`, `[{"id":8002,"number":42,"state":"closed","body":"closed marker","user":{"id":9}},{"id":8003,"number":43,"pull_request":{"url":"https://example.test/pull/43"}}]`}))
	got, err := f.Client.ListFollowupIssues(t.Context(), testRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ID != 8002 || got[1].State != "closed" || got[1].Author.ID != 9 || got[1].Body != "closed marker" {
		t.Fatalf("closed recovery identity lost %+v", got)
	}
	for _, r := range f.Requests() {
		if r.Path != "/repos/acme/acme-api/issues" || r.Query.Get("state") != "all" {
			t.Fatalf("wrong reconciliation request %+v", r)
		}
	}
}
func TestListFollowupIssuesFailureIsNotEmptyInventory(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusInternalServerError, `{"message":"unavailable"}`))
	got, err := f.Client.ListFollowupIssues(t.Context(), testRepo)
	if err == nil || len(got) > 0 {
		t.Fatalf("failed inventory treated as absence %+v %v", got, err)
	}
}
