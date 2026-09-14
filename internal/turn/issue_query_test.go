package turn

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestIssueQuerySocketHTTPProjectionAndReceipts(t *testing.T) {
	db, _, opts := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	o := familyResearchOptions(t, db, app)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/repos/"+o.Receipts.scope.Repository+"/issues") {
			t.Errorf("scope violation: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/issues") {
			q := r.URL.Query()
			if q.Get("state") != "open" || q.Get("page") != "1" || q.Get("per_page") != "30" {
				t.Errorf("defaults=%v", q)
			}
			w.Header().Set("Link", `<https://api.github.com/repos/acme/repo/issues?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"number":283,"title":"Visible","body":"SECRET_BODY","state":"open","labels":[{"name":"Mode: Implement"},{"name":"private-label"}],"user":{"login":"SECRET_LOGIN"}},{"number":284,"title":"PR","state":"open","pull_request":{"url":"https://example.invalid/pr"}}]`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/285"):
			_, _ = w.Write([]byte(`{"number":285,"state":"closed","title":"Closed","body":"SECRET_BODY"}`))
		case strings.HasSuffix(r.URL.Path, "/286"):
			_, _ = w.Write([]byte(`{"number":999,"state":"open","title":"Wrong target"}`))
		case strings.HasSuffix(r.URL.Path, "/287"):
			_, _ = w.Write([]byte(`{"number":287,"state":"open","title":"PR","pull_request":{"url":"https://example.invalid/pr"}}`))
		default:
			_, _ = w.Write([]byte(`{"number":283,"state":"open","title":"Visible","body":"SECRET_BODY","labels":[{"name":"Mode: Implement"},{"name":"private-label"}]}`))
		}
	}))
	t.Cleanup(server.Close)
	forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("fixture-token")})
	mustProgress(t, err)
	service, err := NewIssueQueryService(o.Receipts, forge)
	mustProgress(t, err)
	o.Registrations = service.Registrations()
	o.CommentTargets = []int{7}
	client, guard, _ := familySocketFixture(t, db, o)
	for i, tc := range []struct{ tool, args string }{
		{"issues_list", `{}`},
		{"issues_list", `{"include_mode_labels":true}`},
		{"issues_get", `{"issue_number":283}`},
		{"issues_get", `{"issue_number":283,"include_mode_labels":true}`},
		{"issues_get", `{"issue_number":285,"state":"closed"}`},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			response, e := client.Call(t.Context(), tc.tool, []byte(tc.args))
			mustProgress(t, e)
			if response.Error != "" {
				t.Fatal(response.Error)
			}
			for _, secret := range []string{"SECRET_BODY", "SECRET_LOGIN", "private-label", "pull_request"} {
				if strings.Contains(string(response.Result), secret) {
					t.Fatalf("projection leaked %q: %s", secret, response.Result)
				}
			}
			if len(response.Result) > 600 {
				t.Fatalf("unbounded projection: %s", response.Result)
			}
			if strings.Contains(tc.args, "include_mode_labels") != strings.Contains(string(response.Result), "Mode: Implement") {
				t.Fatalf("optional labels: %s", response.Result)
			}
			if tc.tool == "issues_list" {
				var list IssueListResponse
				mustProgress(t, json.Unmarshal(response.Result, &list))
				if len(list.Items) != 1 || list.Items[0].Number != 283 || list.NextPage != 2 || list.Page != 1 || list.PerPage != 30 {
					t.Fatalf("bad list: %+v", list)
				}
			}
			before := calls.Load()
			receipt, e := client.Call(t.Context(), "receipts_get", mcpReceiptArgs(t, response.RequestID))
			mustProgress(t, e)
			if receipt.Error != "" {
				t.Fatal(receipt.Error)
			}
			var got FamilyReceipt
			mustProgress(t, json.Unmarshal(receipt.Result, &got))
			if got.Disposition != "confirmed" || got.Tool != tc.tool {
				t.Fatalf("receipt=%+v", got)
			}
			if strings.Contains(string(receipt.Result), "Visible") || calls.Load() != before {
				t.Fatalf("receipt retained catalog or redispatched: %s", receipt.Result)
			}
		})
	}
	before := calls.Load()
	for _, tc := range []struct{ tool, args string }{
		{"issues_list", `null`}, {"issues_get", `null`},
		{"issues_list", `{"repository":"foreign/repo"}`}, {"issues_list", `{"state":"all"}`},
		{"issues_list", `{"page":0}`}, {"issues_list", `{"per_page":101}`},
		{"issues_list", `{"state":null}`}, {"issues_list", `{"page":null}`}, {"issues_list", `{"per_page":null}`}, {"issues_list", `{"include_mode_labels":null}`},
		{"issues_get", `{"issue_number":0}`}, {"issues_get", `{"issue_number":null}`}, {"issues_get", `{"issue_number":283,"repository":"foreign/repo"}`},
	} {
		response, e := client.Call(t.Context(), tc.tool, []byte(tc.args))
		mustProgress(t, e)
		if response.Error == "" {
			t.Fatalf("invalid input admitted: %s", tc.args)
		}
		receipt, e := o.Receipts.Get(t.Context(), response.RequestID)
		mustProgress(t, e)
		if receipt.Disposition != "rejected" {
			t.Fatalf("refusal not durable: %+v", receipt)
		}
	}
	if calls.Load() != before {
		t.Fatal("invalid arguments reached forge")
	}
	for _, number := range []int{285, 286, 287} {
		response, e := client.Call(t.Context(), "issues_get", []byte(fmt.Sprintf(`{"issue_number":%d}`, number)))
		mustProgress(t, e)
		if response.Error == "" {
			t.Fatalf("state/target/PR mismatch admitted: %d", number)
		}
	}
	changed := o
	changed.CommentTargets = []int{7, 283}
	if _, e := NewFamilyToolDispatcher(guard, changed); e == nil {
		t.Fatal("discovery enlarged comment authority")
	}
	before = calls.Load()
	mustProgress(t, guard.Revoke(t.Context(), "cancelled"))
	response, e := client.Call(t.Context(), "issues_get", []byte(`{"issue_number":283}`))
	mustProgress(t, e)
	if response.Error == "" || calls.Load() != before {
		t.Fatal("revoked query reached forge")
	}
}
