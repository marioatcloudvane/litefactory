package github_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
)

func TestRepoLabelNodeIDReadsExactName(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, `{"id":5,"node_id":"LA_bug","name":"bug","color":"d73a4a"}`))
	id, found, err := f.Client.RepoLabelNodeID(t.Context(), testRepo, "bug")
	if err != nil || !found || id != "LA_bug" {
		t.Fatalf("label = %q %t %v", id, found, err)
	}
	req := f.Only(t)
	if req.Method != http.MethodGet || req.Path != "/repos/acme/acme-api/labels/bug" || req.Body != "" {
		t.Fatalf("label read was not a bodiless GET of the exact label: %+v", req)
	}
}

func TestRepoLabelNodeIDMissingOrDifferentNameIsNotFound(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]http.HandlerFunc{
		"missing":        respondJSON(http.StatusNotFound, `{"message":"Not Found"}`),
		"different case": respondJSON(http.StatusOK, `{"id":5,"node_id":"LA_bug","name":"Bug"}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, handler)
			id, found, err := f.Client.RepoLabelNodeID(t.Context(), testRepo, "bug")
			if err != nil || found || id != "" {
				t.Fatalf("label = %q %t %v", id, found, err)
			}
			if req := f.Only(t); req.Method != http.MethodGet {
				t.Fatalf("label read wrote: %+v", req)
			}
		})
	}
}

func TestRepoLabelNodeIDServerErrorIsAnError(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusInternalServerError, `{"message":"boom"}`))
	if _, found, err := f.Client.RepoLabelNodeID(t.Context(), testRepo, "bug"); err == nil || found {
		t.Fatalf("server error reported found=%t err=%v", found, err)
	}
}

func TestAddLabelsByIDSendsOnlyLabelIDs(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, `{"data":{"addLabelsToLabelable":{"clientMutationId":null}}}`))
	if err := f.Client.AddLabelsByID(t.Context(), "I_issue", []string{"LA_bug"}); err != nil {
		t.Fatal(err)
	}
	req := f.Only(t)
	if req.Method != http.MethodPost || req.Path != "/graphql" || req.Header.Get("Authorization") != "Bearer "+testToken {
		t.Fatalf("label write did not use the authenticated GraphQL endpoint: %+v", req)
	}
	var body struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	req.JSON(t, &body)
	if !strings.Contains(body.Query, "addLabelsToLabelable(input:{labelableId:$labelable,labelIds:$ids})") {
		t.Fatalf("mutation does not name labels by id: %s", body.Query)
	}
	if strings.Contains(body.Query, "labels:") || strings.Contains(body.Query, "labelNames") || strings.Contains(body.Query, "createLabel") {
		t.Fatalf("mutation carries a name-based or creating field: %s", body.Query)
	}
	if len(body.Variables) != 2 || body.Variables["labelable"] != "I_issue" {
		t.Fatalf("variables = %+v", body.Variables)
	}
	labels, ok := body.Variables["ids"].([]any)
	if !ok || len(labels) != 1 || labels[0] != "LA_bug" {
		t.Fatalf("label ids = %+v", body.Variables["ids"])
	}
}

func TestAddLabelsByIDRefusalAndRateLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, `{"data":{"addLabelsToLabelable":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`))
	if err := f.Client.AddLabelsByID(t.Context(), "I_issue", []string{"LA_gone"}); err == nil {
		t.Fatal("refused mutation reported success")
	}
	f = newFixture(t, respondJSON(http.StatusOK, `{"errors":[{"type":"RATE_LIMITED","message":"slow down"}]}`))
	err := f.Client.AddLabelsByID(t.Context(), "I_issue", []string{"LA_bug"})
	var limited *github.RateLimitError
	if !errors.As(err, &limited) || !errors.Is(err, github.ErrRateLimited) {
		t.Fatalf("rate limit = %v", err)
	}
	f = newFixture(t, respondJSON(http.StatusOK, `{}`))
	for _, call := range []func() error{
		func() error { return f.Client.AddLabelsByID(t.Context(), "", []string{"LA_bug"}) },
		func() error { return f.Client.AddLabelsByID(t.Context(), "I_issue", nil) },
		func() error { return f.Client.AddLabelsByID(t.Context(), "I_issue", []string{""}) },
	} {
		if err := call(); !errors.Is(err, github.ErrInvalidArgument) {
			t.Fatalf("invalid input = %v", err)
		}
	}
	if n := len(f.Requests()); n != 0 {
		t.Fatalf("invalid input sent %d requests", n)
	}
}

func TestCreateIssueReturnsNodeID(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusCreated, `{"id":11,"node_id":"I_kwDO","number":3,"title":"t","body":"b","html_url":"https://github.com/acme/acme-api/issues/3"}`))
	r := redact.New(redact.Options{})
	title, err := r.Apply(t.Context(), "t", 1024)
	if err != nil {
		t.Fatal(err)
	}
	body, err := r.Apply(t.Context(), "b", 1024)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := f.Client.CreateIssue(t.Context(), testRepo, title, body)
	if err != nil || issue.NodeID != "I_kwDO" || issue.ID != 11 {
		t.Fatalf("issue = %+v %v", issue, err)
	}
	var sent map[string]any
	f.Only(t).JSON(t, &sent)
	if _, ok := sent["labels"]; ok {
		t.Fatalf("create carried labels: %+v", sent)
	}
}
