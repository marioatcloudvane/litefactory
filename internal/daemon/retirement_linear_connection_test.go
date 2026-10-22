package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// retirementLinearFixture is a daemon with project connections whose code
// repository (code-owner/code) differs from the Linear ticket container
// (team-uuid/project-uuid), as for the cockpit repository in issue #1029.
type retirementLinearFixture struct {
	remote  *linearWorkflowServer
	failing atomic.Bool
	db      *store.DB
	git     *gittest.Repo
	repo    store.Repo
	session store.Session
	d       *Daemon
	logs    *retirementLockedBuffer
	now     time.Time
}

type retirementLockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *retirementLockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *retirementLockedBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b.buf.Bytes()))
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		out = append(out, record)
	}
	return out
}

func newRetirementLinearFixture(t *testing.T) *retirementLinearFixture {
	t.Helper()
	ctx := t.Context()
	x := &retirementLinearFixture{remote: newLinearWorkflowServer(t), logs: &retirementLockedBuffer{}}
	inner := x.remote.server.Config.Handler
	x.remote.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if x.failing.Load() {
			http.Error(w, "linear-raw-failure-detail", http.StatusInternalServerError)
			return
		}
		inner.ServeHTTP(w, r)
	})
	codeRemote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/user":
			response = map[string]any{"id": 99, "login": "github-publisher", "type": "Bot"}
		case "/repos/code-owner/code":
			response = map[string]any{"id": 101, "full_name": "code-owner/code"}
		default:
			t.Errorf("retirement unexpectedly reached GitHub: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(codeRemote.Close)
	codeURL, err := url.Parse(codeRemote.URL)
	linearWorkflowCheck(t, err)
	httpClient := &http.Client{Transport: linearMixedProviderTransport{githubURL: codeURL}}

	x.db, err = store.Open(ctx, filepath.Join(t.TempDir(), "retirement-linear.db"))
	linearWorkflowCheck(t, err)
	t.Cleanup(func() { _ = x.db.Close() })
	linearWorkflowCheck(t, x.db.Migrate(ctx))

	remoteURL := "git@github.com:code-owner/code.git"
	x.git = gittest.New(t)
	x.git.SetOriginURL(remoteURL)
	ssh := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\ncase \"$*\" in\n*git-upload-pack*) exec git upload-pack '" + x.git.Origin + "';;\n*git-receive-pack*) exec git receive-pack '" + x.git.Origin + "';;\n*) exit 1;;\nesac\n"
	linearWorkflowCheck(t, os.WriteFile(ssh, []byte(script), 0700))
	t.Setenv("PATH", filepath.Dir(ssh)+string(os.PathListSeparator)+os.Getenv("PATH"))
	x.git.Git("config", "ssh.variant", "ssh")

	root := filepath.Join(filepath.Dir(x.git.Dir), "tasks")
	x.repo, err = x.db.UpsertRepo(ctx, store.Repo{Path: x.git.Dir, Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: remoteURL, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: root})
	linearWorkflowCheck(t, err)
	_, err = x.db.SetConnection(ctx, store.ConnectionVersion{RepoID: x.repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: x.repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "CODE_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: x.repo.Path, RemoteURL: x.repo.RemoteURL, BaseBranch: x.repo.BaseBranch, FeaturePrefix: x.repo.FeaturePrefix, WorktreeRoot: x.repo.WorktreeRoot}, 0)
	linearWorkflowCheck(t, err)
	version, err := x.db.SetConnection(ctx, store.ConnectionVersion{RepoID: x.repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(x.remote.server.URL), Destination: "team-uuid/project-uuid", CredentialSource: "env", CredentialEnv: "LINEAR_TOKEN", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}}, 0)
	linearWorkflowCheck(t, err)
	factory := projectconnection.NewFactory(projectconnection.Options{DB: x.db, HTTPClient: httpClient, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		switch name {
		case "LINEAR_TOKEN":
			return "test-linear-token", true
		case "CODE_TOKEN":
			return "test-github-token", true
		}
		return "", false
	}}})
	_, err = x.db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH})
	linearWorkflowCheck(t, err)
	appointment, err := (projectconnection.ApproverService{DB: x.db, Factory: factory}).Set(ctx, x.repo.ID, "human-uuid", version.Revision, 0)
	linearWorkflowCheck(t, err)
	clients, err := factory.Clients(ctx, version, 0)
	linearWorkflowCheck(t, err)
	parent, err := clients.Linear.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: version.Origin, Container: version.Destination, Key: "parent-uuid"})
	linearWorkflowCheck(t, err)
	_, err = x.db.AddTicketBinding(ctx, store.TicketBinding{RepoID: x.repo.ID, ConnectionVersionID: version.ID, Ref: parent.Ref, Enabled: true, Provenance: "explicit"})
	linearWorkflowCheck(t, err)
	botSlot, err := x.db.LocalTicketSlot(ctx, "actor", clients.Publisher.Identity)
	linearWorkflowCheck(t, err)
	ownerSlot, err := x.db.LocalTicketSlot(ctx, "actor", appointment.Actor)
	linearWorkflowCheck(t, err)
	session, err := AdmitProjectTicket(ctx, x.db, factory, x.repo, parent.Ref)
	linearWorkflowCheck(t, err)
	if ref, err := naming.ParseRef(session.TaskRef); err != nil || ref.Forge != string(ticket.Linear) || ref.Owner == x.repo.Owner {
		t.Fatalf("fixture session is not a Linear task in a foreign container: %q %v", session.TaskRef, err)
	}
	branch, err := naming.Branch(x.repo.FeaturePrefix, session.IssueNumber, "retire")
	linearWorkflowCheck(t, err)
	x.session, err = x.db.FreezeSessionSlug(ctx, session.ID, "retire", branch)
	linearWorkflowCheck(t, err)
	path, err := naming.WorktreePath(root, session.IssueNumber)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, x.db.SetSessionWorktree(ctx, session.ID, path))
	x.session.WorktreePath = path
	x.git.Git("worktree", "add", "-b", branch, path, "main")
	x.git.Git("push", "origin", branch)
	x.now = x.session.CreatedAt.Add(31 * 24 * time.Hour)

	filter, err := turn.NewFilter(x.db, turn.Identities{OwnerUserID: ownerSlot, BotUserID: botSlot, TicketOrigin: version.Origin}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	linearWorkflowCheck(t, err)
	x.d = &Daemon{db: x.db, connections: factory, filter: filter, instanceID: "retirement-linear", log: slog.New(slog.NewJSONHandler(x.logs, nil)),
		retirement: RetirementExecutionOptions{CloneGate: newCloneGate(), LockDir: t.TempDir(), RetainDays: 14}}
	return x
}

