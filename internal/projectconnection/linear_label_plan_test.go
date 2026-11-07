package projectconnection_test

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

var planModes = []string{"research", "plan", "implement", "validate", "spike"}

func planWanted() []agent.WantedLabel { return agent.WantedLabels([]string{"claude"}) }

func teamGroup(id, name string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name, IsGroup: true, TeamID: "team"}
}

func workspaceGroup(id, name string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name, IsGroup: true}
}

func teamChild(id, name, parentID, parentName string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name, TeamID: "team", ParentID: parentID, ParentName: parentName}
}

func workspaceChild(id, name, parentID, parentName string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name, ParentID: parentID, ParentName: parentName}
}

func teamFlat(id, name string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name, TeamID: "team"}
}

func workspaceFlat(id, name string) linear.SetupLabel {
	return linear.SetupLabel{ID: id, Name: name}
}

// fullyGrouped returns team labels for every wanted label in grouped form.
func fullyGrouped() []linear.SetupLabel {
	found := []linear.SetupLabel{teamGroup("gm", "Mode"), teamGroup("ga", "Coding Agent")}
	for _, m := range planModes {
		found = append(found, teamChild("m-"+m, agent.Mode(m).Label()[len("Mode: "):], "gm", "Mode"))
	}
	return append(found, teamChild("a-claude", "Claude", "ga", "Coding Agent"), teamFlat("b", "bug"))
}

func storedModes() map[string]string {
	out := map[string]string{}
	for _, m := range planModes {
		out[m] = "m-" + m
	}
	return out
}

func planEntry(t *testing.T, p projectconnection.LinearLabelPlan, name string) projectconnection.LinearLabelEntry {
	t.Helper()
	for _, e := range p.Entries {
		if e.Wanted.Name == name {
			return e
		}
	}
	t.Fatalf("no plan entry for %q", name)
	return projectconnection.LinearLabelEntry{}
}

