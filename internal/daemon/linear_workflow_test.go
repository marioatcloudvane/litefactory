package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// linearWorkflowServer is the external GraphQL boundary. Factory adapters,
// authority checks and durable SQLite records run unchanged in the scenario.
type linearWorkflowServer struct {
	t              *testing.T
	mu             sync.Mutex
	server         *httptest.Server
	issues         map[string]map[string]any
	comments       map[string]map[string]any
	creates, posts int
	loseReply      bool
}

func newLinearWorkflowServer(t *testing.T) *linearWorkflowServer {
	t.Helper()
	s := &linearWorkflowServer{t: t, issues: map[string]map[string]any{}, comments: map[string]map[string]any{}}
	s.issues["parent-uuid"] = s.issue("parent-uuid", "LF-1", "Parent", "/plan connect Linear tickets", "human-uuid")
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}
func (s *linearWorkflowServer) actor(id string) map[string]any {
	return map[string]any{"id": id, "name": id, "displayName": id, "email": id + "@example.test", "active": true, "app": id == "publisher-uuid", "isBot": id == "publisher-uuid", "isMe": id == "publisher-uuid", "organization": map[string]any{"id": "workspace-uuid"}}
}
func (s *linearWorkflowServer) issue(id, key, title, body, actor string) map[string]any {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	return map[string]any{"id": id, "identifier": key, "url": "https://linear.app/test/issue/" + key, "title": title, "description": body, "createdAt": at, "updatedAt": at, "creator": s.actor(actor), "team": map[string]any{"id": "team-uuid", "organization": map[string]any{"id": "workspace-uuid"}}, "project": map[string]any{"id": "project-uuid", "teams": s.page([]any{map[string]any{"id": "team-uuid"}})}, "state": map[string]any{"id": "state-open", "name": "Open", "type": "unstarted"}, "labels": s.page([]any{})}
}
func (s *linearWorkflowServer) page(nodes []any) map[string]any {
	return map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}}
}
func (s *linearWorkflowServer) addReply(id, body, actor string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments[id] = map[string]any{"id": id, "body": body, "user": s.actor(actor), "issue": s.issues["parent-uuid"], "createdAt": time.Now().UTC().Add(time.Hour + time.Duration(len(s.comments))*time.Second).Format(time.RFC3339), "updatedAt": time.Now().UTC().Add(time.Hour + time.Duration(len(s.comments))*time.Second).Format(time.RFC3339), "url": "https://linear.app/test/comment/" + id}
}
func (s *linearWorkflowServer) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-RateLimit-Requests-Limit", "5000")
	w.Header().Set("X-RateLimit-Requests-Remaining", "4900")
	w.Header().Set("X-RateLimit-Requests-Reset", fmt.Sprint(time.Now().Add(24*time.Hour).UnixMilli()))
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "test-linear-token" {
		s.t.Errorf("unexpected Linear transport: %s authorization=%t", r.Method, r.Header.Get("Authorization") == "test-linear-token")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.t.Error(err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	q, v := request.Query, request.Variables
	id, _ := v["id"].(string)
	input, _ := v["input"].(map[string]any)
	var data map[string]any
	switch {
	case strings.Contains(q, "reactionCreate("):
		data = map[string]any{"reactionCreate": map[string]any{"success": true}}
	case strings.Contains(q, "issueCreate("):
		if input["teamId"] != "team-uuid" || input["projectId"] != "project-uuid" {
			s.t.Error("child creation targeted the wrong team or project")
		}
		s.creates++
		createdID, _ := input["id"].(string)
		if createdID == "" {
			s.t.Error("child creation omitted retained UUID")
			createdID = fmt.Sprintf("child-%d", s.creates)
		}
		title, _ := input["title"].(string)
		body, _ := input["description"].(string)
		child := s.issue(createdID, "LF-2", title, body, "publisher-uuid")
		if parentID, _ := input["parentId"].(string); parentID != "" {
			child["parent"] = s.issues[parentID]
		}
		s.issues[createdID] = child
		data = map[string]any{"issueCreate": map[string]any{"success": true, "issue": child}}
	case strings.Contains(q, "commentCreate("):
		s.posts++
		createdID, _ := input["id"].(string)
		if createdID == "" {
			s.t.Error("reply creation omitted retained UUID")
			createdID = fmt.Sprintf("publication-%d", s.posts)
		}
		target, _ := input["issueId"].(string)
		body, _ := input["body"].(string)
		reply := map[string]any{"id": createdID, "body": body, "user": s.actor("publisher-uuid"), "issue": s.issues[target], "url": "https://linear.app/test/comment/" + createdID, "createdAt": "2026-10-11T12:05:00Z", "updatedAt": "2026-10-11T12:05:00Z"}
		s.comments[createdID] = reply
		if s.loseReply {
			s.loseReply = false
			http.Error(w, "lost receipt", http.StatusServiceUnavailable)
			return
		}
		data = map[string]any{"commentCreate": map[string]any{"success": true, "comment": reply}}
	case strings.Contains(q, "issueUpdate("):
		issue := s.issues[id]
		if issue == nil {
			s.t.Errorf("mutation targeted unknown identity %q", id)
		}
		for _, field := range []string{"title", "description"} {
			if value, ok := input[field]; ok {
				issue[field] = value
			}
		}
		if parentID, ok := input["parentId"].(string); ok {
			issue["parent"] = s.issues[parentID]
		}
		if stateID, ok := input["stateId"].(string); ok {
			kind := "unstarted"
			if stateID == "state-completed" {
				kind = "completed"
			}
			if stateID == "state-retired" {
				kind = "canceled"
			}
			issue["state"] = map[string]any{"id": stateID, "name": stateID, "type": kind}
		}
		if labels, ok := input["labelIds"].([]any); ok {
			nodes := []any{}
			for _, label := range labels {
				nodes = append(nodes, map[string]any{"id": label, "name": "Mode: Plan"})
			}
			issue["labels"] = s.page(nodes)
		}
		data = map[string]any{"issueUpdate": map[string]any{"success": true, "issue": issue}}
	case strings.Contains(q, "issueLabel("):
		data = map[string]any{"issueLabel": map[string]any{"id": id, "team": map[string]any{"id": "team-uuid"}, "organization": map[string]any{"id": "workspace-uuid"}}}
	case strings.Contains(q, "workflowState("):
		kind := "unstarted"
		switch id {
		case "state-completed":
			kind = "completed"
		case "state-retired":
			kind = "canceled"
		}
		data = map[string]any{"workflowState": map[string]any{"id": id, "type": kind, "team": map[string]any{"id": "team-uuid"}}}
	case strings.Contains(q, "webhooks("):
		data = map[string]any{"webhooks": s.page([]any{})}
	case strings.Contains(q, "viewer"):
		data = map[string]any{"viewer": s.actor("publisher-uuid"), "organization": map[string]any{"id": "workspace-uuid"}}
	case strings.Contains(q, "user("):
		data = map[string]any{"user": s.actor(id)}
	case strings.Contains(q, "team("):
		data = map[string]any{"team": map[string]any{"id": "team-uuid", "organization": map[string]any{"id": "workspace-uuid"}, "states": s.page([]any{map[string]any{"id": "state-open", "type": "unstarted"}, map[string]any{"id": "state-completed", "type": "completed"}, map[string]any{"id": "state-retired", "type": "canceled"}})}, "project": map[string]any{"id": "project-uuid", "teams": s.page([]any{map[string]any{"id": "team-uuid"}})}}
	case strings.Contains(q, "comment("):
		data = map[string]any{"comment": s.comments[id]}
	case strings.Contains(q, "comments("):
		nodes := []any{}
		for _, reply := range s.comments {
			if wanted := linearFilterID(v, "issue"); wanted == "" || linearWorkflowObject(s.t, reply["issue"])["id"] == wanted {
				nodes = append(nodes, reply)
			}
		}
		sort.Slice(nodes, func(i, j int) bool {
			return fmt.Sprint(linearWorkflowObject(s.t, nodes[i])["createdAt"]) < fmt.Sprint(linearWorkflowObject(s.t, nodes[j])["createdAt"])
		})
		issue := make(map[string]any)
		for key, value := range s.issues[id] {
			issue[key] = value
		}
		issue["comments"] = s.page(nodes)
		data = map[string]any{"comments": s.page(nodes), "issue": issue}
	case strings.Contains(q, "issues("):
		nodes := []any{}
		for _, issue := range s.issues {
			if wanted := linearFilterID(v, "parent"); wanted != "" {
				if parent, ok := issue["parent"].(map[string]any); !ok || parent["id"] != wanted {
					continue
				}
			}
			nodes = append(nodes, issue)
		}
		data = map[string]any{"issues": s.page(nodes)}
	case strings.Contains(q, "issue("):
		issue := s.issues[id]
		if issue == nil {
			for _, candidate := range s.issues {
				if candidate["identifier"] == id {
					issue = candidate
				}
			}
		}
		data = map[string]any{"issue": issue}
	default:
		s.t.Errorf("unexpected GraphQL operation %s", q)
		data = map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		s.t.Error(err)
	}
}

func linearFilterID(variables map[string]any, field string) string {
	filter, _ := variables["filter"].(map[string]any)
	parent, _ := filter[field].(map[string]any)
	id, _ := parent["id"].(map[string]any)
	eq, _ := id["eq"].(string)
	return eq
}

// TestLinearTicketWorkflow follows qualified Linear sources through factory
// project setup, daemon authority admission and the existing Plan question and
// exact-review policy, then exercises the real managed-child and report ledgers.
func TestLinearTicketWorkflow(t *testing.T)      { linearTicketWorkflow(t, false) }
func TestLinearSetApprovalWorkflow(t *testing.T) { linearTicketWorkflow(t, true) }

func linearTicketWorkflow(t *testing.T, setApproval bool) {
	ctx := t.Context()
	remote := newLinearWorkflowServer(t)
	codeRemote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer test-github-token" {
			t.Error("GitHub admission selected a ticket credential")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var response any
		switch r.URL.Path {
		case "/user":
			response = map[string]any{"id": 99, "login": "github-publisher", "type": "Bot"}
		case "/repos/code-owner/code":
			response = map[string]any{"id": 101, "full_name": "code-owner/code"}
		default:
			t.Errorf("ticket workflow unexpectedly reached GitHub: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer codeRemote.Close()
	codeURL, err := url.Parse(codeRemote.URL)
	linearWorkflowCheck(t, err)
	httpClient := &http.Client{Transport: linearMixedProviderTransport{githubURL: codeURL}}
	dbPath := filepath.Join(t.TempDir(), "linear-workflow.db")
	db, err := store.Open(ctx, dbPath)
	linearWorkflowCheck(t, err)
	defer func() { linearWorkflowCheck(t, db.Close()) }()
	linearWorkflowCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	linearWorkflowCheck(t, err)
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "CODE_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	linearWorkflowCheck(t, err)
	version, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(remote.server.URL), Destination: "team-uuid/project-uuid", CredentialSource: "env", CredentialEnv: "LINEAR_TOKEN", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}}, 0)
	linearWorkflowCheck(t, err)
	factoryOptions := projectconnection.Options{DB: db, HTTPClient: httpClient, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		switch name {
		case "LINEAR_TOKEN":
			return "test-linear-token", true
		case "CODE_TOKEN":
			return "test-github-token", true
		default:
			t.Errorf("unexpected credential reference %s", name)
			return "", false
		}
	}}}
	factory := projectconnection.NewFactory(factoryOptions)
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH})
	linearWorkflowCheck(t, err)
	appointment, err := (projectconnection.ApproverService{DB: db, Factory: factory}).Set(ctx, repo.ID, "human-uuid", version.Revision, 0)
	linearWorkflowCheck(t, err)
	clients, err := factory.Clients(ctx, version, 0)
	linearWorkflowCheck(t, err)
	if clients.Linear == nil || appointment.Actor.RemoteID != "human-uuid" || clients.Publisher.Identity.RemoteID != "publisher-uuid" {
		t.Fatalf("unverified setup: %+v %+v", appointment, clients.Publisher)
	}
	adapter := clients.Linear
	inspection, err := adapter.Inspect(ctx)
	linearWorkflowCheck(t, err)
	if inspection.WorkspaceID != "workspace-uuid" || inspection.TeamID != "team-uuid" || inspection.ProjectID != "project-uuid" {
		t.Fatalf("setup did not verify the configured destination: %+v", inspection)
	}
	parent, err := adapter.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: version.Origin, Container: version.Destination, Key: "parent-uuid"})
	linearWorkflowCheck(t, err)
	originalBinding, err := db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: version.ID, Ref: parent.Ref, Enabled: true, Provenance: "explicit"})
	linearWorkflowCheck(t, err)
	issueSlot, err := db.LocalTicketSlot(ctx, "issue", parent.Ref.Identity)
	linearWorkflowCheck(t, err)
	botSlot, err := db.LocalTicketSlot(ctx, "actor", clients.Publisher.Identity)
	linearWorkflowCheck(t, err)
	ownerSlot, err := db.LocalTicketSlot(ctx, "actor", appointment.Actor)
	linearWorkflowCheck(t, err)
	ref := naming.Ref{Forge: "linear", Owner: "team-uuid", Repo: "project-uuid", Issue: int(issueSlot)}
	// A pending reply under a routine new version must reuse the old binding.
	version.Linear.ModeLabelsJSON = `{"plan":"label-plan"}`
	version, err = db.SetConnection(ctx, version, version.Revision)
	linearWorkflowCheck(t, err)
	position, err := db.InitTicketRecoveryPosition(ctx, version, time.Now())
	linearWorkflowCheck(t, err)
	_, err = db.PinTicketRecoveryContainer(ctx, version.ID, position.Revision, "project-uuid")
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.HandoffTicketObservation(ctx, store.PendingTicketObservation{ConnectionVersionID: version.ID, RepoID: repo.ID, Provider: ticket.Linear, Origin: version.Origin, Container: version.Destination, ContainerID: "project-uuid", TicketID: parent.Ref.Identity.RemoteID, TicketKey: parent.Ref.Locator.Key, ObservedAt: time.Now()}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	recoveryDaemon := &Daemon{db: db, connections: factory, log: logger}
	now := time.Now()
	recoveryDaemon.quotas.tick(now)
	factory.SetRequestContext(recoveryDaemon.bindQuotaRequest)
	queue := &repoQueue{repo: repo, forgeRepo: githubRepo(version.Destination), log: logger}
	seen := make(map[int64]*candidate)
	linearWorkflowCheck(t, recoveryDaemon.drainTicketObservations(ctx, map[int64]*repoQueue{repo.ID: queue}, seen))
	session, err := db.GetSessionByTicket(ctx, parent.Ref.Identity)
	linearWorkflowCheck(t, err)
	if len(seen) != 1 || !session.NeedsPoll {
		t.Fatalf("recovery lost pending work: %+v %+v", session, seen)
	}
	linearWorkflowCheck(t, recoveryDaemon.drainTicketObservations(ctx, map[int64]*repoQueue{repo.ID: queue}, seen))
	if len(seen) != 1 {
		t.Fatalf("duplicate recovery: %+v", seen)
	}
	savedBinding, err := db.GetTicketBindingByID(ctx, originalBinding.ID)
	linearWorkflowCheck(t, err)
	if savedBinding != originalBinding {
		t.Fatalf("recovery changed binding: %+v", savedBinding)
	}
	session, err = AdmitProjectTicket(ctx, db, factory, repo, parent.Ref)
	linearWorkflowCheck(t, err)
	snapshot, err := db.GetExecutionSnapshot(ctx, session.ID)
	linearWorkflowCheck(t, err)
	authority, err := db.EnsureDeliveryCodeAuthority(ctx, session.ID)
	linearWorkflowCheck(t, err)
	if snapshot.BindingID != originalBinding.ID || snapshot.TicketVersionID != version.ID || snapshot.ActorID != botSlot || snapshot.CodeActorID != 99 || authority.OwnerID != 7 || authority.PublisherID != 99 || session.TaskRef != ref.String() {
		t.Fatalf("production admission failed to pin distinct ticket and code identities: %+v %+v", snapshot, authority)
	}
	codeClients, err := factory.Clients(ctx, code, 99)
	linearWorkflowCheck(t, err)
	startupTickets, err := github.NewTickets(codeClients.Code)
	linearWorkflowCheck(t, err)
	selector := func(ctx context.Context, r github.Repo, number int, sessionID int64) (turn.WorkflowTickets, int64, error) {
		return selectTicketConnection(ctx, db, factory, r, number, sessionID)
	}
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: ownerSlot, BotUserID: botSlot, TicketOrigin: version.Origin}, logger)
	linearWorkflowCheck(t, err)
	d := &Daemon{db: db, connections: factory, filter: filter, log: logger}
	readAndAdmit := func(id, body string) turn.Admission {
		t.Helper()
		remote.addReply(id, body, "human-uuid")
		page, e := adapter.RepliesAfter(ctx, parent.Ref, linear.ReplyCursor(parent.Ref))
		linearWorkflowCheck(t, e)
		for _, reply := range page.Replies {
			if reply.Identity.RemoteID == id {
				admission, e := d.admitReply(ctx, session.ID, github.Comment{}, &reply)
				linearWorkflowCheck(t, e)
				if !admission.Triggers() {
					t.Fatalf("verified source rejected: %+v", admission)
				}
				return admission
			}
		}
		t.Fatalf("missing source %s", id)
		return turn.Admission{}
	}
	remote.addReply("other-human-uuid", "/plan bypass the approver", "other-human")
	page, err := adapter.RepliesAfter(ctx, parent.Ref, linear.ReplyCursor(parent.Ref))
	linearWorkflowCheck(t, err)
	for _, reply := range page.Replies {
		if reply.Identity.RemoteID != "other-human-uuid" {
			continue
		}
		admission, e := d.admitReply(ctx, session.ID, github.Comment{}, &reply)
		linearWorkflowCheck(t, e)
		if admission.Triggers() || admission.Comment.Classification != store.ClassificationThirdParty {
			t.Fatalf("another Linear user acquired project authority: %+v", admission)
		}
	}
	incident := readAndAdmit("4503599627420835", "`/approve-plan set ab853aa4bf79081a5330b2499672434e34f380bd18013b75a5df1392c2bef64f`")
	if got := turn.ParseCommand(incident.Comment.Body); got.Kind != turn.KindPlanControl {
		t.Fatalf("incident approval classified as %+v", got)
	}
	instruction := readAndAdmit("instruction-uuid", "/plan connect Linear tickets")
	command := turn.ParseCommand(instruction.Comment.Body)
	if mode, ok := command.Mode(); !ok || mode != agent.ModePlan {
		t.Fatalf("Linear instruction changed command policy: %+v", command)
	}
	bridge, err := turn.NewRoutedGitHubTicketWorkflow(db, startupTickets, 99, selector)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, bridge.AddIssueLabel(ctx, github.RepoFromRef(ref), ref.Issue, "Mode: Plan"))
	modeSnapshot, err := adapter.Resolve(ctx, parent.Ref.Locator)
	linearWorkflowCheck(t, err)
	if !slices.Contains(modeSnapshot.Labels, "Mode: Plan") {
		t.Fatalf("configured Plan label was not confirmed: %+v", modeSnapshot.Labels)
	}
	redactor := redact.New(redact.Options{Token: secrets.NewToken("test-linear-token"), Environ: []string{}})
	renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: logger})
	linearWorkflowCheck(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
	linearWorkflowCheck(t, err)
	if setApproval {
		linearSetApproval(t, d, remote, adapter, parent.Ref, bridge, reporter, redactor, repo, session, ref)
		return
	}
	proposal := planbrief.PlanProposal{Version: planbrief.ProposalVersion, Parent: ref.String(), Generation: 1, Stories: []planbrief.StoryProposal{{Key: "story-001", Intent: "create", Title: "Use Linear work tickets", Body: "## User Story\nOwners direct ticket work from Linear.\n## Acceptance Criteria\nQualified answers and exact approvals reach the intended ticket.\n## Tasks\nPreserve immutable provider identity.\n## Architecture\nUse the existing ledgers.\n## Dependencies\nNone.\n"}}}
	index, err := turn.PublishPlan(ctx, turn.PublicationOptions{DB: db, Forge: bridge, Renderer: renderer, Parent: ref, Generation: 1, BotID: botSlot}, proposal)
	linearWorkflowCheck(t, err)
	if !strings.Contains(string(index), "https://linear.app/test/issue/LF-2") {
		t.Fatalf("managed child index has wrong destination: %s", index)
	}
	managed, err := db.ListManagedStories(ctx, ref.String(), 1)
	linearWorkflowCheck(t, err)
	if len(managed) != 1 {
		t.Fatalf("managed children: %+v", managed)
	}
	child, err := bridge.GetIssue(ctx, github.RepoFromRef(ref), managed[0].IssueNumber)
	linearWorkflowCheck(t, err)
	if child.QualifiedSnapshot == nil || child.QualifiedSnapshot.Ref.Identity.Provider != ticket.Linear {
		t.Fatalf("managed child lost its qualified identity: %+v", child.QualifiedSnapshot)
	}
	remote.mu.Lock()
	remoteChild := remote.issues[child.QualifiedSnapshot.Ref.Identity.RemoteID]
	childParent, parentAttached := remoteChild["parent"].(map[string]any)
	remote.mu.Unlock()
	if !parentAttached || childParent["id"] != "parent-uuid" || linearWorkflowObject(t, remoteChild["team"])["id"] != "team-uuid" || linearWorkflowObject(t, remoteChild["project"])["id"] != "project-uuid" {
		t.Fatalf("managed child relationship differs from verified destination: %+v", remoteChild)
	}
	state := planbrief.PlanState{SharedRevision: "linear-workflow-v1"}
	linearWorkflowCheck(t, state.PutCandidate(planbrief.PlanDraft{Key: "story-001", Name: "Use Linear work tickets"}, "story", "candidate-1"))
	linearWorkflowCheck(t, state.OpenPlanWait("story-001", "question-1", "question", issueSlot))
	_, err = db.SavePlanWorkflowState(ctx, ref.String(), 0, state)
	linearWorkflowCheck(t, err)
	question, err := reporter.ReportRun(ctx, turn.RunReport{Run: report.Run{Ref: ref, Ordinal: 1, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Which team should own this work?"}}, SessionID: session.ID, CommentID: instruction.Comment.ID})
	linearWorkflowCheck(t, err)
	if question.CommentID <= 0 {
		t.Fatal("question did not receive a durable qualified receipt")
	}
	answer := readAndAdmit("answer-uuid", "Use the selected team and project.")
	saved, revision, err := db.GetPlanWorkflowState(ctx, ref.String())
	linearWorkflowCheck(t, err)
	promoted, err := saved.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: answer.Comment.ID, OwnerID: ownerSlot, ActorID: ownerSlot, IssueID: issueSlot, Key: "story-001", Stage: "story", Candidate: "candidate-1", WaitID: "question-1", Body: answer.Comment.Body})
	linearWorkflowCheck(t, err)
	if promoted || saved.Stories[0].Wait != nil {
		t.Fatal("answer failed to resume without approving the story")
	}
	linearWorkflowCheck(t, saved.OpenPlanWait("story-001", "review-1", "review", issueSlot))
	_, err = db.SavePlanWorkflowState(ctx, ref.String(), revision, saved)
	linearWorkflowCheck(t, err)
	approval := readAndAdmit("approval-uuid", "/approve-plan story-001 story candidate-1")
	saved, revision, err = db.GetPlanWorkflowState(ctx, ref.String())
	linearWorkflowCheck(t, err)
	_, staleErr := saved.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: approval.Comment.ID, OwnerID: ownerSlot, ActorID: ownerSlot, IssueID: issueSlot, Key: "story-001", Stage: "story", Candidate: "old-candidate", WaitID: "review-1", Body: "/approve-plan story-001 story old-candidate"})
	if staleErr == nil || saved.Stories[0].Stage != "story" || saved.Stories[0].Wait == nil {
		t.Fatal("a stale candidate approval advanced or discarded the current review")
	}
	promoted, err = saved.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: approval.Comment.ID, OwnerID: ownerSlot, ActorID: ownerSlot, IssueID: issueSlot, Key: "story-001", Stage: "story", Candidate: "candidate-1", WaitID: "review-1", Body: approval.Comment.Body})
	linearWorkflowCheck(t, err)
	if !promoted || saved.Stories[0].Stage != "technical" {
		t.Fatal("exact approval did not advance the existing Plan policy")
	}
	_, err = db.SavePlanWorkflowState(ctx, ref.String(), revision, saved)
	linearWorkflowCheck(t, err)
	posted, err := reporter.ReportRun(ctx, turn.RunReport{Run: report.Run{Ref: ref, Ordinal: 2, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "The selected Linear story draft was approved."}}, SessionID: session.ID, CommentID: approval.Comment.ID})
	linearWorkflowCheck(t, err)
	if posted.CommentID <= 0 {
		t.Fatal("report has no confirmed receipt")
	}
	binding, err := db.GetSessionTicket(ctx, session.ID)
	linearWorkflowCheck(t, err)
	if binding.RemoteID != "parent-uuid" || binding.Locator.Provider != ticket.Linear {
		t.Fatalf("qualified identity was replaced by a slot: %+v", binding)
	}
	remote.mu.Lock()
	confirmedPosts := remote.posts
	remote.mu.Unlock()
	linearWorkflowCheck(t, db.Close())
	db, err = store.Open(ctx, dbPath)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.Migrate(ctx))
	factoryOptions.DB = db
	factory = projectconnection.NewFactory(factoryOptions)
	restored, _, err := db.GetPlanWorkflowState(ctx, ref.String())
	linearWorkflowCheck(t, err)
	promoted, err = restored.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: approval.Comment.ID, OwnerID: ownerSlot, ActorID: ownerSlot, IssueID: issueSlot, Key: "story-001", Stage: "story", Candidate: "candidate-1", WaitID: "review-1", Body: approval.Comment.Body})
	linearWorkflowCheck(t, err)
	if promoted || restored.Stories[0].Stage != "technical" {
		t.Fatal("restart replay repeated or lost the approved transition")
	}
	bridge, err = turn.NewRoutedGitHubTicketWorkflow(db, startupTickets, 99, selector)
	linearWorkflowCheck(t, err)
	reporter, err = turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
	linearWorkflowCheck(t, err)
	replayedReport, err := reporter.ReportRun(ctx, turn.RunReport{Run: report.Run{Ref: ref, Ordinal: 2, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "The selected Linear story draft was approved."}}, SessionID: session.ID, CommentID: approval.Comment.ID})
	linearWorkflowCheck(t, err)
	if !replayedReport.Skipped {
		t.Fatal("restart duplicated the confirmed workflow report")
	}
	replayedIndex, err := turn.PublishPlan(ctx, turn.PublicationOptions{DB: db, Forge: bridge, Renderer: renderer, Parent: ref, Generation: 1, BotID: botSlot}, proposal)
	linearWorkflowCheck(t, err)
	if string(replayedIndex) != string(index) {
		t.Fatal("restart changed the retained child index")
	}
	remote.mu.Lock()
	if remote.posts != confirmedPosts {
		t.Fatalf("restart repeated a confirmed workflow publication: before=%d after=%d", confirmedPosts, remote.posts)
	}
	if remote.creates != 1 {
		t.Fatalf("managed child duplicated: %d", remote.creates)
	}
	questionSeen, reportSeen := false, false
	for _, reply := range remote.comments {
		if linearWorkflowObject(t, reply["user"])["id"] == "publisher-uuid" {
			body, _ := reply["body"].(string)
			questionSeen = questionSeen || strings.Contains(body, "Which team should own this work?")
			reportSeen = reportSeen || strings.Contains(body, "The selected Linear story draft was approved.")
		}
		if linearWorkflowObject(t, reply["user"])["id"] == "publisher-uuid" && linearWorkflowObject(t, reply["issue"])["id"] != "parent-uuid" {
			t.Fatalf("question or report redirected: %+v", reply)
		}
	}
	if !questionSeen || !reportSeen {
		t.Fatalf("confirmed publications omitted their prepared contents: question=%t report=%t", questionSeen, reportSeen)
	}
	remote.mu.Unlock()
	linearScopeFreeze(t, db, session, ref, bridge, renderer, reporter, redactor, logger, remote)
	t.Log("Linear verified instruction, published question, answer, exact Plan approval, managed child and report confirmed through HTTP and SQLite")
}

func linearWorkflowCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func linearWorkflowObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected provider object, received %T", value)
	}
	return object
}

// Only the GitHub transport is redirected; Linear requests retain their own
// origin and credential through the same production connection factory.
type linearMixedProviderTransport struct {
	githubURL *url.URL
}

func (transport linearMixedProviderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "api.github.com" {
		return http.DefaultTransport.RoundTrip(request)
	}
	local := request.Clone(request.Context())
	localURL := *request.URL
	localURL.Scheme, localURL.Host = transport.githubURL.Scheme, transport.githubURL.Host
	local.URL = &localURL
	return http.DefaultTransport.RoundTrip(local)
}

// Exercise the real Linear adapter and durable report ledger for fresh and
// retained selections, including a remote post whose receipt is lost.
func TestLinearUnmappedModeRefusal(t *testing.T) {
	for _, scenario := range []string{"fresh", "pending", "lost_receipt", "body/fresh", "body/lost_receipt", "team/fresh", "team/lost_receipt", "team/body/fresh", "team/body/lost_receipt", "workflow/fresh", "workflow/lost_receipt", "workflow/body/fresh", "workflow/body/lost_receipt"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			remote := newLinearWorkflowServer(t)
			codeRemote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer test-github-token" {
					t.Error("GitHub admission selected a ticket credential")
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				var response any
				switch r.URL.Path {
				case "/user":
					response = map[string]any{"id": 99, "login": "github-publisher", "type": "Bot"}
				case "/repos/code-owner/code":
					response = map[string]any{"id": 101, "full_name": "code-owner/code"}
				default:
					t.Errorf("ticket workflow unexpectedly reached GitHub: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer codeRemote.Close()
			codeURL, err := url.Parse(codeRemote.URL)
			linearWorkflowCheck(t, err)
			httpClient := &http.Client{Transport: linearMixedProviderTransport{githubURL: codeURL}}
			dbPath := filepath.Join(t.TempDir(), "linear-workflow.db")
			db, err := store.Open(ctx, dbPath)
			linearWorkflowCheck(t, err)
			defer func() { linearWorkflowCheck(t, db.Close()) }()
			linearWorkflowCheck(t, db.Migrate(ctx))
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			linearWorkflowCheck(t, err)
			code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "CODE_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
			linearWorkflowCheck(t, err)
			version, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(remote.server.URL), Destination: "team-uuid/project-uuid", CredentialSource: "env", CredentialEnv: "LINEAR_TOKEN", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan","implement":"label-implement","validate":"label-validate"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}}, 0)
			linearWorkflowCheck(t, err)
			factoryOptions := projectconnection.Options{DB: db, HTTPClient: httpClient, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
				switch name {
				case "LINEAR_TOKEN":
					return "test-linear-token", true
				case "CODE_TOKEN":
					return "test-github-token", true
				default:
					t.Errorf("unexpected credential reference %s", name)
					return "", false
				}
			}}}
			factory := projectconnection.NewFactory(factoryOptions)
			_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH})
			linearWorkflowCheck(t, err)
			appointment, err := (projectconnection.ApproverService{DB: db, Factory: factory}).Set(ctx, repo.ID, "human-uuid", version.Revision, 0)
			linearWorkflowCheck(t, err)
			clients, err := factory.Clients(ctx, version, 0)
			linearWorkflowCheck(t, err)
			if clients.Linear == nil || appointment.Actor.RemoteID != "human-uuid" || clients.Publisher.Identity.RemoteID != "publisher-uuid" {
				t.Fatalf("unverified setup: %+v %+v", appointment, clients.Publisher)
			}
			adapter := clients.Linear
			inspection, err := adapter.Inspect(ctx)
			linearWorkflowCheck(t, err)
			if inspection.WorkspaceID != "workspace-uuid" || inspection.TeamID != "team-uuid" || inspection.ProjectID != "project-uuid" {
				t.Fatalf("setup did not verify the configured destination: %+v", inspection)
			}
			parent, err := adapter.Resolve(ctx, ticket.Locator{Provider: ticket.Linear, Origin: version.Origin, Container: version.Destination, Key: "parent-uuid"})
			linearWorkflowCheck(t, err)
			_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: version.ID, Ref: parent.Ref, Enabled: true, Provenance: "explicit"})
			linearWorkflowCheck(t, err)
			issueSlot, err := db.LocalTicketSlot(ctx, "issue", parent.Ref.Identity)
			linearWorkflowCheck(t, err)
			botSlot, err := db.LocalTicketSlot(ctx, "actor", clients.Publisher.Identity)
			linearWorkflowCheck(t, err)
			ownerSlot, err := db.LocalTicketSlot(ctx, "actor", appointment.Actor)
			linearWorkflowCheck(t, err)
			ref := naming.Ref{Forge: "linear", Owner: "team-uuid", Repo: "project-uuid", Issue: int(issueSlot)}
			session, err := AdmitProjectTicket(ctx, db, factory, repo, parent.Ref)
			linearWorkflowCheck(t, err)
			snapshot, err := db.GetExecutionSnapshot(ctx, session.ID)
			linearWorkflowCheck(t, err)
			authority, err := db.EnsureDeliveryCodeAuthority(ctx, session.ID)
			linearWorkflowCheck(t, err)
			if snapshot.ActorID != botSlot || snapshot.CodeActorID != 99 || authority.OwnerID != 7 || authority.PublisherID != 99 || session.TaskRef != ref.String() {
				t.Fatalf("production admission failed to pin distinct ticket and code identities: %+v %+v", snapshot, authority)
			}
			codeClients, err := factory.Clients(ctx, code, 99)
			linearWorkflowCheck(t, err)
			startupTickets, err := github.NewTickets(codeClients.Code)
			linearWorkflowCheck(t, err)
			selector := func(ctx context.Context, r github.Repo, number int, sessionID int64) (turn.WorkflowTickets, int64, error) {
				return selectTicketConnection(ctx, db, factory, r, number, sessionID)
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: ownerSlot, BotUserID: botSlot, TicketOrigin: version.Origin}, logger)
			linearWorkflowCheck(t, err)
			d := &Daemon{db: db, connections: factory, filter: filter, log: logger}
			readAndAdmit := func(id, body string) turn.Admission {
				t.Helper()
				remote.addReply(id, body, "human-uuid")
				page, e := adapter.RepliesAfter(ctx, parent.Ref, linear.ReplyCursor(parent.Ref))
				linearWorkflowCheck(t, e)
				for _, reply := range page.Replies {
					if reply.Identity.RemoteID == id {
						admission, e := d.admitReply(ctx, session.ID, github.Comment{}, &reply)
						linearWorkflowCheck(t, e)
						if !admission.Triggers() {
							t.Fatalf("verified source rejected: %+v", admission)
						}
						return admission
					}
				}
				t.Fatalf("missing source %s", id)
				return turn.Admission{}
			}
			bridge, err := turn.NewRoutedGitHubTicketWorkflow(db, startupTickets, 99, selector)
			linearWorkflowCheck(t, err)
			redactor := redact.New(redact.Options{Token: secrets.NewToken("test-linear-token"), Environ: []string{}})
			renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: logger})
			linearWorkflowCheck(t, err)
			reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
			linearWorkflowCheck(t, err)

			bodyTrigger := strings.Contains(scenario, "body/")
			refusalKind := report.ReplyMissingModeMapping
			if strings.HasPrefix(scenario, "team/") {
				refusalKind = report.ReplyMissingTeam
			}
			if strings.HasPrefix(scenario, "workflow/") {
				refusalKind = report.ReplyUnsupportedWorkflow
			}
			if refusalKind != report.ReplyMissingModeMapping {
				linearWorkflowCheck(t, bridge.AddIssueLabel(ctx, github.RepoFromRef(ref), ref.Issue, "Mode: Plan"))
				d.turn = admissionRefusalExecutor{kind: refusalKind}
				d.toolProfiles = config.DefaultToolProfiles().Permissions
			}
			d.reporter = reporter
			d.defaultAgent = agent.Kind("claude")
			d.classifiers = map[agent.Kind]agent.Classifier{d.defaultAgent: missingMappingClassifier{}}
			instruction := readAndAdmit("unmapped-instruction", "Investigate this request")
			trigger := turn.Trigger{CommentID: instruction.Comment.ID}
			if bodyTrigger {
				trigger.CommentID = 0
				_, _, err = db.BootstrapTicketWatermark(ctx, session.ID, 0)
				linearWorkflowCheck(t, err)
				_, err = db.InitTicketRecoveryPosition(ctx, version, version.CreatedAt)
				linearWorkflowCheck(t, err)
			}
			issue, err := bridge.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
			linearWorkflowCheck(t, err)
			task := &task{repo: repo, session: session, ref: ref, forgeRepo: github.RepoFromRef(ref), issue: issue, log: logger, tickAt: time.Now().UTC()}
			if scenario == "pending" {
				_, err = db.GetOrCreateModeSelection(ctx, store.ModeSelection{SessionID: session.ID, TriggerCommentID: trigger.CommentID, Mode: "research", Label: "Mode: Research"})
				linearWorkflowCheck(t, err)
			}
			remote.mu.Lock()
			remote.loseReply = strings.HasSuffix(scenario, "lost_receipt")
			remote.mu.Unlock()
			err = d.run(ctx, task, trigger, turn.ParseCommand(instruction.Comment.Body), logger)
			if strings.HasSuffix(scenario, "lost_receipt") {
				if err == nil {
					t.Fatal("lost delivery receipt reported success")
				}
				before, _, e := db.GetWatermark(ctx, session.ID)
				linearWorkflowCheck(t, e)
				if trigger.CommentID != 0 && before.LastCommentID >= trigger.CommentID {
					t.Fatal("unconfirmed reply consumed source")
				}
				if bodyTrigger {
					pending, e := db.TicketBodyPending(ctx, session.ID)
					linearWorkflowCheck(t, e)
					if !pending {
						t.Fatal("unconfirmed body delivery lost obligation")
					}
				}
				// A fresh reporter after reopening SQLite must reconcile the existing
				// durable intent with the one visible remote comment.
				linearWorkflowCheck(t, db.Close())
				db, err = store.Open(ctx, dbPath)
				linearWorkflowCheck(t, err)
				factoryOptions.DB = db
				factory = projectconnection.NewFactory(factoryOptions)
				bridge, err = turn.NewRoutedGitHubTicketWorkflow(db, startupTickets, 99, selector)
				linearWorkflowCheck(t, err)
				reporter, err = turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
				linearWorkflowCheck(t, err)
				if bodyTrigger {
					d = &Daemon{db: db, connections: factory, reporter: reporter, log: logger}
					task.tickAt = task.tickAt.Add(time.Hour)
					d.quotas.tick(task.tickAt)
					factory.SetRequestContext(d.bindQuotaRequest)
					// Bootstrap sees the visible bot reply, yet must reconcile
					// the original obligation before its eligibility gates.
					recoveryCtx := context.WithValue(sessionContext(ctx, session.ID), recoveryTickKey{}, task.tickAt)
					linearWorkflowCheck(t, d.bootstrap(recoveryCtx, task))
				} else {
					_, pending, e := reporter.ReportPending(ctx, session.ID, trigger.CommentID)
					linearWorkflowCheck(t, e)
					if !pending {
						t.Fatal("lost durable delivery was not recovered")
					}
				}
			} else {
				linearWorkflowCheck(t, err)
			}
			if bodyTrigger {
				pending, e := db.TicketBodyPending(ctx, session.ID)
				linearWorkflowCheck(t, e)
				if pending {
					t.Fatal("confirmed body refusal still pending")
				}
			}
			// Fresh daemon state exercises cleanup and source admission after restart.
			d = &Daemon{db: db, connections: factory, reporter: reporter, log: logger, defaultAgent: agent.Kind("claude"), classifiers: map[agent.Kind]agent.Classifier{agent.Kind("claude"): missingMappingClassifier{}}}
			filter, err = turn.NewFilter(db, turn.Identities{OwnerUserID: ownerSlot, BotUserID: botSlot, TicketOrigin: version.Origin}, logger)
			linearWorkflowCheck(t, err)
			d.filter = filter
			linearWorkflowCheck(t, d.retireModeSelections(ctx, session.ID))
			if _, err := db.GetModeSelection(ctx, session.ID, trigger.CommentID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("journal retained: %v", err)
			}
			consumed, _, err := db.GetWatermark(ctx, session.ID)
			linearWorkflowCheck(t, err)
			if consumed.LastCommentID < trigger.CommentID {
				t.Fatal("confirmed refusal did not consume source")
			}

			d.quotas.tick(task.tickAt)
			factory.SetRequestContext(d.bindQuotaRequest)
			if bodyTrigger {
				// This fixture invokes the admitted body directly; retire its
				// separately-created comment fixture before polling.
				_, e := reporter.Consume(ctx, session.ID, instruction.Comment.ID)
				linearWorkflowCheck(t, e)
			}
			for i := 0; i < 3; i++ {
				wm, _, e := db.GetWatermark(ctx, session.ID)
				linearWorkflowCheck(t, e)
				_, e = d.drain(sessionContext(ctx, session.ID), task, wm.LastCommentID)
				linearWorkflowCheck(t, e)
			}

			remote.mu.Lock()
			posts := remote.posts
			var replyBody string
			for _, reply := range remote.comments {
				if linearWorkflowObject(t, reply["user"])["id"] == "publisher-uuid" {
					replyBody, _ = reply["body"].(string)
				}
			}
			remote.mu.Unlock()
			if posts != 1 {
				t.Fatalf("posts=%d", posts)
			}
			wants := []string{"research", "Nothing was run", "--mode-labels", "label the issue with a mapped mode", "comment again"}
			if refusalKind == report.ReplyMissingTeam {
				wants = []string{"no linked installed team", "Link a suitable installed team", "Nothing was run", "comment again"}
			}
			if refusalKind == report.ReplyUnsupportedWorkflow {
				wants = []string{"does not provide the requested workflow", "Select a workflow supported", "Nothing was run", "comment again"}
			}
			for _, want := range wants {
				if !strings.Contains(replyBody, want) {
					t.Fatalf("reply missing %q: %s", want, replyBody)
				}
			}
			if strings.Contains(logs.String(), "mode selection was refused") {
				t.Fatalf("configuration still deferred: %s", &logs)
			}
			// No executor is installed: reaching execution in d.run would fail this
			// fixture. A newly mapped label and new comment can now select Plan.
			linearWorkflowCheck(t, bridge.AddIssueLabel(ctx, github.RepoFromRef(ref), ref.Issue, "Mode: Plan"))
			next := readAndAdmit("next-instruction", "Continue with this mapped mode")
			mode, err := d.admitMode(sessionContext(ctx, session.ID), task, turn.Trigger{CommentID: next.Comment.ID}, turn.ParseCommand(next.Comment.Body))
			linearWorkflowCheck(t, err)
			if mode != agent.ModePlan {
				t.Fatalf("next mode=%s", mode)
			}
		})
	}
}

