package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/secrets"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync"
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
						// A stalled body never ends. Returning normally would
						// let the server finish the response (an empty chunked
						// body): when the client's TLS close_notify reaches the
						// server before the client drops the socket, the client
						// can read that clean end and decode success. Aborting
						// keeps the body unfinished, as the test means it.
						panic(http.ErrAbortHandler)
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

// fullFirstPage serves a listing whose first page holds exactly 100 items and,
// as GitHub does when there is nothing after it, no Link header. Page 1 keeps
// one ETag throughout and is answered 304, again without a Link header, when
// the client revalidates it: appending item 101 lands on page 2 and leaves
// page 1's body, and so its validator, unchanged (#1402). tail answers page 2.
type fullFirstPage struct {
	mu       sync.Mutex
	issues   bool // serve issue payloads rather than comments
	tail     []int64
	tailFail int // non-zero: page 2 answers this status
	pages    []string
	statuses []int
}

func (s *fullFirstPage) item(id int64) map[string]any {
	if s.issues {
		return map[string]any{"id": id, "number": id, "title": "offline"}
	}
	return map[string]any{"id": id, "body": "offline"}
}

func (s *fullFirstPage) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := r.URL.Query().Get("page")
	s.pages = append(s.pages, page)
	status := http.StatusOK
	defer func() { s.statuses = append(s.statuses, status) }()
	w.Header().Set("Content-Type", "application/json")
	var items []map[string]any
	switch page {
	case "", "1":
		w.Header().Set("ETag", `"first"`)
		if r.Header.Get("If-None-Match") == `"first"` {
			status = http.StatusNotModified
			w.WriteHeader(status)
			return
		}
		for id := int64(1); id <= 100; id++ {
			items = append(items, s.item(id))
		}
	case "2":
		if s.tailFail != 0 {
			status = s.tailFail
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, `{"message":"tail failed"}`)
			return
		}
		items = []map[string]any{}
		for _, id := range s.tail {
			items = append(items, s.item(id))
		}
	default:
		status = http.StatusNotFound
		w.WriteHeader(status)
		return
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (s *fullFirstPage) set(tail []int64, fail int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tail, s.tailFail = tail, fail
}

func (s *fullFirstPage) traffic() ([]string, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, statuses := s.pages, s.statuses
	s.pages, s.statuses = nil, nil
	return pages, statuses
}

// TestPollingCacheHTTPFullFirstPageGainsSecondPage is #1402 at the client: a
// cached full page 1 without a next link must not hide comment 101 behind a
// 304 that restores the old "no next page" headers (C1, C4).
func TestPollingCacheHTTPFullFirstPageGainsSecondPage(t *testing.T) {
	s := &fullFirstPage{}
	f := newFixture(t, s.serve)

	for round := range 2 {
		comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) != 100 || comments[0].ID != 1 || comments[99].ID != 100 {
			t.Fatalf("round %d: %d comments, want 1..100", round, len(comments))
		}
		pages, _ := s.traffic()
		if !reflect.DeepEqual(pages, []string{"", "2"}) {
			t.Fatalf("round %d: pages %q, want page 1 then one page 2 probe", round, pages)
		}
	}

	s.set([]int64{101}, 0)
	for round := range 2 {
		comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) != 1 || comments[0].ID != 101 {
			t.Fatalf("round %d: comments %v, want only comment 101", round, comments)
		}
		pages, statuses := s.traffic()
		if !reflect.DeepEqual(pages, []string{"", "2"}) || statuses[0] != http.StatusNotModified {
			t.Fatalf("round %d: pages %q statuses %v, want a 304 page 1 then page 2", round, pages, statuses)
		}
	}
}

// TestPollingCacheHTTPFullFirstPageTailFailureRefusesTheRead covers C3: a
// failed page 2 probe behind a cached full page 1 is an error, never a
// successful read of page 1 alone.
func TestPollingCacheHTTPFullFirstPageTailFailureRefusesTheRead(t *testing.T) {
	s := &fullFirstPage{}
	f := newFixture(t, s.serve)
	if _, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 0); err != nil {
		t.Fatal(err)
	}
	s.set([]int64{101}, http.StatusInternalServerError)
	comments, err := f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 100)
	if err == nil || comments != nil {
		t.Fatalf("partial success: %v, %v", comments, err)
	}
	s.set([]int64{101}, 0)
	comments, err = f.Client.ListCommentsAfter(t.Context(), testRepo, 7, 100)
	if err != nil || len(comments) != 1 || comments[0].ID != 101 {
		t.Fatalf("after recovery: %v, %v", comments, err)
	}
}
