package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The cast every test in this package works with. The two numeric ids are the
// point: every classification decision litefactory makes compares them, and
// none of them compares a handle (D-I).
const (
	testOwnerName = "acme"
	testRepoName  = "acme-api"
	testIssue     = 123

	ownerLogin = "marioatcloudvane"
	ownerID    = int64(4242)

	botLogin = "litefactory-bot"
	botID    = int64(9999)

	strangerLogin = "octo-stranger"
	strangerID    = int64(777)

	testToken = "ghp_notarealtokenatall0000"
)

// testInterval is inside the D-C clamp, so a daemon built with it starts. No
// test ever waits for it: ticks are driven by calling Tick, or through an
// injected channel.
const testInterval = 10 * time.Second

// ---------------------------------------------------------------------------
// The forge
// ---------------------------------------------------------------------------

// fakeUser is a comment or issue author as GitHub reports one.
type fakeUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

func owner() fakeUser    { return fakeUser{ID: ownerID, Login: ownerLogin, Type: "User"} }
func bot() fakeUser      { return fakeUser{ID: botID, Login: botLogin, Type: "Bot"} }
func stranger() fakeUser { return fakeUser{ID: strangerID, Login: strangerLogin, Type: "User"} }

// fakeLabel is a label object the way GitHub sends one: a name plus fields
// (colour, description, id) this fixture never needs and so never encodes.
// It has to be a struct rather than a bare string because the real payload —
// internal/github's issuePayload — decodes []*labelPayload, not []string
// (T-4), and a fixture that sent strings would never exercise that decode.
type fakeLabel struct {
	Name string `json:"name"`
}

// toFakeLabels is the usual way a test builds an issue's label list: label
// names in, the objects GitHub actually sends out.
func toFakeLabels(names ...string) []fakeLabel {
	if len(names) == 0 {
		return nil
	}
	labels := make([]fakeLabel, len(names))
	for i, name := range names {
		labels[i] = fakeLabel{Name: name}
	}
	return labels
}

type fakeIssue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
	User   fakeUser `json:"user"`
	// Assignees is who the issue is assigned to. GitHub always sends the
	// member, as [] for an unassigned issue, so it is not omitempty: an
	// absent member is a shape the client is tested against directly, not one
	// this fixture should be producing by accident.
	Assignees []fakeUser `json:"assignees"`
	// Labels is every label on the issue. Nil is fine: an unlabelled issue is
	// the overwhelmingly common case and GitHub's own [] round-trips the same
	// way through the decoder as omitting the field entirely.
	Labels      []fakeLabel    `json:"labels"`
	HTMLURL     string         `json:"html_url"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	PullRequest map[string]any `json:"pull_request,omitempty"`
}

type fakeComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      fakeUser  `json:"user"`
	HTMLURL   string    `json:"html_url"`
	IssueURL  string    `json:"issue_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// issue is which issue the comment is on. It is not part of GitHub's shape
	// and is not serialised.
	issue int `json:"-"`
}

// forgeServer is GitHub, over a real HTTP transport.
//
// It is an httptest.Server rather than a hand-written double of the client
// because the behaviours under test are HTTP behaviours: a 403 with rate-limit
// headers has to be classified as a *github.RateLimitError with a RetryAfter on
// it, and a connection that is hung up has to surface as a transport error. A
// fake that returned those values directly would assert that this package
// handles what it was handed, not what GitHub sends.
type forgeServer struct {
	t      *testing.T
	server *httptest.Server

	// now is the fixture's clock. The forge stamps issues and comments with it
	// so that "updated since the last tick" means the same thing on both sides
	// of the wire, and no test depends on the wall clock.
	now func() time.Time

	mu            sync.Mutex
	issues        []fakeIssue
	comments      []fakeComment
	nextCommentID int64

	// Recorded traffic.
	sinceValues      []string
	listIssues       int
	listComments     int
	listRepoComments int
	postedBodies     []string
	postedIssues     []int
	reactedTo        []int64
	reactionCalls    int

	// Injected failures. Each is called with the 1-based call number and
	// reports whether it handled the response itself.
	onListIssues   func(w http.ResponseWriter, call int) bool
	onListComments func(w http.ResponseWriter, call int) bool
	onPostComment  func(w http.ResponseWriter, call int) bool

	// order records the sequence of externally visible actions, so that "the
	// reaction happened before the run" is an assertion rather than a hope.
	order []string
}

