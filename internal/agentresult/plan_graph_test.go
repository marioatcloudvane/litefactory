package agentresult_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

const graphDraft = `{"key":"story-001","name":"Add export","user_story":{"actor":"owner","want":"export data","benefit":"share it","notes":[]},"problem":"No export exists.","in_scope":["export"],"not_in_scope":[],"dependencies":{"story_keys":[],"issue_refs":[]},"frontend":false}`

func graphContext() ar.Context {
	return ar.Context{SchemaVersion: 1, Mode: "plan", WorkflowVersion: 2, QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}, IssueRefs: []string{}}, PlanGraph: &ar.PlanGraphBinding{Story: "story-001", Stage: "story", Candidate: "plan-1-2", RequestID: "plan-request-1-2", Upstream: []string{"a", "b"}, SharedRevision: "initial"}}
}

func graphDone(plan string) []byte {
	return []byte(`{"result":{"outcome":"completed","comment":"Proposed.","payload":{"plan":` + plan + `}}}`)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func storyRequest(extra string) string {
	return `{"storyrequest":{"operation":"create","draft":` + graphDraft + extra + `}}`
}

func diagnosticCodes(d ar.Diagnostics) []string {
	var out []string
	for _, f := range d.Diagnostics {
		out = append(out, f.Path+" "+f.Code)
	}
	return out
}

func hasDiagnostic(d ar.Diagnostics, path, code string) bool {
	return slices.ContainsFunc(d.Diagnostics, func(f ar.Diagnostic) bool { return f.Path == path && f.Code == code })
}

func TestGraphPlan_DoneBranches(t *testing.T) {
	base := "$.result.payload.plan"
	set := `{"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`
	cases := []struct {
		name    string
		plan    string
		outcome string
		path    string
		code    string
	}{
		{name: "story request", plan: storyRequest("")},
		{name: "story set", plan: set},
		{name: "neither", plan: `{}`, path: base, code: "branch"},
		{name: "both", plan: `{"storyrequest":{"operation":"create","draft":` + graphDraft + `},"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`, path: base, code: "branch"},
		{name: "legacy summary beside branch", plan: `{"summary":"x","storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`, path: base + ".summary", code: "legacy_field"},
		{name: "legacy stories", plan: `{"stories":[{"key":"story-001","title":"t","user_story":{"want":"w"}}],"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`, path: base + ".stories", code: "legacy_field"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, d := ar.Create(graphDone(tt.plan), graphContext())
			if tt.code == "" {
				if !d.Valid {
					t.Fatalf("refused: %v", diagnosticCodes(d))
				}
				return
			}
			if d.Valid || !hasDiagnostic(d, tt.path, tt.code) {
				t.Fatalf("want %s %s, got %v", tt.path, tt.code, diagnosticCodes(d))
			}
		})
	}
}

