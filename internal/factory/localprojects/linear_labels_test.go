package localprojects

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// linearLabel is one label held by the fake Linear workspace. Team "" is a
// workspace-level label.
type linearLabel struct {
	ID, Name             string
	IsGroup              bool
	Team                 string
	ParentID, ParentName string
	Archived             bool
}

// linearCreate is one issueLabelCreate input the fake received.
type linearCreate struct {
	Name, Color, Description, TeamID, ParentID string
	IsGroup                                    bool
}

// fakeLinearLabels is a fake Linear GraphQL API for label setup. Hooks run
// with the mutex held and may change the fake's fields directly.
type fakeLinearLabels struct {
	mu        sync.Mutex
	labels    []linearLabel
	teamOrg   string // organization reported for team(; empty means "workspace"
	attempts  []linearCreate
	creates   []linearCreate // successful creates only
	lookups   []string       // issueLabel(id) IDs
	requests  map[string]int
	nextID    int
	failNth   int    // 1-based create attempt that fails
	failName  string // create name that fails
	failHow   string // "403" (default) or "unsuccessful"
	failLabel map[string]bool
	// before runs before each request with its kind; onCreate after each
	// successful create with the attempt number.
	before     func(f *fakeLinearLabels, kind string)
	onCreate   func(f *fakeLinearLabels, n int, c linearCreate, id string)
	unexpected []string
}

func (f *fakeLinearLabels) serve(t *testing.T) *httptest.Server {
	t.Helper()
	f.requests = map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		kind := "unexpected"
		switch {
		case strings.Contains(req.Query, "issueLabelCreate"):
			kind = "create"
		case strings.Contains(req.Query, "issueLabels("):
			kind = "list"
		case strings.Contains(req.Query, "viewer"):
			kind = "viewer"
		case strings.Contains(req.Query, "team("):
			kind = "team"
		case strings.Contains(req.Query, "issueLabel("):
			kind = "label"
		case strings.Contains(req.Query, "workflowState("):
			kind = "state"
		}
		f.requests[kind]++
		if f.before != nil {
			f.before(f, kind)
		}
		id, _ := req.Variables["id"].(string)
		var data any
		fail := false
		switch kind {
		case "create":
			in, _ := req.Variables["input"].(map[string]any)
			str := func(k string) string { s, _ := in[k].(string); return s }
			isGroup, _ := in["isGroup"].(bool)
			c := linearCreate{Name: str("name"), Color: str("color"), Description: str("description"), TeamID: str("teamId"), ParentID: str("parentId"), IsGroup: isGroup}
			f.attempts = append(f.attempts, c)
			n := len(f.attempts)
			if n == f.failNth || (f.failName != "" && c.Name == f.failName) {
				if f.failHow == "unsuccessful" {
					data = map[string]any{"issueLabelCreate": map[string]any{"success": false, "issueLabel": map[string]any{"id": ""}}}
					break
				}
				http.Error(w, `{"errors":[{"message":"forbidden"}]}`, http.StatusForbidden)
				return
			}
			f.nextID++
			newID := fmt.Sprintf("new-%d", f.nextID)
			l := linearLabel{ID: newID, Name: c.Name, IsGroup: c.IsGroup, Team: c.TeamID, ParentID: c.ParentID}
			for _, p := range f.labels {
				if p.ID == c.ParentID {
					l.ParentName = p.Name
				}
			}
			f.labels = append(f.labels, l)
			f.creates = append(f.creates, c)
			data = map[string]any{"issueLabelCreate": map[string]any{"success": true, "issueLabel": map[string]any{"id": newID}}}
			if f.onCreate != nil {
				f.onCreate(f, n, c, newID)
			}
		case "list":
			const size = 100
			offset := 0
			if after, ok := req.Variables["after"].(string); ok {
				offset, _ = strconv.Atoi(strings.TrimPrefix(after, "c"))
			}
			end := min(offset+size, len(f.labels))
			nodes := []any{}
			for _, l := range f.labels[offset:end] {
				n := map[string]any{"id": l.ID, "name": l.Name, "isGroup": l.IsGroup, "archivedAt": nil, "team": nil, "parent": nil}
				if l.Team != "" {
					n["team"] = map[string]string{"id": l.Team}
				}
				if l.ParentID != "" {
					n["parent"] = map[string]string{"id": l.ParentID, "name": l.ParentName}
				}
				if l.Archived {
					n["archivedAt"] = "2026-01-01T00:00:00Z"
				}
				nodes = append(nodes, n)
			}
			info := map[string]any{"hasNextPage": end < len(f.labels), "endCursor": fmt.Sprintf("c%d", end)}
			data = map[string]any{"issueLabels": map[string]any{"nodes": nodes, "pageInfo": info}}
		case "viewer":
			data = map[string]any{"viewer": map[string]any{"id": "bot", "active": true, "displayName": "bot", "organization": map[string]any{"id": "workspace"}}}
		case "team":
			org := f.teamOrg
			if org == "" {
				org = "workspace"
			}
			data = map[string]any{"team": map[string]any{"id": "team", "organization": map[string]any{"id": org}}, "project": map[string]any{"id": "project", "teams": map[string]any{"nodes": []any{map[string]string{"id": "team"}}, "pageInfo": map[string]bool{"hasNextPage": false}}}}
		case "label":
			f.lookups = append(f.lookups, id)
			var found *linearLabel
			for i := range f.labels {
				if f.labels[i].ID == id {
					found = &f.labels[i]
				}
			}
			if found == nil || f.failLabel[id] {
				fail = true
				break
			}
			l := map[string]any{"id": id, "isGroup": found.IsGroup, "archivedAt": nil, "team": nil, "organization": map[string]string{"id": "workspace"}}
			if found.Team != "" {
				l["team"] = map[string]string{"id": found.Team}
			}
			data = map[string]any{"issueLabel": l}
		case "state":
			stateKind := "completed"
			if id == "retired" {
				stateKind = "canceled"
			}
			data = map[string]any{"workflowState": map[string]any{"id": id, "type": stateKind, "team": map[string]string{"id": "team"}}}
		default:
			f.unexpected = append(f.unexpected, req.Query)
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
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.unexpected) != 0 {
			t.Errorf("unexpected Linear queries: %v", f.unexpected)
		}
	})
	return server
}

