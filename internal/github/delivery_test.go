package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDeliveryMergeUsesExpectedHeadAndRequiresSuccessfulResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		success    bool
	}{{"merged", `{"merged":true,"sha":"` + strings.Repeat("c", 40) + `"}`, 200, true}, {"denied", `{"merged":false,"message":"Required checks failed"}`, 200, false}, {"conflict", `{"message":"Head changed"}`, 409, false}, {"invalid confirmation", `{"merged":true,"sha":"unknown"}`, 200, false}} {
		t.Run(tc.name, func(t *testing.T) {
			head := strings.Repeat("b", 40)
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/repos/acme/acme-api/pulls/70/merge" {
					t.Errorf("wrong merge request %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			}))
			err := f.Client.MergeDeliveryPR(t.Context(), testRepo, 70, head)
			var payload struct {
				SHA    string `json:"sha"`
				Method string `json:"merge_method"`
			}
			if err := json.Unmarshal([]byte(f.Requests()[0].Body), &payload); err != nil {
				t.Error(err)
			}
			if payload.SHA != head || payload.Method != "merge" {
				t.Errorf("merge lacks atomic guard %+v", payload)
			}

			if (err == nil) != tc.success {
				t.Fatalf("success=%v err=%v", tc.success, err)
			}
		})
	}
}
func TestDeliveryPRReadRetainsMergeIdentityAndTarget(t *testing.T) {
	body := `{"id":9000,"number":70,"user":{"id":9},"state":"closed","merged":true,"merge_commit_sha":"` + strings.Repeat("c", 40) + `","head":{"ref":"lf/195-task","sha":"` + strings.Repeat("b", 40) + `","repo":{"full_name":"acme/acme-api"}},"base":{"ref":"main","repo":{"full_name":"acme/acme-api"}}}`
	f := newFixture(t, respondJSON(200, body))
	got, err := f.Client.GetDeliveryPR(t.Context(), testRepo, 70)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Merged || got.Target != "main" || got.HeadRepo != "acme/acme-api" || got.BaseRepo != "acme/acme-api" || got.ID != 9000 || got.CreatorID != 9 || got.MergeSHA != strings.Repeat("c", 40) {
		t.Fatalf("confirmation facts lost %+v", got)
	}
}
func TestDeliveryPRInventoryIncludesClosedAcrossPages(t *testing.T) {
	f := newFixture(t, pagedHandler([]string{`[{"id":1,"number":70,"state":"open"}]`, `[{"id":2,"number":71,"state":"closed","merged":true}]`}))
	got, err := f.Client.ListDeliveryPRs(t.Context(), testRepo, "lf/195-task")
	if err != nil || len(got) != 2 || got[1].State != "closed" {
		t.Fatalf("inventory %+v err=%v", got, err)
	}
	for _, r := range f.Requests() {
		if r.Query.Get("state") != "all" || r.Query.Get("head") != "acme:lf/195-task" {
			t.Fatalf("wrong recovery inventory %+v", r)
		}
	}
}
func TestDeliveryParentIdentityMustMatchBeforeClose(t *testing.T) {
	f := newFixture(t, respondJSON(200, `{"id":100,"number":7,"state":"open"}`))
	if err := f.Client.CloseDeliveredParent(t.Context(), testRepo, 7, 999); err == nil {
		t.Fatal("wrong parent closed")
	}
	for _, r := range f.Requests() {
		if r.Method != http.MethodGet {
			t.Fatalf("mutated wrong parent %+v", r)
		}
	}
}

func TestDeliveryHeadRequiresExactBranchAndValidSHA(t *testing.T) {
	for _, tc := range []struct {
		name, branch, sha string
		valid             bool
	}{{"valid", "lf/task", strings.Repeat("b", 40), true}, {"wrong branch", "main", strings.Repeat("b", 40), false}, {"bad SHA", "lf/task", "short", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, respondJSON(200, fmt.Sprintf(`{"name":%q,"commit":{"sha":%q}}`, tc.branch, tc.sha)))
			got, err := f.Client.GetDeliveryHead(t.Context(), testRepo, "lf/task")
			if (err == nil) != tc.valid || (tc.valid && got != tc.sha) {
				t.Fatalf("head=%q error=%v", got, err)
			}
		})
	}
}
func TestDeliveryPRCreationUsesTaskBranchAndMainWithoutClosingParent(t *testing.T) {
	f := newFixture(t, respondJSON(201, `{"id":9000,"number":70,"state":"open"}`))
	_, err := f.Client.CreateDeliveryPR(t.Context(), testRepo, "lf/task", "main", body(t, "Reviewed task"), body(t, "Parent #195. Integration requires separate approval."))
	if err != nil {
		t.Fatal(err)
	}
	requests := f.Requests()
	if len(requests) != 1 || requests[0].Method != http.MethodPost || requests[0].Path != "/repos/acme/acme-api/pulls" {
		t.Fatalf("wrong PR creation %+v", requests)
	}
	var payload struct {
		Head string `json:"head"`
		Base string `json:"base"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(requests[0].Body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Head != "lf/task" || payload.Base != "main" || !strings.Contains(payload.Body, "separate approval") {
		t.Fatalf("wrong delivery proposal %+v", payload)
	}
}