func TestGraphPlan_AskCarriesQuestionsAndNoBranch(t *testing.T) {
	ask := func(plan string) []byte {
		return []byte(`{"result":{"outcome":"incomplete","reason":"Need input.","comment":"Asking.","questions":[{"id":"q1","audience":"business","text":"Which formats?"}],"payload":{"plan":` + plan + `}}}`)
	}
	if _, d := ar.Create(ask(`{}`), graphContext()); !d.Valid {
		t.Fatalf("ask without draft refused: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(ask(storyRequest("")), graphContext()); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest", "branch") {
		t.Fatalf("ask with branch accepted: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(ask(`{"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`), graphContext()); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyset", "branch") {
		t.Fatalf("ask with set accepted: %v", diagnosticCodes(d))
	}
}

func TestGraphPlan_LegacyContractUnchanged(t *testing.T) {
	legacy := graphContext()
	legacy.PlanGraph = nil
	for _, plan := range []string{storyRequest(""), `{"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}}`} {
		if _, d := ar.Create(graphDone(plan), legacy); d.Valid || !slices.ContainsFunc(d.Diagnostics, func(f ar.Diagnostic) bool { return f.Code == "scope" }) {
			t.Fatalf("legacy Plan accepted a graph branch: %v", diagnosticCodes(d))
		}
	}
	r, c := createFixture("plan", "completed")
	if _, d := ar.Create(mustJSON(t, ar.CreateInput{Result: r}), c); !d.Valid {
		t.Fatalf("legacy payload refused: %v", diagnosticCodes(d))
	}
}

func TestGraphPlan_FactoryOwnedFieldsRefusedByName(t *testing.T) {
	for _, field := range []string{`"request_id":"x"`, `"stage":"story"`, `"candidate":"plan-1-2"`, `"upstream":[]`, `"shared_revision":"initial"`} {
		name, _, _ := strings.Cut(strings.TrimPrefix(field, `"`), `"`)
		t.Run(name, func(t *testing.T) {
			_, d := ar.Create(graphDone(storyRequest(","+field)), graphContext())
			path := "$.result.payload.plan.storyrequest." + name
			if d.Valid || !hasDiagnostic(d, path, "factory_owned") {
				t.Fatalf("want factory_owned at %s, got %v", path, diagnosticCodes(d))
			}
		})
	}
}

func TestGraphPlan_AssembleRecordsFactoryValues(t *testing.T) {
	c := graphContext()
	r, d := ar.Create(graphDone(storyRequest("")), c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	snapshot, d := ar.Assemble(r, ar.Metadata{SchemaVersion: 1, Task: "t", RunID: 1, AttemptID: "a", Mode: "plan", ResultID: "r", ResultRevision: 1, WorkflowVersion: 2}, c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	q := snapshot.Result.Payload.Plan.StoryRequest
	if q.Stage != "story" || q.Candidate != "plan-1-2" || q.RequestID != "plan-request-1-2" || q.SharedRevision != "initial" || !slices.Equal(q.Upstream, []string{"a", "b"}) {
		t.Fatalf("factory values not recorded: %+v", q)
	}
	if r.Payload.Plan.StoryRequest.Stage != "" {
		t.Fatal("assembly mutated the authored result")
	}
	// The retained result revalidates against the same binding, and against a
	// changed binding it is stale.
	if d := ar.ValidateResult(snapshot.Result, c); !d.Valid {
		t.Fatalf("retained result refused: %v", diagnosticCodes(d))
	}
	moved := graphContext()
	moved.PlanGraph.SharedRevision = "changed"
	if d := ar.ValidateResult(snapshot.Result, moved); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest.shared_revision", "stale_binding") {
		t.Fatalf("stale retained result accepted: %v", diagnosticCodes(d))
	}
	// An edit is judged without the recorded binding and rebinds the current one.
	moved.PlanGraph.SharedRevision = "later"
	_, diag := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "comment", "", `"Reworded."`)}}, moved, ar.PlanGraphEditTargets())
	if diag.Valid {
		t.Fatal("comment edit granted without its target")
	}
	next, diag := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "comment", "", `"Reworded."`)}}, moved, ar.EditTargets("plan"))
	if !diag.Valid {
		t.Fatalf("edit refused: %v", diagnosticCodes(diag.Diagnostics))
	}
	if got := next.Snapshot.Result.Payload.Plan.StoryRequest.SharedRevision; got != "later" {
		t.Fatalf("edit kept a stale binding: %q", got)
	}
	// Setting a factory-owned member through an edit is refused.
	_, diag = ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "payload.plan.storyrequest", "", `{"stage":"technical"}`)}}, c, ar.PlanGraphEditTargets())
	if diag.Valid || !hasDiagnostic(diag.Diagnostics, "$.result.payload.plan.storyrequest.stage", "factory_owned") {
		t.Fatalf("edit supplied a factory-owned member: %v", diagnosticCodes(diag.Diagnostics))
	}
}