func newForgeServer(t *testing.T, now func() time.Time) *forgeServer {
	t.Helper()

	f := &forgeServer{t: t, now: now, nextCommentID: 500_000}

	mux := http.NewServeMux()
	base := "/repos/" + testOwnerName + "/" + testRepoName
	mux.HandleFunc("GET "+base+"/issues", f.handleListIssues)
	mux.HandleFunc("GET "+base+"/issues/{number}", f.handleGetIssue)
	mux.HandleFunc("GET "+base+"/issues/comments", f.handleListRepoComments)
	mux.HandleFunc("GET "+base+"/issues/{number}/comments", f.handleListComments)
	mux.HandleFunc("POST "+base+"/issues/{number}/comments", f.handlePostComment)
	mux.HandleFunc("POST "+base+"/issues/comments/{id}/reactions", f.handleReaction)
	// A second registered repository, for the O(registered repos) assertion. It
	// has no issues: what is being counted is the number of listings, not what
	// they return.
	other := "/repos/" + testOwnerName + "/other-api"
	mux.HandleFunc("GET "+other+"/issues", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.listIssues++
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, []fakeIssue{})
	})
	mux.HandleFunc("GET "+other+"/issues/comments", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.listRepoComments++
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, []fakeComment{})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *forgeServer) handleListIssues(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.listIssues++
	call := f.listIssues
	f.sinceValues = append(f.sinceValues, r.URL.Query().Get("since"))
	hook := f.onListIssues
	issues := slices.Clone(f.issues)
	f.mu.Unlock()

	if hook != nil && hook(w, call) {
		return
	}
	// The since filter is inclusive, exactly as GitHub's is (D-A).
	since, _ := time.Parse(time.RFC3339, r.URL.Query().Get("since"))
	// And the state filter, also as GitHub applies it: open unless asked
	// otherwise, and "all" is the only way a closed issue comes back. The
	// daemon asks for open, so this is what keeps a closed issue out of *this*
	// listing — handleGetIssue still serves one, because GitHub does, which is
	// why the AC-2.22 skip cannot live in the query.
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	out := make([]fakeIssue, 0, len(issues))
	for _, issue := range issues {
		if state != "all" && issue.State != state {
			continue
		}
		if since.IsZero() || !issue.UpdatedAt.Before(since) {
			out = append(out, issue)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *forgeServer) handleListComments(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		http.Error(w, `{"message":"bad issue"}`, http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.listComments++
	call := f.listComments
	hook := f.onListComments
	var out []fakeComment
	for _, c := range f.comments {
		if c.issue == number {
			out = append(out, c)
		}
	}
	f.mu.Unlock()

	if hook != nil && hook(w, call) {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetIssue serves one issue, which is how the daemon fills in an issue it
// only learned about from the comment listing.
func (f *forgeServer) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		http.Error(w, `{"message":"bad issue"}`, http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, issue := range f.issues {
		if issue.Number == number {
			writeJSON(w, http.StatusOK, issue)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

// handleListRepoComments serves every comment in the repository written at or
// after `since`, which is the listing that actually notices a new comment.
func (f *forgeServer) handleListRepoComments(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.listRepoComments++
	comments := slices.Clone(f.comments)
	f.mu.Unlock()

	since, _ := time.Parse(time.RFC3339, r.URL.Query().Get("since"))
	out := make([]fakeComment, 0, len(comments))
	for _, c := range comments {
		if since.IsZero() || !c.UpdatedAt.Before(since) {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *forgeServer) handlePostComment(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		http.Error(w, `{"message":"bad issue"}`, http.StatusBadRequest)
		return
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"message":"bad body"}`, http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	call := len(f.postedBodies) + 1
	hook := f.onPostComment
	if hook == nil || !hookHandles(hook, w, call) {
		f.nextCommentID++
		id := f.nextCommentID
		f.postedBodies = append(f.postedBodies, payload.Body)
		f.postedIssues = append(f.postedIssues, number)
		f.order = append(f.order, fmt.Sprintf("post:%d", id))
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, fakeComment{
			ID:        id,
			Body:      payload.Body,
			User:      bot(),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		return
	}
	f.mu.Unlock()
}

// hookHandles calls an injected failure hook. It is a function so that the
// mutex-held call above reads as one expression.
func hookHandles(hook func(http.ResponseWriter, int) bool, w http.ResponseWriter, call int) bool {
	return hook(w, call)
}

func (f *forgeServer) handleReaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, `{"message":"bad comment"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.reactionCalls++
	f.reactedTo = append(f.reactedTo, id)
	f.order = append(f.order, fmt.Sprintf("react:%d", id))
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"id": 1, "content": "eyes"})
}

// addIssue registers an issue, replacing one with the same number.
func (f *forgeServer) addIssue(issue fakeIssue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue.State == "" {
		issue.State = "open"
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = f.now()
	}
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = issue.UpdatedAt
	}
	for i := range f.issues {
		if f.issues[i].Number == issue.Number {
			f.issues[i] = issue
			return
		}
	}
	f.issues = append(f.issues, issue)
}

// addComment appends a comment to an issue and touches the issue's update time,
// which is what GitHub does — measured on issue #4, whose comment landed at
// 19:14:47Z and whose issue read 19:14:48Z.
//
// What GitHub does *not* guarantee is that the issue listing reflects that
// bump before the poll cursor has moved past it. Tests that care about the lag
// say so explicitly, with [forgeServer.staleIssue] or by backdating a comment,
// rather than having the fixture pretend it always happens.
func (f *forgeServer) addComment(issue int, id int64, author fakeUser, body string) {
	f.addCommentAt(issue, id, author, body, f.now())
}

// addCommentAt writes a comment stamped at an arbitrary time. Backdating one is
// how a test models an index that caught up late: the comment is real and its
// timestamp is old, so only a listing window that reaches back far enough will
// return it.
func (f *forgeServer) addCommentAt(issue int, id int64, author fakeUser, body string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, fakeComment{
		ID: id, Body: body, User: author, CreatedAt: at, UpdatedAt: at, issue: issue,
		IssueURL: fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d",
			testOwnerName, testRepoName, issue),
	})
	for i := range f.issues {
		if f.issues[i].Number == issue {
			f.issues[i].UpdatedAt = at
		}
	}
}

// closeIssue closes an issue the way a human does: its state changes and its
// update time moves with it. Everything about it stays on the forge, and
// handleGetIssue keeps serving it — closed is a state, not a deletion.
func (f *forgeServer) closeIssue(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.issues {
		if f.issues[i].Number == number {
			f.issues[i].State = "closed"
			f.issues[i].UpdatedAt = f.now()
			return
		}
	}
	f.t.Fatalf("no issue %d to close", number)
}

// setLabels replaces an issue's labels, the way a maintainer relabeling it on
// GitHub does, and touches its update time the same way [forgeServer.addComment]
// does for a comment: relabeling is a real edit to the issue, and a tick that
// filtered on `since` has to see it (AC-2.10).
func (f *forgeServer) setLabels(number int, labels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.issues {
		if f.issues[i].Number == number {
			f.issues[i].Labels = toFakeLabels(labels...)
			f.issues[i].UpdatedAt = f.now()
			return
		}
	}
	f.t.Fatalf("no issue %d to relabel", number)
}

func (f *forgeServer) staleIssue(number int, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.issues {
		if f.issues[i].Number == number {
			f.issues[i].UpdatedAt = at
			return
		}
	}
	f.t.Fatalf("no issue %d to backdate", number)
}

func (f *forgeServer) snapshot() (posted []string, reacted []int64, order []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.postedBodies), slices.Clone(f.reactedTo), slices.Clone(f.order)
}

func (f *forgeServer) counts() (issues, comments, posts, reactions int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listIssues, f.listComments, len(f.postedBodies), f.reactionCalls
}

// repoCommentListings is how many times the repo-level comment listing was
// called. It is the second half of the per-tick API cost, the issue listing
// being the first.
func (f *forgeServer) repoCommentListings() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listRepoComments
}

func (f *forgeServer) since() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sinceValues)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeRateLimit answers the way GitHub refuses a call over quota: 403 with the
// rate-limit headers, which is what makes it a *github.RateLimitError rather
// than a permission failure (AC-2.12).
func writeRateLimit(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Resource", "core")
	writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
}

// hangUp answers with a truncated response and drops the connection, which is
// a genuine transport failure rather than an HTTP status the client could
// reason about (AC-2.13).
//
// The response has to start before the connection dies. net/http retries an
// idempotent request whose connection failed before any byte of a response
// arrived, so a bare close would be transparently repaired and the tick would
// succeed — which is the opposite of what is being tested.
func hangUp(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Fatalf("response writer does not support hijacking")
	}
	conn, buf, err := hijacker.Hijack()
	if err != nil {
		t.Fatalf("hijack: %v", err)
	}
	defer closeConn(conn)

	if _, err := buf.WriteString("HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 4096\r\n\r\n" +
		`[{"id":1`); err != nil {
		t.Errorf("write the truncated response: %v", err)
		return
	}
	if err := buf.Flush(); err != nil {
		t.Errorf("flush the truncated response: %v", err)
	}
}

func closeConn(conn net.Conn) { _ = conn.Close() }

// ---------------------------------------------------------------------------
// The clock
// ---------------------------------------------------------------------------

// testClock is the daemon's only clock seam. Backoff is expressed as "resume at
// this instant" precisely so that asserting on it costs a field assignment
// rather than a sleep.
//
// It advances a whole second per reading, and both of those matter. Advancing
// keeps every event strictly ordered without a test having to say so; a whole
// second is the resolution of GitHub's `since` filter, which is an RFC 3339
// timestamp — at millisecond steps the truncation would put two events in the
// same second and the cursor would appear not to move.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Second)
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// ---------------------------------------------------------------------------
// The turn
// ---------------------------------------------------------------------------

// fakeExecutor stands in for turn.Execute.
//
// internal/turn has its own tests over a real clone and a real database; what
// this package needs is control over what a turn *did*, so that the loop's
// response to a lock, a failure and a replay can be asserted without producing
// each of them for real.
type fakeExecutor struct {
	mu       sync.Mutex
	fn       func(ctx context.Context, req turn.Request) (turn.Result, error)
	requests []turn.Request
	order    *[]string
	orderMu  *sync.Mutex
}

func (e *fakeExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	e.mu.Lock()
	e.requests = append(e.requests, req)
	fn := e.fn
	e.mu.Unlock()

	if e.order != nil {
		e.orderMu.Lock()
		*e.order = append(*e.order, fmt.Sprintf("turn:%d", req.Trigger.CommentID))
		e.orderMu.Unlock()
	}
	return fn(ctx, req)
}

func (e *fakeExecutor) calls() []turn.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.requests)
}

