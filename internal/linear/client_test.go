package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(Config{Origin: server.URL, WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key"}, secrets.NewToken("sensitive-token"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func issueJSON(id, project string) string {
	return fmt.Sprintf(`{"id":%q,"identifier":"LIN-1","url":"https://linear.app/test/issue/LIN-1","title":"Title","description":"Body","creator":{"id":"owner","active":true},"team":{"id":"team"},"project":{"id":%q},"state":{"id":"open","type":"unstarted"},"labels":{"nodes":[],"pageInfo":{"hasNextPage":false}}}`, id, project)
}
func TestFailuresStayExplicitAndSecretSafe(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		mutation   bool
		kind       ticket.FailureKind
	}{{"graphql", `{"data":{"value":{}},"errors":[{"message":"sensitive-token"}]}`, false, ticket.Unavailable}, {"lost mutation", `{"data":null,"errors":[{"message":"sensitive-token"}]}`, true, ticket.Uncertain}, {"malformed", `sensitive-token`, true, ticket.Uncertain}, {"null", `{"data":null}`, false, ticket.Unavailable}} {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "sensitive-token" {
					t.Error("missing credential")
				}
				_, _ = w.Write([]byte(tt.body))
			})
			var out any
			err := c.query(context.Background(), "test", "query { viewer { id } }", nil, tt.mutation, &out)
			var failure *ticket.Failure
			if !errors.As(err, &failure) || failure.Kind != tt.kind {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), "sensitive-token") {
				t.Fatal("credential exposed")
			}
		})
	}
}
func TestReadChecksScopeAndImmutableIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, id, project string
		wantErr           bool
	}{{"opaque identity", "uuid-ticket", "project", false}, {"moved issue", "uuid-ticket", "other", true}, {"replaced issue", "other-ticket", "project", true}} {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, issueJSON(tt.id, tt.project))
			})
			ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
			got, err := c.Read(context.Background(), ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("read error=%v", err)
			}
			if err == nil && (got.Ref.Locator.Key != "uuid-ticket" || got.DisplayKey != "LIN-1") {
				t.Fatalf("opaque identity replaced: %+v", got)
			}
		})
	}
}
func TestRedirectNeverCarriesCredentials(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := c.Publisher(context.Background())
	if err == nil || received {
		t.Fatal("followed redirect or claimed success")
	}
}
func TestCreationUsesPersistedUUIDAndRejectsMissingReceipt(t *testing.T) {
	uuid := "573796c5-4a0c-4fe7-a5c8-260be55f17b0"
	var mutationInput map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(req.Query, "issueCreate") {
			if err := json.Unmarshal(req.Variables["input"], &mutationInput); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(w, `{"data":{"issueCreate":{"success":true,"issue":%s}}}`, issueJSON(uuid, "project"))
			return
		}
		t.Fatalf("unexpected query %s", req.Query)
	})
	body := mustBody(t, "Body")
	title := mustBody(t, "Title")
	got, err := c.CreateWithID(context.Background(), c.Container(), uuid, title, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref.Identity.RemoteID != uuid || mutationInput["id"] != uuid || mutationInput["teamId"] != "team" || mutationInput["projectId"] != "project" {
		t.Fatalf("creation correlation missing %+v", mutationInput)
	}
}
func TestDiscoveryContinuationBindsSourceAndCoverage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"data":{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":true,"endCursor":"opaque-cursor"}}}}`, issueJSON("uuid-ticket", "project"))
	})
	page, err := c.ChangesPage(context.Background(), c.Container(), time.Time{}, ticket.DiscoveryCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Complete || page.Next.Value == "" {
		t.Fatal("partial page claimed coverage")
	}
	_, err = c.ReplyChangesPage(context.Background(), c.Container(), time.Time{}, page.Next, 10)
	if err == nil {
		t.Fatal("issue cursor accepted for replies")
	}
}