func (f *fakeLinearLabels) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.creates)
}

func (f *fakeLinearLabels) createdNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.creates {
		out = append(out, c.Name)
	}
	return out
}

func (f *fakeLinearLabels) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[kind]
}

func (f *fakeLinearLabels) add(l ...linearLabel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels = append(f.labels, l...)
}

func linearLabelFixture(t *testing.T, fake *fakeLinearLabels, cfg store.LinearConfiguration) (*Service, *store.DB, store.ConnectionVersion) {
	t.Helper()
	server := fake.serve(t)
	t.Setenv("LINEAR_LABEL_TEST_TOKEN", "test-token-value")
	home := t.TempDir()
	db, err := store.Open(t.Context(), config.NewPaths(home).DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(home, "repo"), Forge: store.ForgeGitHub, Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(home, "wt")})
	if err != nil {
		t.Fatal(err)
	}
	cfg.WorkspaceID, cfg.TeamID, cfg.ProjectID, cfg.AuthKind = "workspace", "team", "project", "api_key"
	cfg.LifecycleStatesJSON = `{"completed":"completed","retired":"retired"}`
	v := store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(server.URL), Destination: "team/project", CredentialSource: "env", CredentialEnv: "LINEAR_LABEL_TEST_TOKEN", Enabled: true, Provenance: "explicit", Linear: cfg}
	saved, err := db.SetConnection(t.Context(), v, 0)
	if err != nil {
		t.Fatal(err)
	}
	service, err := Open(t.Context(), db, repo.ID, Options{LabelAgents: []string{"claude", "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	return service, db, saved
}

type storedLabels struct {
	revision      int64
	modes, agents map[string]string
	bug           string
	version       store.ConnectionVersion
}

func currentLabels(t *testing.T, db *store.DB, repoID int64) storedLabels {
	t.Helper()
	v, err := db.CurrentConnection(t.Context(), repoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	modes, err := v.Linear.ModeLabels()
	if err != nil {
		t.Fatal(err)
	}
	agents, err := v.Linear.CodingAgentLabels()
	if err != nil {
		t.Fatal(err)
	}
	return storedLabels{revision: v.Revision, modes: modes, agents: agents, bug: v.Linear.BugLabelID, version: v}
}

func planLinear(t *testing.T, s *Service) *LabelSetup {
	t.Helper()
	setup, err := s.PlanLabelSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if setup.Linear == nil {
		t.Fatal("PlanLabelSetup did not route the Linear connection")
	}
	return setup
}

func entryFor(t *testing.T, plan projectconnection.LinearLabelPlan, name string) projectconnection.LinearLabelEntry {
	t.Helper()
	for _, e := range plan.Entries {
		if e.Wanted.Name == name {
			return e
		}
	}
	t.Fatalf("no plan entry for %q", name)
	return projectconnection.LinearLabelEntry{}
}

var wantedModeKeys = []string{"implement", "plan", "research", "spike", "validate"}

// The main path: an empty Linear team gets both label groups and the bug label,
// the IDs are saved, and a rerun stores everything and writes nothing.
func TestLinearLabelSetupEndToEnd(t *testing.T) {
	fake := &fakeLinearLabels{}
	// Paging, other teams and archived labels are read but not used.
	for i := range 150 {
		fake.labels = append(fake.labels, linearLabel{ID: fmt.Sprintf("other-%d", i), Name: fmt.Sprintf("Other %d", i), Team: "team"})
	}
	fake.labels = append(fake.labels,
		linearLabel{ID: "foreign-plan", Name: "Mode: Plan", Team: "other-team"},
		linearLabel{ID: "archived-bug", Name: "bug", Team: "team", Archived: true})
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})

	setup := planLinear(t, service)
	if fake.count("list") != 2 {
		t.Fatalf("label pages read = %d, want 2", fake.count("list"))
	}
	if !setup.Linear.Applicable() || !setup.Linear.Plan.Changed {
		t.Fatalf("dry run not applicable: %+v invalid %q", setup.Linear.Plan, setup.Linear.Invalid)
	}
	var planned []string
	for _, c := range setup.Linear.Plan.Creates {
		planned = append(planned, createName(c))
	}
	want := []string{"Mode (group)", "Mode: Research (in group)", "Mode: Plan (in group)", "Mode: Implement (in group)", "Mode: Validate (in group)", "Mode: Spike (in group)", "Coding Agent (group)", "Coding Agent: Claude (in group)", "Coding Agent: Codex (in group)", "bug"}
	if !slices.Equal(planned, want) {
		t.Fatalf("planned creates:\n got %v\nwant %v", planned, want)
	}
	if fake.createCount() != 0 || currentLabels(t, db, v.RepoID).revision != v.Revision {
		t.Fatal("dry run wrote")
	}

	res := setup.Apply(t.Context())
	if res.Refused != "" || res.Failed != "" || res.SaveRefused != "" || res.NothingToSave {
		t.Fatalf("apply: %+v", res)
	}
	if !slices.Equal(res.Created, want) || len(res.LinearCreated) != 10 {
		t.Fatalf("created %v / %+v", res.Created, res.LinearCreated)
	}
	fake.mu.Lock()
	creates := slices.Clone(fake.creates)
	fake.mu.Unlock()
	wantNames := []string{"Mode", "Research", "Plan", "Implement", "Validate", "Spike", "Coding Agent", "Claude", "Codex", "bug"}
	if len(creates) != len(wantNames) {
		t.Fatalf("creates: %+v", creates)
	}
	for i, c := range creates {
		wantParent := ""
		switch {
		case i >= 1 && i <= 5:
			wantParent = "new-1"
		case i == 7 || i == 8:
			wantParent = "new-7"
		}
		if c.Name != wantNames[i] || c.TeamID != "team" || c.ParentID != wantParent || c.IsGroup != (i == 0 || i == 6) || len(c.Color) != 7 || c.Color[0] != '#' {
			t.Errorf("create %d = %+v, want name %q parent %q", i, c, wantNames[i], wantParent)
		}
	}
	saved := currentLabels(t, db, v.RepoID)
	if res.SavedRevision != saved.revision || saved.revision <= v.Revision {
		t.Fatalf("saved revision %d, current %d, before %d", res.SavedRevision, saved.revision, v.Revision)
	}
	wantModes := map[string]string{"research": "new-2", "plan": "new-3", "implement": "new-4", "validate": "new-5", "spike": "new-6"}
	wantAgents := map[string]string{"Claude": "new-8", "Codex": "new-9"}
	if !slices.Equal(slices.Sorted(maps.Keys(saved.modes)), wantedModeKeys) || !maps.Equal(saved.modes, wantModes) || !maps.Equal(saved.agents, wantAgents) || saved.bug != "new-10" {
		t.Fatalf("saved maps: %+v %+v %q", saved.modes, saved.agents, saved.bug)
	}

	again := planLinear(t, service)
	for _, e := range again.Linear.Plan.Entries {
		if e.Status != projectconnection.StatusStored {
			t.Errorf("rerun entry %s = %s", e.Wanted.Name, e.Status)
		}
	}
	if again.Linear.Plan.Changed || !again.Linear.Applicable() || len(again.Linear.Plan.Creates) != 0 {
		t.Fatalf("rerun plan: %+v", again.Linear.Plan)
	}
	res = again.Apply(t.Context())
	if !res.NothingToSave || res.Refused != "" || res.SavedRevision != 0 || fake.createCount() != 10 || currentLabels(t, db, v.RepoID).revision != saved.revision {
		t.Fatalf("rerun apply: %+v creates %d", res, fake.createCount())
	}
}

// Existing labels in either scope and form are adopted; only the rest is created.
func TestLinearLabelSetupAdoptsExistingLabels(t *testing.T) {
	fake := &fakeLinearLabels{labels: []linearLabel{
		{ID: "team-plan", Name: "Mode: Plan", Team: "team"},
		{ID: "ws-mode", Name: "Mode", IsGroup: true},
		{ID: "ws-research", Name: "Research", ParentID: "ws-mode", ParentName: "Mode"},
		{ID: "ws-implement", Name: "Implement", ParentID: "ws-mode", ParentName: "Mode"},
		{ID: "ws-validate", Name: "Validate", ParentID: "ws-mode", ParentName: "Mode"},
		{ID: "ws-spike", Name: "Spike", ParentID: "ws-mode", ParentName: "Mode"},
	}}
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
	setup := planLinear(t, service)
	if !setup.Linear.Applicable() {
		t.Fatalf("not applicable: %+v %q", setup.Linear.Plan.Entries, setup.Linear.Invalid)
	}
	plan := entryFor(t, setup.Linear.Plan, "Mode: Plan")
	research := entryFor(t, setup.Linear.Plan, "Mode: Research")
	if plan.Status != projectconnection.StatusAdopted || plan.Chosen.Scope != projectconnection.ScopeTeam || plan.Chosen.Form != projectconnection.FormFlat ||
		research.Status != projectconnection.StatusAdopted || research.Chosen.Scope != projectconnection.ScopeWorkspace || research.Chosen.Form != projectconnection.FormGrouped {
		t.Fatalf("adoption: %+v / %+v", plan, research)
	}
	res := setup.Apply(t.Context())
	if res.Refused != "" || res.Failed != "" || res.SaveRefused != "" {
		t.Fatalf("apply: %+v", res)
	}
	if got := fake.createdNames(); !slices.Equal(got, []string{"Coding Agent", "Claude", "Codex", "bug"}) {
		t.Fatalf("created %v", got)
	}
	saved := currentLabels(t, db, v.RepoID)
	wantModes := map[string]string{"research": "ws-research", "plan": "team-plan", "implement": "ws-implement", "validate": "ws-validate", "spike": "ws-spike"}
	if res.SavedRevision != saved.revision || saved.revision <= v.Revision || !maps.Equal(saved.modes, wantModes) || !maps.Equal(saved.agents, map[string]string{"Claude": "new-2", "Codex": "new-3"}) || saved.bug != "new-4" {
		t.Fatalf("saved: %+v", saved)
	}
}

func TestLinearLabelSetupLabelAppearsBeforeApply(t *testing.T) {
	t.Run("one new match is adopted", func(t *testing.T) {
		fake := &fakeLinearLabels{}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		setup := planLinear(t, service)
		fake.add(linearLabel{ID: "late-bug", Name: "bug", Team: "team"})
		res := setup.Apply(t.Context())
		if res.Refused != "" || res.SaveRefused != "" || res.Failed != "" || !slices.Equal(res.AppearedAdopted, []string{"bug"}) {
			t.Fatalf("apply: %+v", res)
		}
		if names := fake.createdNames(); len(names) != 9 || slices.Contains(names, "bug") {
			t.Fatalf("created %v", names)
		}
		saved := currentLabels(t, db, v.RepoID)
		if saved.bug != "late-bug" || res.SavedRevision != saved.revision || saved.revision <= v.Revision || len(saved.modes) != 5 || len(saved.agents) != 2 {
			t.Fatalf("saved: %+v", saved)
		}
	})
	t.Run("two new matches refuse", func(t *testing.T) {
		fake := &fakeLinearLabels{}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		setup := planLinear(t, service)
		fake.add(linearLabel{ID: "team-spike", Name: "Mode: Spike", Team: "team"}, linearLabel{ID: "ws-spike", Name: "Mode: Spike"})
		res := setup.Apply(t.Context())
		if res.Refused == "" || res.SavedRevision != 0 || len(res.Created) != 0 {
			t.Fatalf("apply: %+v", res)
		}
		if res.Replanned == nil || entryFor(t, *res.Replanned, "Mode: Spike").Status != projectconnection.StatusAmbiguous {
			t.Fatalf("replanned: %+v", res.Replanned)
		}
		saved := currentLabels(t, db, v.RepoID)
		if fake.createCount() != 0 || saved.revision != v.Revision || len(saved.modes) != 0 {
			t.Fatalf("wrote: creates %d saved %+v", fake.createCount(), saved)
		}
	})
}

func TestLinearLabelSetupAmbiguity(t *testing.T) {
	labels := []linearLabel{
		{ID: "team-plan", Name: "Mode: Plan", Team: "team"},
		{ID: "ws-mode", Name: "Mode", IsGroup: true},
		{ID: "ws-plan", Name: "Plan", ParentID: "ws-mode", ParentName: "Mode"},
		{ID: "r", Name: "Mode: Research", Team: "team"},
		{ID: "i", Name: "Mode: Implement", Team: "team"},
		{ID: "v", Name: "Mode: Validate", Team: "team"},
		{ID: "s", Name: "Mode: Spike", Team: "team"},
	}
	t.Run("refuses", func(t *testing.T) {
		fake := &fakeLinearLabels{labels: slices.Clone(labels)}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		setup := planLinear(t, service)
		if setup.Linear.Applicable() || setup.Linear.Plan.Applicable {
			t.Fatal("ambiguous plan is applicable")
		}
		e := entryFor(t, setup.Linear.Plan, "Mode: Plan")
		wantCandidates := []projectconnection.LinearCandidate{
			{ID: "team-plan", Name: "Mode: Plan", Scope: projectconnection.ScopeTeam, Form: projectconnection.FormFlat},
			{ID: "ws-plan", Name: "Plan", Group: "Mode", Scope: projectconnection.ScopeWorkspace, Form: projectconnection.FormGrouped},
		}
		if e.Status != projectconnection.StatusAmbiguous || e.Severity != projectconnection.SeverityBlocking || !slices.Equal(e.Candidates, wantCandidates) {
			t.Fatalf("entry: %+v", e)
		}
		res := setup.Apply(t.Context())
		if res.Refused == "" || res.SavedRevision != 0 || fake.createCount() != 0 || currentLabels(t, db, v.RepoID).revision != v.Revision {
			t.Fatalf("apply: %+v creates %d", res, fake.createCount())
		}
	})
	t.Run("stored ID settles it", func(t *testing.T) {
		fake := &fakeLinearLabels{labels: slices.Clone(labels)}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{ModeLabelsJSON: `{"plan":"team-plan"}`})
		setup := planLinear(t, service)
		if !setup.Linear.Applicable() {
			t.Fatalf("not applicable: %+v %q", setup.Linear.Plan.Entries, setup.Linear.Invalid)
		}
		e := entryFor(t, setup.Linear.Plan, "Mode: Plan")
		if e.Status != projectconnection.StatusStored || e.Chosen == nil || e.Chosen.ID != "team-plan" || len(e.Mismatch) != 1 || e.Mismatch[0].ID != "ws-plan" || e.Mismatch[0].Scope != projectconnection.ScopeWorkspace {
			t.Fatalf("entry: %+v", e)
		}
		res := setup.Apply(t.Context())
		if res.Refused != "" || res.SaveRefused != "" || res.Failed != "" {
			t.Fatalf("apply: %+v", res)
		}
		saved := currentLabels(t, db, v.RepoID)
		wantModes := map[string]string{"research": "r", "plan": "team-plan", "implement": "i", "validate": "v", "spike": "s"}
		if fake.createCount() != 4 || res.SavedRevision != saved.revision || saved.revision <= v.Revision || !maps.Equal(saved.modes, wantModes) || len(saved.agents) != 2 || saved.bug == "" {
			t.Fatalf("creates %v saved %+v", fake.createdNames(), saved)
		}
	})
}