func (e *fakeExecutor) triggers() []int64 {
	ids := make([]int64, 0, len(e.calls()))
	for _, req := range e.calls() {
		ids = append(ids, req.Trigger.CommentID)
	}
	return ids
}

func (e *fakeExecutor) set(fn func(ctx context.Context, req turn.Request) (turn.Result, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fn = fn
}

// ---------------------------------------------------------------------------
// The log sink
// ---------------------------------------------------------------------------

// syncBuffer is the sink every logger in a fixture writes into, and the only
// thing a test reads its logs back out of.
//
// slog's TextHandler does take a mutex around each Handle call, but that mutex
// belongs to the handler: it serialises that one handler's writes and nothing
// else. It does not reach a test goroutine calling String while the daemon is
// logging, and it never covered the older arrangement here at all — two
// handlers over one bytes.Buffer are two mutexes over one unsynchronised
// writer, which -race reports as soon as anything in the fixture runs
// concurrently. The fixture now builds exactly one handler (see newFixture) and
// this type closes the remaining gap, the one between the writer and the
// reader.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String makes a syncBuffer a fmt.Stringer, which is what keeps the lock
// invisible at the call sites: every failure message in this package formats
// the sink with %s or calls String directly, and both now read it held.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ---------------------------------------------------------------------------
// The fixture
// ---------------------------------------------------------------------------

type fixture struct {
	t     *testing.T
	forge *forgeServer
	db    *store.DB
	paths config.Paths
	repo  store.Repo
	turns *fakeExecutor
	clock *testClock

	// logs is the one sink; logger is the one handler over it. Both are on the
	// fixture so that every dependency a test builds — the github client, the
	// filter, the reporter, the daemon — logs through the same handler rather
	// than standing up a second one over the same buffer.
	logs   *syncBuffer
	logger *slog.Logger

	client   *github.Client
	filter   *turn.Filter
	reporter *turn.Reporter
	daemon   *daemon.Daemon
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	home := filepath.Join(t.TempDir(), "lfhome")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create LF_HOME: %v", err)
	}
	paths := config.NewPaths(home)

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	for _, identity := range []store.Identity{
		{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: ownerLogin, UserID: ownerID, Source: store.SourceGH},
		{Forge: store.ForgeGitHub, Role: store.RoleBot, Handle: botLogin, UserID: botID, Source: store.SourceForgeAPI},
	} {
		if _, err := db.UpsertIdentity(t.Context(), identity); err != nil {
			t.Fatalf("store the %s identity: %v", identity.Role, err)
		}
	}

	repo, err := db.UpsertRepo(t.Context(), store.Repo{
		Path:          filepath.Join(home, "clone"),
		Forge:         store.ForgeGitHub,
		Owner:         testOwnerName,
		Name:          testRepoName,
		RemoteURL:     "git@github.com:" + testOwnerName + "/" + testRepoName + ".git",
		BaseBranch:    "main",
		FeaturePrefix: naming.DefaultFeaturePrefix,
		WorktreeRoot:  filepath.Join(home, "worktrees"),
	})
	if err != nil {
		t.Fatalf("register the repository: %v", err)
	}

	clock := newTestClock()
	forge := newForgeServer(t, clock.Now)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	token := secrets.NewToken(testToken)

	client, err := github.New(github.Options{
		APIBase: forge.server.URL,
		Token:   token,
		Logger:  logger,
	})
	if err != nil {
		t.Fatalf("build the github client: %v", err)
	}

	identities, err := turn.LoadIdentities(t.Context(), db, store.ForgeGitHub)
	if err != nil {
		t.Fatalf("load identities: %v", err)
	}
	filter, err := turn.NewFilter(db, identities, logger)
	if err != nil {
		t.Fatalf("build the filter: %v", err)
	}

	renderer, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Logger: logger, Token: token, Environ: []string{}}),
		Limit:    github.CommentSizeLimit,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("build the renderer: %v", err)
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{
		DB: db, Forge: client, Renderer: renderer, Logger: logger,
	})
	if err != nil {
		t.Fatalf("build the reporter: %v", err)
	}

	f := &fixture{
		t:        t,
		forge:    forge,
		db:       db,
		paths:    paths,
		repo:     repo,
		clock:    clock,
		logs:     logs,
		logger:   logger,
		client:   client,
		filter:   filter,
		reporter: reporter,
	}
	f.turns = &fakeExecutor{fn: f.ranSuccessfully, order: &forge.order, orderMu: &forge.mu}
	f.daemon = f.newDaemon(daemon.Options{})
	return f
}