func TestGraphPlan_CreateRefusalsAreSeparatelyNamed(t *testing.T) {
	base := "$.result.payload.plan.storyrequest."
	create := func(extra string, c ar.Context) ar.Diagnostics {
		_, d := ar.Create(graphDone(storyRequest(extra)), c)
		return d
	}
	message := func(d ar.Diagnostics, path string) string {
		for _, f := range d.Diagnostics {
			if f.Path == path {
				return f.Message
			}
		}
		return ""
	}
	d := create(`,"changed_sections":["intent"]`, graphContext())
	if m := message(d, base+"changed_sections"); !strings.Contains(m, "changed_sections") || !strings.Contains(m, "remove") || strings.Contains(m, "initial story candidate") {
		t.Fatalf("section list message: %q (%v)", m, diagnosticCodes(d))
	}
	d = create(`,"expected_revision":"`+strings.Repeat("a", 64)+`"`, graphContext())
	if m := message(d, base+"expected_revision"); !strings.Contains(m, "expected_revision") || !strings.Contains(m, "remove") {
		t.Fatalf("expected revision message: %q (%v)", m, diagnosticCodes(d))
	}
	late := graphContext()
	late.PlanGraph.Stage = "technical"
	d = create("", late)
	if m := message(d, base+"stage"); !strings.Contains(m, "story stage") || !strings.Contains(m, "update") {
		t.Fatalf("stage message: %q (%v)", m, diagnosticCodes(d))
	}
	d = create(`,"changed_sections":["conversation"],"expected_revision":"`+strings.Repeat("a", 64)+`"`, late)
	seen := 0
	for _, f := range d.Diagnostics {
		if f.Code == "story_request" {
			seen++
		}
	}
	if seen != 3 {
		t.Fatalf("want three separate story_request messages, got %d: %v", seen, diagnosticCodes(d))
	}
}

func TestGraphPlan_UpdateWithSectionsIsValid(t *testing.T) {
	c := graphContext()
	c.PlanGraph.ExistingRevision = strings.Repeat("a", 64)
	update := func(sections string) ar.Diagnostics {
		_, d := ar.Create(graphDone(`{"storyrequest":{"operation":"update","expected_revision":"`+strings.Repeat("a", 64)+`","changed_sections":`+sections+`,"draft":`+graphDraft+`}}`), c)
		return d
	}
	if d := update(`["intent","dependencies","conversation"]`); !d.Valid {
		t.Fatalf("update refused: %v", diagnosticCodes(d))
	}
	if d := update(`["criteria"]`); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest.changed_sections", "section") {
		t.Fatalf("story stage authored criteria: %v", diagnosticCodes(d))
	}
	stale := graphContext()
	stale.PlanGraph.ExistingRevision = strings.Repeat("b", 64)
	if _, d := ar.Create(graphDone(`{"storyrequest":{"operation":"update","expected_revision":"`+strings.Repeat("a", 64)+`","draft":`+graphDraft+`}}`), stale); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest.expected_revision", "stale_revision") {
		t.Fatalf("stale expected revision accepted: %v", diagnosticCodes(d))
	}
	other := strings.Replace(graphDraft, `"key":"story-001"`, `"key":"story-002"`, 1)
	if _, d := ar.Create(graphDone(`{"storyrequest":{"operation":"create","draft":`+other+`}}`), graphContext()); d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest.draft.key", "binding") {
		t.Fatalf("foreign story accepted: %v", diagnosticCodes(d))
	}
}

func setJSON(entries ...string) []byte {
	return graphDone(`{"storyset":{"entries":[` + strings.Join(entries, ",") + `]}}`)
}

func entry(id, name, intent string, deps ...string) string {
	list := ""
	if len(deps) > 0 {
		list = `,"depends_on":["` + strings.Join(deps, `","`) + `"]`
	}
	return fmt.Sprintf(`{"id":%q,"name":%q,"intent":%q%s}`, id, name, intent, list)
}

