package github_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestCommentPageBoundedObservation(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/acme/acme-api/issues/7/comments?page=3>; rel="next"`, r.Host))
		respondJSON(http.StatusOK, `[{"id":555,"body":"exact marker content","user":{"id":9}}]`)(w, r)
	})
	page, err := f.Client.ListCommentsPage(t.Context(), testRepo, 7, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextPage != 3 || len(page.Comments) != 1 || page.Comments[0].Author.ID != 9 || page.Comments[0].Body != "exact marker content" {
		t.Fatalf("page %+v", page)
	}
	r := f.Only(t)
	if r.Path != "/repos/acme/acme-api/issues/7/comments" || r.Query.Get("page") != "2" || r.Query.Get("per_page") != "10" {
		t.Fatalf("request %+v", r)
	}
}

func TestCommentPageRefusesBoundsAndOversizedResponse(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, `[{"id":1},{"id":2}]`))
	for _, q := range [][3]int{{0, 1, 1}, {7, 0, 1}, {7, 1, 0}, {7, 1, 101}} {
		if _, err := f.Client.ListCommentsPage(t.Context(), testRepo, q[0], q[1], q[2]); err == nil {
			t.Fatalf("accepted bounds %v", q)
		}
	}
	if len(f.Requests()) != 0 {
		t.Fatal("invalid arguments reached HTTP")
	}
	if _, err := f.Client.ListCommentsPage(t.Context(), testRepo, 7, 1, 1); err == nil {
		t.Fatal("oversized response accepted")
	}
	if len(f.Requests()) != 1 {
		t.Fatal("bounded observation scanned multiple pages")
	}
}