// newDaemon builds a daemon over the fixture's dependencies, letting a test
// override whichever it is about.
func (f *fixture) newDaemon(overrides daemon.Options) *daemon.Daemon {
	f.t.Helper()

	opts := daemon.Options{
		DB:            f.db,
		Forge:         f.client,
		Filter:        f.filter,
		Turn:          f.turns,
		Reporter:      f.reporter,
		InstanceID:    "daemon-instance-test",
		Interval:      testInterval,
		Logger:        f.logger,
		Now:           f.clock.Now,
		ShutdownGrace: 500 * time.Millisecond,
	}
	if overrides.DB != nil {
		opts.DB = overrides.DB
	}
	if overrides.Forge != nil {
		opts.Forge = overrides.Forge
	}
	if overrides.Filter != nil {
		opts.Filter = overrides.Filter
	}
	if overrides.Turn != nil {
		opts.Turn = overrides.Turn
	}
	if overrides.Reporter != nil {
		opts.Reporter = overrides.Reporter
	}
	if overrides.InstanceID != "" {
		opts.InstanceID = overrides.InstanceID
	}
	if overrides.Interval != 0 {
		opts.Interval = overrides.Interval
	}
	// Zero means "not overridden" here for the same reason it means "use the
	// default" in Options.validate, so the base opts keeps the default.
	if overrides.MaxConcurrentRuns != 0 {
		opts.MaxConcurrentRuns = overrides.MaxConcurrentRuns
	}
	if overrides.Ticks != nil {
		opts.Ticks = overrides.Ticks
	}
	if overrides.ShutdownGrace != 0 {
		opts.ShutdownGrace = overrides.ShutdownGrace
	}
	if overrides.Logger != nil {
		opts.Logger = overrides.Logger
	}
	if overrides.ToolProfiles != nil {
		opts.ToolProfiles = overrides.ToolProfiles
	}
	if overrides.ToolCeiling != nil {
		opts.ToolCeiling = overrides.ToolCeiling
	}

	d, err := daemon.New(opts)
	if err != nil {
		f.t.Fatalf("build the daemon: %v", err)
	}
	return d
}

