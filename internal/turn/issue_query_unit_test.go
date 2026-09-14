package turn

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

type queryUnitForge struct {
	page                               github.IssuePage
	issue                              github.Issue
	err                                error
	repo                               github.Repo
	state                              string
	pageNumber, perPage, number, calls int
}

func (f *queryUnitForge) ListIssuesPage(_ context.Context, repo github.Repo, state string, page, perPage int) (github.IssuePage, error) {
	f.repo, f.state, f.pageNumber, f.perPage = repo, state, page, perPage
	f.calls++
	return f.page, f.err
}
func (f *queryUnitForge) GetIssue(_ context.Context, repo github.Repo, number int) (github.Issue, error) {
	f.repo, f.number = repo, number
	f.calls++
	return f.issue, f.err
}

// Construct only the query facade here; immutable receipt binding and transport
// are exercised by the SQLite/broker tests, not this fake-only unit fixture.
func queryUnitService(f *queryUnitForge) *IssueQueryService {
	return &IssueQueryService{forge: f, repo: github.Repo{Owner: "owner", Name: "project"}}
}

func TestIssueQueryService_ListBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, input, state string
		page, perPage      int
		valid              bool
	}{
		{"defaults/open first thirty", `{}`, "open", 1, 30, true},
		{"closed/custom page", `{"state":"closed","page":2,"per_page":100}`, "closed", 2, 100, true},
		{"minimum/one item", `{"per_page":1}`, "open", 1, 1, true},
		{name: "all/refused", input: `{"state":"all"}`},
		{name: "empty state/refused", input: `{"state":""}`},
		{name: "zero page/refused", input: `{"page":0}`},
		{name: "negative page/refused", input: `{"page":-1}`},
		{name: "zero size/refused", input: `{"per_page":0}`},
		{name: "negative size/refused", input: `{"per_page":-1}`},
		{name: "oversized page/refused", input: `{"per_page":101}`},
		{name: "null page/refused", input: `{"page":null}`},
		{name: "null size/refused", input: `{"per_page":null}`},
		{name: "null state/refused", input: `{"state":null}`},
		{name: "null labels/refused", input: `{"include_mode_labels":null}`},
		{name: "foreign repo/refused", input: `{"repository":"other/private"}`},
		{name: "foreign owner/refused", input: `{"owner":"other"}`},
		{name: "unknown field/refused", input: `{"body":true}`},
		{name: "wrong type/refused", input: `{"page":"1"}`},
		{name: "fraction/refused", input: `{"page":1.5}`},
		{name: "array/refused", input: `[]`},
		{name: "null/refused", input: `null`},
		{name: "trailing value/refused", input: `{} {}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &queryUnitForge{}
			got, err := queryUnitService(f).HandleList(context.Background(), "req", []byte(tt.input))
			if !tt.valid {
				if err == nil || len(got) != 0 || f.calls != 0 {
					t.Fatalf("invalid input result=%s err=%v calls=%d", got, err, f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var out IssueListResponse
			if err := json.Unmarshal(got, &out); err != nil {
				t.Fatal(err)
			}
			if f.repo != (github.Repo{Owner: "owner", Name: "project"}) || f.state != tt.state || f.pageNumber != tt.page || f.perPage != tt.perPage || f.calls != 1 {
				t.Fatalf("query=%+v", f)
			}
			if out.Page != tt.page || out.PerPage != tt.perPage || out.Items == nil || len(out.Items) != 0 || out.NextPage != 0 {
				t.Fatalf("response=%+v", out)
			}
		})
	}
}

func TestIssueQueryService_GetBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, input, state string
		number             int
		valid              bool
	}{
		{"default/open", `{"issue_number":7}`, "open", 7, true},
		{"explicit closed/closed", `{"issue_number":7,"state":"closed"}`, "closed", 7, true},
		{name: "missing number/refused", input: `{}`},
		{name: "zero/refused", input: `{"issue_number":0}`},
		{name: "negative/refused", input: `{"issue_number":-7}`},
		{name: "null number/refused", input: `{"issue_number":null}`},
		{name: "all state/refused", input: `{"issue_number":7,"state":"all"}`},
		{name: "null state/refused", input: `{"issue_number":7,"state":null}`},
		{name: "foreign repo/refused", input: `{"issue_number":7,"repo":"other"}`},
		{name: "page field/refused", input: `{"issue_number":7,"page":1}`},
		{name: "url number/refused", input: `{"issue_number":"https://github.com/other/repo/issues/7"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &queryUnitForge{issue: github.Issue{Number: 7, Title: "A title", State: tt.state}}
			got, err := queryUnitService(f).HandleGet(context.Background(), "req", []byte(tt.input))
			if !tt.valid {
				if err == nil || len(got) != 0 || f.calls != 0 {
					t.Fatalf("result=%s err=%v calls=%d", got, err, f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != `{"number":7,"title":"A title"}` || f.number != tt.number || f.repo != (github.Repo{Owner: "owner", Name: "project"}) {
				t.Fatalf("result=%s query=%+v", got, f)
			}
		})
	}
}

func TestIssueQueryService_MinimalProjection(t *testing.T) {
	t.Parallel()
	for _, labels := range []bool{false, true} {
		t.Run(map[bool]string{false: "labels omitted", true: "recognized labels only"}[labels], func(t *testing.T) {
			t.Parallel()
			issue := github.Issue{Number: 7, Title: "A title", Body: "secret body", ID: 12345, APIURL: "private URL", State: "open", Labels: []string{"security-sensitive", "Mode: Research", "Mode: Implement", "Mode: Research", "mode: validate"}}
			f := &queryUnitForge{page: github.IssuePage{Items: []github.Issue{issue}, NextPage: 3}, issue: issue}
			args := `{"include_mode_labels":false}`
			if labels {
				args = `{"include_mode_labels":true}`
			}
			got, err := queryUnitService(f).HandleList(context.Background(), "req", []byte(args))
			if err != nil {
				t.Fatal(err)
			}
			want := `{"items":[{"number":7,"title":"A title"}],"page":1,"per_page":30,"next_page":3}`
			if labels {
				want = `{"items":[{"number":7,"title":"A title","mode_labels":["Mode: Implement","Mode: Research"]}],"page":1,"per_page":30,"next_page":3}`
			}
			if string(got) != want {
				t.Fatalf("got=%s want=%s", got, want)
			}
			args = `{"issue_number":7,"include_mode_labels":false}`
			if labels {
				args = `{"issue_number":7,"include_mode_labels":true}`
			}
			got, err = queryUnitService(f).HandleGet(context.Background(), "req", []byte(args))
			if err != nil {
				t.Fatal(err)
			}
			var page IssueListResponse
			if err := json.Unmarshal([]byte(want), &page); err != nil {
				t.Fatal(err)
			}
			var summary IssueSummary
			if err := json.Unmarshal(got, &summary); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(summary, page.Items[0]) || strings.Contains(string(got), "secret") || strings.Contains(string(got), "private") {
				t.Fatalf("get projection=%s", got)
			}
		})
	}
}

func TestIssueQueryService_UpstreamRefusals(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("forge unavailable")
	for _, tt := range []struct {
		name  string
		issue github.Issue
		err   error
	}{
		{"state mismatch", github.Issue{Number: 7, State: "closed", Title: "closed"}, nil},
		{"target mismatch", github.Issue{Number: 8, State: "open", Title: "other"}, nil},
		{"oversized title", github.Issue{Number: 7, State: "open", Title: strings.Repeat("a", github.IssueTitleSizeLimit+1)}, nil},
		{"invalid utf8", github.Issue{Number: 7, State: "open", Title: string([]byte{255})}, nil},
		{"forge error", github.Issue{}, sentinel},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &queryUnitForge{issue: tt.issue, err: tt.err}
			got, err := queryUnitService(f).HandleGet(context.Background(), "req", []byte(`{"issue_number":7}`))
			if err == nil || len(got) != 0 {
				t.Fatalf("result=%s err=%v", got, err)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("lost forge error: %v", err)
			}
		})
	}
}