// A failed create stops the run with nothing saved; the rerun adopts what was
// created and creates only the rest.
func TestLinearLabelSetupFailedCreateAndRerun(t *testing.T) {
	for _, how := range []string{"403", "unsuccessful"} {
		t.Run(how, func(t *testing.T) {
			fake := &fakeLinearLabels{failNth: 3, failHow: how}
			service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
			res := planLinear(t, service).Apply(t.Context())
			if res.Failed != "Mode: Plan (in group)" || res.FailedWhy == "" || !slices.Equal(res.Created, []string{"Mode (group)", "Mode: Research (in group)"}) || len(res.NotTried) != 7 || res.SavedRevision != 0 {
				t.Fatalf("report: %+v", res)
			}
			before := currentLabels(t, db, v.RepoID)
			if fake.createCount() != 2 || before.revision != v.Revision || len(before.modes) != 0 {
				t.Fatalf("after failure: creates %d saved %+v", fake.createCount(), before)
			}
			fake.mu.Lock()
			fake.failNth = 0
			fake.mu.Unlock()
			setup := planLinear(t, service)
			if e := entryFor(t, setup.Linear.Plan, "Mode: Research"); e.Status != projectconnection.StatusAdopted || e.Chosen.ID != "new-2" {
				t.Fatalf("rerun research: %+v", e)
			}
			res = setup.Apply(t.Context())
			if res.Failed != "" || res.Refused != "" || res.SaveRefused != "" {
				t.Fatalf("rerun: %+v", res)
			}
			names := fake.createdNames()
			wantNames := []string{"Mode", "Research", "Plan", "Implement", "Validate", "Spike", "Coding Agent", "Claude", "Codex", "bug"}
			if !slices.Equal(names, wantNames) {
				t.Fatalf("each label once: %v", names)
			}
			fake.mu.Lock()
			for _, c := range fake.creates[2:6] {
				if c.ParentID != "new-1" {
					t.Errorf("rerun create %+v not under the existing group", c)
				}
			}
			fake.mu.Unlock()
			saved := currentLabels(t, db, v.RepoID)
			if res.SavedRevision != saved.revision || saved.revision <= v.Revision || saved.modes["research"] != "new-2" || len(saved.modes) != 5 || len(saved.agents) != 2 || saved.bug == "" {
				t.Fatalf("saved: %+v", saved)
			}
		})
	}
}