// tick runs one tick and fails the test if it did not complete.
func (f *fixture) tick() daemon.TickResult {
	f.t.Helper()

	result, err := f.daemon.Tick(f.t.Context())
	if err != nil {
		f.t.Fatalf("Tick: %v\nlogs:\n%s", err, f.logs)
	}
	return result
}

// tickErr runs one tick and returns whatever it did, for the tests that are
// about failure.
func (f *fixture) tickErr() (daemon.TickResult, error) {
	f.t.Helper()

	return f.daemon.Tick(f.t.Context())
}

// settle runs the tick that plants the poll cursor.
//
// It is the first tick of a daemon's life and it is expected to find nothing:
// AC-2.9 initialises the cursor to "now", so an issue that already existed is
// not listed. Every test that wants to observe a first sight therefore settles
// first and registers its issue afterwards — which is also the order in which
// this happens on a real machine.
func (f *fixture) settle() {
	f.t.Helper()

	result := f.tick()
	if result.Issues != 0 {
		f.t.Fatalf("the settling tick listed %d issues, want none", result.Issues)
	}
}

// issue registers the task every test polls.
//
// It is deliberately inert: a stranger opened it, nobody is assigned to it, and
// its body is a statement of fact rather than an instruction. Nothing about it
// can start a run on its own, so a test that adds comments to it is testing
// only its comments. That is what makes it useless for a body test — use
// [fixture.assignedIssue], which says at the call site that the owner asked for
// something.
func (f *fixture) issue(title string) {
	f.t.Helper()

	f.forge.addIssue(fakeIssue{
		Number: testIssue,
		Title:  title,
		Body:   "The service has no way to say whether it is up.",
		User:   stranger(),
	})
}

