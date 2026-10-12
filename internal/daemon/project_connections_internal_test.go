package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestProjectDiscoveryRetainsBacklogAndQualifiedWorkspace exercises the actual
// discovery/admission and backlog materialization paths around source changes.
func TestProjectDiscoveryRetainsBacklogAndQualifiedWorkspace(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "projects.db"))
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
	type provider struct {
		server          *httptest.Server
		origin          string
		reads, comments atomic.Int64
		remote          int
	}
	makeProvider := func(remote int) *provider {
		t.Helper()
		p := &provider{remote: remote}
		p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "4999")
			w.Header().Set("X-RateLimit-Resource", "core")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
			if r.Method != http.MethodGet {
				t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			issue := map[string]any{"id": p.remote, "number": 7, "url": p.origin + "/repos/owner/tasks/issues/7", "state": "open", "title": "saved source", "body": "work", "user": map[string]any{"id": 42, "login": "owner"}}
			var response any
			switch r.URL.Path {
			case "/user":
				response = map[string]any{"id": 99, "login": "factory-bot"}
			case "/users/owner":
				response = map[string]any{"id": 42, "login": "owner", "type": "User"}
			case "/repos/owner/code", "/repos/owner/replacement-code", "/repos/owner/tasks":
				id := 101
				if r.URL.Path == "/repos/owner/tasks" {
					id = 202
				}
				if r.URL.Path == "/repos/owner/replacement-code" {
					id = 303
				}
				response = map[string]any{"id": id, "full_name": strings.TrimPrefix(r.URL.Path, "/repos/"), "has_issues": true}
			case "/repos/owner/tasks/issues":
				response = []any{issue}
			case "/repos/owner/tasks/issues/7":
				p.reads.Add(1)
				response = issue
			case "/repos/owner/tasks/issues/7/comments":
				p.comments.Add(1)
				response = []any{map[string]any{"id": 10, "url": p.origin + "/repos/owner/tasks/issues/comments/10", "issue_url": p.origin + "/repos/owner/tasks/issues/7", "body": "/implement resume", "user": map[string]any{"id": 42, "login": "owner"}}}
			default:
				t.Errorf("unexpected provider request %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if e := json.NewEncoder(w).Encode(response); e != nil {
				t.Error(e)
			}
		}))
		p.origin = p.server.URL
		t.Cleanup(p.server.Close)
		return p
	}
	original := makeProvider(700)
	replacement := makeProvider(800)
	set := func(role store.ConnectionRole, origin, destination, remote string, expected int64) store.ConnectionVersion {
		t.Helper()
		v, e := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: role, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: destination, RemoteID: remote, CredentialSource: "env", CredentialEnv: "LF_DISCOVERY_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, expected)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	code := set(store.CodeConnection, original.origin, "owner/code", "101", 0)
	tickets := set(store.TicketConnection, original.origin, "owner/tasks", "202", 0)
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: ticket.Origin(original.origin)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d := &Daemon{db: db, connections: projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99}), filter: filter, log: slog.New(slog.DiscardHandler), now: func() time.Time { return now }}
	d.quotas.tick(now)
	d.connections.SetRequestContext(d.bindQuotaRequest)
	approvers := projectconnection.ApproverService{DB: db, Factory: d.connections}
	appointment, err := approvers.Set(t.Context(), repo.ID, "owner", tickets.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	q := &repoQueue{repo: repo, forgeRepo: github.Repo{Owner: "owner", Name: "tasks"}, log: d.log}
	discover := func() *candidate {
		t.Helper()
		issues, e := d.discoverIssues(projectContext(t.Context(), repo.ID), q.forgeRepo, time.Time{})
		if e != nil || len(issues) != 1 {
			t.Fatalf("saved-source listing: %+v, %v", issues, e)
		}
		c, e := d.sight(t.Context(), q, issues[0])
		if e != nil || c == nil {
			t.Fatalf("actual discovery admission: %+v, %v", c, e)
		}
		return c
	}
	first := discover()
	firstSnapshot, err := db.GetExecutionSnapshot(t.Context(), first.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSnapshot.CodeVersionID != code.ID || firstSnapshot.TicketVersionID != tickets.ID || first.session.TaskRef != "github:owner/tasks#7" {
		t.Fatalf("discovery ignored saved roles: %+v %+v", first.session, firstSnapshot)
	}
	staleIssues, err := d.discoverIssues(projectContext(t.Context(), repo.ID), q.forgeRepo, time.Time{})
	if err != nil || len(staleIssues) != 1 {
		t.Fatalf("capture discovery before edit: %+v %v", staleIssues, err)
	}
	canonicalIssue := staleIssues[0]
	canonicalIssue.APIURL = original.origin + "/repos/Owner/TASKS/issues/7"
	canonicalCandidate, err := d.sight(t.Context(), q, canonicalIssue)
	if err != nil || canonicalCandidate == nil || canonicalCandidate.session.ID != first.session.ID {
		t.Fatalf("canonical repository casing rejected: %+v %v", canonicalCandidate, err)
	}
	set(store.CodeConnection, replacement.origin, "owner/replacement-code", "303", code.Revision)
	replacementTickets := set(store.TicketConnection, replacement.origin, "owner/tasks", "202", tickets.Revision)
	if _, err = d.sight(t.Context(), q, staleIssues[0]); !errors.Is(err, store.ErrConnectionConflict) {
		t.Fatalf("old discovery was qualified with replacement origin: %v", err)
	}
	staleIdentity := ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(replacement.origin), RemoteID: "700"}
	if _, err = db.GetTicketBinding(t.Context(), staleIdentity); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale observation created a replacement binding: %v", err)
	}
	// The new origin needs its own verified appointment; a bare account ID
	// from the original instance cannot authorize its admissions.
	if _, err := approvers.Set(t.Context(), repo.ID, "owner", replacementTickets.Revision, appointment.Revision); err != nil {
		t.Fatal(err)
	}
	second := discover()
	if first.session.ID == second.session.ID || first.session.TaskRef == second.session.TaskRef {
		t.Fatal("qualified source collision reused saved work")
	}
	secondSnapshot, err := db.GetExecutionSnapshot(t.Context(), second.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondSnapshot.NamingSuffix == "" || secondSnapshot.CodeTarget.Slug() != "owner/replacement-code" {
		t.Fatalf("new source did not retain independent naming and code target: %+v", secondSnapshot)
	}
	oldPath, err := naming.QualifiedWorktreePath(repo.WorktreeRoot, 7, first.session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	newPath, err := naming.QualifiedWorktreePath(repo.WorktreeRoot, 7, second.session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if oldPath == newPath || !strings.HasSuffix(oldPath, "task-7") {
		t.Fatalf("colliding ticket workspaces: %q / %q", oldPath, newPath)
	}
	if _, err = db.DisableConnection(t.Context(), repo.ID, store.TicketConnection, replacementTickets.Revision); err != nil {
		t.Fatal(err)
	}
	if err = db.SetSessionNeedsPoll(t.Context(), first.session.ID); err != nil {
		t.Fatal(err)
	}
	seen := make(map[int64]*candidate)
	q.candidates = nil
	if err = d.backlog(t.Context(), map[int64]*repoQueue{repo.ID: q}, seen); err != nil {
		t.Fatal(err)
	}
	saved := seen[first.session.ID]
	if saved == nil {
		t.Fatal("disabled current source dropped old backlog")
	}
	oldReads := original.reads.Load()
	newReads := replacement.reads.Load()
	task, err := d.materialise(t.Context(), saved, now)
	if err != nil || task == nil {
		t.Fatalf("retained backlog materialization: %+v, %v", task, err)
	}
	if task.repo.Slug() != "owner/code" || task.forgeRepo.String() != "owner/tasks" || task.issue.ID != 700 {
		t.Fatalf("saved work redirected: %+v", task)
	}
	replies, err := d.readComments(sessionContext(t.Context(), first.session.ID), task.forgeRepo, 7, 0)
	if err != nil || len(replies) != 1 || !strings.HasPrefix(replies[0].IssueAPIURL, original.origin) {
		t.Fatalf("retained comment routing: %+v, %v", replies, err)
	}
	if original.reads.Load() <= oldReads || replacement.reads.Load() != newReads || original.comments.Load() != 1 || replacement.comments.Load() != 0 {
		t.Fatal("backlog reads reached the replacement source")
	}
	if _, err = d.discoverIssues(projectContext(t.Context(), repo.ID), q.forgeRepo, time.Time{}); err == nil {
		t.Fatal("disabled connection still admitted discovery")
	}
	// A refusal belongs to the retained connection even after both current
	// roles have moved, and recovery for the replacement remains eligible.
	oldCtx := sessionContext(t.Context(), first.session.ID)
	newCtx := sessionContext(t.Context(), second.session.ID)
	_, finish, err := d.bindQuotaRequest(oldCtx, tickets)
	if err != nil {
		t.Fatal(err)
	}
	finish(&ticket.RateLimitError{RetryAfter: time.Minute})
	if !d.scopedQuotaDeferred(oldCtx, errors.New("sanitized connection failure")) || d.scopedQuotaDeferred(newCtx, errors.New("sanitized connection failure")) {
		t.Fatal("sanitized errors lost their actual connection hold")
	}
	before := original.comments.Load()
	if _, err := d.readComments(oldCtx, task.forgeRepo, 7, 0); !errors.Is(err, ErrDiscoveryDeferred) {
		t.Fatalf("retained hold bypassed: %v", err)
	}
	if original.comments.Load() != before {
		t.Fatal("retained held request reached provider")
	}
	if _, err := d.readComments(newCtx, task.forgeRepo, 7, 0); err != nil {
		t.Fatalf("replacement shares retained hold: %v", err)
	}
	if allowed, err := d.recoveryAllowed(t.Context(), store.ReportRecovery{Key: "original", SessionID: first.session.ID}, now); err != nil || allowed {
		t.Fatalf("retained recovery escaped hold: %t %v", allowed, err)
	}
	if allowed, err := d.recoveryAllowed(t.Context(), store.ReportRecovery{Key: "replacement", SessionID: second.session.ID}, now); err != nil || !allowed {
		t.Fatalf("independent recovery blocked: %t %v", allowed, err)
	}
	now = now.Add(time.Minute)
	d.quotas.tick(now)
	if _, err := d.readComments(oldCtx, task.forgeRepo, 7, 0); err != nil {
		t.Fatalf("retained recovery did not resume: %v", err)
	}

}
