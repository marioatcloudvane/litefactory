package github_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/secrets"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// These fixtures exercise real SDK decoding and network I/O, not live GitHub
// validator support or production savings.
func TestPollingCacheHTTPRepresentations(t *testing.T) {
	for _, endpoint := range []string{"issue", "parent", "comments"} {
		t.Run(endpoint, func(t *testing.T) {
			var step atomic.Int32
			var observations []github.RequestObservation
			f := newCustomFixture(t, func(w http.ResponseWriter, r *http.Request) {
				n := step.Add(1)
				want := ""
				if n == 2 || n == 3 {
					want = `"v1"`
				}
				if n == 4 {
					want = `"v2"`
				}
				if got := r.Header.Get("If-None-Match"); got != want {
					t.Errorf("request %d validator %q want %q", n, got, want)
				}
				if r.Header.Get("Authorization") == "" {
					t.Error("missing authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				if n == 2 {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				w.Header().Set("X-RateLimit-Resource", "core")
				w.Header().Set("X-RateLimit-Limit", "5000")
				w.Header().Set("X-RateLimit-Remaining", fmt.Sprint(5000-n))
				w.Header().Set("X-RateLimit-Reset", "2000000000")
				version := 1
				if n >= 3 {
					version = 2
				}
				w.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
				if n == 4 {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				if endpoint == "comments" {
					_, _ = fmt.Fprintf(w, `[{"id":%d,"body":"body%d"}]`, version, version)
				} else {
					_, _ = fmt.Fprintf(w, `{"id":%d,"number":%d,"title":"body%d"}`, version, version, version)
				}
			}, func(o *github.Options) {
				o.Observe = func(v github.RequestObservation) { observations = append(observations, v) }
			})
			for _, want := range []int{1, 1, 2, 2} {
				var got int
				var err error
				switch endpoint {
				case "issue":
					var issue github.Issue
					issue, err = f.Client.GetIssue(t.Context(), testRepo, 7)
					got = issue.Number
				case "parent":
					var issue github.Issue
					issue, err = f.Client.GetParentIssue(t.Context(), testRepo, 7)
					got = issue.Number
				case "comments":
					var comments []github.Comment
					comments, err = f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
					if len(comments) == 1 {
						got = int(comments[0].ID)
					}
				}
				if err != nil || got != want {
					t.Fatalf("got %d, %v; want %d", got, err, want)
				}
			}
			if len(observations) != 4 {
				t.Fatalf("observations: %v", observations)
			}
			for i, want := range []int{200, 304, 200, 304} {
				o := observations[i]
				if o.StatusCode != want || o.Endpoint != endpoint || o.Page != (endpoint == "comments") || !o.Authenticated {
					t.Errorf("observation %d: %+v", i, o)
				}
				if i == 1 {
					if o.CoreRateValid {
						t.Error("warm response reused historical quota")
					}
				} else if !o.CoreRateValid || o.Remaining != 4999-i {
					t.Errorf("fresh quota missing: %+v", o)
				}
			}
		})
	}
}

func TestPollingCacheHTTPFailuresInvalidate(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		for _, endpoint := range []string{"issue", "parent", "comments"} {
			t.Run(fmt.Sprintf("%s/%d", endpoint, status), func(t *testing.T) {
				var step atomic.Int32
				f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
					n := step.Add(1)
					if n == 2 && r.Header.Get("If-None-Match") != `"v1"` {
						t.Error("expected revalidation")
					}
					if n == 3 && r.Header.Get("If-None-Match") != "" {
						t.Error("failed representation retained")
					}
					w.Header().Set("Content-Type", "application/json")
					if n == 2 {
						w.WriteHeader(status)
						_, _ = fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					}
					w.Header().Set("ETag", `"v1"`)
					if endpoint == "comments" {
						_, _ = fmt.Fprint(w, `[{"id":1}]`)
					} else {
						_, _ = fmt.Fprint(w, `{"id":1,"number":1}`)
					}
				})
				fetch := func() error {
					switch endpoint {
					case "issue":
						_, err := f.Client.GetIssue(t.Context(), testRepo, 7)
						return err
					case "parent":
						_, err := f.Client.GetParentIssue(t.Context(), testRepo, 7)
						return err
					default:
						_, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
						return err
					}
				}
				if err := fetch(); err != nil {
					t.Fatal(err)
				}
				if err := fetch(); err == nil {
					t.Fatal("failed validation returned stale success")
				}
				if err := fetch(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPollingCacheHTTPChangedTailAndFailure(t *testing.T) {
	var phase atomic.Int32
	var requests atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		page := r.URL.Query().Get("page")
		p := phase.Load()
		w.Header().Set("Content-Type", "application/json")
		if page != "2" {
			if r.Header.Get("If-None-Match") != "" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"first"`)
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?per_page=100&page=2>; rel="next"`, r.Host, r.URL.Path))
			_, _ = fmt.Fprint(w, `[{"id":10}]`)
			return
		}
		if p == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"message":"tail failed"}`)
			return
		}
		if p == 3 {
			cancel()
			<-r.Context().Done()
			return
		}
		if p == 0 {
			w.Header().Set("ETag", `"tail1"`)
			_, _ = fmt.Fprint(w, `[{"id":20}]`)
			return
		}
		w.Header().Set("ETag", `"tail2"`)
		if r.Header.Get("If-None-Match") == `"tail2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = fmt.Fprint(w, `[{"id":20},{"id":30}]`)
	})
	for _, tc := range []struct {
		phase int32
		after int64
		want  []int64
	}{{0, 0, []int64{10, 20}}, {1, 20, []int64{30}}, {1, 0, []int64{10, 20, 30}}} {
		phase.Store(tc.phase)
		comments, err := f.Client.ListCommentsAfter(ctx, testRepo, 7, tc.after)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, c := range comments {
			ids = append(ids, c.ID)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Fatalf("ids %v want %v", ids, tc.want)
		}
	}
	for _, p := range []int32{2, 3} {
		phase.Store(p)
		comments, err := f.Client.ListCommentsAfter(ctx, testRepo, 7, 20)
		if err == nil || comments != nil {
			t.Fatalf("partial success on phase %d: %v, %v", p, comments, err)
		}
		if p == 3 && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	}
	if requests.Load() != 10 {
		t.Fatalf("requests %d want 10, two per traversal", requests.Load())
	}
}

func TestStalledBodyTimeoutComparison(t *testing.T) {
	t.Logf("runtime: %s", runtime.Version())
	for _, h2 := range []bool{false, true} {
		for _, endpoint := range []string{"sub_issues", "issue"} {
			for _, explicit := range []bool{false, true} {
				t.Run(fmt.Sprintf("http2=%t/%s/context=%t", h2, endpoint, explicit), func(t *testing.T) {
					cancelled := make(chan struct{})
					server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if (r.ProtoMajor == 2) != h2 {
							t.Errorf("protocol %s", r.Proto)
						}
						if r.Header.Get("Authorization") == "" {
							t.Error("missing auth")
						}
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("ETag", `"stalled"`)
						w.WriteHeader(http.StatusOK)
						if err := http.NewResponseController(w).Flush(); err != nil {
							t.Error(err)
							return
						}
						select {
						case <-r.Context().Done():
							close(cancelled)
						case <-t.Context().Done():
						}
					}))
					server.EnableHTTP2 = h2
					server.StartTLS()
					defer server.Close()
					hc := server.Client()
					hc.Timeout = 200 * time.Millisecond
					ctx := t.Context()
					if explicit {
						hc.Timeout = 3 * time.Second
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
						defer cancel()
					}
					c, err := github.New(github.Options{APIBase: server.URL, HTTPClient: hc, Token: secrets.NewToken("offline-token")})
					if err != nil {
						t.Fatal(err)
					}
					start := time.Now()
					if endpoint == "sub_issues" {
						_, err = c.ListSubIssues(ctx, testRepo, 7)
					} else {
						_, err = c.GetIssue(ctx, testRepo, 7)
					}
					if err == nil {
						t.Fatal("stalled read succeeded")
					}
					if elapsed := time.Since(start); elapsed > 2*time.Second {
						t.Fatalf("read took %s: %v", elapsed, err)
					}
					select {
					case <-cancelled:
					case <-time.After(time.Second):
						t.Fatal("server did not observe cancellation")
					}
					t.Logf("cancelled: %v", err)
				})
			}
		}
	}
}