func TestGraphPlan_StorySetBounds(t *testing.T) {
	base := "$.result.payload.plan.storyset.entries"
	many := func(n int) []byte {
		var entries []string
		for i := range n {
			entries = append(entries, entry(fmt.Sprintf("s%d", i), fmt.Sprintf("Story %d", i), "Intent."))
		}
		return setJSON(entries...)
	}
	for _, n := range []int{1, 12} {
		if _, d := ar.Create(many(n), graphContext()); !d.Valid {
			t.Fatalf("%d entries refused: %v", n, diagnosticCodes(d))
		}
	}
	_, d := ar.Create(many(13), graphContext())
	if d.Valid || !hasDiagnostic(d, base, "limit") || !strings.Contains(d.Diagnostics[0].Message, "split the parent") {
		t.Fatalf("13 entries: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(setJSON(), graphContext()); d.Valid || !hasDiagnostic(d, base, "limit") {
		t.Fatalf("empty set accepted: %v", diagnosticCodes(d))
	}
	long := strings.Repeat("x", 281)
	if _, d := ar.Create(setJSON(entry("s1", "One", long)), graphContext()); d.Valid || !hasDiagnostic(d, base+"[0].intent", "intent") {
		t.Fatalf("long intent accepted: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(setJSON(entry("s1", "One", strings.Repeat("y", 280))), graphContext()); !d.Valid {
		t.Fatalf("280 character intent refused: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(setJSON(entry("s1", "One", "Two\nlines")), graphContext()); d.Valid || !hasDiagnostic(d, base+"[0].intent", "intent") {
		t.Fatalf("multi-line intent accepted: %v", diagnosticCodes(d))
	}
	if _, d := ar.Create(setJSON(entry("s1", "Same", "A."), entry("s2", "same", "B.")), graphContext()); d.Valid || !hasDiagnostic(d, base+"[1].name", "duplicate") {
		t.Fatalf("duplicate name accepted: %v", diagnosticCodes(d))
	}
}

func TestGraphPlan_StorySetIdentityRefusedBeforeDependencies(t *testing.T) {
	base := "$.result.payload.plan.storyset.entries"
	_, d := ar.Create(setJSON(entry("", "One", "A.", "ghost"), entry("s2", "Two", "B."), entry("s2", "Three", "C.", "ghost")), graphContext())
	if d.Valid || !hasDiagnostic(d, base+"[0].id", "blank") || !hasDiagnostic(d, base+"[2].id", "duplicate") {
		t.Fatalf("identity faults: %v", diagnosticCodes(d))
	}
	for _, f := range d.Diagnostics {
		if f.Code == "reference" || f.Code == "cycle" || f.Code == "self_reference" {
			t.Fatalf("dependency resolved before identity was settled: %v", diagnosticCodes(d))
		}
	}
}

func TestGraphPlan_StorySetDependencies(t *testing.T) {
	base := "$.result.payload.plan.storyset.entries"
	if _, d := ar.Create(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B.", "s1")), graphContext()); !d.Valid {
		t.Fatalf("valid dependency refused: %v", diagnosticCodes(d))
	}
	cases := []struct {
		name    string
		entries []string
		path    string
		code    string
	}{
		{"unknown", []string{entry("s1", "One", "A.", "ghost")}, base + "[0].depends_on[0]", "reference"},
		{"self", []string{entry("s1", "One", "A.", "s1")}, base + "[0].depends_on[0]", "self_reference"},
		{"cycle", []string{entry("s1", "One", "A.", "s2"), entry("s2", "Two", "B.", "s1")}, base + "[0].depends_on", "cycle"},
		{"duplicate dependency", []string{entry("s1", "One", "A."), entry("s2", "Two", "B.", "s1", "s1")}, base + "[1].depends_on[1]", "duplicate"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, d := ar.Create(setJSON(tt.entries...), graphContext()); d.Valid || !hasDiagnostic(d, tt.path, tt.code) {
				t.Fatalf("want %s %s, got %v", tt.path, tt.code, diagnosticCodes(d))
			}
		})
	}
	// List order is preserved through assembly.
	r, d := ar.Create(setJSON(entry("b", "Bee", "B."), entry("a", "Ay", "A.")), graphContext())
	if !d.Valid || r.Payload.Plan.StorySet.Entries[0].ID != "b" || r.Payload.Plan.StorySet.Entries[1].ID != "a" {
		t.Fatalf("order not preserved: %v", diagnosticCodes(d))
	}
}

func TestDecodePlanNode_AskOrDoneNeverApproved(t *testing.T) {
	c := graphContext()
	var wrapped struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(graphDone(storyRequest("")), &wrapped); err != nil {
		t.Fatal(err)
	}
	done := `{"version":1,"response":"done","result":` + string(wrapped.Result) + `}`
	if _, d := ar.DecodePlanNode([]byte(done), c); !d.Valid {
		t.Fatalf("done refused: %v", diagnosticCodes(d))
	}
	approved := strings.Replace(done, `"done"`, `"approved"`, 1)
	if _, d := ar.DecodePlanNode([]byte(approved), c); d.Valid || !hasDiagnostic(d, "$.response", "enum") {
		t.Fatalf("approved accepted: %v", diagnosticCodes(d))
	}
	review := strings.Replace(done, `"done"`, `"review_ready"`, 1)
	if _, d := ar.DecodePlanNode([]byte(review), c); d.Valid {
		t.Fatal("private review_ready response accepted")
	}
	wrongMode := c
	wrongMode.Mode = "research"
	if _, d := ar.DecodePlanNode([]byte(done), wrongMode); d.Valid {
		t.Fatal("plan node accepted outside Plan mode")
	}
	ask := `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Need input.","comment":"Asking.","questions":[{"id":"q1","audience":"business","text":"Which?"}],"payload":{"plan":{}}}}`
	if _, d := ar.DecodePlanNode([]byte(ask), c); !d.Valid {
		t.Fatalf("ask refused: %v", diagnosticCodes(d))
	}
	// Research keeps its own vocabulary.
	research := ar.Context{SchemaVersion: 1, Mode: "research", WorkflowVersion: 2}
	if _, d := ar.DecodeResearchNode([]byte(done), research); d.Valid {
		t.Fatal("research accepted a plan payload")
	}
}

// The terminal envelope is compared against the retained result and never used to
// build one: the factory-bound members an agent cannot supply do not count, and any
// authored difference does.
func TestSameResult_ComparesAuthoredContentOnly(t *testing.T) {
	c := graphContext()
	created, d := ar.Create(graphDone(storyRequest("")), c)
	if !d.Valid {
		t.Fatalf("fixture invalid: %v", diagnosticCodes(d))
	}
	snapshot, d := ar.Assemble(created, ar.Metadata{SchemaVersion: 1, Task: "acme/api#1", RunID: 1, AttemptID: "a", Mode: "plan", ResultID: "result-1", ResultRevision: 1}, c)
	if !d.Valid {
		t.Fatalf("assemble refused: %v", diagnosticCodes(d))
	}
	if snapshot.Result.Payload.Plan.StoryRequest.Candidate == "" {
		t.Fatal("the retained result records no factory binding")
	}
	if !ar.SameResult(snapshot.Result, created) {
		t.Fatal("the factory's binding made the same result differ")
	}
	other := created
	other.Comment = "A different comment."
	if ar.SameResult(snapshot.Result, other) {
		t.Fatal("a different authored result was accepted as the same")
	}
	effect := snapshot.Result.Payload.Plan.StoryRequest.Effect()
	if effect.Candidate != "plan-1-2" || effect.RequestID != "plan-request-1-2" || effect.Stage != "story" || effect.SharedRevision != "initial" || !slices.Equal(effect.Upstream, []string{"a", "b"}) || effect.Draft.Key != "story-001" {
		t.Fatalf("effect request is not the retained, bound request: %+v", effect)
	}
}

func TestGraphPlan_RenderedSizeCheckedAtPreflight(t *testing.T) {
	if _, d := ar.Create(graphDone(storyRequest("")), graphContext()); hasDiagnostic(d, "$.result.payload.plan.storyrequest.draft", "draft_size") {
		t.Fatalf("small draft flagged: %v", diagnosticCodes(d))
	}
	big := strings.Replace(graphDraft, `"problem":"No export exists."`, `"problem":"`+strings.Repeat("x", 40000)+`"`, 1)
	_, d := ar.Create(graphDone(`{"storyrequest":{"operation":"create","draft":`+big+`}}`), graphContext())
	if d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest.draft", "draft_size") {
		t.Fatalf("draft too large to render passed preflight: %v", diagnosticCodes(d))
	}
}
