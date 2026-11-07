package projectconnection

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type fakeLabel struct {
	ID, Name, ParentID, ParentName string
	IsGroup                        bool
	Workspace                      bool
}

// fakeLinear answers the read queries of a Linear connection check.
type fakeLinear struct {
	labels          []fakeLabel
	deleted         map[string]bool // issueLabel(id) answers a GraphQL error
	destinationDown bool            // team( answers a GraphQL error

	mu         sync.Mutex
	listReads  int
	mutations  []string
	unexpected []string
}

func (fl *fakeLinear) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		fl.mu.Lock()
		defer fl.mu.Unlock()
		if strings.Contains(req.Query, "mutation") {
			fl.mutations = append(fl.mutations, req.Query)
		}
		id := fmt.Sprint(req.Variables["id"])
		fail := false
		var data any
		switch {
		case strings.Contains(req.Query, "issueLabels("):
			fl.listReads++
			nodes := []any{}
			for _, l := range fl.labels {
				n := map[string]any{"id": l.ID, "name": l.Name, "isGroup": l.IsGroup}
				if !l.Workspace {
					n["team"] = map[string]string{"id": "team"}
				}
				if l.ParentID != "" {
					n["parent"] = map[string]string{"id": l.ParentID, "name": l.ParentName}
				}
				nodes = append(nodes, n)
			}
			data = map[string]any{"issueLabels": map[string]any{"nodes": nodes, "pageInfo": map[string]bool{"hasNextPage": false}}}
		case strings.Contains(req.Query, "viewer"):
			data = map[string]any{"viewer": map[string]any{"id": "bot", "active": true, "name": "bot", "organization": map[string]any{"id": "workspace"}}}
		case strings.Contains(req.Query, "team("):
			fail = fl.destinationDown
			data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]any{"id": "workspace"}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{map[string]string{"id": "team"}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
		case strings.Contains(req.Query, "issueLabel("):
			fail = fl.deleted[id]
			data = map[string]any{"issueLabel": map[string]any{"id": id, "organization": map[string]string{"id": "workspace"}, "team": map[string]string{"id": "team"}}}
		case strings.Contains(req.Query, "workflowState("):
			kind := "completed"
			if id == "retired" {
				kind = "canceled"
			}
			data = map[string]any{"workflowState": map[string]any{"id": id, "type": kind, "team": map[string]string{"id": "team"}}}
		case strings.Contains(req.Query, "webhooks("):
			data = map[string]any{"webhooks": map[string]any{"nodes": []any{}, "pageInfo": map[string]bool{"hasNextPage": false}}}
		default:
			fl.unexpected = append(fl.unexpected, req.Query)
		}
		body := map[string]any{"data": data}
		if fail {
			body = map[string]any{"data": nil, "errors": []any{map[string]string{"message": "Entity not found"}}}
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if len(fl.mutations) != 0 {
			t.Errorf("check sent a Linear mutation: %v", fl.mutations)
		}
		if len(fl.unexpected) != 0 {
			t.Errorf("unexpected queries: %v", fl.unexpected)
		}
	})
	return server
}

func checkDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "check.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func checkVersion(origin string, cfg store.LinearConfiguration) store.ConnectionVersion {
	cfg.WorkspaceID, cfg.TeamID, cfg.ProjectID, cfg.AuthKind = "workspace", "team", "project", "api_key"
	cfg.LifecycleStatesJSON = `{"completed":"completed","retired":"retired"}`
	return store.ConnectionVersion{ID: 1, RepoID: 7, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(origin), Destination: "team/project", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Linear: cfg}
}

func checkFactory(db *store.DB, agents []string) *Factory {
	return NewFactory(Options{DB: db, LabelAgents: agents, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "test-token", true }}})
}

var allModeLabels = []fakeLabel{{ID: "r", Name: "Mode: Research"}, {ID: "p", Name: "Mode: Plan"}, {ID: "i", Name: "Mode: Implement"}, {ID: "v", Name: "Mode: Validate"}, {ID: "s", Name: "Mode: Spike"}}

const fullModes = `{"research":"r","plan":"p","implement":"i","validate":"v","spike":"s"}`

const inspectFailRemedy = "Verify workspace, team, project and explicit label/state mappings."

