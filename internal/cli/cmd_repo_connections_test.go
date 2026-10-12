package cli

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestConnectionBindingDiagnostics(t *testing.T) {
	paths := newRepoHome(t)
	clone := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)
	run := func(args ...string) {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, stderr)
		}
	}
	run("repo", "add", clone.Dir)
	repo := listRepos(t, paths)[0]
	id := strconv.FormatInt(repo.ID, 10)
	set := func(revision, destination, remoteID string) {
		t.Helper()
		run("repo", "connection", "set", id, "ticket", "--revision", revision, "--origin", "https://api.github.com", "--destination", destination, "--credential", "env:DIAGNOSTIC_TOKEN", "--remote-id", remoteID)
	}
	set("1", "acme/tasks", "101")
	run("repo", "binding", "add", id, "--remote-id", "700", "--number", "42", "--container", "acme/tasks")
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	bindings, err := db.ListTicketBindings(t.Context(), repo.ID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings: %+v %v", bindings, err)
	}
	binding := bindings[0]
	set("2", "acme/tasks", "101")
	if strings.Contains(stdout.String(), "cannot inherit") {
		t.Fatalf("routine edit warned: %s", stdout)
	}
	set("3", "acme/replacement", "102")
	current, err := db.CurrentConnection(t.Context(), repo.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		for _, want := range []string{fmt.Sprintf("binding %d", binding.ID), fmt.Sprintf("version id %d", binding.ConnectionVersionID), fmt.Sprintf("current version id %d", current.ID), "mismatch", "verify or restore"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("missing %q: %s", want, stdout)
			}
		}
		if strings.Contains(stdout.String(), "DIAGNOSTIC_TOKEN") {
			t.Fatalf("credential exposed: %s", stdout)
		}
	}
	check()
	run("repo", "connection", "list", id, "ticket")
	check()
	saved, err := db.GetTicketBindingByID(t.Context(), binding.ID)
	if err != nil || saved != binding {
		t.Fatalf("replacement altered history: %+v %v", saved, err)
	}
	set("4", "acme/tasks", "")
	if !strings.Contains(stdout.String(), "unproven immutable source identity") {
		t.Fatalf("missing unproven warning: %s", stdout)
	}

	// The Linear IDs in saved versions prove identity without a --remote-id.
	linear := store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "PRIVATE_REFERENCE", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}
	linear, err = db.SetConnection(t.Context(), linear, 5)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: linear.Provider, Origin: linear.Origin, RemoteID: "uuid"}, Locator: ticket.Locator{Provider: linear.Provider, Origin: linear.Origin, Container: linear.Destination, Key: "uuid"}}
	lb, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: linear.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	linear.CredentialEnv = "ROTATED_PRIVATE_REFERENCE"
	linear, err = db.SetConnection(t.Context(), linear, linear.Revision)
	if err != nil {
		t.Fatal(err)
	}
	run("repo", "connection", "list", id, "ticket")
	if strings.Contains(stdout.String(), fmt.Sprintf("binding %d cannot inherit", lb.ID)) {
		t.Fatalf("routine Linear edit warned: %s", stdout)
	}
	linear.Linear.WorkspaceID = "replacement"
	_, err = db.SetConnection(t.Context(), linear, linear.Revision)
	if err != nil {
		t.Fatal(err)
	}
	run("repo", "connection", "list", id, "ticket")
	if !strings.Contains(stdout.String(), fmt.Sprintf("binding %d cannot inherit", lb.ID)) || !strings.Contains(stdout.String(), "Linear workspace, team or project mismatch") || strings.Contains(stdout.String(), "PRIVATE_REFERENCE") {
		t.Fatalf("incorrect Linear diagnostic: %s", stdout)
	}
}