func mustBody(t *testing.T, text string) redact.Body {
	t.Helper()
	body, err := redact.New(redact.Options{}).Apply(context.Background(), text, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestIncompletePagesDoNotAdvanceCoverage(t *testing.T) {
	for _, body := range []string{`{"data":{"issues":{}}}`, `{"data":{"issues":{"nodes":[],"pageInfo":{}}}}`, `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.ChangesPage(context.Background(), c.Container(), time.Time{}, ticket.DiscoveryCursor{}, 10); err == nil {
			t.Fatal("incomplete page established coverage")
		}
	}
}
func TestMissingMutationSuccessKeepsCreationUncertain(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"data":{"issueCreate":{"issue":%s}}}`, issueJSON("573796c5-4a0c-4fe7-a5c8-260be55f17b0", "project"))
	})
	_, err := c.CreateWithID(context.Background(), c.Container(), "573796c5-4a0c-4fe7-a5c8-260be55f17b0", mustBody(t, "Title"), mustBody(t, "Body"))
	var failed *ticket.Failure
	if !errors.As(err, &failed) || failed.Kind != ticket.Uncertain {
		t.Fatalf("missing success error=%v", err)
	}
}
func TestMappedModesPreserveCanonicalWorkflowMeaning(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})
	c.config.ModeLabels = map[string]string{"plan": "plan-id"}
	var i issue
	if err := json.Unmarshal([]byte(issueJSON("issue-id", "project")), &i); err != nil {
		t.Fatal(err)
	}
	i.Labels.Nodes = append(i.Labels.Nodes, struct{ ID, Name string }{"plan-id", "custom provider name"}, struct{ ID, Name string }{"unrelated", "owner label"})
	snapshot, err := c.snapshot(i)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Labels) != 2 || snapshot.Labels[0] != "Mode: Plan" || snapshot.Labels[1] != "owner label" {
		t.Fatalf("labels=%v", snapshot.Labels)
	}
}

func TestInvalidReadCursorHasNeutralRecoveryFailure(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Error("GraphQL endpoint missing")
		}
		_, _ = w.Write([]byte(`{"errors":[{"message":"invalid cursor sensitive-token","extensions":{"code":"INVALID_CURSOR"}}]}`))
	})
	_, err := c.ChangesPage(context.Background(), c.Container(), time.Time{}, ticket.DiscoveryCursor{}, 10)
	if !errors.Is(err, ticket.ErrInvalidCursor) || strings.Contains(err.Error(), "sensitive-token") {
		t.Fatalf("cursor error=%v", err)
	}
	var result any
	err = c.query(context.Background(), "mutation", "mutation { issueCreate { success } }", nil, true, &result)
	var failed *ticket.Failure
	if errors.Is(err, ticket.ErrInvalidCursor) || !errors.As(err, &failed) || failed.Kind != ticket.Uncertain {
		t.Fatalf("mutation gained retry permission: %v", err)
	}
}

func TestReplyMutationsRejectForeignQualifiedIdentity(t *testing.T) {
	requests := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	reply := ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: "github", Origin: c.origin, RemoteID: "comment"}, Author: ticket.Actor{Identity: c.identity("owner")}}
	if err := c.EditReply(context.Background(), reply, mustBody(t, "edited")); err == nil {
		t.Fatal("foreign reply accepted for editing")
	}
	if err := c.React(context.Background(), reply, "eyes"); err == nil {
		t.Fatal("foreign reply accepted for reaction")
	}
	if requests != 0 {
		t.Fatal("foreign identity reached transport")
	}
}

func TestIssueReadRejectsUnobservedLabelCoverage(t *testing.T) {
	for _, missing := range []string{`"pageInfo":{"hasNextPage":false}`, `"hasNextPage":false`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			body := strings.Replace(issueJSON("ticket", "project"), missing, "", 1)
			body = strings.ReplaceAll(body, ",}", "}")
			_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, body)
		})
		if _, err := c.Resolve(context.Background(), c.locator("ticket")); err == nil {
			t.Fatal("unobserved labels accepted as complete")
		}
	}
}

func TestModeMutationRejectsTicketMovedBetweenReads(t *testing.T) {
	reads := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(request.Query, "mutation") {
			t.Fatal("moved ticket reached mutation")
		}
		reads++
		project := "project"
		if reads > 1 {
			project = "other-project"
		}
		_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, issueJSON("ticket", project))
	})
	c.config.ModeLabels = map[string]string{"plan": "mode-label"}
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	if err := c.SetMode(context.Background(), ref, "plan"); err == nil {
		t.Fatal("moved ticket accepted")
	}
}