func TestIssueQueryService_EmptyPageKeepsContinuation(t *testing.T) {
	t.Parallel()
	f := &queryUnitForge{page: github.IssuePage{Items: []github.Issue{}, NextPage: 2}}
	got, err := queryUnitService(f).HandleList(context.Background(), "req", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"items":[],"page":1,"per_page":30,"next_page":2}` || f.calls != 1 {
		t.Fatalf("result=%s calls=%d", got, f.calls)
	}
}

func TestIssueQueryService_ListOutputBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		page  github.IssuePage
		valid bool
		want  string
	}{
		{name: "oversized upstream page/refused", page: github.IssuePage{Items: []github.Issue{{Number: 1, State: "open", Title: "first"}, {Number: 2, State: "open", Title: "second"}}}},
		{name: "negative continuation/refused", page: github.IssuePage{NextPage: -1}},
		{name: "nonadvancing continuation/refused", page: github.IssuePage{NextPage: 1}},
		{name: "oversized upstream title/refused", page: github.IssuePage{Items: []github.Issue{{Number: 1, State: "open", Title: strings.Repeat("a", github.IssueTitleSizeLimit+1)}}}},
		{name: "foreign state excluded/empty continued page", page: github.IssuePage{Items: []github.Issue{{Number: 1, State: "closed", Title: "closed"}}, NextPage: 2}, valid: true, want: `{"items":[],"page":1,"per_page":1,"next_page":2}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &queryUnitForge{page: tt.page}
			got, err := queryUnitService(f).HandleList(context.Background(), "req", []byte(`{"per_page":1}`))
			if !tt.valid {
				if err == nil || len(got) != 0 {
					t.Fatalf("result=%s err=%v", got, err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("result=%s err=%v want=%s", got, err, tt.want)
			}
		})
	}
}
