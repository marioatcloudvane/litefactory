package github_test

import (
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
)

func TestManagedIssueEndpointsCarryNumericIdentityAndFullEdit(t *testing.T) {
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/acme-api/issues/2/parent":
			if r.Method != http.MethodGet {
				t.Errorf("method %s", r.Method)
			}
			respondJSON(http.StatusOK, `{"number":1,"id":100}`)(w, r)
		case "/repos/acme/acme-api/issues/2":
			if r.Method != http.MethodPatch {
				t.Errorf("method %s", r.Method)
			}
			respondJSON(http.StatusOK, `{"number":2,"id":200,"title":"Full title","body":"Full technical body"}`)(w, r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	parent, err := f.Client.GetParentIssue(t.Context(), testRepo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Number != 1 || parent.ID != 100 {
		t.Fatalf("wrong parent %+v", parent)
	}
	red := redact.New(redact.Options{Environ: []string{}})
	title, err := red.Apply(t.Context(), "Full title", 65536)
	if err != nil {
		t.Fatal(err)
	}
	body, err := red.Apply(t.Context(), "Full technical body", 65536)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := f.Client.UpdateIssue(t.Context(), testRepo, 2, title, body)
	if err != nil {
		t.Fatal(err)
	}
	requests := f.Requests()
	var fields struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	requests[len(requests)-1].JSON(t, &fields)
	if fields.Title != "Full title" || fields.Body != "Full technical body" {
		t.Errorf("edit lost fields: %+v", fields)
	}
	if issue.Number != 2 || issue.ID != 200 {
		t.Fatalf("wrong updated identity %+v", issue)
	}
}