type missingMappingClassifier struct{}

func (missingMappingClassifier) Classify(context.Context, agent.Classification) (agent.Mode, error) {
	return agent.ModeResearch, nil
}

type admissionRefusalExecutor struct{ kind report.ReplyKind }

func (e admissionRefusalExecutor) Execute(context.Context, turn.Request) (turn.Result, error) {
	return turn.Result{}, fmt.Errorf("capture: %w", &workflowRefusal{kind: e.kind, cause: errors.New("configuration fixture")})
}

// The HTTP fixture seeds and publishes the proposal; approval, reservation and
// child publication then run through handleComment and the real turn executor.
func linearSetApproval(t *testing.T, d *Daemon, remote *linearWorkflowServer, adapter *linear.Client, parent ticket.Ref, bridge *turn.TicketWorkflowBridge, reporter *turn.Reporter, redactor *redact.Redactor, repo store.Repo, session store.Session, ref naming.Ref) {
	t.Helper()
	ctx := t.Context()
	state := planbrief.PlanState{SharedRevision: "initial"}
	revision, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "ledger", Name: "Remove Band A", Intent: "Remove Band A from the Ledger review screen"}})
	linearWorkflowCheck(t, err)
	_, err = d.db.SavePlanWorkflowState(ctx, ref.String(), 0, state)
	linearWorkflowCheck(t, err)
	proposal, err := planbrief.RenderPlanStorySet(state.StorySet)
	linearWorkflowCheck(t, err)
	safe, err := redactor.Apply(ctx, proposal, github.CommentSizeLimit)
	linearWorkflowCheck(t, err)
	published, err := bridge.PostCommentEffect(ctx, "set-proposal", github.RepoFromRef(ref), ref.Issue, safe)
	linearWorkflowCheck(t, err)
	if published.ID == 0 || !strings.Contains(published.Body, revision) {
		t.Fatal("proposal was not published")
	}
	_, err = d.db.AdmitPlanWorkflowPath(ctx, session.ID, "graph")
	linearWorkflowCheck(t, err)
	paths := config.NewPaths(t.TempDir())
	linearWorkflowCheck(t, paths.EnsureHome())
	linearWorkflowCheck(t, os.MkdirAll(paths.StoryProgressLocksDir(), 0700))
	binary, err := git.Resolve(ctx)
	linearWorkflowCheck(t, err)
	opts := turn.Options{DB: d.db, Git: binary, Paths: paths, InstanceID: "linear-set-test", Logger: d.log, ProjectApprovers: true, CloneGate: linearApprovalNoAgent{}, Runners: turn.RunnerSet{"claude": linearApprovalNoAgent{}}, ResearchReporter: reporter, SubIssueCreator: bridge, MCP: &turn.MCPRuntime{Paths: paths, Redactor: redactor, Now: time.Now}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		definition, err := teamsource.CapturePlan(ctx, specs)
		if err != nil {
			return nil, err
		}
		configBytes := []byte("plan: plan/plan.dot\n")
		digest := sha256.Sum256(configBytes)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "plan/plan.dot", GraphFile: "plan.dot", Team: definition.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	d.reporter = reporter
	d.defaultAgent = "claude"
	d.toolProfiles = config.DefaultToolProfiles().Permissions
	executor := &linearApprovalExecutor{t: t, opts: opts}
	d.turn = executor
	issue, err := bridge.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
	linearWorkflowCheck(t, err)
	task := &task{repo: repo, session: session, ref: ref, forgeRepo: github.RepoFromRef(ref), issue: issue, log: d.log, tickAt: time.Now()}
	body := "`" + planbrief.PlanSetPromotionCommand(revision) + "`"
	remote.addReply("set-approval", body, "human-uuid")
	page, err := adapter.RepliesAfter(ctx, parent, linear.ReplyCursor(parent))
	linearWorkflowCheck(t, err)
	var source github.Comment
	for _, reply := range page.Replies {
		if reply.Identity.RemoteID == "set-approval" {
			if reply.Body != body || reply.ProviderBody != body {
				t.Fatal("adapter changed raw approval")
			}
			source, err = d.legacyReply(ctx, reply)
			linearWorkflowCheck(t, err)
		}
	}
	if source.ID == 0 {
		t.Fatal("approval absent")
	}
	for attempt := 0; attempt < 2; attempt++ {
		linearWorkflowCheck(t, d.handleComment(ctx, task, source))
		saved, _, err := d.db.GetPlanWorkflowState(ctx, ref.String())
		linearWorkflowCheck(t, err)
		if saved.StorySet == nil || !saved.StorySet.Promoted || saved.StorySet.PromotedRevision != revision || saved.StorySet.Keys["ledger"] != "story-001" || !slices.Contains(saved.ConsumedReplies, source.ID) {
			t.Fatalf("approval not applied: %+v", saved)
		}
		drafts, err := d.db.ListPlanStoryDrafts(ctx, ref.String())
		linearWorkflowCheck(t, err)
		if len(drafts) != 1 || drafts[0].Key != "story-001" || drafts[0].Ordinal != 1 || drafts[0].IssueID == 0 {
			t.Fatalf("unstable reservations: %+v", drafts)
		}
		child, err := bridge.GetIssue(ctx, github.RepoFromRef(ref), drafts[0].IssueNumber)
		linearWorkflowCheck(t, err)
		remote.mu.Lock()
		childRemote := remote.issues[child.QualifiedSnapshot.Ref.Identity.RemoteID]
		valid := remote.creates == 1 && linearWorkflowObject(t, childRemote["parent"])["id"] == "parent-uuid" && linearWorkflowObject(t, childRemote["team"])["id"] == "team-uuid" && linearWorkflowObject(t, childRemote["project"])["id"] == "project-uuid"
		remote.mu.Unlock()
		if !valid {
			t.Fatal("child duplicated or bound to wrong parent/team/project")
		}
	}
	if executor.calls == 0 {
		t.Fatal("production dispatch never executed")
	}
}