// setState moves the fake Linear parent issue to a workflow state type.
func (x *retirementLinearFixture) setState(kind string) {
	x.remote.mu.Lock()
	defer x.remote.mu.Unlock()
	x.remote.issues["parent-uuid"]["state"] = map[string]any{"id": "state-" + kind, "name": kind, "type": kind}
}

func (x *retirementLinearFixture) ctx(t *testing.T) context.Context {
	return sessionContext(t.Context(), x.session.ID)
}

// retirementObserveHook runs after each production Git observation.
type retirementObserveHook struct {
	RetirementGit
	calls  int
	after  int
	onCall func()
}

func (g *retirementObserveHook) Observe(ctx context.Context) (git.RetirementObservation, error) {
	o, err := g.RetirementGit.Observe(ctx)
	g.calls++
	if g.calls == g.after && g.onCall != nil {
		g.onCall()
	}
	return o, err
}

func retirementLinearDecision(t *testing.T, e RetirementEvaluation, kind string) RetirementDecision {
	t.Helper()
	for _, d := range e.Decisions {
		if d.Kind == kind {
			return d
		}
	}
	t.Fatalf("missing %s decision in %+v", kind, e.Decisions)
	return RetirementDecision{}
}

// Issue #1029 C2: connection-backed Linear retirement decides on the real
// Linear state, through the final closure re-read, like a GitHub issue.
func TestRetirementLinearConnection_Execute(t *testing.T) {
	for _, tt := range []struct {
		name, state string
		failFinal   bool
	}{{"completed", "completed", false}, {"canceled", "canceled", false}, {"open", "unstarted", false}, {"final_read_fails", "completed", true}} {
		t.Run(tt.name, func(t *testing.T) {
			x := newRetirementLinearFixture(t)
			x.setState(tt.state)
			opts := x.d.retirement
			opts.OpenGit = func(ctx context.Context, owner store.RetirementOwner) (RetirementGit, error) {
				inner, err := openRetirementClone(ctx, owner)
				if err != nil {
					return nil, err
				}
				// Observation 2 is the worktree's revalidation, just before the final read.
				return &retirementObserveHook{RetirementGit: inner, after: 2, onCall: func() { x.failing.Store(tt.failFinal) }}, nil
			}
			e, err := x.d.executeRetirement(x.ctx(t), "sweep-linear", x.session.ID, x.now, opts)
			worktree := retirementLinearDecision(t, e, "worktree")
			_, statErr := os.Stat(x.session.WorktreePath)
			records := x.logs.records(t)
			for _, record := range records {
				if record["source"] != nil && record["source"] != "linear_ticket" {
					t.Errorf("record names source %v, want linear_ticket: %v", record["source"], record)
				}
			}
			if strings.Contains(x.logs.buf.String(), "linear-raw-failure-detail") {
				t.Fatalf("raw error text logged: %s", x.logs.buf.String())
			}
			switch tt.name {
			case "completed", "canceled":
				if err != nil || e.IssueState != "closed" {
					t.Fatalf("closed Linear retirement failed: state=%q err=%v", e.IssueState, err)
				}
				for _, kind := range []string{"worktree", "local_branch", "remote_branch"} {
					if d := retirementLinearDecision(t, e, kind); (d.Action != "removed" && d.Action != "already_absent") || d.Reason != "confirmed_absence" {
						t.Fatalf("%s = %+v, want removed with confirmed_absence", kind, d)
					}
				}
				if !os.IsNotExist(statErr) {
					t.Fatalf("closed Linear worktree still present: %v", statErr)
				}
				saved, err := x.db.GetSession(t.Context(), x.session.ID)
				if err != nil || saved.WorktreePath != "" {
					t.Fatalf("confirmed worktree path not cleared: %q %v", saved.WorktreePath, err)
				}
			case "open":
				if err != nil || e.IssueState != "open" || worktree.Action != "kept" || worktree.Reason != "issue_open" {
					t.Fatalf("open Linear task = %+v state=%q err=%v, want kept issue_open", worktree, e.IssueState, err)
				}
				if statErr != nil {
					t.Fatalf("open Linear worktree removed: %v", statErr)
				}
			case "final_read_fails":
				if err == nil || worktree.Action != "unknown" || worktree.Reason != "issue_lookup_failed" {
					t.Fatalf("final read failure = %+v err=%v, want unknown issue_lookup_failed", worktree, err)
				}
				if statErr != nil {
					t.Fatalf("worktree removed after failed final read: %v", statErr)
				}
				var final, incomplete map[string]any
				for _, record := range records {
					if record["msg"] == "retirement observation" && record["phase"] == "final_closure" {
						final = record
					}
					if record["msg"] == "retirement execution incomplete" {
						incomplete = record
					}
				}
				if final == nil || final["issue_error"] != "issue_lookup_failed" || final["error_class"] == nil || final["error_class"] == "" {
					t.Fatalf("final closure failure lacks bounded cause: %v", final)
				}
				if incomplete == nil || incomplete["error_class"] != final["error_class"] {
					t.Fatalf("execution incomplete lacks bounded cause: %v", incomplete)
				}
			}
		})
	}
}
