package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestLinearCodeDeliveryRequestUsesSeparateGitHubAuthority(t *testing.T) {
	ctx := t.Context()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var response any
		switch r.URL.Path {
		case "/user":
			id, kind := int64(8), "User"
			switch credential {
			case "OWNER", "CHANGED_REPO":
				id = 7
			case "PUBLISHER":
				id, kind = 99, "Bot"
			case "LINEAR_APPROVER":
				id = 42
			}
			response = map[string]any{"id": id, "login": "account", "type": kind}
		case "/repos/code-owner/code":
			id := 101
			if credential == "CHANGED_REPO" {
				id = 999
			}
			response = map[string]any{"id": id, "full_name": "code-owner/code"}
		default:
			t.Errorf("unexpected code request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	linearWorkflowCheck(t, err)
	client := &http.Client{Transport: approverLoopbackTransport{target: target}}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "code-request.db"))
	linearWorkflowCheck(t, err)
	defer func() { linearWorkflowCheck(t, db.Close()) }()
	linearWorkflowCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	linearWorkflowCheck(t, err)
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "PUBLISHER", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	linearWorkflowCheck(t, err)
	linearVersion, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "LINEAR_PUBLISHER", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"open","completed":"done","retired":"retired"}`}}, 0)
	linearWorkflowCheck(t, err)
	identity := ticket.Identity{Provider: ticket.Linear, Origin: linearVersion.Origin, RemoteID: "parent-uuid"}
	ref := ticket.Ref{Identity: identity, Locator: ticket.Locator{Provider: ticket.Linear, Origin: identity.Origin, Container: linearVersion.Destination, Key: identity.RemoteID}}
	_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: linearVersion.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	linearWorkflowCheck(t, err)
	publisher, err := db.LocalTicketSlot(ctx, "actor", ticket.Identity{Provider: ticket.Linear, Origin: linearVersion.Origin, RemoteID: "publisher-uuid"})
	linearWorkflowCheck(t, err)
	appointment, err := db.SetApprover(ctx, store.ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.Linear, Origin: linearVersion.Origin, RemoteID: "human-uuid"}, Handle: "account", VerifiedAt: time.Now(), Provenance: "explicit"}, linearVersion.Revision, 0)
	linearWorkflowCheck(t, err)
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH})
	linearWorkflowCheck(t, err)
	session, err := db.AdmitSessionWithPublishers(ctx, repo, ref, code.Revision, linearVersion.Revision, store.PublishingActor{ID: publisher, Login: "linear-publisher"}, store.PublishingActor{ID: 99, Login: "github-publisher"}, "linear:team/project#1", appointment.Revision)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	request := func(credential, id string) (store.DeliveryControlSource, error) {
		return RequestCodeDelivery(ctx, DeliveryRequestOptions{DB: db, HTTPClient: client, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) { return name, true }}, Credential: secrets.Reference{Source: secrets.SourceEnv, EnvVar: credential}, Parent: session.TaskRef, Generation: 1, RequestID: id})
	}
	for _, credential := range []string{"PUBLISHER", "NONOWNER", "LINEAR_APPROVER", "CHANGED_REPO"} {
		t.Run(credential, func(t *testing.T) {
			if _, err := request(credential, credential); err == nil {
				t.Fatal("unqualified credential authorized code delivery")
			}
			if _, err := db.GetDeliveryControlSource(ctx, "github-cli:"+credential); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected request left executable work: %v", err)
			}
		})
	}
	approved, err := request("OWNER", "request-1")
	linearWorkflowCheck(t, err)
	if approved.ActorID != 7 || approved.CommentID != 0 || approved.Kind != "github-cli-request" || approved.Completed {
		t.Fatalf("CLI source impersonated a ticket reply or publisher: %+v", approved)
	}
	replayed, err := request("OWNER", "request-1")
	linearWorkflowCheck(t, err)
	if replayed != approved {
		t.Fatalf("stable request replay changed its source: %+v %+v", approved, replayed)
	}
	// Installation identity changes cannot redirect already admitted authority.
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 8, Handle: "replacement", Source: store.SourceGH})
	linearWorkflowCheck(t, err)
	if _, err := request("NONOWNER", "replacement-request"); err == nil {
		t.Fatal("installation replacement retargeted saved code authority")
	}
	_, err = request("OWNER", "request-2")
	linearWorkflowCheck(t, err)
}