// issueWithLabels is [fixture.issue] carrying one or more lf: labels from the
// moment it is opened, for a resolver test that wants a label present on the
// very first tick that sees the issue.
func (f *fixture) issueWithLabels(title string, labels ...string) {
	f.t.Helper()

	f.forge.addIssue(fakeIssue{
		Number: testIssue,
		Title:  title,
		Body:   "The service has no way to say whether it is up.",
		User:   stranger(),
		Labels: toFakeLabels(labels...),
	})
}

// relabel replaces the labels on the fixture's one issue (AC-2.10): a
// maintainer changing their mind about which profile a run should have,
// between one comment and the next.
func (f *fixture) relabel(labels ...string) {
	f.t.Helper()

	f.forge.setLabels(testIssue, labels...)
}

// assignedIssue registers an issue with the given body, assigned to the owner:
// the shape that runs its body as a first instruction (AC-2.16b).
//
// The author is the *stranger*, on purpose. The gate is assignment and not
// authorship — assigning an issue to yourself is the act of instruction, and
// anyone at all may open one — so the harder of the two cases is the default
// here, and a body test cannot pass by accident because the owner happened to
// write it. Where authorship is the point, [fixture.assignedIssueBy] says so.
func (f *fixture) assignedIssue(title, body string) {
	f.t.Helper()

	f.assignedIssueBy(stranger(), title, body)
}