func TestQuotaFailuresCarrySafeSchedulingAdvice(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     string
		mutation bool
	}{
		{"http", http.StatusTooManyRequests, `sensitive-token`, false},
		{"graphql400", http.StatusBadRequest, `{"errors":[{"message":"sensitive-token","extensions":{"code":"RATELIMITED"}}]}`, false},
		{"graphql200", http.StatusOK, `{"errors":[{"extensions":{"code":"RATELIMITED"}}]}`, false},
		{"uncertain mutation", http.StatusBadRequest, `{"errors":[{"extensions":{"code":"RATELIMITED"}}]}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			var result any
			err := c.query(t.Context(), "quota", "query { viewer { id } }", nil, test.mutation, &result)
			var quota *ticket.RateLimitError
			var failed *ticket.Failure
			if !errors.As(err, &quota) || quota.RetryAfter != 2*time.Minute || strings.Contains(err.Error(), "sensitive-token") {
				t.Fatalf("unsafe or missing quota advice: %v", err)
			}
			if test.mutation && (!errors.As(err, &failed) || failed.Kind != ticket.Uncertain) {
				t.Fatalf("quota advice granted mutation retry: %v", err)
			}
		})
	}
}

func TestQuotaDelayUsesDocumentedExhaustedBuckets(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		headers http.Header
		want    time.Duration
	}{
		{"missing", http.Header{}, time.Minute},
		{"http date", http.Header{"Retry-After": []string{now.Add(2 * time.Minute).Format(http.TimeFormat)}}, 2 * time.Minute},
		{"request reset", http.Header{"X-Ratelimit-Requests-Remaining": []string{"0"}, "X-Ratelimit-Requests-Reset": []string{fmt.Sprint(now.Add(3 * time.Minute).UnixMilli())}}, 3 * time.Minute},
		{"complexity reset", http.Header{"X-Ratelimit-Complexity-Remaining": []string{"0"}, "X-Ratelimit-Complexity-Reset": []string{fmt.Sprint(now.Add(4 * time.Minute).UnixMilli())}}, 4 * time.Minute},
		{"available bucket", http.Header{"X-Ratelimit-Requests-Remaining": []string{"1"}, "X-Ratelimit-Requests-Reset": []string{fmt.Sprint(now.Add(time.Hour).UnixMilli())}}, time.Minute},
		{"bounded seconds", http.Header{"Retry-After": []string{"9223372036854775807"}}, 24 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := quotaDelay(test.headers, now); got != test.want {
				t.Fatalf("delay=%s want=%s", got, test.want)
			}
		})
	}
}

func TestMissingModeMappingIdentity(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unmapped label performed remote I/O") })
	c.config.ModeLabels = map[string]string{"plan": "p", "implement": "i", "validate": "v"}
	for _, label := range []string{"research", "Mode: Research", "spike", "Mode: Spike", "unknown"} {
		err := c.AddLabel(t.Context(), ticket.Ref{}, label)
		var failure *ticket.Failure
		var missing *ticket.MissingModeMapping
		if !errors.As(err, &failure) || failure.Kind != ticket.Refused {
			t.Fatalf("%s: %v", label, err)
		}
		if label == "unknown" {
			if errors.As(err, &missing) {
				t.Fatal("unknown label classified as missing canonical mode")
			}
		} else if !errors.As(fmt.Errorf("wrapped: %w", err), &missing) || canonicalModeLabel(missing.Mode) != canonicalModeLabel(label) {
			t.Fatalf("%s lost canonical mode: %v", label, err)
		}
	}
}

func TestMissingModeLabels(t *testing.T) {
	for _, labels := range []map[string]string{
		{"plan": "p", "implement": "i", "validate": "v"},
		{"Mode: Plan": "p", "Mode: Implement": "i", "Mode: Validate": "v"},
		{"Plan": "p", "Implement": "i", "Validate": "v"},
	} {
		if got := strings.Join(MissingModeLabels(labels), ","); got != "research,spike" {
			t.Fatalf("missing=%s", got)
		}
		labels["research"], labels["Mode: Spike"] = "r", "s"
		if got := MissingModeLabels(labels); len(got) != 0 {
			t.Fatalf("complete=%v", got)
		}
	}
}

func TestRequestObservations(t *testing.T) {
	t.Parallel()
	reset := time.Now().Add(time.Hour).UnixMilli()
	for _, tt := range []struct {
		name, limit, remaining, reset string
		valid                         bool
	}{
		{"valid", "1000", "900", fmt.Sprint(reset), true},
		{"absent", "", "", "", false},
		{"partial", "1000", "900", "", false},
		{"malformed", "many", "900", fmt.Sprint(reset), false},
		{"overflow", "1000", "900", "9223372036854775808", false},
		{"zero limit", "0", "0", fmt.Sprint(reset), false},
		{"negative", "1000", "-1", fmt.Sprint(reset), false},
		{"inconsistent", "1000", "1001", fmt.Sprint(reset), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Requests-Limit", tt.limit)
				w.Header().Set("X-RateLimit-Requests-Remaining", tt.remaining)
				w.Header().Set("X-RateLimit-Requests-Reset", tt.reset)
				w.Header().Set("X-RateLimit-Endpoint-Requests-Limit", "1")
				w.Header().Set("X-RateLimit-Endpoint-Requests-Remaining", "0")
				w.Header().Set("X-RateLimit-Complexity-Limit", "999999")
				w.Header().Set("X-RateLimit-Complexity-Remaining", "0")
				_, _ = fmt.Fprint(w, `{"data":{"value":{}}}`)
			})
			var observations []RequestObservation
			var inherited int
			ctx := WithRequestObserver(t.Context(), func(RequestObservation) { inherited++ })
			ctx = WithRequestObserver(ctx, func(o RequestObservation) { observations = append(observations, o) })
			var out any
			if err := c.query(ctx, "test", "query { value }", nil, false, &out); err != nil {
				t.Fatal(err)
			}
			if inherited != 1 || len(observations) != 1 {
				t.Fatalf("observations=%v inherited=%d", observations, inherited)
			}
			o := observations[0]
			if o.StatusCode != http.StatusOK || o.RequestRateValid != tt.valid || o.RetryAfter != 0 {
				t.Fatalf("observation=%+v", o)
			}
			if tt.valid && (o.Limit != 1000 || o.Remaining != 900 || o.Reset.UnixMilli() != reset) {
				t.Fatalf("request tuple replaced: %+v", o)
			}
		})
	}
}

func TestRequestObservationFailuresAndOptionalInspection(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusOK, http.StatusBadRequest} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"errors":[{"extensions":{"code":"RATELIMITED"}}]}`)
			})
			var got []RequestObservation
			ctx := WithRequestObserver(t.Context(), func(o RequestObservation) { got = append(got, o) })
			var out any
			if err := c.query(ctx, "test", "query { value }", nil, false, &out); err == nil {
				t.Fatal("refusal lost")
			}
			if len(got) != 1 || got[0].RetryAfter != 2*time.Minute || got[0].StatusCode != status {
				t.Fatalf("observations=%+v", got)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch {
		case strings.Contains(body.Query, "viewer"):
			_, _ = fmt.Fprint(w, `{"data":{"viewer":{"id":"bot","active":true,"organization":{"id":"workspace"}}}}`)
		case strings.Contains(body.Query, "project(id:"):
			_, _ = fmt.Fprint(w, `{"data":{"team":{"id":"team","organization":{"id":"workspace"}},"project":{"id":"project","teams":{"nodes":[{"id":"team"}],"pageInfo":{"hasNextPage":false}}}}}`)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})
	var got []RequestObservation
	ctx := WithRequestObserver(t.Context(), func(o RequestObservation) { got = append(got, o) })
	if _, err := c.ContainerIdentity(ctx, "team/project"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("container read must use one request: %+v", got)
	}
	got = nil
	if _, err := c.Inspect(ctx); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].RetryAfter != time.Minute {
		t.Fatalf("hidden refusal or missing attempt: %+v", got)
	}
}

