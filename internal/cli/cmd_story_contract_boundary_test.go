package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"gopkg.in/yaml.v3"
)

const cliUpgradeParent = "github:owner/repo#1"
const cliUpgradeBody = `<!-- lf:story task=github:owner/repo#1 story=US-001 -->

## User Story

As an owner, I want explicit upgrades, so that history stays intact.

## Acceptance Criteria

- [ ] AC-001: String must be x.

## Tasks

| Task ID | Description | Agent | Size | Depends On | Status |
|---|---|---|---|---|---|
| T-001 | Implement | go-implementation-engineer | M | — | TODO |

## Architecture

SQL stays in store.

## Dependencies

None.
`

type cliUpgradeHTTP struct {
	mu     sync.Mutex
	body   string
	writes int
	botID  int64
}

func (f *cliUpgradeHTTP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var value any
	switch r.URL.Path {
	case "/repos/owner/repo":
		value = map[string]any{"id": 500, "full_name": "owner/repo", "has_issues": true}
	case "/user":
		value = map[string]any{"id": f.botID, "login": "renamed-bot"}
	case "/repos/owner/repo/issues/2/parent", "/repos/owner/repo/issues/1":
		value = map[string]any{"id": 100, "number": 1, "url": "http://" + r.Host + "/repos/owner/repo/issues/1", "html_url": "http://" + r.Host + "/owner/repo/issues/1", "title": "Parent", "state": "open", "user": map[string]any{"id": 42}}
	case "/repos/owner/repo/issues/2":
		if r.Method == http.MethodPatch {
			var update struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.body = update.Body
			f.writes++
		}
		value = map[string]any{"id": 200, "number": 2, "url": "http://" + r.Host + "/repos/owner/repo/issues/2", "html_url": "http://" + r.Host + "/owner/repo/issues/2", "title": "US-001", "body": f.body, "state": "open", "user": map[string]any{"id": 9}}
	default:
		http.Error(w, "unexpected endpoint "+r.URL.Path, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func newCLIUpgradeBoundary(t *testing.T, body string, botID int64) (*store.DB, *cliUpgradeHTTP, string) {
	t.Helper()
	paths := newRepoHome(t)
	fake := &cliUpgradeHTTP{body: body, botID: botID}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	raw, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["providers"] = map[string]any{"github": map[string]any{"api_base": server.URL + "/", "token_env": initTokenEnvVar}}
	raw, err = yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigPath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(initTokenEnvVar, "test-only-upgrade-credential")
	// A successful run with an empty executable search path cannot invoke gh.
	t.Setenv("PATH", t.TempDir())
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, id := range []store.Identity{{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 42, Source: store.SourceGH}, {Forge: store.ForgeGitHub, Role: store.RoleBot, Handle: "old-bot-name", UserID: 9, Source: store.SourceForgeAPI}} {
		if _, err := db.UpsertIdentity(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(paths.Home(), "repo"), Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", RemoteURL: "git@github.com:owner/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(paths.Home(), "worktrees")}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: cliUpgradeParent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: cliUpgradeParent, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}); err != nil {
		t.Fatal(err)
	}
	revision := planbrief.StoryHash("US-001", body)
	if err := db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: cliUpgradeParent, Generation: 1, Key: "US-001", IssueID: 200, IssueNumber: 2, BodyHash: revision, Dependencies: []string{}, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: cliUpgradeParent, Generation: 1, Key: "US-002", IssueID: 300, IssueNumber: 3, BodyHash: strings.Repeat("c", 64), Dependencies: []string{}, State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), cliUpgradeParent, 1, true); err != nil {
		t.Fatal(err)
	}
	return db, fake, revision
}