// A stale optional ID is a warning; it is kept as stored and never looked up.
func TestLinearLabelSetupKeepsStaleOptionalID(t *testing.T) {
	fake := &fakeLinearLabels{}
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{CodingAgentLabelsJSON: `{"Claude":"gone"}`})
	setup := planLinear(t, service)
	e := entryFor(t, setup.Linear.Plan, "Coding Agent: Claude")
	if e.Status != projectconnection.StatusStale || e.Severity != projectconnection.SeverityWarning || !setup.Linear.Applicable() {
		t.Fatalf("entry %+v applicable %v %q", e, setup.Linear.Applicable(), setup.Linear.Invalid)
	}
	res := setup.Apply(t.Context())
	if res.Refused != "" || res.Failed != "" || res.SaveRefused != "" {
		t.Fatalf("apply: %+v", res)
	}
	if names := fake.createdNames(); slices.Contains(names, "Claude") || !slices.Contains(names, "Codex") || len(names) != 9 {
		t.Fatalf("created %v", names)
	}
	saved := currentLabels(t, db, v.RepoID)
	if res.SavedRevision != saved.revision || saved.revision <= v.Revision || len(saved.modes) != 5 || saved.agents["Claude"] != "gone" || saved.agents["Codex"] == "" {
		t.Fatalf("saved: %+v", saved)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if slices.Contains(fake.lookups, "gone") || len(fake.lookups) == 0 {
		t.Fatalf("issueLabel lookups: %v", fake.lookups)
	}
}