type failingObservationTransport struct{}

func (failingObservationTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

func TestRequestObservationTransportFailure(t *testing.T) {
	t.Parallel()
	c, err := New(Config{Origin: "https://fixture.invalid", WorkspaceID: "workspace", TeamID: "team", ProjectID: "project"}, secrets.NewToken("offline-token"), &http.Client{Transport: failingObservationTransport{}})
	if err != nil {
		t.Fatal(err)
	}
	var got []RequestObservation
	ctx := WithRequestObserver(t.Context(), func(o RequestObservation) { got = append(got, o) })
	if _, err := c.Publisher(ctx); err == nil {
		t.Fatal("missing transport error")
	}
	if len(got) != 1 || got[0] != (RequestObservation{}) {
		t.Fatalf("observations=%+v", got)
	}
}

func TestReplyHistoryCombinedIdentity(t *testing.T) {
	for _, mode := range []string{"complete", "later identity", "null issue", "project", "team", "labels", "partial error", "missing cursor", "repeated cursor", "excess", "over bound", "wrong comment", "foreign reference"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct {
					Query     string
					Variables map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if !strings.Contains(request.Query, "issue(id: $id) { "+issueFields) || !strings.Contains(request.Query, "comments(filter: $filter") || !strings.Contains(request.Query, commentFields) || request.Variables["id"] != "uuid-ticket" {
					t.Errorf("missing combined identity query: %+v", request)
				}
				filter, _ := json.Marshal(request.Variables["filter"])
				if string(filter) != `{"issue":{"id":{"eq":"uuid-ticket"}}}` {
					t.Errorf("filter=%s", filter)
				}
				issue := issueJSON("uuid-ticket", "project")
				switch mode {
				case "later identity":
					if calls == 2 {
						issue = issueJSON("other", "project")
					}
				case "null issue":
					issue = "null"
				case "project":
					issue = issueJSON("uuid-ticket", "other")
				case "team":
					issue = strings.ReplaceAll(issue, `"team"`, `"other"`)
				case "labels":
					issue = strings.ReplaceAll(issue, `"hasNextPage":false`, `"hasNextPage":true`)
				}
				next := calls < 100 || mode == "over bound"
				cursor := fmt.Sprint(calls)
				if mode == "missing cursor" {
					cursor = ""
				}
				if mode == "repeated cursor" {
					cursor = "same"
				}
				nodes := []any{}
				if mode == "excess" {
					nodes = make([]any, 101)
				}
				if mode == "wrong comment" {
					nodes = append(nodes, map[string]any{"id": "reply", "user": map[string]string{"id": "owner"}, "issue": map[string]any{"id": "other"}})
				}
				result := map[string]any{"data": map[string]any{"issue": json.RawMessage(issue), "comments": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": next, "endCursor": cursor}}}}
				if mode == "partial error" {
					result["errors"] = []any{map[string]string{"message": "failed"}}
				}
				if err := json.NewEncoder(w).Encode(result); err != nil {
					t.Error(err)
				}
			})
			ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
			if mode == "foreign reference" {
				ref.Locator.Container = "foreign"
			}
			for _, paged := range []bool{false, true} {
				calls = 0
				var page ticket.ReplyPage
				var err error
				if paged {
					page, err = c.RepliesPage(t.Context(), ref, 100, 100)
				} else {
					page, err = c.RepliesAfter(t.Context(), ref, ticket.Cursor{})
				}
				wantSuccess := mode == "complete" || (paged && mode == "over bound")
				if (err == nil) != wantSuccess {
					t.Fatalf("paged=%t error=%v", paged, err)
				}
				if mode == "complete" && (!page.Complete || calls != 100) {
					t.Fatalf("complete=%t calls=%d", page.Complete, calls)
				}
				if mode == "foreign reference" && calls != 0 {
					t.Fatal("foreign reference made a request")
				}
				if !wantSuccess && page.Complete {
					t.Fatal("failed history claimed completion")
				}
			}
		})
	}
}

