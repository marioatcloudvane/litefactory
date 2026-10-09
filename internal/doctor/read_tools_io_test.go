package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type commentsFixture struct {
	calls              int
	repo               github.Repo
	issue, page, limit int
	failure            error
	result             github.CommentPage
}

func (f *commentsFixture) ListCommentsPage(_ context.Context, repo github.Repo, issue, page, limit int) (github.CommentPage, error) {
	f.calls++
	f.repo = repo
	f.issue = issue
	f.page = page
	f.limit = limit
	return f.result, f.failure
}
func readResultsFixture(t *testing.T) (*Service, *ResultService) {
	t.Helper()
	s := doctorIOService(t)
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.BeginDoctorAttempt(t.Context(), target.String(), store.DoctorAttempt{ID: "read-attempt", SourceID: "read-source", OwnerText: "inspect", PromptVersion: PromptVersion, GuideVersion: GuideVersion}); err != nil {
		t.Fatal(err)
	}
	results, err := BindResults(t.Context(), s.DB, "read-attempt", store.DoctorEvidence{Live: true, TaskRef: target.String()}, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	return s, results
}
func readTool(t *testing.T, results *ResultService, name, input string) (json.RawMessage, error) {
	t.Helper()
	call, err := json.Marshal(struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{name, json.RawMessage(input)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := results.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "read-request", Payload: call})
	if err != nil {
		return nil, err
	}
	var failure struct {
		Valid *bool  `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Valid != nil && !*failure.Valid {
		return raw, errors.New(failure.Error)
	}
	return raw, nil
}
func TestDoctorBrokerReadToolsExactCatalogAndRedactedQuery(t *testing.T) {
	s, results := readResultsFixture(t)
	list, err := results.handle(t.Context(), submission.Request{Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(list, &catalog); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range catalog.Tools {
		names[tool.Name] = true
	}
	if len(catalog.Tools) != 5 || !names["database_query"] || !names["issue_comments"] {
		t.Fatalf("catalog: %s", list)
	}
	out, err := readTool(t, results, "database_query", `{"sql":"SELECT 'doctor-secret-canary-001' AS secret"}`)
	if err != nil || strings.Contains(string(out), "doctor-secret-canary-001") || !strings.Contains(string(out), "[redacted]") {
		t.Fatalf("query disclosure: %s %v", out, err)
	}
	if _, err := readTool(t, results, "database_query", `{"sql":"DELETE FROM sessions"}`); err == nil {
		t.Fatal("write admitted")
	}
	if _, err := readTool(t, results, "database_query", `{"sql":"SELECT 1","database":"other"}`); err == nil {
		t.Fatal("foreign database input admitted")
	}
	if _, err := readTool(t, results, "issues_comment", `{}`); err == nil {
		t.Fatal("publication tool admitted")
	}
	if err := s.DB.StopDoctorConversation(t.Context(), results.binding.Task); err != nil {
		t.Fatal(err)
	}
	if _, err := readTool(t, results, "database_query", `{"sql":"SELECT 1"}`); err == nil {
		t.Fatal("stopped attempt read was not fenced")
	}
}
func TestDoctorIssueCommentsPagingRedactionAndFixedTarget(t *testing.T) {
	_, results := readResultsFixture(t)
	fixture := &commentsFixture{result: github.CommentPage{NextPage: 3, Comments: []github.Comment{{ID: 91, Author: github.User{Login: "owner"}, CreatedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), Body: "doctor-secret-canary-001 live finding"}}}}
	results.comments = fixture
	raw, err := readTool(t, results, "issue_comments", `{"page":2}`)
	if err != nil {
		t.Fatal(err)
	}
	var page commentPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if fixture.repo.Owner != "acme" || fixture.repo.Name != "api" || fixture.issue != 406 || fixture.page != 2 || fixture.limit != 20 || page.NextPage != 3 || page.Truncated || len(page.Comments) != 1 || page.Comments[0].ID != 91 || page.Comments[0].Author != "owner" || strings.Contains(string(raw), "doctor-secret-canary-001") {
		t.Fatalf("bound redacted page: %+v %s", fixture, raw)
	}
	for _, input := range []string{`{"issue":407}`, `{"repository":"other/repo"}`, `{"page":-1}`} {
		if _, err := readTool(t, results, "issue_comments", input); err == nil {
			t.Fatalf("unbound input accepted: %s", input)
		}
	}
	if fixture.calls != 1 {
		t.Fatal("invalid input fetched comments")
	}
	for _, message := range []string{"403 refused secret_canary", "429 rate-limited secret_canary"} {
		fixture.failure = errors.New(message)
		if _, err := readTool(t, results, "issue_comments", `{}`); err == nil || strings.Contains(err.Error(), "secret_canary") {
			t.Fatalf("fetch failure disclosure: %v", err)
		}
	}
	fixture.failure = nil
	fixture.result.Comments[0].Body = strings.Repeat("x", 10000)
	raw, err = readTool(t, results, "issue_comments", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &page); err != nil || !page.Truncated {
		t.Fatalf("body limit: %s %v", raw, err)
	}
	results.repairID = "repair"
	if _, err := readTool(t, results, "issue_comments", `{}`); err == nil {
		t.Fatal("repair acquired diagnosis read authority")
	}
}
