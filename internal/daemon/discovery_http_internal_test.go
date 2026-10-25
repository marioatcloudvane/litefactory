package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// This is an offline GitHub boundary. All client decoding, conditional cache,
// daemon discovery and SQLite cursor/admission operations remain real.
type discoveryHTTPFixture struct {
	d          *Daemon
	client     *github.Client
	server     *httptest.Server
	now        time.Time
	mu         sync.Mutex
	histories  map[string]int
	versions   map[string]int
	failPage   int
	failStatus int
	requests   int
	statuses   map[int]int
	actorID    int64
	logs       bytes.Buffer
	// listings answers the repository issue and comment listings with nothing
	// new; without it they are not found.
	listings bool
}

// budgetAtOneList leaves traffic for exactly one maximum history plus the
// scalar reads around it, and never for two, so a test asserting that a second
// concurrent list defers says what it means whatever discoveryBurst is set to.
// Without it such a test silently stops testing anything the moment the burst
// grows large enough to fund two lists at once. Call it immediately before the
// first list: earlier, and the setup's own scalar reads eat the margin.
func (f *discoveryHTTPFixture) budgetAtOneList() {
	f.d.discovery.mu.Lock()
	defer f.d.discovery.mu.Unlock()
	f.d.discovery.tokens = float64(discoveryListBound + discoveryListBound/2)
}