// An adopted label whose ID is already stored elsewhere makes the candidate
// unsavable, so nothing is written.
func TestLinearLabelSetupRefusesInvalidCandidate(t *testing.T) {
	fake := &fakeLinearLabels{labels: []linearLabel{{ID: "x", Name: "bug", Team: "team"}}}
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{CodingAgentLabelsJSON: `{"Codex":"x"}`})
	setup := planLinear(t, service)
	if e := entryFor(t, setup.Linear.Plan, "bug"); e.Status != projectconnection.StatusAdopted || e.Chosen.ID != "x" {
		t.Fatalf("bug entry: %+v", e)
	}
	if setup.Linear.Invalid == "" || setup.Linear.Applicable() {
		t.Fatalf("invalid %q applicable %v", setup.Linear.Invalid, setup.Linear.Applicable())
	}
	res := setup.Apply(t.Context())
	saved := currentLabels(t, db, v.RepoID)
	if res.Refused == "" || res.SavedRevision != 0 || fake.createCount() != 0 || saved.revision != v.Revision || saved.bug != "" || !maps.Equal(saved.agents, map[string]string{"Codex": "x"}) {
		t.Fatalf("apply %+v creates %d saved %+v", res, fake.createCount(), saved)
	}
}

// Linear not confirming a created label at save time saves nothing; the rerun
// adopts the created labels and saves.
func TestLinearLabelSetupSaveTimeVerificationRefusal(t *testing.T) {
	fake := &fakeLinearLabels{failLabel: map[string]bool{"new-3": true}}
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
	res := planLinear(t, service).Apply(t.Context())
	if res.SaveRefused == "" || res.SavedRevision != 0 || len(res.Created) != 10 {
		t.Fatalf("apply: %+v", res)
	}
	saved := currentLabels(t, db, v.RepoID)
	if fake.createCount() != 10 || saved.revision != v.Revision || len(saved.modes) != 0 || len(saved.agents) != 0 || saved.bug != "" {
		t.Fatalf("saved %+v", saved)
	}
	fake.mu.Lock()
	fake.failLabel = nil
	fake.mu.Unlock()
	setup := planLinear(t, service)
	if len(setup.Linear.Plan.Creates) != 0 || !setup.Linear.Applicable() {
		t.Fatalf("rerun plan: %+v", setup.Linear.Plan)
	}
	res = setup.Apply(t.Context())
	saved = currentLabels(t, db, v.RepoID)
	if res.SaveRefused != "" || res.Refused != "" || fake.createCount() != 10 || res.SavedRevision != saved.revision || saved.revision <= v.Revision || saved.modes["plan"] != "new-3" || len(saved.modes) != 5 || len(saved.agents) != 2 || saved.bug != "new-10" {
		t.Fatalf("rerun %+v saved %+v", res, saved)
	}
}