func TestUpdateContentNormalizedReadback(t *testing.T) {
	marExpected, marObserved := mar12Pair(t)
	metadata := "\n<!-- lf:plan-content v=1 data={} -->"
	for _, tt := range []struct{ name, written, observed, title, failure string }{
		{name: "MAR-12", written: marExpected, observed: marObserved},
		{name: "CRLF", written: "a\r\nb", observed: "a\nb"},
		{name: "trailing space", written: "a \nb", observed: "a\nb"},
		{name: "table", written: "| A | B |\n| --- | :---: |\n| one | two |", observed: "|A|B|\n|-----|:-----:|\n|one|two|"},
		{name: "escape", written: `Hello\!`, observed: "Hello!"},
		{name: "combined metadata", written: "Hello\\! \r\n| A | B |\r\n| --- | :---: |\r\n| one | two |" + metadata, observed: "Hello!\n|A|B|\n|-----|:-----:|\n|one|two|" + metadata},
		{name: "prose with matching metadata", written: "a" + metadata, observed: "b" + metadata, failure: "conflict"},
		{name: "cell", written: "| A |\n| --- |\n| one |", observed: "| A |\n| --- |\n| two |", failure: "conflict"},
		{name: "link", written: "[x](https://a.test)", observed: "[x](https://b.test)", failure: "conflict"},
		{name: "code", written: "```\na \n```", observed: "```\na\n```", failure: "conflict"},
		{name: "hard break", written: "a  \nb", observed: "a\nb", failure: "conflict"},
		{name: "title", written: "a", observed: "a", title: "Edited", failure: "conflict"},
		{name: "marker", written: "<!-- lf:plan-story key=a -->", observed: "<!-- lf:plan-story key=b -->", failure: "conflict"},
		{name: "payload", written: metadata, observed: strings.ReplaceAll(metadata, "{}", "{ }"), failure: "conflict"},
		{name: "malformed metadata", written: metadata, observed: metadataFence + "\nbroken", failure: "uncertain"},
		{name: "stale base", written: "a", observed: "a", failure: "stale"},
		{name: "missing confirmation", written: "a", observed: "a", failure: "confirmation"},
		{name: "failed readback", written: "a", observed: "a", failure: "readback"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutations := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query     string
					Variables map[string]json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(req.Query, "issueUpdate") {
					mutations++
					var input struct{ Title, Description string }
					if err := json.Unmarshal(req.Variables["input"], &input); err != nil {
						t.Error(err)
						return
					}
					if input.Description != EncodeContent(tt.written) || input.Title != "Title" {
						t.Error("mutation did not contain prepared content")
					}
					_, _ = fmt.Fprintf(w, `{"data":{"issueUpdate":{"success":%t,"issue":{"id":"uuid-ticket"}}}}`, tt.failure != "confirmation")
					return
				}
				raw := issueJSON("uuid-ticket", "project")
				if mutations > 0 {
					if tt.failure == "readback" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					body, err := json.Marshal(EncodeContent(tt.observed))
					if err != nil {
						t.Error(err)
						return
					}
					raw = strings.Replace(raw, `"description":"Body"`, `"description":`+string(body), 1)
					if tt.title != "" {
						raw = strings.Replace(raw, `"title":"Title"`, `"title":"Edited"`, 1)
					}
				}
				_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, raw)
			})
			ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
			prior, err := c.Read(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			revision := prior.Revision
			if tt.failure == "stale" {
				revision = "stale"
			}
			got, err := c.UpdateContent(t.Context(), ref, revision, mustBody(t, "Title"), mustBody(t, tt.written))
			if tt.failure != "" {
				kind := ticket.Conflict
				if tt.failure == "uncertain" || tt.failure == "confirmation" || tt.failure == "readback" {
					kind = ticket.Uncertain
				}
				var failure *ticket.Failure
				if !errors.As(err, &failure) || failure.Kind != kind {
					t.Fatalf("error = %v; want %s", err, kind)
				}
				if tt.failure == "stale" && mutations != 0 {
					t.Fatal("stale revision dispatched")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fetched, err := c.Read(t.Context(), ref)
			if err != nil || got.Body != tt.observed || got.ProviderBody != EncodeContent(tt.observed) || got.Revision != fetched.Revision || mutations != 1 {
				t.Fatalf("actual readback lost: %+v %v", got, err)
			}
		})
	}
}