func newDiscoveryHTTPFixture(t *testing.T) *discoveryHTTPFixture {
	t.Helper()
	f := &discoveryHTTPFixture{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), histories: map[string]int{}, versions: map[string]int{}, statuses: map[int]int{}, actorID: 42}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "discovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(&f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99}, logger)
	if err != nil {
		t.Fatal(err)
	}
	f.client, err = github.New(github.Options{APIBase: f.server.URL, Token: secrets.NewToken("offline-token"), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.d = &Daemon{db: db, forge: f.client, filter: filter, log: logger}
	f.d.discovery.tick(f.now)
	if err = f.d.discoveryProbe(f.d.discoveryContext(t.Context()), github.Repo{Owner: "acme", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *discoveryHTTPFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(5000-f.requests))
	w.Header().Set("X-RateLimit-Resource", "core")
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC).Unix(), 10))
	status := 200
	defer func() { f.statuses[status]++ }()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if f.listings && len(parts) >= 4 && parts[3] == "issues" && (len(parts) == 4 || (len(parts) == 5 && parts[4] == "comments")) {
		_, _ = w.Write([]byte("[]"))
		return
	}
	if len(parts) < 5 {
		status = 404
		w.WriteHeader(status)
		return
	}
	number, _ := strconv.Atoi(parts[4])
	if number == 1 {
		status = 404
		w.WriteHeader(status)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page == 0 {
		page = 1
	}
	if len(parts) == 6 && parts[5] == "comments" {
		if f.failPage == page {
			status = f.failStatus
			if status == 403 {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Remaining", "0")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"fixture failure"}`))
			return
		}
		count := max(1, f.histories[r.URL.Path])
		if page < count {
			w.Header().Set("Link", fmt.Sprintf("<%s%s?per_page=100&page=%d>; rel=\"next\"", f.server.URL, r.URL.Path, page+1))
		}
	}
	etag := fmt.Sprintf("\"%s-%d-%d\"", r.URL.Path, page, f.versions[r.URL.Path])
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		status = 304
		w.WriteHeader(status)
		return
	}
	if len(parts) == 6 && parts[5] == "comments" {
		id := int64(number*1000 + page + f.versions[r.URL.Path]*100)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": id, "body": "offline owner refinement", "user": map[string]any{"id": f.actorID, "login": "owner"}}})
		return
	}
	if len(parts) == 6 && parts[5] == "parent" {
		number = 100
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": number * 10, "number": number, "state": "open", "title": "offline fixture"})
}

func (f *discoveryHTTPFixture) task(t *testing.T, repoName string) (*task, store.ManagedStory) {
	t.Helper()
	repo, err := f.d.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: repoName, RemoteURL: "git@github.com:acme/" + repoName + ".git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.d.db.GetOrCreateSession(t.Context(), repo, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.d.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	story := store.ManagedStory{ParentTask: session.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 2000, BodyHash: "offline", State: "planning"}
	if err = f.d.db.PutManagedStory(t.Context(), story); err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	return &task{repo: repo, forgeRepo: github.Repo{Owner: "acme", Name: repoName}, issue: github.Issue{ID: 1000, Number: 100}, session: session, ref: ref, log: f.d.log}, story
}
func (f *discoveryHTTPFixture) discover(t *testing.T, task *task, story store.ManagedStory) error {
	t.Helper()
	return f.d.discoverStoryComments(f.d.discoveryContext(t.Context()), task, f.client, story, nil)
}
func (f *discoveryHTTPFixture) cursor(t *testing.T, task *task, child int) int64 {
	t.Helper()
	v, err := f.d.db.StoryDiscoveryCursor(t.Context(), task.session.ID, child)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDiscoveryHTTP_OfflineColdWarmChangedMultiParent(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	a, sa := f.task(t, "a")
	b, sb := f.task(t, "b")
	for _, phase := range []string{"cold", "warm", "changed"} {
		if phase == "changed" {
			f.versions["/repos/acme/b/issues/200/comments"]++
		}
		before := map[int]int{}
		for k, v := range f.statuses {
			before[k] = v
		}
		counts := &discoveryCounts{}
		ctx := github.WithRequestObserver(f.d.discoveryContext(t.Context()), counts.observe)
		for _, pair := range []struct {
			task  *task
			story store.ManagedStory
		}{{a, sa}, {b, sb}} {
			if err := f.d.discoverStoryComments(ctx, pair.task, f.client, pair.story, nil); err != nil {
				t.Fatal(err)
			}
		}
		f.d.discoverySummary(ctx, "offline-"+phase, counts)
		got200, got304 := f.statuses[200]-before[200], f.statuses[304]-before[304]
		want200, want304 := 6, 0
		if phase == "warm" {
			want200, want304 = 0, 6
		}
		if phase == "changed" {
			want200, want304 = 1, 5
		}
		if got200 != want200 || got304 != want304 {
			t.Fatalf("offline %s:200=%d 304=%d want %d/%d", phase, got200, got304, want200, want304)
		}
		t.Logf("OFFLINE fixture %s: actual requests=%d status_200=%d status_304=%d comment_pages=2 parents=2", phase, got200+got304, got200, got304)
	}
	if got := f.cursor(t, a, 200); got != 200001 {
		t.Fatalf("a cursor %d", got)
	}
	if got := f.cursor(t, b, 200); got != 200101 {
		t.Fatalf("b cursor %d", got)
	}
	pending, err := f.d.db.ListPendingStoryRefinements(t.Context(), b.session.ID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if !strings.Contains(f.logs.String(), "not_modified_responses") {
		t.Fatalf("missing structured request accounting: %s", &f.logs)
	}
	for _, old := range []string{"local_deferrals", "traffic_available", "outstanding_reserved", "discovery request summary"} {
		if strings.Contains(f.logs.String(), old) {
			t.Fatalf("request accounting still uses the old name %q: %s", old, &f.logs)
		}
	}
}

func TestDiscoveryHTTP_MaximumHistoryDefersThenResumesAcrossRepositories(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	a, sa := f.task(t, "long")
	b, sb := f.task(t, "short")
	f.histories["/repos/acme/long/issues/200/comments"] = 100
	f.budgetAtOneList()
	if err := f.discover(t, a, sa); err != nil {
		t.Fatal(err)
	}
	if got := f.cursor(t, a, 200); got != 200100 {
		t.Fatalf("100-page cursor=%d", got)
	}
	if err := f.discover(t, b, sb); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("short history error=%v", err)
	}
	if got := f.cursor(t, b, 200); got != 0 {
		t.Fatalf("deferred cursor=%d", got)
	}
	f.d.discovery.tick(f.now.Add(time.Minute))
	// A competing list cannot consume the reservation needed by the deferred
	// short parent's history even after the traffic bucket refills.
	if _, err := f.d.discoveryComments(f.d.discoveryContext(t.Context()), a.forgeRepo, 200, 0); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("queue bypass=%v", err)
	}
	if err := f.discover(t, b, sb); err != nil {
		t.Fatalf("deferred short did not resume: %v", err)
	}
	if got := f.cursor(t, a, 200); got != 200100 {
		t.Fatalf("prior child progress lost: %d", got)
	}
	if got := f.cursor(t, b, 200); got != 200001 {
		t.Fatalf("short cursor=%d", got)
	}
	if err := f.discover(t, a, sa); err != nil {
		t.Fatalf("queued 100-page history did not finish: %v", err)
	}
}

func TestDiscoveryHTTP_LaterFailurePreservesCursorAndRealRefusal(t *testing.T) {
	for _, status := range []int{502, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newDiscoveryHTTPFixture(t)
			a, sa := f.task(t, "a")
			f.histories["/repos/acme/a/issues/200/comments"] = 3
			f.failPage = 2
			f.failStatus = status
			err := f.discover(t, a, sa)
			if err == nil {
				t.Fatal("failure returned success")
			}
			if got := f.cursor(t, a, 200); got != 0 {
				t.Fatalf("partial cursor=%d", got)
			}
			pending, e := f.d.db.ListPendingStoryRefinements(t.Context(), a.session.ID)
			if e != nil || len(pending) != 0 {
				t.Fatalf("partial admission=%v %v", pending, e)
			}
			if status == 403 {
				var rate *github.RateLimitError
				if !errors.As(err, &rate) || rate.RetryAfter != time.Minute {
					t.Fatalf("not real refusal: %v", err)
				}
			}
			f.d.discovery.mu.Lock()
			reserved := f.d.discovery.reserved
			f.d.discovery.mu.Unlock()
			if reserved != 0 {
				t.Fatalf("leaked %d reservations", reserved)
			}
		})
	}
}