func TestLinearLabelSetupRevisionConflict(t *testing.T) {
	t.Run("before apply", func(t *testing.T) {
		fake := &fakeLinearLabels{}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		setup := planLinear(t, service)
		other, err := db.SetConnection(t.Context(), v, v.Revision)
		if err != nil {
			t.Fatal(err)
		}
		res := setup.Apply(t.Context())
		if res.Refused == "" || fake.createCount() != 0 || currentLabels(t, db, v.RepoID).revision != other.Revision {
			t.Fatalf("apply %+v creates %d", res, fake.createCount())
		}
	})
	t.Run("between creates and save", func(t *testing.T) {
		fake := &fakeLinearLabels{}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		t.Setenv("LINEAR_LABEL_OTHER_TOKEN", "test-token-value")
		var other store.ConnectionVersion
		fake.onCreate = func(_ *fakeLinearLabels, n int, _ linearCreate, _ string) {
			if n != 10 {
				return
			}
			changed := v
			changed.CredentialEnv = "LINEAR_LABEL_OTHER_TOKEN"
			var err error
			if other, err = db.SetConnection(t.Context(), changed, v.Revision); err != nil {
				t.Error(err)
			}
		}
		res := planLinear(t, service).Apply(t.Context())
		if res.SaveRefused == "" || res.SavedRevision != 0 || fake.createCount() != 10 {
			t.Fatalf("apply %+v", res)
		}
		fake.mu.Lock() // other is written by the hook under the fake's mutex
		otherRevision := other.Revision
		fake.mu.Unlock()
		saved := currentLabels(t, db, v.RepoID)
		if otherRevision == 0 || saved.revision != otherRevision || saved.version.CredentialEnv != "LINEAR_LABEL_OTHER_TOKEN" || len(saved.modes) != 0 || len(saved.agents) != 0 || saved.bug != "" {
			t.Fatalf("stored settings are not the other writer's: %+v", saved)
		}
		fake.mu.Lock()
		fake.onCreate = nil
		fake.mu.Unlock()
		res = planLinear(t, service).Apply(t.Context())
		saved = currentLabels(t, db, v.RepoID)
		if res.SaveRefused != "" || res.Refused != "" || fake.createCount() != 10 || res.SavedRevision != saved.revision || saved.revision <= otherRevision || len(saved.modes) != 5 || len(saved.agents) != 2 || saved.bug != "new-10" || saved.version.CredentialEnv != "LINEAR_LABEL_OTHER_TOKEN" {
			t.Fatalf("rerun %+v saved %+v", res, saved)
		}
	})
}