// assignedIssueBy is [fixture.assignedIssue] with the issue's author spelled
// out — the owner's own issue, most usefully, which is what happens on a real
// machine when Mario opens one and assigns it to himself.
func (f *fixture) assignedIssueBy(author fakeUser, title, body string) {
	f.t.Helper()

	f.forge.addIssue(fakeIssue{
		Number:    testIssue,
		Title:     title,
		Body:      body,
		User:      author,
		Assignees: []fakeUser{owner()},
	})
}

func (f *fixture) session() store.Session {
	f.t.Helper()

	session, err := f.db.GetSessionByTaskRef(f.t.Context(),
		store.TaskRef(store.ForgeGitHub, testOwnerName, testRepoName, testIssue))
	if err != nil {
		f.t.Fatalf("read the session: %v", err)
	}
	return session
}

// watermark reads how far through the task's comments litefactory has got. It
// is the fact almost every test in this package turns on.
//
// A task litefactory has never seen, and one whose watermark has never been
// planted, both read as -1: "nothing has been consumed", which is materially
// different from "comment 0 has been consumed".
func (f *fixture) watermark() int64 {
	f.t.Helper()

	session, err := f.db.GetSessionByTaskRef(f.t.Context(),
		store.TaskRef(store.ForgeGitHub, testOwnerName, testRepoName, testIssue))
	if errors.Is(err, store.ErrNotFound) {
		return -1
	}
	if err != nil {
		f.t.Fatalf("read the session: %v", err)
	}

	watermark, exists, err := f.db.GetWatermark(f.t.Context(), session.ID)
	if err != nil {
		f.t.Fatalf("read the watermark: %v", err)
	}
	if !exists {
		return -1
	}
	return watermark.LastCommentID
}

func (f *fixture) cursor() store.PollCursor {
	f.t.Helper()

	cursor, err := f.db.GetPollCursor(f.t.Context())
	if err != nil {
		f.t.Fatalf("read the poll cursor: %v", err)
	}
	return cursor
}

func (f *fixture) runs() []store.Run {
	f.t.Helper()

	runs, err := f.db.ListRuns(f.t.Context(), f.session().ID)
	if err != nil {
		f.t.Fatalf("list the runs: %v", err)
	}
	return runs
}

