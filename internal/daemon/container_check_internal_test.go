package daemon

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
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