func TestLinearLabelSetupOverlappingRuns(t *testing.T) {
	fake := &fakeLinearLabels{}
	service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
	setup := planLinear(t, service)
	release, err := db.LockLabelSetup(t.Context(), "workspace/team")
	if err != nil {
		t.Fatal(err)
	}
	res := setup.Apply(t.Context())
	if !strings.Contains(res.Refused, "another label setup") || fake.createCount() != 0 || currentLabels(t, db, v.RepoID).revision != v.Revision {
		t.Fatalf("apply while locked: %+v creates %d", res, fake.createCount())
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	res = planLinear(t, service).Apply(t.Context())
	saved := currentLabels(t, db, v.RepoID)
	if res.Refused != "" || res.SaveRefused != "" || fake.createCount() != 10 || res.SavedRevision != saved.revision || len(saved.modes) != 5 {
		t.Fatalf("rerun %+v saved %+v", res, saved)
	}
}

func TestLinearLabelSetupRefusesBeforeReading(t *testing.T) {
	t.Run("destination mismatch", func(t *testing.T) {
		fake := &fakeLinearLabels{teamOrg: "other-workspace"}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		if _, err := service.PlanLabelSetup(t.Context()); err == nil {
			t.Fatal("mismatched destination planned")
		}
		if fake.count("list") != 0 || fake.count("create") != 0 || currentLabels(t, db, v.RepoID).revision != v.Revision {
			t.Fatalf("requests %v", fake.requests)
		}
	})
	t.Run("disabled connection", func(t *testing.T) {
		fake := &fakeLinearLabels{}
		service, db, v := linearLabelFixture(t, fake, store.LinearConfiguration{})
		disabled, err := db.DisableConnection(t.Context(), v.RepoID, store.TicketConnection, v.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PlanLabelSetup(t.Context()); err == nil {
			t.Fatal("disabled connection planned")
		}
		fake.mu.Lock()
		total := 0
		for _, n := range fake.requests {
			total += n
		}
		fake.mu.Unlock()
		if total != 0 || currentLabels(t, db, v.RepoID).revision != disabled.Revision {
			t.Fatalf("requests %v", fake.requests)
		}
	})
}
