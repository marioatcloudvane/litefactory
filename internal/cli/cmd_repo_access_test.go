package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestRepoAccessGrantReplaceListRevokeHistory(t *testing.T) {
	users := map[string]int{"alice": 42, "bob": 43}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"id":7,"login":"publisher","type":"User"}`))
			return
		}
		login := strings.TrimPrefix(r.URL.Path, "/users/")
		id, ok := users[login]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%d,"login":%q,"type":"User"}`, id, login)
	}))
	defer server.Close()
	t.Setenv("LF_ACCESS_TEST_TOKEN", "opaque-test-token")

	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/access", Forge: "github", Owner: "owner", Name: "access", RemoteURL: "git@github.com:owner/access.git"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "owner/tickets", CredentialSource: "env", CredentialEnv: "LF_ACCESS_TEST_TOKEN", Enabled: true, Provenance: "explicit"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleBot, UserID: 7, Handle: "publisher", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(repo.ID, 10)

	run := func(action, text, file string, textSet bool, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := newRepoAccessCommand(action)
		if err := runRepoAccess(t.Context(), &Env{Stdout: &out}, cmd, action, text, file, textSet, args); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return out.String()
	}
	const first, second, third = "may review docs only", "may review docs and tests", "bob handles releases"

	run("grant", first, "", true, id, "alice")
	run("grant", second, "", true, id, "alice")
	list := run("list", "", "", false, id)
	if !strings.Contains(list, "revision 2") || !strings.Contains(list, second) || strings.Contains(list, first) || strings.Contains(list, "revision 1") {
		t.Fatalf("list should show only the active revision: %s", list)
	}
	// The grant ID is the one the list shows.
	var grantID string
	if _, rest, ok := strings.Cut(list, "grant "); ok {
		grantID, _, _ = strings.Cut(rest, ":")
	}
	if grantID == "" {
		t.Fatalf("no grant id in list: %s", list)
	}
	run("revoke", "", "", false, id, grantID)
	if out := run("list", "", "", false, id); strings.Contains(out, second) {
		t.Fatalf("revoked grant still listed: %s", out)
	}

	path := filepath.Join(t.TempDir(), "grant.txt")
	if err := os.WriteFile(path, []byte(third), 0o600); err != nil {
		t.Fatal(err)
	}
	run("grant", "", path, false, id, "bob")
	if out := run("list", "", "", false, id); !strings.Contains(out, third) || strings.Contains(out, second) {
		t.Fatalf("list after file grant: %s", out)
	}

	history := run("history", "", "", false, id)
	i1, i2, i3 := strings.Index(history, first), strings.Index(history, second), strings.Index(history, third)
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Fatalf("history missing rows or out of order: %s", history)
	}
	if !strings.Contains(history, "replaced") || !strings.Contains(history, "revoked") || !strings.Contains(history, "active") {
		t.Fatalf("history should show how each row closed: %s", history)
	}

	// Refusals store nothing: blank text and neither/both inputs.
	var out bytes.Buffer
	cmd := newRepoAccessCommand("grant")
	if err := runRepoAccess(t.Context(), &Env{Stdout: &out}, cmd, "grant", "  ", "", true, []string{id, "alice"}); err == nil {
		t.Fatal("blank text accepted")
	}
	if err := runRepoAccess(t.Context(), &Env{Stdout: &out}, cmd, "grant", "x", path, true, []string{id, "alice"}); err == nil {
		t.Fatal("both --text and --file accepted")
	}
	if after := run("history", "", "", false, id); after != history {
		t.Fatalf("refused grants changed history: %s", after)
	}
}
