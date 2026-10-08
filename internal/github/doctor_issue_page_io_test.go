package github_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestDoctorIssuePageFixedRouteAndCursor(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/acme/acme-api/issues/406/comments?page=3>; rel=\"next\"", r.Host))
		_, _ = fmt.Fprint(w, `[{"id":91,"body":"live comment","user":{"login":"owner"},"created_at":"2026-10-08T10:00:00Z"}]`)
	})
	out, err := f.Client.ListCommentsPage(t.Context(), testRepo, 406, 2, 20)
	if err != nil || out.NextPage != 3 || len(out.Comments) != 1 || out.Comments[0].ID != 91 {
		t.Fatalf("page: %+v %v", out, err)
	}
	req := f.Only(t)
	if req.Method != http.MethodGet || req.Path != "/repos/acme/acme-api/issues/406/comments" || req.Query.Get("page") != "2" || req.Query.Get("per_page") != "20" {
		t.Fatalf("route: %+v", req)
	}
}

func TestDoctorIssuePageRefusesBroadAndUnboundedReads(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	for _, test := range []struct{ issue, page, limit int }{{0, 1, 20}, {406, 0, 20}, {406, 1, 101}} {
		if _, err := f.Client.ListCommentsPage(t.Context(), testRepo, test.issue, test.page, test.limit); err == nil {
			t.Fatalf("invalid query accepted: %+v", test)
		}
	}
	if len(f.Requests()) != 0 {
		t.Fatal("invalid request reached forge")
	}
}
