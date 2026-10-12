package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	body, err := redact.New(redact.Options{}).Apply(context.Background(), text, 1000)
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
