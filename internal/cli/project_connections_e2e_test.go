package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestProjectConnectionsMainPath runs owner management, provider reads and
// admission against a real persisted database. Only GitHub's HTTP boundary is
// controlled; the router, credential resolver and admission code are real.
func TestProjectConnectionsMainPath(t *testing.T) {
	paths := newRepoHome(t)
	clone := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)
	run := func(args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d, stderr %s", args, code, stderr)
		}
	}
	run("repo", "add", clone.Dir)
	registered := listRepos(t, paths)
	if len(registered) != 1 {
		t.Fatalf("registered projects = %d, want 1", len(registered))
	}
	project := registered[0]
	id := strconv.FormatInt(project.ID, 10)

	const codeToken = "project-code-credential"
	const ticketToken = "project-ticket-credential"
	t.Setenv("LF_E2E_CODE_TOKEN", codeToken)
	t.Setenv("LF_E2E_TICKET_TOKEN", ticketToken)
	var mu sync.Mutex
	requests := make(map[string]int)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("unexpected provider mutation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/user":
			response = map[string]any{"id": 99, "login": "factory-bot"}
		case "/repos/acme/does-not-exist", "/repos/acme/tasks":
			expected := codeToken
			remoteID := 101
			if r.URL.Path == "/repos/acme/tasks" {
				expected = ticketToken
				remoteID = 202
			}
			if !strings.Contains(r.Header.Get("Authorization"), expected) {
				t.Errorf("wrong credential for %s", r.URL.Path)
			}
			response = map[string]any{"id": remoteID, "full_name": strings.TrimPrefix(r.URL.Path, "/repos/"), "has_issues": true, "permissions": map[string]bool{"pull": true}}
		case "/repos/acme/tasks/issues/42":
			if !strings.Contains(r.Header.Get("Authorization"), ticketToken) {
				t.Error("ticket read used the code credential")
			}
			response = map[string]any{"id": 700, "number": 42, "url": origin + r.URL.Path, "title": "Configure separate destinations", "body": "main path", "state": "open", "user": map[string]any{"id": 42, "login": "owner"}}
		default:
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("provider response: %v", err)
		}
	}))
	defer server.Close()
	origin = server.URL

	run("repo", "connection", "set", id, "code", "--revision", "1", "--origin", origin, "--destination", "acme/does-not-exist", "--credential", "env:LF_E2E_CODE_TOKEN", "--remote-id", "101")
	run("repo", "connection", "set", id, "ticket", "--revision", "1", "--origin", origin, "--destination", "acme/tasks", "--credential", "env:LF_E2E_TICKET_TOKEN", "--remote-id", "202")
	run("repo", "binding", "add", id, "--remote-id", "700", "--number", "42", "--container", "acme/tasks")
	// Repeating owner operations must retain the explicit configuration and the
	// same binding, rather than silently importing fresh legacy defaults.
	run("repo", "binding", "add", id, "--remote-id", "700", "--number", "42", "--container", "acme/tasks")
	run("repo", "add", clone.Dir)
	run("repo", "binding", "list", id)
	if strings.Count(stdout.String(), "binding ") != 1 || !strings.Contains(stdout.String(), "ticket 700 at acme/tasks#42") {
		t.Fatalf("repeatable binding add lost its qualified identity: %s", stdout)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := router.Run(t.Context(), []string{"repo", "connection", "disable", id, "ticket", "--revision", "1"}); exit == ExitOK {
		t.Fatal("stale connection edit unexpectedly succeeded")
	}
	run("repo", "connection", "list", id)
	for _, destination := range []string{"acme/does-not-exist", "acme/tasks"} {
		if !strings.Contains(stdout.String(), destination) {
			t.Fatalf("connection inspection omits %s: %s", destination, stdout)
		}
	}
	if strings.Contains(stdout.String(), codeToken) || strings.Contains(stdout.String(), ticketToken) {
		t.Fatal("inspection exposed a credential")
	}
	run("repo", "connection", "list", id, "ticket")
	if !strings.Contains(stdout.String(), "acme/tasks") || strings.Contains(stdout.String(), "acme/does-not-exist") {
		t.Fatalf("role inspection did not select the ticket connection: %s", stdout)
	}

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	code, err := db.CurrentConnection(t.Context(), project.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.CurrentConnection(t.Context(), project.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	if code.Revision != 2 || tickets.Revision != 2 || code.Provenance != "explicit" || tickets.Provenance != "explicit" || !tickets.Enabled {
		t.Fatalf("registration or stale edit changed explicit configuration: code %+v, ticket %+v", code, tickets)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "acme/tasks", Key: "42"}}
	factory := projectconnection.NewFactory(projectconnection.Options{HTTPClient: server.Client(), ExpectedActorID: 99})
	session, err := daemon.AdmitProjectTicket(t.Context(), db, factory, project, ref)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CodeVersionID != code.ID || snapshot.TicketVersionID != tickets.ID || snapshot.ActorID != 99 {
		t.Fatalf("admission did not pin configured versions and actor: %+v", snapshot)
	}
	if snapshot.CodeTarget.Path != clone.Dir || snapshot.CodeTarget.Slug() != "acme/does-not-exist" {
		t.Fatalf("admission code target = %+v, want registered clone and separate code destination", snapshot.CodeTarget)
	}
	bindings, err := db.ListTicketBindings(t.Context(), project.ID)
	if err != nil || len(bindings) != 1 || bindings[0].ID != snapshot.BindingID {
		t.Fatalf("admission lost configured qualified binding: %+v, %v", bindings, err)
	}
	if session.TaskRef != "github:acme/tasks#42" {
		t.Fatalf("task reference = %q, want configured ticket destination", session.TaskRef)
	}
	mu.Lock()
	actorChecks := requests["/user"]
	codeChecks := requests["/repos/acme/does-not-exist"]
	ticketReads := requests["/repos/acme/tasks/issues/42"]
	mu.Unlock()
	if actorChecks < 2 || codeChecks == 0 || ticketReads == 0 {
		t.Fatalf("admission skipped verified destinations: actor checks %d, code checks %d, ticket reads %d", actorChecks, codeChecks, ticketReads)
	}

	// Management replaces both current versions, then disables future ticket
	// admission. A new process retains the original execution destinations.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	run("repo", "connection", "set", id, "code", "--revision", fmt.Sprint(code.Revision), "--origin", origin, "--destination", "acme/does-not-exist", "--credential", "env:LF_E2E_CODE_TOKEN", "--remote-id", "101")
	run("repo", "connection", "set", id, "ticket", "--revision", fmt.Sprint(tickets.Revision), "--origin", origin, "--destination", "acme/replacement-tasks", "--credential", "env:LF_E2E_TICKET_TOKEN", "--remote-id", "303")
	run("repo", "connection", "disable", id, "ticket", "--revision", fmt.Sprint(tickets.Revision+1))
	db, err = store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.CodeVersionID != snapshot.CodeVersionID || retained.TicketVersionID != snapshot.TicketVersionID || retained.ActorID != snapshot.ActorID {
		t.Fatalf("restart redirected execution: before %+v, after %+v", snapshot, retained)
	}
	currentCode, err := db.CurrentConnection(t.Context(), project.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	currentTickets, err := db.CurrentConnection(t.Context(), project.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	if currentCode.ID == retained.CodeVersionID || currentTickets.ID == retained.TicketVersionID || currentTickets.Destination != "acme/replacement-tasks" || currentTickets.Enabled {
		t.Fatalf("management did not replace current configuration independently of saved execution: code %+v, ticket %+v", currentCode, currentTickets)
	}
	retainedTickets, err := db.GetConnectionVersion(t.Context(), retained.TicketVersionID)
	if err != nil || retainedTickets.Destination != "acme/tasks" || !retainedTickets.Enabled {
		t.Fatalf("original ticket version was changed: %+v, %v", retainedTickets, err)
	}
	factory = projectconnection.NewFactory(projectconnection.Options{HTTPClient: server.Client(), ExpectedActorID: 99})
	resumed, err := daemon.AdmitProjectTicket(t.Context(), db, factory, project, ref)
	if err != nil || resumed.ID != session.ID || resumed.TaskRef != session.TaskRef {
		t.Fatalf("disabled replacement blocked or redirected saved session: %+v, %v", resumed, err)
	}
	// Local doctor recovery must also load retained project credentials when
	// the former global credential is unavailable and current routing is disabled.
	cfg := config.Defaults()
	cfg.Providers.GitHub.TokenKeychain.Service = ""
	cfg.Providers.GitHub.TokenKeychain.Account = ""
	cfg.Providers.GitHub.TokenEnv = "LF_E2E_UNAVAILABLE_GLOBAL_TOKEN"
	t.Setenv(cfg.Providers.GitHub.TokenEnv, "")
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleBot, UserID: 99, Handle: "factory-bot", Source: store.SourceForgeAPI}); err != nil {
		t.Fatal(err)
	}
	service := doctor.Service{DB: db, Config: &cfg}
	if err := configureDoctorTicketWorkflow(t.Context(), &service, &Env{}, nil); err != nil || service.Comments == nil {
		t.Fatalf("doctor lost retained project coverage: %v", err)
	}
	workflow, ok := service.Comments.(*turn.TicketWorkflowBridge)
	if !ok {
		t.Fatalf("doctor configured unexpected ticket reader %T", service.Comments)
	}
	issue, err := workflow.GetIssue(t.Context(), github.Repo{Owner: "acme", Name: "tasks"}, 42)
	if err != nil || issue.ID != 700 {
		t.Fatalf("doctor redirected retained ticket read: %+v, %v", issue, err)
	}
	newRef := ref
	newRef.Identity.RemoteID = "701"
	newRef.Locator.Container = "acme/replacement-tasks"
	newRef.Locator.Key = "43"
	if _, err := daemon.AdmitProjectTicket(t.Context(), db, factory, project, newRef); err == nil {
		t.Fatal("disabled ticket connection admitted new work")
	}
	t.Logf("owner CLI configured independent code and ticket destinations; admitted session %d retained versions %d/%d and actor %d across disable and restart", session.ID, retained.CodeVersionID, retained.TicketVersionID, retained.ActorID)
}