func (f *fixture) comments() []store.Comment {
	f.t.Helper()

	comments, err := f.db.ListComments(f.t.Context(), f.session().ID)
	if err != nil {
		f.t.Fatalf("list the comments: %v", err)
	}
	return comments
}

// ranSuccessfully is the default turn: it allocates a real run row, finishes
// it, and reports a clean success.
//
// The row is real because the reporting path is real — turn.Reporter moves that
// row to reported and advances the watermark in one transaction, and a test
// that faked the row would be asserting against a transaction that never ran.
func (f *fixture) ranSuccessfully(ctx context.Context, req turn.Request) (turn.Result, error) {
	return f.turnResult(ctx, req, turn.DispositionRan, harness.OutcomeSucceeded)
}

func (f *fixture) turnResult(
	ctx context.Context,
	req turn.Request,
	disposition turn.Disposition,
	outcome harness.Outcome,
) (turn.Result, error) {
	session, err := f.db.GetSession(ctx, req.SessionID)
	if err != nil {
		return turn.Result{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return turn.Result{}, err
	}

	result := turn.Result{
		Disposition:      disposition,
		SessionID:        session.ID,
		TriggerCommentID: req.Trigger.CommentID,
		Ref:              ref,
		Session:          session,
		Outcome:          outcome,
		Branch:           "lf/123-add-a-health-endpoint",
		Remote:           "origin",
	}

	// The replay guard turn.Execute performs for real (AC-3.4). Without it a
	// retried comment would hit the (session, trigger comment) unique index
	// instead of being reported.
	existing, replay, err := f.db.GetRunByTrigger(ctx, session.ID, req.Trigger.CommentID)
	if err != nil {
		return turn.Result{}, err
	}
	if replay {
		result.Disposition = turn.DispositionAlreadyRan
		if existing.Status == store.RunReported {
			result.Disposition = turn.DispositionAlreadyReported
		}
		result.Run = existing
		result.LogPath = existing.LogPath
		result.Harness = fakeRunResult(existing, outcome)
		return result, nil
	}

	claudeSessionID, err := naming.ClaudeSessionID(ref, store.FirstSegmentOrdinal)
	if err != nil {
		return turn.Result{}, err
	}
	segment, err := f.db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, claudeSessionID)
	if err != nil {
		return turn.Result{}, err
	}
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             store.ModeSpike,
		TriggerCommentID: req.Trigger.CommentID,
		LogPath: func(ordinal int) string {
			path, pathErr := naming.RunLogPath(f.paths.Home(), ref, ordinal)
			if pathErr != nil {
				return ""
			}
			return path
		},
	})
	if err != nil {
		return turn.Result{}, err
	}

	status := store.RunSucceeded
	exitCode := int64(0)
	if outcome != harness.OutcomeSucceeded {
		status = store.RunFailed
		exitCode = 1
	}
	finished, err := f.db.FinishRun(ctx, run.ID, store.RunOutcome{
		Status:   status,
		ExitCode: &exitCode,
	})
	if err != nil {
		return turn.Result{}, err
	}

	result.Run = finished
	result.LogPath = finished.LogPath
	result.Harness = fakeRunResult(finished, outcome)
	result.Changed = outcome == harness.OutcomeSucceeded
	result.Pushed = outcome == harness.OutcomeSucceeded
	result.CommitSHA = "0123456789abcdef0123456789abcdef01234567"
	return result, nil
}

func fakeRunResult(run store.Run, outcome harness.Outcome) *harness.RunResult {
	exit := 0
	if outcome != harness.OutcomeSucceeded {
		exit = 1
	}
	return &harness.RunResult{
		Outcome:    outcome,
		ExitCode:   exit,
		LogPath:    run.LogPath,
		Model:      "claude-test",
		ResultText: "Added a /health endpoint.",
		HasResult:  true,
		Events:     3,
		StartedAt:  run.StartedAt,
		FinishedAt: run.StartedAt.Add(2 * time.Second),
		Duration:   2 * time.Second,
	}
}