func TestStoryContractUpgrade_CLIBoundary(t *testing.T) {
	for _, tt := range []struct {
		name     string
		flags    []string
		apply    bool
		conflict bool
		botID    int64
	}{
		{name: "default preview", botID: 9},
		{name: "explicit preview", flags: []string{"--preview"}, botID: 9},
		{name: "explicit apply", flags: []string{"--apply"}, apply: true, botID: 9},
		{name: "ambiguous refusal", flags: []string{"--apply"}, conflict: true, botID: 9},
		{name: "foreign numeric bot", flags: []string{"--apply"}, botID: 99},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := cliUpgradeBody
			if tt.conflict {
				body = strings.Replace(body, "AC-001: ", "", 1)
			}
			db, fake, revision := newCLIUpgradeBoundary(t, body, tt.botID)
			before, err := db.GetManagedFeature(t.Context(), cliUpgradeParent)
			if err != nil {
				t.Fatal(err)
			}
			router, out, errout := newTestRouter(t)
			args := append([]string{"story-contract", "upgrade", cliUpgradeParent}, tt.flags...)
			code := router.Run(t.Context(), args)
			after, err := db.GetManagedFeature(t.Context(), cliUpgradeParent)
			if err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			writes, current := fake.writes, fake.body
			fake.mu.Unlock()
			if tt.conflict || tt.botID != 9 {
				if code == ExitOK || writes != 0 || current != body || !reflect.DeepEqual(before, after) {
					t.Fatalf("refusal mutated state: exit=%d writes=%d before=%+v after=%+v stdout=%s stderr=%s", code, writes, before, after, out, errout)
				}
				if tt.conflict && !strings.Contains(out.String(), "Conflict:") {
					t.Fatalf("missing conflict output: %s %s", out, errout)
				}
				if tt.botID != 9 && !strings.Contains(errout.String(), "numeric bot identity") {
					t.Fatalf("wrong identity reason: %s", errout)
				}
				return
			}
			if code != ExitOK {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, out, errout)
			}
			for _, text := range []string{cliUpgradeParent, "US-001 #2", revision, "contract 1 -> 2"} {
				if !strings.Contains(out.String(), text) {
					t.Errorf("missing %q: %s", text, out)
				}
			}
			if strings.Contains(out.String(), "US-002 #3") {
				t.Fatal("completed child included in unfinished cohort")
			}
			if !tt.apply {
				if writes != 0 || current != body || !reflect.DeepEqual(before, after) || !strings.Contains(out.String(), "Preview only") {
					t.Fatalf("preview changed state: writes=%d before=%+v after=%+v stdout=%s", writes, before, after, out)
				}
			} else {
				if writes != 1 || after.Generation != 2 || after.WorkflowVersion != before.WorkflowVersion || !strings.Contains(current, planbrief.StoryGuidance) || !strings.Contains(out.String(), "approved Plan") {
					t.Fatalf("apply not durable/bounded: writes=%d state=%+v stdout=%s", writes, after, out)
				}
			}
			contract, err := db.GetGenerationContract(t.Context(), cliUpgradeParent, 1)
			if err != nil {
				t.Fatal(err)
			}
			if contract.ContentVersion != 1 || contract.SubmissionVersion != 1 {
				t.Fatalf("historical contract rewritten: %+v", contract)
			}
		})
	}
}

func TestStoryContractUpgradeRetainsParentConnection(t *testing.T) {
	db, fake, _ := newCLIUpgradeBoundary(t, cliUpgradeBody, 9)
	repo, err := db.GetRepoBySlug(t.Context(), store.ForgeGitHub, "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := config.Locate()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := ticket.CanonicalOrigin(cfg.Providers.GitHub.APIBase)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LF_UPGRADE_PINNED_TOKEN", "retained-upgrade-credential")
	version := store.ConnectionVersion{RepoID: repo.ID, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: "500", CredentialSource: "env", CredentialEnv: "LF_UPGRADE_PINNED_TOKEN", Enabled: true, Provenance: "explicit"}
	version.Role = store.CodeConnection
	version.ClonePath = repo.Path
	version.RemoteURL = repo.RemoteURL
	version.BaseBranch = repo.BaseBranch
	version.FeaturePrefix = repo.FeaturePrefix
	version.WorktreeRoot = repo.WorktreeRoot
	code, err := db.SetConnection(t.Context(), version, 0)
	if err != nil {
		t.Fatal(err)
	}
	version.Role = store.TicketConnection
	tickets, err := db.SetConnection(t.Context(), version, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "100"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.Slug(), Key: "1"}}
	if _, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	parent, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 9, "renamed-bot", cliUpgradeParent, "500", "500")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DisableConnection(t.Context(), repo.ID, store.TicketConnection, tickets.Revision); err != nil {
		t.Fatal(err)
	}
	// The current legacy credential is unavailable, and the child has no
	// session. Every child read must use the parent's retained credential.
	t.Setenv(initTokenEnvVar, "")
	router, out, errout := newTestRouter(t)
	if code := router.Run(t.Context(), []string{"story-contract", "upgrade", cliUpgradeParent}); code != ExitOK {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out, errout)
	}
	if !strings.Contains(out.String(), "US-001 #2") {
		t.Fatalf("preview omitted managed child: %s", out)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), parent.ID)
	if err != nil || snapshot.TicketVersionID != tickets.ID {
		t.Fatalf("preview changed parent routing: %+v, %v", snapshot, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.writes != 0 {
		t.Fatal("preview published changes")
	}
}
