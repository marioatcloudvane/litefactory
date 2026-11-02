package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func parentObservationJSON(parent string) string {
	return fmt.Sprintf(`{"data":{"repository":{"issue":{"databaseId":700,"number":7,"repository":{"nameWithOwner":%q},"parent":%s}}}}`, testRepo.String(), parent)
}

func TestObserveParentIssueAuthenticatedNullableRelationship(t *testing.T) {
	t.Parallel()
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("present=%t", present), func(t *testing.T) {
			t.Parallel()
			parent := "null"
			if present {
				parent = `{"databaseId":900,"number":42,"repository":{"nameWithOwner":"other/project"}}`
			}
			f := newFixture(t, respondJSON(http.StatusOK, parentObservationJSON(parent)))
			a := newTicketAdapter(t, f)
			child := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.Origin(), RemoteID: "700"}, Locator: ticketLocator(a)}
			observed, err := a.ObserveParent(t.Context(), child)
			if err != nil {
				t.Fatal(err)
			}
			if !present && observed != nil {
				t.Fatalf("null relationship returned parent: %+v", observed)
			}
			if present && (observed == nil || observed.Identity.RemoteID != "900" || observed.Identity.Origin != a.Origin() || observed.Locator.Container != "other/project" || observed.Locator.Key != "42") {
				t.Fatalf("parent lost canonical identity: %+v", observed)
			}
			req := f.Only(t)
			if req.Path != "/graphql" || req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer "+testToken {
				t.Fatalf("parent query did not reuse authenticated endpoint: %+v", req)
			}
			var body struct {
				Query     string `json:"query"`
				Variables struct {
					Owner  string `json:"owner"`
					Name   string `json:"name"`
					Number int    `json:"number"`
				} `json:"variables"`
			}
			if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
				t.Fatal(err)
			}
			if body.Variables.Owner != testRepo.Owner || body.Variables.Name != testRepo.Name || body.Variables.Number != 7 || !strings.Contains(body.Query, "parent{") {
				t.Fatalf("wrong relationship query: %+v", body)
			}
		})
	}
}

func TestObserveParentIssueRefusesIncompleteOrMismatchedEvidence(t *testing.T) {
	t.Parallel()
	valid := parentObservationJSON("null")
	for name, body := range map[string]string{
		"missing data":        `{}`,
		"inaccessible child":  `{"data":{"repository":{"issue":null}}}`,
		"inaccessible repo":   `{"data":{"repository":null}}`,
		"different child ID":  strings.Replace(valid, `"databaseId":700`, `"databaseId":701`, 1),
		"different number":    strings.Replace(valid, `"number":7`, `"number":8`, 1),
		"different repo":      strings.Replace(valid, testRepo.String(), "other/repository", 1),
		"missing parent":      strings.Replace(valid, `,"parent":null`, "", 1),
		"empty parent":        parentObservationJSON(`{}`),
		"missing parent repo": parentObservationJSON(`{"databaseId":900,"number":42}`),
		"self parent":         parentObservationJSON(fmt.Sprintf(`{"databaseId":700,"number":7,"repository":{"nameWithOwner":%q}}`, testRepo.String())),
		"partial response":    strings.TrimSuffix(valid, "}") + `,"errors":[{"type":"FORBIDDEN"}]}`,
		"decode failure":      `<html>proxy error</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, respondJSON(http.StatusOK, body))
			parent, err := f.Client.ObserveParentIssue(t.Context(), testRepo, 7, 700)
			if err == nil || parent != nil {
				t.Fatalf("incomplete evidence treated as parent absence: parent=%+v err=%v", parent, err)
			}
		})
	}
}

func TestObserveParentIssuePreservesFailureAndAccounting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{name: "REST not found is not absence", status: http.StatusNotFound, body: `{"message":"Not Found"}`, want: github.ErrNotFound},
		{name: "HTTP rate limit", status: http.StatusTooManyRequests, body: `{"message":"rate limit"}`, want: github.ErrRateLimited},
		{name: "GraphQL rate limit", status: http.StatusOK, body: `{"data":null,"errors":[{"type":"RATE_LIMITED"}]}`, want: github.ErrRateLimited},
		{name: "GraphQL extension rate limit", status: http.StatusOK, body: `{"data":null,"errors":[{"extensions":{"code":"RATE_LIMITED"}}]}`, want: github.ErrRateLimited},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`, want: github.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var finishes int
			var finishedErr error
			f := newCustomFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "17")
				respondJSON(tc.status, tc.body)(w, r)
			}, func(opts *github.Options) {
				opts.RequestContext = func(ctx context.Context) (context.Context, func(error), error) {
					return ctx, func(err error) { finishes++; finishedErr = err }, nil
				}
			})
			a := newTicketAdapter(t, f)
			child := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.Origin(), RemoteID: "700"}, Locator: ticketLocator(a)}
			parent, err := a.ObserveParent(t.Context(), child)
			if parent != nil || !errors.Is(err, tc.want) || finishes != 1 || !errors.Is(finishedErr, tc.want) {
				t.Fatalf("failure/accounting lost: parent=%+v err=%v finishes=%d finishErr=%v", parent, err, finishes, finishedErr)
			}
			if errors.Is(tc.want, github.ErrRateLimited) {
				var limited *github.RateLimitError
				if !errors.As(err, &limited) || limited.RetryAfter != 17*time.Second {
					t.Fatalf("retry deferral lost: %v", err)
				}
			}
		})
	}
}

func TestObserveParentIssueUsesEnterpriseGraphQLEndpoint(t *testing.T) {
	t.Parallel()
	f := newCustomFixture(t, respondJSON(http.StatusOK, parentObservationJSON("null")), func(opts *github.Options) {
		opts.APIBase += "/proxy/api/v3"
	})
	if _, err := f.Client.ObserveParentIssue(t.Context(), testRepo, 7, 700); err != nil {
		t.Fatal(err)
	}
	if got := f.Only(t).Path; got != "/proxy/api/graphql" {
		t.Fatalf("endpoint ignored configured Enterprise prefix: %s", got)
	}
}

func TestObserveParentIssueRefusesUnsupportedEndpointAndInvalidArguments(t *testing.T) {
	t.Parallel()
	f := newCustomFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected network request") }, func(opts *github.Options) {
		opts.APIBase += "/unsupported"
	})
	if _, err := f.Client.ObserveParentIssue(t.Context(), testRepo, 7, 700); err == nil {
		t.Fatal("unsupported endpoint did not fail")
	}
	if _, err := f.Client.ObserveParentIssue(t.Context(), testRepo, 7, 0); !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("missing immutable ID accepted: %v", err)
	}
	var zero github.Client
	if _, err := zero.ObserveParentIssue(t.Context(), testRepo, 7, 700); !errors.Is(err, github.ErrUninitialized) {
		t.Fatalf("zero client did not refuse: %v", err)
	}
}

func TestObserveParentIssuePreservesCancellationAndSchedulingDeferral(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := newFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("cancelled query reached server") })
	if parent, err := f.Client.ObserveParentIssue(ctx, testRepo, 7, 700); parent != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query returned absence: parent=%+v err=%v", parent, err)
	}
	refused := errors.New("connection deferred")
	f = newCustomFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("deferred query reached server") }, func(opts *github.Options) {
		opts.RequestContext = func(ctx context.Context) (context.Context, func(error), error) {
			return ctx, nil, refused
		}
	})
	if parent, err := f.Client.ObserveParentIssue(t.Context(), testRepo, 7, 700); parent != nil || !errors.Is(err, refused) {
		t.Fatalf("scheduling deferral lost: parent=%+v err=%v", parent, err)
	}
}
