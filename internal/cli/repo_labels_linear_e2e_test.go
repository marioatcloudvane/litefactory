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

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// fakeLinearLabel is one label held by the fake Linear workspace.
type fakeLinearLabel struct {
	ID, Name, ParentID string
	IsGroup            bool
}

// fakeLinearLabels is a stateful Linear GraphQL boundary: label creates are
// recorded and become visible to later label reads.
type fakeLinearLabels struct {
	t       *testing.T
	mu      sync.Mutex
	labels  []fakeLinearLabel
	creates []map[string]any
}

func (f *fakeLinearLabels) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.creates)
}

func (f *fakeLinearLabels) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string
		Variables map[string]any
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Error(err)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	team := map[string]string{"id": "team"}
	var data any
	switch {
	case strings.Contains(req.Query, "mutation"):
		if !strings.Contains(req.Query, "issueLabelCreate(") {
			f.t.Errorf("unexpected mutation: %s", req.Query)
			break
		}
		input, _ := req.Variables["input"].(map[string]any)
		if input["teamId"] != "team" {
			f.t.Errorf("label created outside the connection's team: %v", input)
		}
		f.creates = append(f.creates, input)
		l := fakeLinearLabel{ID: fmt.Sprintf("created-%d", len(f.creates)), Name: fmt.Sprint(input["name"])}
		l.IsGroup, _ = input["isGroup"].(bool)
		l.ParentID, _ = input["parentId"].(string)
		f.labels = append(f.labels, l)
		data = map[string]any{"issueLabelCreate": map[string]any{"success": true, "issueLabel": map[string]string{"id": l.ID}}}
	case strings.Contains(req.Query, "issueLabels("):
		nodes := []any{}
		for _, l := range f.labels {
			node := map[string]any{"id": l.ID, "name": l.Name, "isGroup": l.IsGroup, "archivedAt": nil, "team": team, "parent": nil}
			for _, p := range f.labels {
				if l.ParentID != "" && p.ID == l.ParentID {
					node["parent"] = map[string]string{"id": p.ID, "name": p.Name}
				}
			}
			nodes = append(nodes, node)
		}
		data = map[string]any{"issueLabels": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}}}
	case strings.Contains(req.Query, "viewer"):
		data = map[string]any{"viewer": map[string]any{"id": "bot", "active": true, "name": "bot", "organization": map[string]string{"id": "workspace"}}}
	case strings.Contains(req.Query, "team("):
		data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]string{"id": "workspace"}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{team}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
	case strings.Contains(req.Query, "issueLabel("):
		id := fmt.Sprint(req.Variables["id"])
		var label any
		for _, l := range f.labels {
			if l.ID == id {
				label = map[string]any{"id": l.ID, "isGroup": l.IsGroup, "archivedAt": nil, "organization": map[string]string{"id": "workspace"}, "team": team}
			}
		}
		if label == nil {
			f.t.Errorf("lookup of unknown label %q", id)
		}
		data = map[string]any{"issueLabel": label}
	case strings.Contains(req.Query, "workflowState("):
		kind := "completed"
		if req.Variables["id"] == "retired" {
			kind = "canceled"
		}
		data = map[string]any{"workflowState": map[string]any{"id": req.Variables["id"], "type": kind, "team": team}}
	case strings.Contains(req.Query, "webhooks("):
		data = map[string]any{"webhooks": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}
	default:
		f.t.Errorf("unexpected Linear query: %s", req.Query)
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		f.t.Error(err)
	}
}

