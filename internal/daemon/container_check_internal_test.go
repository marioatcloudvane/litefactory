package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestTickContainerCheckOncePerConnection counts the original-container
// inspections of one poll pass over N session-bound tasks of one project.
func TestTickContainerCheckOncePerConnection(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "checks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "clone"), Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LF_DISCOVERY_TOKEN", "external-discovery-credential")
	numbers := []int{7, 8, 9}
	var inspections atomic.Int64
	var limited atomic.Bool
	var origin string
	issue := func(n int) map[string]any {
		return map[string]any{"id": 700 + n, "number": n, "url": origin + "/repos/owner/tasks/issues/" + strconv.Itoa(n), "state": "open", "title": "task", "body": "work", "user": map[string]any{"id": 42, "login": "owner"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch path := r.URL.Path; {
		case path == "/user":
			response = map[string]any{"id": 99, "login": "factory-bot"}
		case path == "/users/owner":
			response = map[string]any{"id": 42, "login": "owner", "type": "User"}
		case path == "/repos/owner/code":
			response = map[string]any{"id": 101, "full_name": "owner/code", "has_issues": true}
		case path == "/repos/owner/tasks":
			inspections.Add(1)
			if limited.Load() {
				w.Header().Set("X-RateLimit-Limit", "5000")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Resource", "core")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
				return
			}
			response = map[string]any{"id": 202, "full_name": "owner/tasks", "has_issues": true}
		case path == "/repos/owner/tasks/issues":
			list := []any{}
			for _, n := range numbers {
				list = append(list, issue(n))
			}
			response = list
		case strings.HasPrefix(path, "/repos/owner/tasks/issues/"):
			n, e := strconv.Atoi(strings.TrimPrefix(path, "/repos/owner/tasks/issues/"))
			if e != nil {
				t.Errorf("unexpected provider request %s", path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			response = issue(n)
		default:
			t.Errorf("unexpected provider request %s", path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if e := json.NewEncoder(w).Encode(response); e != nil {
			t.Error(e)
		}
	}))
	defer server.Close()
	origin = server.URL
	set := func(role store.ConnectionRole, destination, remote string) store.ConnectionVersion {
		t.Helper()
		v, e := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: role, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: destination, RemoteID: remote, CredentialSource: "env", CredentialEnv: "LF_DISCOVERY_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	set(store.CodeConnection, "owner/code", "101")
	tickets := set(store.TicketConnection, "owner/tasks", "202")
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: ticket.Origin(origin)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d := &Daemon{db: db, connections: projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99}), filter: filter, log: slog.New(slog.DiscardHandler)}
	if _, err = (projectconnection.ApproverService{DB: db, Factory: d.connections}).Set(t.Context(), repo.ID, "owner", tickets.Revision, 0); err != nil {
		t.Fatal(err)
	}
	d.discovery.tick(now)
	d.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 4999, Reset: now.Add(time.Hour), StatusCode: http.StatusOK})
	q := &repoQueue{repo: repo, forgeRepo: github.Repo{Owner: "owner", Name: "tasks"}, log: d.log}
	issues, err := d.discoverIssues(projectContext(t.Context(), repo.ID), q.forgeRepo, time.Time{})
	if err != nil || len(issues) != len(numbers) {
		t.Fatalf("discovery: %+v %v", issues, err)
	}
	for _, i := range issues {
		if c, e := d.sight(t.Context(), q, i); e != nil || c == nil {
			t.Fatalf("admission of #%d: %+v %v", i.Number, c, e)
		}
	}

	// pass reads every task the way a poll does: session-bound by number.
	pass := func() (int64, []error) {
		t.Helper()
		before := inspections.Load()
		ctx, checks := withContainerChecks(t.Context())
		defer checks.close()
		var errs []error
		for _, n := range numbers {
			if got, e := d.readIssue(ctx, q.forgeRepo, n); e != nil {
				errs = append(errs, e)
			} else if got.Number != n {
				t.Fatalf("read #%d returned %+v", n, got)
			}
		}
		return inspections.Load() - before, errs
	}

	// C1: one inspection for N tasks in one cycle.
	if count, errs := pass(); count != 1 || len(errs) != 0 {
		t.Fatalf("one cycle over %d tasks inspected the container %d times (errors %v), want 1", len(numbers), count, errs)
	}
	// C3: the next cycle verifies again, exactly once.
	if count, errs := pass(); count != 1 || len(errs) != 0 {
		t.Fatalf("second cycle inspected the container %d times (errors %v), want 1", count, errs)
	}
	// C2: a rate-limit refusal is reused for the cycle, not retried per task.
	limited.Store(true)
	count, errs := pass()
	if count != 1 {
		t.Fatalf("rate-limited cycle inspected the container %d times, want 1", count)
	}
	if len(errs) != len(numbers) {
		t.Fatalf("rate-limited reads: %v", errs)
	}
	for _, e := range errs {
		if !errors.Is(e, github.ErrRateLimited) {
			t.Fatalf("task read lost the rate-limit refusal: %v", e)
		}
	}
	// Outside a cycle (and after one closes) every read still checks itself.
	limited.Store(false)
	before := inspections.Load()
	for _, n := range numbers {
		if _, e := d.readIssue(t.Context(), q.forgeRepo, n); e != nil {
			t.Fatal(e)
		}
	}
	if got := inspections.Load() - before; got < int64(len(numbers)) {
		t.Fatalf("non-tick reads skipped the per-call check: %d inspections", got)
	}
}