func TestLinearCheckReportsDeletedModeIDAsBlocking(t *testing.T) {
	fl := &fakeLinear{labels: slices.Concat(allModeLabels[1:], []fakeLabel{{ID: "b", Name: "bug"}}), deleted: map[string]bool{"r": true}}
	server := fl.serve(t)
	db := checkDB(t)
	o := checkFactory(db, nil).Check(t.Context(), checkVersion(server.URL, store.LinearConfiguration{ModeLabelsJSON: fullModes, BugLabelID: "b"}))
	if o.Status != "inaccessible_or_missing_destination" || o.Reason != "linear_configuration_unverified" {
		t.Fatalf("status changed: %+v", o)
	}
	if !strings.HasPrefix(o.Remedy, inspectFailRemedy) {
		t.Fatalf("remedy lost today's text: %s", o.Remedy)
	}
	for _, want := range []string{"Blocking: stored ID not found: Mode: Research.", "`litefactory repo labels setup 7`"} {
		if !strings.Contains(o.Remedy, want) {
			t.Errorf("remedy lacks %q: %s", want, o.Remedy)
		}
	}
	if strings.Contains(o.Remedy, "Mode: Plan") {
		t.Errorf("a found Mode label is reported: %s", o.Remedy)
	}
	if fl.listReads != 1 {
		t.Errorf("label list reads = %d, want 1", fl.listReads)
	}
}

func TestLinearCheckDestinationFailureSkipsLabelRead(t *testing.T) {
	fl := &fakeLinear{labels: allModeLabels, destinationDown: true}
	server := fl.serve(t)
	o := checkFactory(checkDB(t), []string{"claude"}).Check(t.Context(), checkVersion(server.URL, store.LinearConfiguration{ModeLabelsJSON: fullModes}))
	if o.Status != "inaccessible_or_missing_destination" || o.Reason != "linear_configuration_unverified" || !strings.HasPrefix(o.Remedy, inspectFailRemedy) {
		t.Fatalf("observation: %+v", o)
	}
	if fl.listReads != 0 {
		t.Fatalf("destination failure read labels %d times", fl.listReads)
	}
	if strings.Contains(o.Remedy, "Blocking") || strings.Contains(o.Remedy, "for runner") {
		t.Fatalf("label diagnostics without a read: %s", o.Remedy)
	}
}

func TestLinearCheckWarnsPerUnmappedRunner(t *testing.T) {
	fl := &fakeLinear{labels: slices.Concat(allModeLabels, []fakeLabel{{ID: "a", Name: "Coding Agent: Claude"}, {ID: "b", Name: "bug"}})}
	server := fl.serve(t)
	o := checkFactory(checkDB(t), []string{"claude", "codex"}).Check(t.Context(), checkVersion(server.URL, store.LinearConfiguration{ModeLabelsJSON: fullModes, CodingAgentLabelsJSON: `{"Claude":"a"}`, BugLabelID: "b"}))
	if o.Status != "usable_for_checked_capabilities" {
		t.Fatalf("status: %+v", o)
	}
	if got := strings.Count(o.Remedy, "no coding agent label is mapped for runner"); got != 1 {
		t.Fatalf("per-runner warnings = %d: %s", got, o.Remedy)
	}
	if !strings.Contains(o.Remedy, "no coding agent label is mapped for runner: codex.") || strings.Contains(o.Remedy, "claude") {
		t.Fatalf("wrong runner named: %s", o.Remedy)
	}
	if strings.Contains(o.Remedy, "Warning: no coding agent label is mapped.") {
		t.Fatalf("generic warning not replaced: %s", o.Remedy)
	}
	if !strings.Contains(o.Remedy, "`litefactory repo labels setup 7`") {
		t.Fatalf("setup command missing: %s", o.Remedy)
	}
}

func TestLinearCheckListsMissingLabelsAndRefusedOptionalAsWarning(t *testing.T) {
	fl := &fakeLinear{labels: []fakeLabel{{ID: "p", Name: "Mode: Plan"}, {ID: "B", Name: "Bug"}}}
	server := fl.serve(t)
	db := checkDB(t)
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "wt")})
	if err != nil {
		t.Fatal(err)
	}
	v := checkVersion(server.URL, store.LinearConfiguration{ModeLabelsJSON: `{"plan":"p"}`})
	v.ID, v.RepoID, v.Provenance = 0, repo.ID, "explicit"
	v.ClonePath, v.RemoteURL, v.BaseBranch, v.FeaturePrefix, v.WorktreeRoot = repo.Path, repo.RemoteURL, repo.BaseBranch, repo.FeaturePrefix, repo.WorktreeRoot
	saved, err := db.SetConnection(t.Context(), v, 0)
	if err != nil {
		t.Fatal(err)
	}
	o, err := checkFactory(db, []string{"claude"}).CheckAndSave(t.Context(), db, saved)
	if err != nil {
		t.Fatalf("observation not saved: %v (%d bytes: %s)", err, len(o.Remedy), o.Remedy)
	}
	if o.Status != "usable_for_checked_capabilities" {
		t.Fatalf("status: %+v", o)
	}
	command := "`" + LabelSetupCommand(repo.ID) + "`"
	for _, want := range []string{"Blocking: missing in Linear: Mode: Research", "Warning: missing in Linear: Coding Agent: Claude; name the factory cannot read: bug; no coding agent label is mapped for runner: claude.", command} {
		if !strings.Contains(o.Remedy, want) {
			t.Errorf("remedy lacks %q: %s", want, o.Remedy)
		}
	}
	if strings.Count(o.Remedy, "Blocking") != 1 || strings.Contains(o.Remedy, "Mode: Plan") {
		t.Errorf("wrong severity or found label reported: %s", o.Remedy)
	}
	stored, err := db.GetConnectionObservation(t.Context(), saved.ID)
	if err != nil || stored.Remedy != o.Remedy {
		t.Fatalf("stored observation: %+v, %v", stored, err)
	}
}

