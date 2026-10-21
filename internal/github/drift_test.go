package github_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDriftDiscussionBoundedEnvelopesBeforeDecode(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t, respondJSON(status, `{"message":"`+strings.Repeat("x", 2048)+`","body":"canary"}`))
			got, err := f.Client.ReadDriftDiscussion(t.Context(), testRepo, 7, 128)
			if err == nil || got.Bytes != 128 || got.Issue.Body != "" {
				t.Fatalf("unbounded body admitted: %+v %v", got, err)
			}
			if len(f.Requests()) != 1 {
				t.Fatal("bounded read retried")
			}
		})
	}
}
func TestDriftDiscussionAdmitsPRBodyAndStopsAtSelectedCommentPage(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/acme/acme-api/issues/7/comments?page=2>; rel="next"`, r.Host))
			respondJSON(200, `[{"id":99,"body":"discussion"}]`)(w, r)
			return
		}
		respondJSON(200, `{"number":7,"body":"PR body","pull_request":{"url":"https://example.test/pr/7"}}`)(w, r)
	})
	body, err := f.Client.ReadDriftDiscussion(t.Context(), testRepo, 7, 4096)
	if err != nil || body.Issue.Body != "PR body" || body.Bytes == 0 {
		t.Fatalf("explicit PR body missing: %+v %v", body, err)
	}
	page, err := f.Client.ReadDriftComments(t.Context(), testRepo, 7, 1, 1, 4096)
	if err != nil || page.NextPage != 2 || len(page.Comments) != 1 {
		t.Fatalf("page lost provenance: %+v %v", page, err)
	}
	if len(f.Requests()) != 2 {
		t.Fatal("page read expanded remote scope")
	}
}

func TestDriftDiscussionRejectsDifferentSelectedIssue(t *testing.T) {
	f := newFixture(t, respondJSON(200, `{"number":8,"body":"unselected"}`))
	got, err := f.Client.ReadDriftDiscussion(t.Context(), testRepo, 7, 4096)
	if err == nil || got.Issue.Body != "" || got.Bytes == 0 {
		t.Fatalf("unselected discussion accepted: %+v %v", got, err)
	}
}