func candidateIDs(cs []projectconnection.LinearCandidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func validLinearVersion(cfg store.LinearConfiguration) store.ConnectionVersion {
	cfg.WorkspaceID, cfg.TeamID, cfg.ProjectID, cfg.AuthKind = "w", "team", "project", "api_key"
	cfg.LifecycleStatesJSON = `{"completed":"c","retired":"r"}`
	return store.ConnectionVersion{
		RepoID: 1, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin("https://api.linear.app"),
		Destination: "team/project", CredentialSource: "env", CredentialEnv: "X", Enabled: true, Provenance: "explicit", Linear: cfg,
	}
}

func TestPlanLinearStoredIDWinsAndShowsMismatch(t *testing.T) {
	found := append(fullyGrouped(), teamFlat("flat-plan", "Mode: Plan"), workspaceFlat("ws-plan", "mode: plan"))
	tests := []struct {
		name  string
		modes map[string]string
	}{
		{"short key", storedModes()},
		{"canonical key", func() map[string]string {
			m := storedModes()
			delete(m, "plan")
			m["Mode: Plan"] = "m-plan"
			return m
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := projectconnection.PlanLinearLabels(planWanted(), found, tc.modes, map[string]string{"Claude": "a-claude"}, "b")
			e := planEntry(t, p, "Mode: Plan")
			if e.Status != projectconnection.StatusStored || e.Chosen == nil || e.Chosen.ID != "m-plan" || e.Severity != "" {
				t.Fatalf("entry = %+v", e)
			}
			if got := candidateIDs(e.Mismatch); !slices.Equal(got, []string{"flat-plan", "ws-plan"}) {
				t.Fatalf("mismatch = %v", got)
			}
			if !p.Applicable || p.Changed || len(p.Creates) != 0 {
				t.Fatalf("plan applicable=%v changed=%v creates=%v", p.Applicable, p.Changed, p.Creates)
			}
			if _, ok := p.Modes["plan"]; tc.name == "canonical key" && ok {
				t.Fatalf("modes gained a second plan key: %v", p.Modes)
			}
		})
	}
}

func TestPlanLinearAmbiguity(t *testing.T) {
	tests := []struct {
		name      string
		wanted    string
		found     []linear.SetupLabel
		wantIDs   []string
		wantScope []projectconnection.LabelScope
		wantForm  []projectconnection.LabelForm
	}{
		{
			name:      "team and workspace",
			wanted:    "Mode: Plan",
			found:     []linear.SetupLabel{teamFlat("t", "Mode: Plan"), workspaceFlat("w", "Mode: Plan")},
			wantIDs:   []string{"t", "w"},
			wantScope: []projectconnection.LabelScope{projectconnection.ScopeTeam, projectconnection.ScopeWorkspace},
			wantForm:  []projectconnection.LabelForm{projectconnection.FormFlat, projectconnection.FormFlat},
		},
		{
			name:      "grouped and flat",
			wanted:    "Mode: Plan",
			found:     []linear.SetupLabel{teamGroup("gm", "Mode"), teamChild("g", "Plan", "gm", "Mode"), teamFlat("f", "Mode: Plan")},
			wantIDs:   []string{"g", "f"},
			wantScope: []projectconnection.LabelScope{projectconnection.ScopeTeam, projectconnection.ScopeTeam},
			wantForm:  []projectconnection.LabelForm{projectconnection.FormGrouped, projectconnection.FormFlat},
		},
		{
			name:      "agent grouped workspace and flat team",
			wanted:    "Coding Agent: Claude",
			found:     []linear.SetupLabel{workspaceGroup("ga", "coding agent"), workspaceChild("g", "claude", "ga", "coding agent"), teamFlat("f", "Coding Agent: Claude")},
			wantIDs:   []string{"g", "f"},
			wantScope: []projectconnection.LabelScope{projectconnection.ScopeWorkspace, projectconnection.ScopeTeam},
			wantForm:  []projectconnection.LabelForm{projectconnection.FormGrouped, projectconnection.FormFlat},
		},
		{
			name:      "two bug labels",
			wanted:    "bug",
			found:     []linear.SetupLabel{teamFlat("b1", "bug"), workspaceFlat("b2", "Bug")},
			wantIDs:   []string{"b1", "b2"},
			wantScope: []projectconnection.LabelScope{projectconnection.ScopeTeam, projectconnection.ScopeWorkspace},
			wantForm:  []projectconnection.LabelForm{projectconnection.FormFlat, projectconnection.FormFlat},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := projectconnection.PlanLinearLabels(planWanted(), tc.found, nil, nil, "")
			e := planEntry(t, p, tc.wanted)
			if e.Status != projectconnection.StatusAmbiguous || e.Severity != projectconnection.SeverityBlocking || e.Create != nil || e.Chosen != nil {
				t.Fatalf("entry = %+v", e)
			}
			if got := candidateIDs(e.Candidates); !slices.Equal(got, tc.wantIDs) {
				t.Fatalf("candidates = %v, want %v", got, tc.wantIDs)
			}
			for i, c := range e.Candidates {
				if c.Scope != tc.wantScope[i] || c.Form != tc.wantForm[i] {
					t.Fatalf("candidate %d = %+v, want %s %s", i, c, tc.wantScope[i], tc.wantForm[i])
				}
			}
			if p.Applicable {
				t.Fatal("an ambiguous label left the plan applicable")
			}
			for _, id := range tc.wantIDs {
				if slices.Contains(p.VerifyIDs, id) || p.Bug == id || slices.Contains(mapValues(p.Modes), id) || slices.Contains(mapValues(p.Agents), id) {
					t.Fatalf("ambiguous candidate %s was stored: %+v", id, p)
				}
			}
		})
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestPlanLinearStoredIDSettlesSeveralMatches(t *testing.T) {
	found := []linear.SetupLabel{teamFlat("t", "Mode: Plan"), workspaceFlat("w", "Mode: Plan"), teamGroup("gm", "Mode"), teamChild("g", "Plan", "gm", "Mode")}
	p := projectconnection.PlanLinearLabels(planWanted(), found, map[string]string{"plan": "w"}, nil, "")
	e := planEntry(t, p, "Mode: Plan")
	if e.Status != projectconnection.StatusStored || e.Chosen.ID != "w" || e.Chosen.Scope != projectconnection.ScopeWorkspace || e.Severity != "" {
		t.Fatalf("entry = %+v", e)
	}
	if got := candidateIDs(e.Mismatch); !slices.Equal(got, []string{"t", "g"}) {
		t.Fatalf("mismatch = %v", got)
	}
	if p.Modes["plan"] != "w" {
		t.Fatalf("stored ID replaced: %v", p.Modes)
	}
}

func TestPlanLinearStaleStoredID(t *testing.T) {
	found := []linear.SetupLabel{teamFlat("x", "Mode: Plan"), teamFlat("bx", "bug"), teamGroup("grp", "Coding Agent")}
	tests := []struct {
		name, wanted string
		modes        map[string]string
		agents       map[string]string
		bug          string
		severity     projectconnection.LabelSeverity
		candidates   []string
	}{
		{"mode blocking", "Mode: Plan", map[string]string{"plan": "gone"}, nil, "", projectconnection.SeverityBlocking, []string{"x"}},
		{"group ID is not a label", "Mode: Plan", map[string]string{"plan": "grp"}, nil, "", projectconnection.SeverityBlocking, []string{"x"}},
		{"agent warning", "Coding Agent: Claude", nil, map[string]string{"claude": "gone"}, "", projectconnection.SeverityWarning, nil},
		{"bug warning", "bug", nil, nil, "gone", projectconnection.SeverityWarning, []string{"bx"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := projectconnection.PlanLinearLabels(planWanted(), found, tc.modes, tc.agents, tc.bug)
			e := planEntry(t, p, tc.wanted)
			if e.Status != projectconnection.StatusStale || e.Severity != tc.severity || e.Create != nil {
				t.Fatalf("entry = %+v", e)
			}
			if got := candidateIDs(e.Candidates); !slices.Equal(got, tc.candidates) {
				t.Fatalf("candidates = %v, want %v", got, tc.candidates)
			}
			if p.Applicable != (tc.severity != projectconnection.SeverityBlocking) {
				t.Fatalf("applicable = %v", p.Applicable)
			}
			for k, v := range tc.modes {
				if p.Modes[k] != v {
					t.Fatalf("stale mode ID replaced: %v", p.Modes)
				}
			}
			for k, v := range tc.agents {
				if p.Agents[k] != v {
					t.Fatalf("stale agent ID replaced: %v", p.Agents)
				}
			}
			if tc.bug != "" && p.Bug != tc.bug {
				t.Fatalf("stale bug ID replaced: %q", p.Bug)
			}
		})
	}
}

func TestPlanLinearAdoptsSingleCandidate(t *testing.T) {
	found := []linear.SetupLabel{
		workspaceGroup("gm", "mode"), workspaceChild("plan-id", "plan", "gm", "mode"),
		teamFlat("claude-id", "Coding Agent: Claude"), teamFlat("bug-id", "bug"),
	}
	p := projectconnection.PlanLinearLabels(planWanted(), found, nil, nil, "")
	if e := planEntry(t, p, "Mode: Plan"); e.Status != projectconnection.StatusAdopted || e.Severity != projectconnection.SeverityBlocking {
		// lower-case group "mode" reads as "plan", which the reader refuses
		t.Fatalf("plan entry = %+v", e)
	}
	if _, ok := p.Modes["plan"]; ok {
		t.Fatalf("refused Mode label stored: %v", p.Modes)
	}
	if e := planEntry(t, p, "Coding Agent: Claude"); e.Status != projectconnection.StatusAdopted || e.Severity != "" || p.Agents["Claude"] != "claude-id" {
		t.Fatalf("agent entry = %+v agents=%v", e, p.Agents)
	}
	if e := planEntry(t, p, "bug"); e.Status != projectconnection.StatusAdopted || e.Severity != "" || p.Bug != "bug-id" {
		t.Fatalf("bug entry = %+v bug=%q", e, p.Bug)
	}
}

func TestPlanLinearWorkspaceOnlyGroupIsMissing(t *testing.T) {
	found := []linear.SetupLabel{workspaceGroup("wm", "Mode"), workspaceGroup("wa", "Coding Agent")}
	p := projectconnection.PlanLinearLabels(planWanted(), found, nil, nil, "")
	for _, w := range planWanted() {
		e := planEntry(t, p, w.Name)
		switch w.Family {
		case agent.LabelFamilyMode, agent.LabelFamilyAgent:
			want := projectconnection.SeverityBlocking
			wantGroup := "wm"
			if w.Family == agent.LabelFamilyAgent {
				want, wantGroup = projectconnection.SeverityWarning, "wa"
			}
			if e.Status != projectconnection.StatusMissing || e.Severity != want || e.Create != nil {
				t.Fatalf("%s entry = %+v", w.Name, e)
			}
			if got := candidateIDs(e.Candidates); !slices.Equal(got, []string{wantGroup}) || e.Candidates[0].Scope != projectconnection.ScopeWorkspace {
				t.Fatalf("%s candidates = %+v", w.Name, e.Candidates)
			}
		case agent.LabelFamilyBug:
			if e.Status != projectconnection.StatusCreate {
				t.Fatalf("bug entry = %+v", e)
			}
		}
	}
	if len(p.Creates) != 1 || p.Creates[0].Name != "bug" || p.Creates[0].Form != projectconnection.FormFlat {
		t.Fatalf("creates = %+v", p.Creates)
	}
	if p.Applicable || len(p.Modes) != 0 || len(p.Agents) != 0 {
		t.Fatalf("plan = %+v", p)
	}
}

func TestPlanLinearCreatePlacement(t *testing.T) {
	type create struct {
		name, parentID, parentPlaceholder string
		group                             bool
		form                              projectconnection.LabelForm
	}
	modeGroup := "pending-create:group:Mode"
	agentGroup := "pending-create:group:Coding Agent"
	tests := []struct {
		name  string
		found []linear.SetupLabel
		want  []create
	}{
		{
			name:  "one team group gets children",
			found: []linear.SetupLabel{teamGroup("gm", "Mode"), teamGroup("ga", "coding agent"), workspaceGroup("wm", "Mode")},
			want: []create{
				{name: "Research", parentID: "gm", form: projectconnection.FormGrouped},
				{name: "Plan", parentID: "gm", form: projectconnection.FormGrouped},
				{name: "Implement", parentID: "gm", form: projectconnection.FormGrouped},
				{name: "Validate", parentID: "gm", form: projectconnection.FormGrouped},
				{name: "Spike", parentID: "gm", form: projectconnection.FormGrouped},
				{name: "Claude", parentID: "ga", form: projectconnection.FormGrouped},
				{name: "bug", form: projectconnection.FormFlat},
			},
		},
		{
			name: "empty Linear creates groups then children",
			want: []create{
				{name: "Mode", group: true, form: projectconnection.FormGrouped},
				{name: "Research", parentPlaceholder: modeGroup, form: projectconnection.FormGrouped},
				{name: "Plan", parentPlaceholder: modeGroup, form: projectconnection.FormGrouped},
				{name: "Implement", parentPlaceholder: modeGroup, form: projectconnection.FormGrouped},
				{name: "Validate", parentPlaceholder: modeGroup, form: projectconnection.FormGrouped},
				{name: "Spike", parentPlaceholder: modeGroup, form: projectconnection.FormGrouped},
				{name: "Coding Agent", group: true, form: projectconnection.FormGrouped},
				{name: "Claude", parentPlaceholder: agentGroup, form: projectconnection.FormGrouped},
				{name: "bug", form: projectconnection.FormFlat},
			},
		},
		{
			name:  "flat family creates flat",
			found: []linear.SetupLabel{teamFlat("fr", "Mode: Research"), workspaceFlat("fx", "coding agent: Codex")},
			want: []create{
				{name: "Mode: Plan", form: projectconnection.FormFlat},
				{name: "Mode: Implement", form: projectconnection.FormFlat},
				{name: "Mode: Validate", form: projectconnection.FormFlat},
				{name: "Mode: Spike", form: projectconnection.FormFlat},
				{name: "Coding Agent: Claude", form: projectconnection.FormFlat},
				{name: "bug", form: projectconnection.FormFlat},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := projectconnection.PlanLinearLabels(planWanted(), tc.found, nil, nil, "")
			var got []create
			for _, c := range p.Creates {
				got = append(got, create{c.Name, c.ParentID, c.ParentPlaceholder, c.IsGroup, c.Form})
				if !projectconnection.IsPlaceholder(c.Placeholder) {
					t.Fatalf("create %q has placeholder %q", c.Name, c.Placeholder)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("creates =\n%+v\nwant\n%+v", got, tc.want)
			}
			if !p.Applicable || !p.Changed {
				t.Fatalf("applicable=%v changed=%v", p.Applicable, p.Changed)
			}
			for _, e := range p.Entries {
				if e.Status == projectconnection.StatusCreate && e.Create == nil {
					t.Fatalf("entry %q would create without a create", e.Wanted.Name)
				}
			}
			for _, m := range planModes {
				if id := p.Modes[m]; !projectconnection.IsPlaceholder(id) && (tc.name != "flat family creates flat" || m != "research") {
					t.Fatalf("mode %s mapped to %q", m, id)
				}
			}
			if !projectconnection.IsPlaceholder(p.Agents["Claude"]) || !projectconnection.IsPlaceholder(p.Bug) {
				t.Fatalf("agents=%v bug=%q", p.Agents, p.Bug)
			}
		})
	}
}

func TestPlanLinearTwoTeamGroupsBlockCreate(t *testing.T) {
	found := []linear.SetupLabel{teamGroup("g1", "Mode"), teamGroup("g2", "mode")}
	p := projectconnection.PlanLinearLabels(planWanted(), found, nil, nil, "")
	e := planEntry(t, p, "Mode: Plan")
	if e.Status != projectconnection.StatusAmbiguous || e.Severity != projectconnection.SeverityBlocking || e.Create != nil {
		t.Fatalf("entry = %+v", e)
	}
	if got := candidateIDs(e.Candidates); !slices.Equal(got, []string{"g1", "g2"}) {
		t.Fatalf("candidates = %v", got)
	}
	for _, c := range p.Creates {
		if c.Family == agent.LabelFamilyMode {
			t.Fatalf("mode create planned under two groups: %+v", c)
		}
	}
	if p.Applicable {
		t.Fatal("plan applicable with two Mode groups")
	}
}

func TestPlanLinearReaderSeverity(t *testing.T) {
	tests := []struct {
		name     string
		wanted   string
		found    []linear.SetupLabel
		bug      string
		severity projectconnection.LabelSeverity
		stored   bool
	}{
		{"refused flat Mode is blocking", "Mode: Plan", []linear.SetupLabel{teamFlat("p", "mode: plan")}, "", projectconnection.SeverityBlocking, false},
		{"refused adopted Bug is warning", "bug", []linear.SetupLabel{workspaceFlat("B", "Bug")}, "", projectconnection.SeverityWarning, true},
		{"refused stored Bug is warning", "bug", []linear.SetupLabel{teamFlat("B", "Bug")}, "B", projectconnection.SeverityWarning, true},
		{"refused agent is warning", "Coding Agent: Claude", []linear.SetupLabel{teamFlat("c", "coding agent: claude")}, "", projectconnection.SeverityWarning, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := projectconnection.PlanLinearLabels(planWanted(), tc.found, nil, nil, tc.bug)
			e := planEntry(t, p, tc.wanted)
			if e.Severity != tc.severity || e.Reason == "" || e.Chosen == nil {
				t.Fatalf("entry = %+v", e)
			}
			id := tc.found[0].ID
			var got string
			switch tc.wanted {
			case "Mode: Plan":
				got = p.Modes["plan"]
			case "bug":
				got = p.Bug
			default:
				got = p.Agents["Claude"]
			}
			if (got == id) != tc.stored {
				t.Fatalf("stored ID = %q, want stored=%v", got, tc.stored)
			}
			if p.Applicable != (tc.severity != projectconnection.SeverityBlocking) {
				t.Fatalf("applicable = %v", p.Applicable)
			}
		})
	}
}

func TestPlanLinearStoredModeNotReaderChecked(t *testing.T) {
	found := []linear.SetupLabel{teamFlat("p", "mode: plan")}
	p := projectconnection.PlanLinearLabels(planWanted(), found, map[string]string{"plan": "p"}, nil, "")
	if e := planEntry(t, p, "Mode: Plan"); e.Status != projectconnection.StatusStored || e.Severity != "" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestPlanLinearVerifyIDs(t *testing.T) {
	found := fullyGrouped()
	modes := storedModes()
	delete(modes, "spike")
	p := projectconnection.PlanLinearLabels(planWanted(), found, modes, map[string]string{"Claude": "stale-agent"}, "stale-bug")
	if p.Agents["Claude"] != "stale-agent" || p.Bug != "stale-bug" {
		t.Fatalf("stale optional entries changed: agents=%v bug=%q", p.Agents, p.Bug)
	}
	for _, id := range []string{"stale-agent", "stale-bug"} {
		if slices.Contains(p.VerifyIDs, id) {
			t.Fatalf("stale optional %s in VerifyIDs %v", id, p.VerifyIDs)
		}
	}
	for _, m := range planModes {
		if !slices.Contains(p.VerifyIDs, "m-"+m) {
			t.Fatalf("mode %s missing from VerifyIDs %v", m, p.VerifyIDs)
		}
	}
	if p.Modes["spike"] != "m-spike" || len(p.VerifyIDs) != len(planModes) {
		t.Fatalf("modes=%v verify=%v", p.Modes, p.VerifyIDs)
	}
	if e := planEntry(t, p, "Mode: Spike"); e.Status != projectconnection.StatusAdopted {
		t.Fatalf("spike entry = %+v", e)
	}
	if !p.Applicable || !p.Changed {
		t.Fatalf("applicable=%v changed=%v", p.Applicable, p.Changed)
	}

	// Optional IDs added in this run are verified.
	p = projectconnection.PlanLinearLabels(planWanted(), found, storedModes(), nil, "")
	for _, id := range []string{"a-claude", "b"} {
		if !slices.Contains(p.VerifyIDs, id) {
			t.Fatalf("added optional %s missing from VerifyIDs %v", id, p.VerifyIDs)
		}
	}
}

func TestPlanLinearNoChange(t *testing.T) {
	p := projectconnection.PlanLinearLabels(planWanted(), fullyGrouped(), storedModes(), map[string]string{"Claude": "a-claude"}, "b")
	if p.Changed || len(p.Creates) != 0 || !p.Applicable {
		t.Fatalf("plan = %+v", p)
	}
	for _, e := range p.Entries {
		if e.Status != projectconnection.StatusStored || e.Severity != "" {
			t.Fatalf("entry = %+v", e)
		}
	}
	modesJSON := `{"implement":"m-implement","plan":"m-plan","research":"m-research","spike":"m-spike","validate":"m-validate"}`
	current := store.LinearConfiguration{ModeLabelsJSON: modesJSON, CodingAgentLabelsJSON: `{"Claude":"a-claude"}`, BugLabelID: "b"}
	cand, err := p.Candidate(current)
	if err != nil || cand != current {
		t.Fatalf("candidate = %+v, %v", cand, err)
	}
}

func TestLinearLabelPlanCandidate(t *testing.T) {
	stored := map[string]string{"Mode: Plan": "m-plan", "research": "m-research"}
	p := projectconnection.PlanLinearLabels(planWanted(), fullyGrouped(), stored, map[string]string{"Claude": "a-claude"}, "")
	cand, err := p.Candidate(store.LinearConfiguration{TeamID: "team"})
	if err != nil {
		t.Fatal(err)
	}
	modes, err := cand.ModeLabels()
	if err != nil {
		t.Fatal(err)
	}
	if modes["Mode: Plan"] != "m-plan" || modes["research"] != "m-research" || modes["implement"] != "m-implement" || len(modes) != 5 {
		t.Fatalf("modes = %v", modes)
	}
	if cand.BugLabelID != "b" || cand.TeamID != "team" {
		t.Fatalf("candidate = %+v", cand)
	}

	// Placeholders are replaced by created IDs; unknown ones stay.
	p = projectconnection.PlanLinearLabels(planWanted(), nil, map[string]string{"plan": "m-plan"}, nil, "")
	created := map[string]string{}
	for _, c := range p.Creates {
		if c.Name != "Spike" {
			created[c.Placeholder] = "real-" + c.Name
		}
	}
	cand, err = p.CandidateWith(store.LinearConfiguration{}, created)
	if err != nil {
		t.Fatal(err)
	}
	modes, _ = cand.ModeLabels()
	agents, _ := cand.CodingAgentLabels()
	if modes["plan"] != "m-plan" || modes["research"] != "real-Research" || !projectconnection.IsPlaceholder(modes["spike"]) || agents["Claude"] != "real-Claude" || cand.BugLabelID != "real-bug" {
		t.Fatalf("modes=%v agents=%v bug=%q", modes, agents, cand.BugLabelID)
	}
	if e := planEntry(t, p, "Mode: Plan"); e.Status != projectconnection.StatusStale {
		t.Fatalf("plan entry = %+v", e)
	}
}

func TestLinearLabelPlanCandidateValidation(t *testing.T) {
	// "x" is stored as the Claude agent label and is also the only "Mode: Plan" match.
	found := []linear.SetupLabel{teamGroup("gm", "Mode")}
	for _, m := range planModes {
		if m != "plan" {
			found = append(found, teamChild("m-"+m, agent.Mode(m).Label()[len("Mode: "):], "gm", "Mode"))
		}
	}
	found = append(found, teamFlat("x", "Mode: Plan"), teamFlat("b", "bug"))
	modes := storedModes()
	delete(modes, "plan")
	valid := validLinearVersion(store.LinearConfiguration{})
	if err := store.ValidateConnectionVersion(valid); err != nil {
		t.Fatalf("base version invalid: %v", err)
	}

	p := projectconnection.PlanLinearLabels(planWanted(), found, modes, map[string]string{"Claude": "x"}, "b")
	if e := planEntry(t, p, "Mode: Plan"); e.Status != projectconnection.StatusAdopted || p.Modes["plan"] != "x" {
		t.Fatalf("entry = %+v modes=%v", e, p.Modes)
	}
	cand, err := p.Candidate(valid.Linear)
	if err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.Linear = cand
	if err := store.ValidateConnectionVersion(bad); err == nil {
		t.Fatal("candidate mapping one ID twice passed validation")
	}

	// The same candidate without the clash validates.
	p = projectconnection.PlanLinearLabels(planWanted(), found, modes, nil, "b")
	if cand, err = p.Candidate(valid.Linear); err != nil {
		t.Fatal(err)
	}
	good := valid
	good.Linear = cand
	if err := store.ValidateConnectionVersion(good); err != nil {
		t.Fatalf("candidate = %+v: %v", cand, err)
	}
}