func TestDiscoveryHTTP_CancelledCallReleasesReservation(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	ctx, cancel := context.WithCancel(f.d.discoveryContext(t.Context()))
	cancel()
	comments, err := f.d.discoveryComments(ctx, github.Repo{Owner: "acme", Name: "a"}, 200, 0)
	if !errors.Is(err, context.Canceled) || comments != nil {
		t.Fatalf("cancelled result=%v %v", comments, err)
	}
	if f.d.discovery.reserved != 0 {
		t.Fatalf("reservation leaked: %d", f.d.discovery.reserved)
	}
}

func TestDiscoveryHTTP_RefinementRotationRetainsSuccessfulChildProgress(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	parent, first := f.task(t, "a")
	second := first
	second.Key = "US-2"
	second.IssueNumber = 201
	second.IssueID = 2010
	if err := f.d.db.PutManagedStory(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	f.actorID = 7 // Non-owner comments still advance discovery, without active work.
	f.budgetAtOneList()
	f.histories["/repos/acme/a/issues/200/comments"] = 100
	handled, err := f.d.refineStories(f.d.discoveryContext(t.Context()), parent)
	if !handled || !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got := f.cursor(t, parent, 200); got != 200100 {
		t.Fatalf("successful child cursor=%d", got)
	}
	if got := f.cursor(t, parent, 201); got != 0 {
		t.Fatalf("deferred child cursor=%d", got)
	}
	rotation, err := f.d.db.StoryDiscoveryRotation(t.Context(), parent.session.ID)
	if err != nil || rotation != 200 {
		t.Fatalf("rotation=%d err=%v", rotation, err)
	}
	f.d.discovery.tick(f.now.Add(time.Minute))
	_, err = f.d.refineStories(f.d.discoveryContext(t.Context()), parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.cursor(t, parent, 201); got != 201001 {
		t.Fatalf("resumed child cursor=%d", got)
	}
	if got := f.cursor(t, parent, 200); got != 200100 {
		t.Fatalf("earlier successful progress lost=%d", got)
	}
}

func TestDiscoveryHTTP_ConcurrentReservationsSettleActualResponses(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	boundary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		f.serve(w, r)
	}))
	defer boundary.Close()
	defer unblock()
	client, err := github.New(github.Options{APIBase: boundary.URL, Token: secrets.NewToken("offline-concurrent")})
	if err != nil {
		t.Fatal(err)
	}
	f.d.forge = client
	done := make(chan error, 2)
	for _, number := range []int{200, 201} {
		go func() {
			_, err := f.d.discoveryIssue(f.d.discoveryContext(t.Context()), github.Repo{Owner: "acme", Name: "a"}, number)
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
	f.d.discovery.mu.Lock()
	reserved := f.d.discovery.reserved
	f.d.discovery.mu.Unlock()
	if reserved != 2 {
		t.Errorf("concurrent outstanding reservations=%d, want2", reserved)
	}
	unblock()
	for range 2 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if f.d.discovery.reserved != 0 {
		t.Fatalf("settled reservations=%d", f.d.discovery.reserved)
	}
	if f.d.discovery.spent != 3 {
		t.Fatalf("probe plus two charged responses=%d", f.d.discovery.spent)
	}
}

// A held background history must not block the client used by active execution.
// The HTTP boundary supplies the rendezvous; all client and reservation code is real.
func TestDiscoveryHTTP_ActiveReadBypassesHeldBackgroundHistory(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	arrived := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	boundary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/200/comments") {
			close(arrived)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		f.serve(w, r)
	}))
	defer boundary.Close()
	defer unblock()
	client, err := github.New(github.Options{APIBase: boundary.URL, Token: secrets.NewToken("offline-active")})
	if err != nil {
		t.Fatal(err)
	}
	f.d.forge = client
	background := make(chan error, 1)
	// One fixture goroutine, bounded by ctx and joined before the test returns.
	go func() {
		_, err := f.d.discoveryComments(f.d.discoveryContext(ctx), github.Repo{Owner: "acme", Name: "a"}, 200, 0)
		background <- err
	}()
	select {
	case <-arrived:
	case <-ctx.Done():
		t.Fatal("waiting for held background history: ", ctx.Err())
	}
	comments, err := client.ListCommentsAfter(f.d.discoveryContext(ctx), github.Repo{Owner: "acme", Name: "a"}, 201, 0)
	if err != nil || len(comments) != 1 {
		t.Fatalf("active read while background reservation held: comments=%v error=%v", comments, err)
	}
	f.d.discovery.mu.Lock()
	reserved, remaining := f.d.discovery.reserved, f.d.discovery.remaining
	f.d.discovery.mu.Unlock()
	if reserved != listAdmission || remaining != 4998 {
		t.Errorf("active response must update quota without consuming background reservation: reserved=%d remaining=%d", reserved, remaining)
	}
	unblock()
	select {
	case err := <-background:
		if err != nil {
			t.Fatal("background history: ", err)
		}
	case <-ctx.Done():
		t.Fatal("waiting for background history settlement: ", ctx.Err())
	}
}