// scopeFixture is one project whose tasks live in owner/tasks, served by an
// httptest GitHub that counts repository inspections (GET /repos/owner/tasks).
type scopeFixture struct {
	d           *Daemon
	db          *store.DB
	repo        store.Repo
	tickets     store.ConnectionVersion
	parent      store.Session
	task        *task
	inspections atomic.Int64
	issueReads  atomic.Int64
	limited     atomic.Bool
	repoID      atomic.Int64
	hookFails   atomic.Bool
	mu          sync.Mutex
	closed      map[int]bool
	comments    map[int][]map[string]any
	origin      string
}

const scopeParent = 1

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	f := &scopeFixture{closed: map[int]bool{}, comments: map[int][]map[string]any{}}
	f.repoID.Store(202)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.db = db
	f.repo, err = db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "clone"), Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LF_DISCOVERY_TOKEN", "external-discovery-credential")
	issue := func(n int) map[string]any {
		state := "open"
		f.mu.Lock()
		if f.closed[n] {
			state = "closed"
		}
		f.mu.Unlock()
		return map[string]any{"id": 700 + n, "number": n, "url": f.origin + "/repos/owner/tasks/issues/" + strconv.Itoa(n), "state": state, "title": "task", "body": "work", "user": map[string]any{"id": 42, "login": "owner"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4000")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		var response any
		path := r.URL.Path
		switch {
		case path == "/user":
			response = map[string]any{"id": 99, "login": "factory-bot"}
		case path == "/users/owner":
			response = map[string]any{"id": 42, "login": "owner", "type": "User"}
		case path == "/repos/owner/code":
			response = map[string]any{"id": 101, "full_name": "owner/code", "has_issues": true}
		case strings.HasPrefix(path, "/repos/owner/code/issues/"):
			// The discovery probe's cheap read.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		case path == "/repos/owner/tasks":
			f.inspections.Add(1)
			if f.limited.Load() {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
				return
			}
			response = map[string]any{"id": f.repoID.Load(), "full_name": "owner/tasks", "has_issues": true}
		case strings.HasPrefix(path, "/repos/owner/tasks/hooks/"):
			if f.hookFails.Load() {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"message":"unavailable"}`))
				return
			}
			response = []any{}
		case path == "/repos/owner/tasks/issues" || path == "/repos/owner/tasks/issues/comments":
			response = []any{}
		case strings.HasPrefix(path, "/repos/owner/tasks/issues/"):
			rest := strings.Split(strings.TrimPrefix(path, "/repos/owner/tasks/issues/"), "/")
			n, e := strconv.Atoi(rest[0])
			if e != nil {
				t.Errorf("unexpected provider request %s", path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch {
			case len(rest) == 1:
				f.issueReads.Add(1)
				response = issue(n)
			case rest[1] == "parent":
				response = issue(scopeParent)
			case rest[1] == "comments":
				f.mu.Lock()
				list := []map[string]any{}
				for _, c := range f.comments[n] {
					id, _ := c["id"].(int)
					list = append(list, map[string]any{"id": id, "body": c["body"], "user": c["user"], "created_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
						"url":       f.origin + "/repos/owner/tasks/issues/comments/" + strconv.Itoa(id),
						"issue_url": f.origin + "/repos/owner/tasks/issues/" + strconv.Itoa(n)})
				}
				f.mu.Unlock()
				response = list
			default:
				t.Errorf("unexpected provider request %s", path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
		default:
			t.Errorf("unexpected provider request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if e := json.NewEncoder(w).Encode(response); e != nil {
			t.Error(e)
		}
	}))
	t.Cleanup(server.Close)
	f.origin = server.URL
	set := func(role store.ConnectionRole, destination, remote string) store.ConnectionVersion {
		t.Helper()
		v, e := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: f.repo.ID, Role: role, Provider: ticket.GitHub, Origin: ticket.Origin(f.origin), Destination: destination, RemoteID: remote, CredentialSource: "env", CredentialEnv: "LF_DISCOVERY_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: f.repo.Path, RemoteURL: f.repo.RemoteURL, BaseBranch: f.repo.BaseBranch, FeaturePrefix: f.repo.FeaturePrefix, WorktreeRoot: f.repo.WorktreeRoot}, 0)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	code := set(store.CodeConnection, "owner/code", "101")
	f.tickets = set(store.TicketConnection, "owner/tasks", "202")
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: ticket.Origin(f.origin)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	forge, err := github.New(github.Options{APIBase: f.origin, Token: secrets.NewToken("offline-token")})
	if err != nil {
		t.Fatal(err)
	}
	f.d = &Daemon{db: db, forge: forge, connections: projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99}), filter: filter, log: slog.New(slog.DiscardHandler)}
	if _, err = (projectconnection.ApproverService{DB: db, Factory: f.d.connections}).Set(t.Context(), f.repo.ID, "owner", f.tickets.Revision, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f.d.quotas.tick(now)
	f.d.discovery.tick(now)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(f.origin), RemoteID: strconv.Itoa(700 + scopeParent)}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(f.origin), Container: "owner/tasks", Key: strconv.Itoa(scopeParent)}}
	if _, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: f.repo.ID, ConnectionVersionID: f.tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	f.parent, err = db.AdmitSession(t.Context(), f.repo, ref, code.Revision, f.tickets.Revision, 99, "factory-bot", "github:owner/tasks#1")
	if err != nil {
		t.Fatal(err)
	}
	taskRef, err := naming.ParseRef(f.parent.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	f.task = &task{repo: f.repo, forgeRepo: github.Repo{Owner: "owner", Name: "tasks"}, issue: github.Issue{ID: 700 + scopeParent, Number: scopeParent, State: "open"}, session: f.parent, ref: taskRef, log: f.d.log}
	return f
}

// workerContext is a worker's context after its tick returned: the tick's
// scope is inherited but already closed.
func (f *scopeFixture) workerContext(t *testing.T) context.Context {
	ctx, checks := withContainerChecks(f.d.discoveryContext(sessionContext(t.Context(), f.parent.ID)))
	checks.close()
	return ctx
}

// planStories binds graph Plan stories to issues 2..n+1 of the parent.
func (f *scopeFixture) planStories(t *testing.T, n int) []int {
	t.Helper()
	ctx := t.Context()
	if _, err := f.db.AdmitPlanWorkflowPath(ctx, f.parent.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared"}
	var entries []planbrief.PlanStorySetEntry
	for i := range n {
		entries = append(entries, planbrief.PlanStorySetEntry{ID: "item-" + strconv.Itoa(i), Name: "Story " + strconv.Itoa(i), Intent: "Count repository inspections"})
	}
	revision, _, err := state.ProposeStorySet(entries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.PromoteStorySet(revision); err != nil {
		t.Fatal(err)
	}
	var numbers []int
	for i := range n {
		number := 2 + i
		key := state.StorySet.Keys["item-"+strconv.Itoa(i)]
		if _, err := f.db.ReservePlanStory(ctx, f.parent.TaskRef, key); err != nil {
			t.Fatal(err)
		}
		if err := f.db.BindPlanStoryIssue(ctx, f.parent.TaskRef, key, int64(700+number), number); err != nil {
			t.Fatal(err)
		}
		numbers = append(numbers, number)
	}
	if _, err := f.db.SavePlanWorkflowState(ctx, f.parent.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	return numbers
}

func (f *scopeFixture) count(fn func()) int64 {
	before := f.inspections.Load()
	fn()
	return f.inspections.Load() - before
}

// C1: graphPlanStories over N stories of one parent in one tick scope.
func TestContainerScopeGraphPlanStoriesInOneTick(t *testing.T) {
	f := newScopeFixture(t)
	f.planStories(t, 3)
	q := &repoQueue{repo: f.repo, forgeRepo: f.task.forgeRepo, log: f.d.log}
	got := f.count(func() {
		ctx, checks := withContainerChecks(f.d.discoveryContext(t.Context()))
		defer checks.close()
		if err := f.d.graphPlanStories(ctx, map[int64]*repoQueue{f.repo.ID: q}, map[int64]*candidate{}); err != nil {
			t.Fatal(err)
		}
	})
	if len(q.candidates) != 3 {
		t.Fatalf("offered %d story tasks, want 3", len(q.candidates))
	}
	if got != 1 {
		t.Fatalf("one tick over 3 stories inspected the container %d times, want 1", got)
	}
}

// C6: the worker's Plan parent pass after its tick scope closed.
func TestContainerScopePlanParentPassInWorker(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.planStories(t, 3)
	pass := func() int64 {
		return f.count(func() {
			if _, err := f.d.refinePlanChildren(f.workerContext(t), f.task); err != nil {
				t.Fatal(err)
			}
		})
	}
	if got := pass(); got != 1 {
		t.Fatalf("one Plan parent pass over %d stories inspected the container %d times, want 1", len(numbers), got)
	}
	for _, n := range numbers {
		child, err := f.db.GetSessionByTicket(t.Context(), ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(f.origin), RemoteID: strconv.Itoa(700 + n)})
		if err != nil || !child.NeedsPoll {
			t.Fatalf("story #%d was not handed to its child: %+v %v", n, child, err)
		}
		if err = f.db.MarkStoryDiscoveryPending(t.Context(), f.parent.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	if got := pass(); got != 1 {
		t.Fatalf("second Plan parent pass inspected the container %d times, want 1", got)
	}
}

// C3: a new scope inspects again and a changed repository ID still conflicts.
func TestContainerScopeDetectsTransferInNextScope(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.planStories(t, 2)
	handoff := func() error {
		ctx, checks := withContainerChecks(t.Context())
		defer checks.close()
		_, err := f.d.managedChildSession(ctx, f.repo, f.parent.TaskRef, numbers[0], int64(700+numbers[0]))
		return err
	}
	if err := handoff(); err != nil {
		t.Fatal(err)
	}
	f.repoID.Store(303)
	before := f.inspections.Load()
	err := handoff()
	if err == nil || !strings.Contains(err.Error(), "original ticket container identity changed or unresolved") {
		t.Fatalf("transferred repository was not detected: %v", err)
	}
	if got := f.inspections.Load() - before; got != 1 {
		t.Fatalf("next scope inspected %d times, want 1", got)
	}
}

// C4: a rate-limit refusal is reused for the scope, not retried per story.
func TestContainerScopeReusesRateLimitRefusal(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.planStories(t, 3)
	f.limited.Store(true)
	var errs []error
	got := f.count(func() {
		ctx, checks := withContainerChecks(t.Context())
		defer checks.close()
		for _, n := range numbers {
			if _, err := f.d.managedChildSession(ctx, f.repo, f.parent.TaskRef, n, int64(700+n)); err != nil {
				errs = append(errs, err)
			}
		}
	})
	if got != 1 {
		t.Fatalf("rate-limited scope inspected the container %d times, want 1", got)
	}
	if len(errs) != len(numbers) {
		t.Fatalf("rate-limited handoffs: %v", errs)
	}
	for _, e := range errs {
		if !errors.Is(e, github.ErrRateLimited) {
			t.Fatalf("handoff lost the rate-limit refusal: %v", e)
		}
	}
}

// managedStories registers N planning managed stories (issues 2..n+1) of the
// parent that each owe a sweep and carry one new comment by actor.
func (f *scopeFixture) managedStories(t *testing.T, n int, actor int64, body string) []int {
	t.Helper()
	ctx := t.Context()
	if err := f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: f.parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	var numbers []int
	for i := range n {
		number := 2 + i
		story := store.ManagedStory{ParentTask: f.parent.TaskRef, Generation: 1, Key: "US-" + strconv.Itoa(number), IssueNumber: number, IssueID: int64(700 + number), BodyHash: "offline", State: "planning"}
		if err := f.db.PutManagedStory(ctx, story); err != nil {
			t.Fatal(err)
		}
		if err := f.db.MarkStoryDiscoveryPending(ctx, f.parent.ID, number); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		f.comments[number] = []map[string]any{{"id": number*1000 + 1, "body": body, "user": map[string]any{"id": actor, "login": "commenter"}}}
		f.mu.Unlock()
		numbers = append(numbers, number)
	}
	return numbers
}

// C9: the worker's managed-story sweep after its tick scope closed.
func TestContainerScopeManagedStorySweepInWorker(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.managedStories(t, 3, 7, "a reader's remark")
	pass := func() int64 {
		return f.count(func() {
			if _, err := f.d.refineStories(f.workerContext(t), f.task); err != nil {
				t.Fatal(err)
			}
		})
	}
	if got := pass(); got != 1 {
		t.Fatalf("one sweep over %d managed stories inspected the container %d times, want 1", len(numbers), got)
	}
	for _, n := range numbers {
		cursor, err := f.db.StoryDiscoveryCursor(t.Context(), f.parent.ID, n)
		if err != nil || cursor != int64(n*1000+1) {
			t.Fatalf("story #%d was not swept: cursor=%d %v", n, cursor, err)
		}
		if err = f.db.MarkStoryDiscoveryPending(t.Context(), f.parent.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	if got := pass(); got != 1 {
		t.Fatalf("second sweep inspected the container %d times, want 1", got)
	}
}

// C10a: a Judge step reached from the Plan parent pass checks freshly.
func TestContainerScopePlanPassJudgeStepChecksFreshly(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.planStories(t, 3)
	ctx := t.Context()
	// One more bound child that is closed and no longer part of the plan, so the
	// pass reads its comments; it carries a judge command.
	judged := 2 + len(numbers)
	if _, err := f.db.ReservePlanStory(ctx, f.parent.TaskRef, "gone"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.BindPlanStoryIssue(ctx, f.parent.TaskRef, "gone", int64(700+judged), judged); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.closed[judged] = true
	f.comments[judged] = []map[string]any{{"id": 9001, "body": "/judge is this right?", "user": map[string]any{"id": 42, "login": "owner"}}}
	f.mu.Unlock()
	// Hold the judged child's polling slot so the Judge step stops right after
	// its own handoff check, before any execution.
	handoffCtx, checks := withContainerChecks(ctx)
	child, err := f.d.managedChildSession(handoffCtx, f.repo, f.parent.TaskRef, judged, int64(700+judged))
	checks.close()
	if err != nil {
		t.Fatal(err)
	}
	if !f.d.inflight.enter(child.ID) {
		t.Fatal("could not hold the judged child")
	}
	defer f.d.inflight.leave(child.ID)

	var passErr error
	got := f.count(func() { _, passErr = f.d.refinePlanChildren(f.workerContext(t), f.task) })
	if passErr == nil || !strings.Contains(passErr.Error(), "waits for its current polling worker") {
		t.Fatalf("pass did not reach the Judge step: %v", passErr)
	}
	// The observation and handoff part costs 1; the Judge step adds its own.
	if got < 2 {
		t.Fatalf("Plan parent pass with a Judge step inspected the container %d times, want the pass's 1 plus a fresh Judge check", got)
	}
}

// C10b: the refinement execution recheck runs after the sweep scope closed.
func TestContainerScopeRefinementRecheckChecksFreshly(t *testing.T) {
	f := newScopeFixture(t)
	f.managedStories(t, 3, 0, "")
	// Only the first story carries an owner refinement.
	f.mu.Lock()
	f.comments[3], f.comments[4] = nil, nil
	f.comments[2] = []map[string]any{{"id": 2001, "body": "please narrow this story", "user": map[string]any{"id": 42, "login": "owner"}}}
	f.mu.Unlock()
	var passErr error
	got := f.count(func() { _, passErr = f.d.refineStories(f.workerContext(t), f.task) })
	pending, err := f.db.ListPendingStoryRefinements(t.Context(), f.parent.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending refinements=%+v %v (pass error %v)", pending, err, passErr)
	}
	// Without a reporter the execution stops at acknowledgement, after its recheck.
	if passErr == nil || !strings.Contains(passErr.Error(), "acknowledgement unavailable") {
		t.Fatalf("pass did not reach the execution recheck: %v", passErr)
	}
	if got < 2 {
		t.Fatalf("sweep plus execution recheck inspected the container %d times, want the sweep's 1 plus a fresh recheck", got)
	}
}

// hookSubscription confirms a GitHub hook (id 9) for the ticket source.
func (f *scopeFixture) hookSubscription(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	sub, err := f.db.PutTicketSubscription(ctx, store.TicketSubscription{
		ID: "scope-hook", RepoID: f.repo.ID, ConnectionVersionID: f.tickets.ID,
		Provider: ticket.GitHub, Origin: ticket.Origin(f.origin), Container: "owner/tasks", ContainerID: "202",
		CallbackURL: "http://127.0.0.1:1/webhook?subscription=scope-hook", Events: []string{"issues", "issue_comment", "sub_issues"},
		DesiredEnabled: true, SecretService: WebhookKeychainService, SecretAccount: "scope-hook", SecretVersion: "1",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.db.ClaimSubscriptionOperation(ctx, sub.ID, sub.Revision, "create")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.FinishSubscriptionOperation(ctx, op, "confirmed", "9", "active", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

// C2: hook recovery whose delivery history is unavailable inspects once and
// marks the repository owed a listing instead of recovering the source itself;
// recovery (actor 0) and session checks are cached separately.
func TestContainerScopeHookRecoveryFallbackInspectsOnce(t *testing.T) {
	for _, session := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovery", true: "recovery-and-session"}[session], func(t *testing.T) {
			f := newScopeFixture(t)
			f.hookSubscription(t)
			f.hookFails.Store(true)
			q := &repoQueue{repo: f.repo, forgeRepo: f.task.forgeRepo, log: f.d.log}
			now := time.Now().UTC()
			f.d.schedule = newDiscoverySchedule(time.Hour)
			f.d.schedule.ListingFinished(f.repo.ID, now)
			got := f.count(func() {
				ctx, checks := withContainerChecks(f.d.discoveryContext(projectContext(t.Context(), f.repo.ID)))
				defer checks.close()
				ctx = context.WithValue(ctx, recoveryTickKey{}, now)
				if err := f.d.recoverHookDeliveries(ctx, q, now, false); err != nil && !errors.Is(err, ErrDiscoveryDeferred) {
					t.Fatal(err)
				}
				if _, err := f.db.GetTicketRecoveryPosition(t.Context(), f.tickets.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("hook recovery recovered the source itself: %v", err)
				}
				if !f.d.schedule.Due(f.repo.ID, now) {
					t.Fatal("unavailable delivery history did not mark the repository owed a listing")
				}
				if session {
					if _, err := f.d.readIssue(sessionContext(ctx, f.parent.ID), f.task.forgeRepo, scopeParent); err != nil {
						t.Fatal(err)
					}
				}
			})
			want := int64(1)
			if session {
				want = 2
			}
			if got != want {
				t.Fatalf("one tick inspected the container %d times, want %d", got, want)
			}
		})
	}
}

// C8: a cache hit at a discoveryCall-wrapped site takes no budget step.
func TestContainerScopeCacheHitSkipsDiscoveryBudget(t *testing.T) {
	for _, primed := range []bool{false, true} {
		t.Run(map[bool]string{false: "miss", true: "hit"}[primed], func(t *testing.T) {
			f := newScopeFixture(t)
			q := &repoQueue{repo: f.repo, forgeRepo: f.task.forgeRepo, log: f.d.log}
			ctx, checks := withContainerChecks(f.d.discoveryContext(projectContext(t.Context(), f.repo.ID)))
			defer checks.close()
			if primed {
				clients, err := f.d.connections.Clients(ctx, f.tickets, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tickContainerIdentity(ctx, clients.Tickets, f.tickets.ID, 0, f.tickets.Destination); err != nil {
					t.Fatal(err)
				}
			}
			// Hold the source's budget: the first budget step recoverSource takes
			// ends it with a deferral.
			budget := f.d.quotas.forVersion(f.tickets)
			budget.backoff.fail(time.Now().UTC(), time.Hour)
			budget.discovery.mu.Lock()
			spent := budget.discovery.spent
			budget.discovery.mu.Unlock()
			got := f.count(func() {
				if _, err := f.d.recoverSource(ctx, q, time.Now().UTC()); !errors.Is(err, ErrDiscoveryDeferred) {
					t.Fatalf("recovery passed a held budget: %v", err)
				}
			})
			budget.discovery.mu.Lock()
			after := budget.discovery.spent
			budget.discovery.mu.Unlock()
			position, err := f.db.GetTicketRecoveryPosition(t.Context(), f.tickets.ID)
			if !primed {
				// A miss takes the budget step first and stops before any position.
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("container miss skipped its budget step: %+v %v", position, err)
				}
				return
			}
			if got != 0 {
				t.Fatalf("cache hit sent %d inspections", got)
			}
			if err != nil || position.ContainerID != "202" {
				t.Fatalf("cache hit still took the container budget step: %+v %v", position, err)
			}
			if after != spent {
				t.Fatalf("cache hit charged the discovery budget: %d -> %d", spent, after)
			}
		})
	}
}

// slowTickets is a TicketClient whose inspections block until released.
type slowTickets struct {
	projectconnection.TicketClient
	calls   atomic.Int64
	started chan string
	release map[string]chan struct{}
}

func (s *slowTickets) ContainerIdentity(ctx context.Context, destination string) (ticket.Identity, error) {
	s.calls.Add(1)
	s.started <- destination
	select {
	case <-s.release[destination]:
		return ticket.Identity{Provider: ticket.GitHub, RemoteID: destination}, nil
	case <-ctx.Done():
		return ticket.Identity{}, ctx.Err()
	}
}

// C7: the scope lock never spans the inspection; one key's in-flight call is
// shared and never blocks another key; a cancelled leader is not cached.
func TestContainerScopeInFlightSharingWithoutSharedLock(t *testing.T) {
	tickets := &slowTickets{started: make(chan string, 8), release: map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{})}}
	close(tickets.release["b"])
	ctx, checks := withContainerChecks(t.Context())
	defer checks.close()
	type result struct {
		identity ticket.Identity
		err      error
	}
	leader := make(chan result, 1)
	go func() {
		id, err := tickContainerIdentity(ctx, tickets, 1, 99, "a")
		leader <- result{id, err}
	}()
	if got := <-tickets.started; got != "a" {
		t.Fatalf("started %q", got)
	}
	follower := make(chan result, 1)
	go func() {
		id, err := tickContainerIdentity(ctx, tickets, 1, 99, "a")
		follower <- result{id, err}
	}()
	// Another key completes while key A's inspection is still in flight.
	other, err := tickContainerIdentity(ctx, tickets, 2, 99, "b")
	if err != nil || other.RemoteID != "b" {
		t.Fatalf("key B: %+v %v", other, err)
	}
	<-tickets.started
	close(tickets.release["a"])
	for _, ch := range []chan result{leader, follower} {
		if r := <-ch; r.err != nil || r.identity.RemoteID != "a" {
			t.Fatalf("key A: %+v", r)
		}
	}
	if got := tickets.calls.Load(); got != 2 {
		t.Fatalf("inspections=%d, want one per key", got)
	}

	// A cancelled leader leaves no entry; its waiter inspects for itself.
	cancelled := &slowTickets{started: make(chan string, 8), release: map[string]chan struct{}{"c": make(chan struct{})}}
	leaderCtx, cancel := context.WithCancel(ctx)
	go func() {
		_, err := tickContainerIdentity(leaderCtx, cancelled, 3, 99, "c")
		leader <- result{err: err}
	}()
	<-cancelled.started
	go func() {
		id, err := tickContainerIdentity(ctx, cancelled, 3, 99, "c")
		follower <- result{id, err}
	}()
	// Let the waiter queue on the in-flight entry before cancelling its leader.
	deadline := time.Now().Add(5 * time.Second)
	for {
		checks.mu.Lock()
		_, inFlight := checks.entries[[2]int64{3, 99}]
		checks.mu.Unlock()
		if inFlight || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if r := <-leader; !errors.Is(r.err, context.Canceled) {
		t.Fatalf("leader: %+v", r)
	}
	<-cancelled.started
	close(cancelled.release["c"])
	if r := <-follower; r.err != nil || r.identity.RemoteID != "c" {
		t.Fatalf("waiter after cancelled leader: %+v", r)
	}
	if got := cancelled.calls.Load(); got != 2 {
		t.Fatalf("inspections after cancellation=%d, want 2", got)
	}
}

// storyPass runs one discovery pass of a story kind in a fresh tick scope and
// returns its queue and how many story issues it read.
func (f *scopeFixture) storyPass(t *testing.T, discover func(context.Context, map[int64]*repoQueue) error) (*repoQueue, int64) {
	t.Helper()
	q := &repoQueue{repo: f.repo, forgeRepo: f.task.forgeRepo, log: f.d.log}
	before := f.issueReads.Load()
	ctx, checks := withContainerChecks(f.d.discoveryContext(t.Context()))
	defer checks.close()
	if err := discover(ctx, map[int64]*repoQueue{f.repo.ID: q}); err != nil {
		t.Fatal(err)
	}
	return q, f.issueReads.Load() - before
}

// A Plan story whose task is already admitted is offered from local state: a
// later tick neither inspects the container nor reads the story issue. The
// first tick, which has to create the tasks, still resolves each story.
func TestGraphPlanStoriesKnownTaskResolvesNothing(t *testing.T) {
	f := newScopeFixture(t)
	numbers := f.planStories(t, 3)
	discover := func(ctx context.Context, byRepo map[int64]*repoQueue) error {
		return f.d.graphPlanStories(ctx, byRepo, map[int64]*candidate{})
	}
	q, reads := f.storyPass(t, discover)
	if len(q.candidates) != len(numbers) || reads != int64(len(numbers)) {
		t.Fatalf("first pass offered %d and read %d story issues, want %d and %d", len(q.candidates), reads, len(numbers), len(numbers))
	}
	inspected := f.count(func() { q, reads = f.storyPass(t, discover) })
	if len(q.candidates) != len(numbers) || reads != 0 || inspected != 0 {
		t.Fatalf("second pass offered %d, read %d story issues and inspected %d times, want %d, 0 and 0", len(q.candidates), reads, inspected, len(numbers))
	}
}

// An Implement story whose worker session is bound is checked from local
// state: a later tick reads no story issue. Binding the worker in the first
// tick still resolves the story remotely.
func TestGraphImplementStoriesBoundWorkerResolvesNothing(t *testing.T) {
	f := newScopeFixture(t)
	ctx := t.Context()
	parent := f.parent.TaskRef
	if err := f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CompareManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "implementing"}, "planning"); err != nil {
		t.Fatal(err)
	}
	var seeds []store.ImplementStorySeed
	for i, key := range []string{"story-001", "story-002"} {
		number := 2 + i
		if err := f.db.PutManagedStory(ctx, store.ManagedStory{ParentTask: parent, Generation: 1, Key: key, IssueNumber: number, IssueID: int64(700 + number), State: "planning"}); err != nil {
			t.Fatal(err)
		}
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	if err := f.db.EnsureImplementStories(ctx, parent, 1, seeds, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	for _, seed := range seeds {
		row, err := f.db.GetImplementStory(ctx, parent, 1, seed.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: implementDaemonClock}); err != nil {
			t.Fatal(err)
		}
	}
	discover := func(ctx context.Context, byRepo map[int64]*repoQueue) error {
		return f.d.graphImplementStories(ctx, byRepo, map[int64]*candidate{}, implementDaemonClock)
	}
	q, reads := f.storyPass(t, discover)
	if len(q.candidates) != len(seeds) || reads != int64(len(seeds)) {
		t.Fatalf("first pass offered %d and read %d story issues, want %d and %d", len(q.candidates), reads, len(seeds), len(seeds))
	}
	for _, seed := range seeds {
		if row, err := f.db.GetImplementStory(ctx, parent, 1, seed.Key); err != nil || row.WorkerSession == 0 {
			t.Fatalf("%s worker not bound: %+v %v", seed.Key, row, err)
		}
	}
	inspected := f.count(func() { q, reads = f.storyPass(t, discover) })
	if len(q.candidates) != len(seeds) || reads != 0 || inspected != 0 {
		t.Fatalf("second pass offered %d, read %d story issues and inspected %d times, want %d, 0 and 0", len(q.candidates), reads, inspected, len(seeds))
	}
}