// TestRepoLabelsSetupLinearMainPath runs `repo labels setup` on a Linear
// project: a dry run without a terminal writes nothing, --yes creates the
// missing labels and stores their IDs, and a second --yes run changes nothing.
// Only Linear's HTTP boundary is faked; the CLI, service and store are real.
func TestRepoLabelsSetupLinearMainPath(t *testing.T) {
	paths := newRepoHome(t)
	clone := newRepoFixture(t)
	router, stdout, stderr := newTestRouter(t)
	run := func(args ...string) string {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := router.Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d, stdout %s, stderr %s", args, code, stdout, stderr)
		}
		return stdout.String()
	}
	run("repo", "add", clone.Dir)
	registered := listRepos(t, paths)
	if len(registered) != 1 {
		t.Fatalf("registered projects = %d, want 1", len(registered))
	}
	projectID := registered[0].ID
	id := strconv.FormatInt(projectID, 10)

	fake := &fakeLinearLabels{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	t.Setenv("LF_E2E_LINEAR_TOKEN", "linear-test-token")

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	revision := int64(0)
	if current, err := db.CurrentConnection(t.Context(), projectID, store.TicketConnection); err == nil {
		revision = current.Revision
	}
	saved, err := db.SetConnection(t.Context(), store.ConnectionVersion{
		RepoID: projectID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(server.URL),
		Destination: "team/project", CredentialSource: "env", CredentialEnv: "LF_E2E_LINEAR_TOKEN", Enabled: true, Provenance: "explicit",
		Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", LifecycleStatesJSON: `{"completed":"completed","retired":"retired"}`},
	}, revision)
	if err != nil {
		t.Fatalf("set Linear connection: %v", err)
	}
	current := func() store.ConnectionVersion {
		t.Helper()
		v, err := db.CurrentConnection(t.Context(), projectID, store.TicketConnection)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	lineOf := func(out, label string) string {
		t.Helper()
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), label+":") {
				return line
			}
		}
		t.Fatalf("output has no line for %q:\n%s", label, out)
		return ""
	}
	grouped := []string{"Mode: Research", "Mode: Plan", "Mode: Implement", "Mode: Validate", "Mode: Spike", "Coding Agent: Claude", "Coding Agent: Codex"}
	wanted := append(append([]string{}, grouped...), "bug")
	form := func(label string) string {
		if label == "bug" {
			return "flat"
		}
		return "grouped"
	}

	// 1. Dry run without a terminal and without --yes: plan shown, nothing written.
	out := run("repo", "labels", "setup", id)
	for _, label := range wanted {
		line := lineOf(out, label)
		for _, word := range []string{"would create", "team", form(label)} {
			if !strings.Contains(line, word) {
				t.Errorf("dry-run line for %s lacks %q: %s", label, word, line)
			}
		}
		if label != "bug" && !strings.Contains(line, "new team group") {
			t.Errorf("dry-run line for %s does not say it goes in a new group: %s", label, line)
		}
	}
	if !strings.Contains(out, "nothing written, rerun with --yes") {
		t.Fatalf("dry run did not refuse to write without --yes:\n%s", out)
	}
	if n := fake.createCount(); n != 0 {
		t.Fatalf("dry run created %d labels", n)
	}
	if v := current(); v.Revision != saved.Revision || v.ID != saved.ID {
		t.Fatalf("dry run changed the connection: revision %d -> %d", saved.Revision, v.Revision)
	}

	// 2. Apply with --yes: the missing labels are created and their IDs saved.
	out = run("repo", "labels", "setup", id, "--yes")
	created := fake.createCount()
	// The default config adds Codex: eight labels plus the Mode and Coding Agent groups.
	if created != len(wanted)+2 {
		t.Fatalf("apply created %d labels, want %d:\n%s", created, len(wanted)+2, out)
	}
	if got := strings.Count(out, "created "); got != created {
		t.Errorf("apply reported %d created lines for %d creates:\n%s", got, created, out)
	}
	for i := 1; i <= created; i++ {
		if !strings.Contains(out, fmt.Sprintf("id=created-%d", i)) {
			t.Errorf("apply output lacks the id of create %d:\n%s", i, out)
		}
	}
	after := current()
	if after.Revision <= saved.Revision {
		t.Fatalf("apply did not save a new connection revision: %d", after.Revision)
	}
	if !strings.Contains(out, fmt.Sprintf("saved label IDs on the connection, revision %d", after.Revision)) {
		t.Errorf("apply output does not name the saved revision %d:\n%s", after.Revision, out)
	}
	modes, err := after.Linear.ModeLabels()
	if err != nil {
		t.Fatal(err)
	}
	storedAgents, err := after.Linear.CodingAgentLabels()
	if err != nil {
		t.Fatal(err)
	}
	// Readers match agent keys case-insensitively, so compare in lower case.
	agents := map[string]string{}
	for k, v := range storedAgents {
		agents[strings.ToLower(k)] = v
	}
	bug, err := after.Linear.BugLabel()
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]bool{}
	for _, v := range modes {
		stored[v] = true
	}
	if len(modes) != 5 || len(agents) != 2 || agents["claude"] == "" || agents["codex"] == "" || bug == "" {
		t.Fatalf("connection lacks label IDs: modes %v, agents %v, bug %q", modes, agents, bug)
	}
	stored[agents["claude"]] = true
	stored[agents["codex"]] = true
	stored[bug] = true
	if len(stored) != len(wanted) {
		t.Fatalf("stored label IDs are not distinct: modes %v, agents %v, bug %q", modes, agents, bug)
	}
	for v := range stored {
		if !strings.HasPrefix(v, "created-") {
			t.Errorf("stored label ID %q is not a created label", v)
		}
	}

	// 3. Rerun with --yes: every label is stored, nothing is created or saved.
	out = run("repo", "labels", "setup", id, "--yes")
	ids := map[string]string{"bug": bug, "Coding Agent: Claude": agents["claude"], "Coding Agent: Codex": agents["codex"]}
	for key, v := range modes {
		ids["Mode: "+strings.ToUpper(key[:1])+key[1:]] = v
	}
	for _, label := range wanted {
		line := lineOf(out, label)
		words := []string{"stored", "team", form(label)}
		if ids[label] == "" {
			t.Fatalf("no stored ID for %s", label)
		}
		words = append(words, "id "+ids[label])
		for _, word := range words {
			if !strings.Contains(line, word) {
				t.Errorf("rerun line for %s lacks %q: %s", label, word, line)
			}
		}
	}
	if !strings.Contains(out, "nothing to create or store") {
		t.Errorf("rerun does not say nothing changes:\n%s", out)
	}
	if n := fake.createCount(); n != created {
		t.Fatalf("rerun created %d more labels", n-created)
	}
	if v := current(); v.Revision != after.Revision || v.ID != after.ID {
		t.Fatalf("rerun changed the connection: revision %d -> %d", after.Revision, v.Revision)
	}
	t.Logf("created %d labels, saved revision %d; rerun output:\n%s", created, after.Revision, out)
}