func TestReadbackDifferenceDiagnostic(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	c := testClient(t, func(http.ResponseWriter, *http.Request) {})
	ref := ticket.Ref{Identity: ticket.Identity{RemoteID: "MAR-12"}}
	for _, title := range []string{"Title", "Changed"} {
		output.Reset()
		c.logContentDifference(t.Context(), ref, "Title", "same\nsensitive-token"+strings.Repeat("x", 500), ticket.Snapshot{Title: title, Body: "same\nother"})
		var got struct {
			Ticket, Field, Expected, Observed string
			Line                              int
		}
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Ticket != "MAR-12" || strings.Contains(output.String(), "sensitive-token") || len(got.Expected) > 256 {
			t.Fatalf("unsafe diagnostic: %s", output.String())
		}
		if title == "Title" && (got.Field != "body" || got.Line != 2) {
			t.Fatal("wrong body difference")
		}
		if title != "Title" && (got.Field != "title" || got.Line != 1) {
			t.Fatal("wrong title difference")
		}
	}
	output.Reset()
	prefix := strings.Repeat("y", 300)
	c.logContentDifference(t.Context(), ref, "Title", "same\n"+prefix+"EXPECTED-TAIL sensitive-token", ticket.Snapshot{Title: "Title", Body: "same\n" + prefix + "OBSERVED-TAIL sensitive-token"})
	var got struct {
		Expected, Observed string
		Line, Column       int
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "sensitive-token") || len(got.Expected) > 256 || len(got.Observed) > 256 {
		t.Fatalf("unsafe diagnostic: %s", output.String())
	}
	if got.Line != 2 || got.Column != len(prefix)+1 || !strings.Contains(got.Expected, "EXPECTED-TAIL") || !strings.Contains(got.Observed, "OBSERVED-TAIL") {
		t.Fatalf("difference window missing: %s", output.String())
	}
}