func TestLinearCheckRemedyBoundWithManyRunners(t *testing.T) {
	fl := &fakeLinear{}
	server := fl.serve(t)
	var runners []string
	for i := range 40 {
		runners = append(runners, fmt.Sprintf("runner%02d", i))
	}
	db := checkDB(t)
	o := checkFactory(db, runners).Check(t.Context(), checkVersion(server.URL, store.LinearConfiguration{}))
	if len(o.Remedy) > maxRemedy {
		t.Fatalf("remedy is %d bytes: %s", len(o.Remedy), o.Remedy)
	}
	for _, want := range []string{"`litefactory repo labels setup 7`", "Blocking", "Warning", "more"} {
		if !strings.Contains(o.Remedy, want) {
			t.Errorf("bounded remedy lacks %q: %s", want, o.Remedy)
		}
	}
}

func TestBoundRemedy(t *testing.T) {
	many := func(prefix string, n int) []string {
		var out []string
		for i := range n {
			out = append(out, fmt.Sprintf("%s%03d", prefix, i))
		}
		return out
	}
	tail := " Run `litefactory repo labels setup 12`."
	findings := func(n int) []labelFinding {
		return []labelFinding{
			{Severity: "Blocking", Lead: "missing in Linear", Noun: "labels", Items: many("Mode: Thing ", 5)},
			{Severity: "Warning", Lead: "missing in Linear", Noun: "labels", Items: many("Coding Agent: Runner", n)},
			{Severity: "Warning", Lead: "no coding agent label is mapped for runner", Noun: "runners", Items: many("runner", n)},
		}
	}
	t.Run("fits unchanged", func(t *testing.T) {
		got := boundRemedy("Head.", "H.", findings(2), tail)
		want := "Head. Blocking: missing in Linear: Mode: Thing 000, Mode: Thing 001, Mode: Thing 002, Mode: Thing 003, Mode: Thing 004." +
			" Warning: missing in Linear: Coding Agent: Runner000, Coding Agent: Runner001; no coding agent label is mapped for runner: runner000, runner001." + tail
		if got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
	for _, n := range []int{10, 40, 200} {
		for _, head := range []string{"Head.", strings.Repeat("Head text. ", 30)} {
			t.Run(fmt.Sprintf("%d names/head %d", n, len(head)), func(t *testing.T) {
				got := boundRemedy(head, "Compact.", findings(n), tail)
				if len(got) > maxRemedy {
					t.Fatalf("%d bytes: %s", len(got), got)
				}
				for _, want := range []string{"Blocking: missing in Linear: Mode: Thing 000", "Warning: missing in Linear: Coding Agent: Runner000", "no coding agent label is mapped for runner: runner000", "more", tail} {
					if !strings.Contains(got, want) {
						t.Errorf("lacks %q: %s", want, got)
					}
				}
				if !strings.HasSuffix(got, tail) {
					t.Errorf("tail not last: %s", got)
				}
			})
		}
	}
	t.Run("long head kept while names shorten", func(t *testing.T) {
		head := strings.Repeat("Head text. ", 20)
		got := boundRemedy(head, "Compact.", findings(40), tail)
		if !strings.HasPrefix(got, head) || len(got) > maxRemedy {
			t.Fatalf("got %d bytes: %s", len(got), got)
		}
	})
	t.Run("compact head before counts", func(t *testing.T) {
		head := strings.Repeat("Head text. ", 40)
		got := boundRemedy(head, "Compact.", findings(40), tail)
		if !strings.HasPrefix(got, "Compact. Blocking:") || len(got) > maxRemedy || !strings.Contains(got, "more;") {
			t.Fatalf("got %d bytes: %s", len(got), got)
		}
	})
	t.Run("huge head is cut, findings kept", func(t *testing.T) {
		head := strings.Repeat("é", 400)
		got := boundRemedy(head, head, findings(40), tail)
		if len(got) > maxRemedy || !strings.Contains(got, "Blocking: missing in Linear") || !strings.Contains(got, "Warning: missing in Linear") || !strings.HasSuffix(got, tail) || !strings.HasPrefix(got, "é") {
			t.Fatalf("got %d bytes: %q", len(got), got)
		}
		if strings.ToValidUTF8(got, "?") != got {
			t.Fatalf("invalid UTF-8: %q", got)
		}
	})
}