type linearApprovalExecutor struct {
	t     *testing.T
	opts  turn.Options
	calls int
}

func (e *linearApprovalExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	e.calls++
	if req.Mode != agent.ModePlan || req.Command.Kind != turn.KindPlanControl || !strings.HasPrefix(req.Trigger.Body, "`/approve-plan set ") {
		e.t.Fatalf("wrong dispatch: %+v", req)
	}
	out, err := turn.Execute(ctx, e.opts, req)
	if err != nil || out.Err != nil {
		e.t.Fatalf("approval turn failed: err=%v result=%+v", err, out)
	}
	return out, err
}

// A set approval creates shells and leaves drafting to child tasks.
type linearApprovalNoAgent struct{}

func (linearApprovalNoAgent) Acquire(context.Context, string) (func(), error) {
	return nil, errors.New("set approval unexpectedly requested git")
}
func (linearApprovalNoAgent) Run(context.Context, agent.Request) (*agent.RunResult, error) {
	return nil, errors.New("set approval unexpectedly requested an agent")
}

// Stop graph admission after scope preparation, before any worker or git effect.
func linearScopeFreeze(t *testing.T, db *store.DB, session store.Session, ref naming.Ref, bridge *turn.TicketWorkflowBridge, renderer *report.Reporter, reporter *turn.Reporter, redactor *redact.Redactor, logger *slog.Logger, remote *linearWorkflowServer) {
	ctx := t.Context()
	if _, err := db.GetIdentity(ctx, "linear", store.RoleBot); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fixture has global Linear bot: %v", err)
	}
	linearWorkflowCheck(t, db.SetManagedFeatureReady(ctx, ref.String(), 1, true))
	paths := config.NewPaths(t.TempDir())
	linearWorkflowCheck(t, paths.EnsureHome())
	binary, err := git.Resolve(ctx)
	linearWorkflowCheck(t, err)
	stop := errors.New("scope prepared; stop before graph execution")
	opts := turn.Options{DB: db, Git: binary, Paths: paths, InstanceID: "linear-scope-test", Logger: logger, CloneGate: linearApprovalNoAgent{}, Runners: turn.RunnerSet{"claude": linearApprovalNoAgent{}}, ResearchReporter: reporter, SubIssueCreator: bridge, SubIssueRenderer: renderer, MCP: &turn.MCPRuntime{Paths: paths, Redactor: redactor, Now: time.Now}}
	opts.WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return nil, stop
	}
	issue, err := bridge.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
	linearWorkflowCheck(t, err)
	remote.mu.Lock()
	before := remote.posts
	remote.mu.Unlock()
	for range 2 {
		_, err := turn.Execute(ctx, opts, turn.Request{SessionID: session.ID, Mode: agent.ModeImplement, Agent: "claude", Issue: turn.Issue{Number: ref.Issue, Title: issue.Title, Body: issue.Body}, Trigger: turn.Trigger{CommentID: 9999999999999999, Body: "/implement approved stories"}, Command: turn.ParseCommand("/implement approved stories")})
		if !errors.Is(err, stop) {
			t.Fatalf("scope preparation failed before graph boundary: %v", err)
		}
		feature, err := db.GetManagedFeature(ctx, ref.String())
		linearWorkflowCheck(t, err)
		delivered, err := db.StoryScopeNoticeDelivered(ctx, ref.String(), 1, "story-001", "freeze")
		linearWorkflowCheck(t, err)
		if feature.State != "implementing" || !delivered {
			t.Fatalf("scope not activated: %+v delivered=%t", feature, delivered)
		}
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.posts != before+1 {
		t.Fatalf("scope notice duplicated: before=%d after=%d", before, remote.posts)
	}
	found := false
	for _, reply := range remote.comments {
		body, _ := reply["body"].(string)
		if strings.Contains(body, "litefactory:scope:") {
			found = true
			if linearWorkflowObject(t, reply["user"])["id"] != "publisher-uuid" || linearWorkflowObject(t, reply["issue"])["id"] == "parent-uuid" {
				t.Fatal("scope notice used wrong publisher or target")
			}
		}
	}
	if !found {
		t.Fatal("scope notice missing")
	}
}