func TestMetadataFooterProviderReadWriteRecovery(t *testing.T) {
	for _, initial := range []string{legacyStoryFixture, footerStoryFixture} {
		t.Run(initial[:5], func(t *testing.T) {
			providerBody := initial
			mutations := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query     string
					Variables map[string]json.RawMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(req.Query, "issueUpdate") || strings.Contains(req.Query, "commentUpdate") {
					var input struct{ Description, Body string }
					if err := json.Unmarshal(req.Variables["input"], &input); err != nil {
						t.Error(err)
						return
					}
					raw := input.Description
					if input.Body != "" {
						raw = input.Body
					}
					if raw != strings.Replace(footerStoryFixture, "Story.", `Story\. `, 1) {
						t.Errorf("unexpected write: %q", raw)
					}
					// Normalize only the visible prose and wrapper spacing, never
					// regenerate metadata from the authored canonical body.
					providerBody = strings.Replace(raw, `Story\. `, "Story.", 1)
					providerBody = strings.Replace(providerBody, "metadata\n```", "metadata\n\n```", 1)
					providerBody = strings.Replace(providerBody, "\n```\n+++", "\n```\n\n+++\n", 1)
					mutations++
				}
				encoded, _ := json.Marshal(providerBody)
				comment := fmt.Sprintf(`{"id":"comment","body":%s,"user":{"id":"owner","active":true},"issue":{"id":"uuid-ticket","team":{"id":"team"},"project":{"id":"project"}}}`, encoded)
				switch {
				case strings.Contains(req.Query, "commentUpdate"):
					_, _ = fmt.Fprintf(w, `{"data":{"commentUpdate":{"success":true,"comment":%s}}}`, comment)
				case strings.Contains(req.Query, "comment(id:"):
					_, _ = fmt.Fprintf(w, `{"data":{"comment":%s}}`, comment)
				case strings.Contains(req.Query, "issueUpdate"):
					_, _ = fmt.Fprint(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"uuid-ticket"}}}}`)
				default:
					raw := strings.Replace(issueJSON("uuid-ticket", "project"), `"description":"Body"`, `"description":`+string(encoded), 1)
					_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, raw)
				}
			})
			ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
			prior, err := c.Read(t.Context(), ref)
			if err != nil || prior.Body != canonicalStoryFixture || prior.ProviderBody != initial || prior.Representation != Representation {
				t.Fatalf("issue read: %+v %v", prior, err)
			}
			reply, err := c.ReadReply(t.Context(), ref, "comment")
			if err != nil || reply.Body != canonicalStoryFixture || reply.ProviderBody != initial || reply.Representation != Representation {
				t.Fatalf("reply read: %+v %v", reply, err)
			}
			written := strings.Replace(canonicalStoryFixture, "Story.", `Story\. `, 1)
			updated, err := c.UpdateContent(t.Context(), ref, prior.Revision, mustBody(t, "Title"), mustBody(t, written))
			if err != nil || updated.Body != canonicalStoryFixture || updated.ProviderBody != providerBody {
				t.Fatalf("update: %+v %v", updated, err)
			}
			if err := c.EditReply(t.Context(), reply, mustBody(t, written)); err != nil {
				t.Fatal(err)
			}
			recovered, err := c.ReadReply(t.Context(), ref, "comment")
			if err != nil || recovered.Body != canonicalStoryFixture || recovered.ProviderBody != providerBody || recovered.Representation != Representation {
				t.Fatalf("recovery read: %+v %v", recovered, err)
			}
			// A raw-only provider edit still invalidates the recorded revision.
			providerBody = strings.Replace(providerBody, "metadata\n\n```", "metadata\n```", 1)
			_, err = c.UpdateContent(t.Context(), ref, updated.Revision, mustBody(t, "Title"), mustBody(t, written))
			var failure *ticket.Failure
			if !errors.As(err, &failure) || failure.Kind != ticket.Conflict || mutations != 2 {
				t.Fatalf("stale raw revision dispatched: %v, mutations=%d", err, mutations)
			}
		})
	}
}

func TestMetadataFooterCommentConfirmationRejectsEdits(t *testing.T) {
	for _, altered := range []string{
		strings.Replace(footerStoryFixture, "Story.", "Owner edit.", 1),
		strings.Replace(footerStoryFixture, "key=def", "key=other", 1),
		strings.TrimSuffix(footerStoryFixture, "+++"),
	} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			var req struct{ Query string }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			raw := legacyStoryFixture
			if strings.Contains(req.Query, "commentUpdate") {
				raw = altered
			}
			body, _ := json.Marshal(raw)
			comment := fmt.Sprintf(`{"id":"comment","body":%s,"user":{"id":"owner"},"issue":{"id":"uuid-ticket","team":{"id":"team"},"project":{"id":"project"}}}`, body)
			switch {
			case strings.Contains(req.Query, "commentUpdate"):
				_, _ = fmt.Fprintf(w, `{"data":{"commentUpdate":{"success":true,"comment":%s}}}`, comment)
			case strings.Contains(req.Query, "comment(id:"):
				_, _ = fmt.Fprintf(w, `{"data":{"comment":%s}}`, comment)
			default:
				_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, issueJSON("uuid-ticket", "project"))
			}
		})
		ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
		reply, err := c.ReadReply(t.Context(), ref, "comment")
		if err != nil {
			t.Fatal(err)
		}
		err = c.EditReply(t.Context(), reply, mustBody(t, canonicalStoryFixture))
		var failure *ticket.Failure
		if !errors.As(err, &failure) || failure.Kind != ticket.Uncertain {
			t.Fatalf("altered confirmation accepted: %v", err)
		}
	}
}

func TestSearchIssuesStaysInTheProjectAndBounded(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Variables struct {
				Filter map[string]any
				First  int
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Variables.First != 5 || request.Variables.Filter["project"] == nil || request.Variables.Filter["or"] == nil {
			t.Errorf("unexpected search request: %+v %v", request, err)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":true,"endCursor":"c"}}}}`, issueJSON("issue-id", "project"))
	})
	found, err := c.SearchIssues(context.Background(), "make  test", 5)
	if err != nil || len(found) != 1 || found[0].DisplayKey != "LIN-1" {
		t.Fatalf("search = %+v, %v", found, err)
	}
	if _, err = c.SearchIssues(context.Background(), "", 5); err == nil {
		t.Fatal("empty search accepted")
	}
}
