package agentresult_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

// Gap tests for the graph Plan payload contract (story-001 T-006). They extend
// plan_graph_test.go and reuse its fixtures; nothing here restates a case it has.

func askEnvelope(plan string) []byte {
	return []byte(`{"result":{"outcome":"incomplete","reason":"Need input.","comment":"Asking.","questions":[{"id":"q1","audience":"business","text":"Which formats?"}],"payload":{"plan":` + plan + `}}}`)
}

func oneEntrySet() string {
	return `"storyset":{"entries":[{"id":"s1","name":"Export","intent":"Export data."}]}`
}

// AC-034: an ask carries shared questions and neither branch; a done carries exactly
// one branch and never a legacy planning field, whichever legacy field it is.
func TestGraphPlan_LegacyFieldsRefusedOnEveryOutcome(t *testing.T) {
	t.Parallel()
	base := "$.result.payload.plan"
	legacy := []struct{ name, member, path string }{
		{"summary", `"summary":"x"`, base + ".summary"},
		{"context", `"context":{"scope":"s","decisions":"d","assumptions":"a","questions":"q"}`, base + ".context"},
		{"stories", `"stories":[{"key":"story-001","title":"t","user_story":{"want":"w"}}]`, base + ".stories"},
	}
	for _, l := range legacy {
		t.Run("done/"+l.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(graphDone(`{`+l.member+`,`+oneEntrySet()+`}`), graphContext())
			if d.Valid || !hasDiagnostic(d, l.path, "legacy_field") {
				t.Fatalf("legacy %s beside a branch: %v", l.name, diagnosticCodes(d))
			}
		})
		t.Run("ask/"+l.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(askEnvelope(`{`+l.member+`}`), graphContext())
			if d.Valid || !hasDiagnostic(d, l.path, "legacy_field") {
				t.Fatalf("legacy %s on an ask: %v", l.name, diagnosticCodes(d))
			}
		})
	}
}

// AC-034: the envelope enforces the shared ask/done pairing for Plan, so an ask
// with no questions, a done that asks questions, and an ask that claims completion
// are each refused, and no branch is fabricated for a question.
func TestDecodePlanNode_AskDonePairing(t *testing.T) {
	t.Parallel()
	branch := storyRequest("")
	question := `"questions":[{"id":"q1","audience":"business","text":"Which?"}]`
	wrap := func(response, outcome, extra, plan string) []byte {
		return []byte(`{"version":1,"response":"` + response + `","result":{"outcome":"` + outcome + `","comment":"c",` + extra + `"payload":{"plan":` + plan + `}}}`)
	}
	cases := []struct {
		name  string
		input []byte
		valid bool
	}{
		{"ask with incomplete, reason, questions and no branch", wrap("ask", "incomplete", `"reason":"r",`+question+`,`, `{}`), true},
		{"ask without questions", wrap("ask", "incomplete", `"reason":"r",`, `{}`), false},
		{"ask claiming completion", wrap("ask", "completed", question+`,`, `{}`), false},
		{"ask carrying a story request", wrap("ask", "incomplete", `"reason":"r",`+question+`,`, branch), false},
		{"done with a story request", wrap("done", "completed", ``, branch), true},
		{"done with a story set", wrap("done", "completed", ``, `{`+oneEntrySet()+`}`), true},
		{"done with questions", wrap("done", "completed", question+`,`, branch), false},
		{"done incomplete", wrap("done", "incomplete", `"reason":"r",`, branch), false},
		{"done with neither branch", wrap("done", "completed", ``, `{}`), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, d := ar.DecodePlanNode(tt.input, graphContext())
			if d.Valid != tt.valid {
				t.Fatalf("valid=%v want %v: %v", d.Valid, tt.valid, diagnosticCodes(d))
			}
			if tt.valid && node.Response == "ask" && (node.Result.Payload.Plan.StoryRequest != nil || node.Result.Payload.Plan.StorySet != nil) {
				t.Fatal("a valid ask carries a fabricated draft")
			}
		})
	}
}

// AC-035: temporary ids are nonempty and unique. Each fault is reported at its own
// index, blank and whitespace-only ids are distinct from a malformed one, and a
// duplicate names the entry it repeats.
func TestGraphPlan_StorySetIdentityFaultsAreIndexed(t *testing.T) {
	t.Parallel()
	base := "$.result.payload.plan.storyset.entries"
	cases := []struct {
		name    string
		entries []string
		want    []string // "path code"
	}{
		{"whitespace id", []string{entry("  ", "One", "A.")}, []string{base + "[0].id blank"}},
		{"malformed id", []string{entry("1bad", "One", "A."), entry("has space", "Two", "B.")}, []string{base + "[0].id identity", base + "[1].id identity"}},
		{"duplicate keeps first and flags later", []string{entry("s1", "One", "A."), entry("s2", "Two", "B."), entry("s1", "Three", "C.")}, []string{base + "[2].id duplicate"}},
		{"every fault reported at once", []string{entry("", "One", "A."), entry("s2", "Two", "B."), entry("s2", "Three", "C."), entry("", "Four", "D.")}, []string{base + "[0].id blank", base + "[2].id duplicate", base + "[3].id blank"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(setJSON(tt.entries...), graphContext())
			if d.Valid {
				t.Fatal("identity fault accepted")
			}
			for _, want := range tt.want {
				path, code, _ := strings.Cut(want, " ")
				if !hasDiagnostic(d, path, code) {
					t.Errorf("missing %s: got %v", want, diagnosticCodes(d))
				}
			}
			if len(d.Diagnostics) != len(tt.want) {
				t.Errorf("want exactly %d diagnostics, got %v", len(tt.want), diagnosticCodes(d))
			}
		})
	}
	_, d := ar.Create(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B."), entry("s1", "Three", "C.")), graphContext())
	for _, f := range d.Diagnostics {
		if f.Code == "duplicate" && !strings.Contains(f.Message, "entries[0]") {
			t.Errorf("duplicate does not name the entry it repeats: %q", f.Message)
		}
	}
}

// AC-036: the set's shape limits. Names and intents are bounded in characters, not
// bytes; blank and multi-line values are refused; a maximum set with dependencies
// is valid, and every fault of a many-fault set is reported in one pass.
func TestGraphPlan_StorySetFieldLimits(t *testing.T) {
	t.Parallel()
	base := "$.result.payload.plan.storyset.entries"
	cases := []struct {
		name  string
		input []byte
		path  string
		code  string // empty means valid
	}{
		{"280 multibyte characters is a valid intent", setJSON(entry("s1", "One", strings.Repeat("é", 280))), "", ""},
		{"281 multibyte characters is too long", setJSON(entry("s1", "One", strings.Repeat("é", 281))), base + "[0].intent", "intent"},
		{"blank intent", setJSON(entry("s1", "One", "   ")), base + "[0].intent", "intent"},
		{"80 character name is valid", setJSON(entry("s1", strings.Repeat("n", 80), "A.")), "", ""},
		{"81 character name is too long", setJSON(entry("s1", strings.Repeat("n", 81), "A.")), base + "[0].name", "name"},
		{"blank name", setJSON(entry("s1", " ", "A.")), base + "[0].name", "name"},
		{"multi-line name", setJSON(entry("s1", "One\nTwo", "A.")), base + "[0].name", "name"},
		{"duplicate name ignoring case and padding", setJSON(entry("s1", "Export", "A."), entry("s2", " EXPORT ", "B.")), base + "[1].name", "duplicate"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(tt.input, graphContext())
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
	// The maximum set, chained by dependencies, is valid and keeps its order.
	var entries []string
	for i := range ar.MaxPlanStorySetEntries {
		if i == 0 {
			entries = append(entries, entry("s0", "Story 0", "Intent."))
			continue
		}
		entries = append(entries, entry(fmt.Sprintf("s%d", i), fmt.Sprintf("Story %d", i), "Intent.", fmt.Sprintf("s%d", i-1)))
	}
	r, d := ar.Create(setJSON(entries...), graphContext())
	if !d.Valid {
		t.Fatalf("12 chained entries refused: %v", diagnosticCodes(d))
	}
	for i, e := range r.Payload.Plan.StorySet.Entries {
		if e.ID != fmt.Sprintf("s%d", i) {
			t.Fatalf("entry %d reordered to %s", i, e.ID)
		}
	}
}

// AC-036: the over-limit refusal is a single bounded diagnostic that advises
// splitting; it does not report per-entry noise for an oversized set.
func TestGraphPlan_TooManyEntriesAdvisesSplit(t *testing.T) {
	t.Parallel()
	var entries []string
	for range 40 {
		entries = append(entries, entry("", "", "")) // every entry is also invalid
	}
	_, d := ar.Create(setJSON(entries...), graphContext())
	if d.Valid || len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "limit" || !strings.Contains(d.Diagnostics[0].Message, "split the parent") {
		t.Fatalf("oversized set: %v", diagnosticCodes(d))
	}
}

// AC-036: dependency resolution, cycles included, is complete once identity is sound.
func TestGraphPlan_StorySetDependencyShapes(t *testing.T) {
	t.Parallel()
	base := "$.result.payload.plan.storyset.entries"
	cases := []struct {
		name    string
		entries []string
		want    []string
		unwant  []string
	}{
		{"diamond is acyclic", []string{entry("a", "A", "x."), entry("b", "B", "x.", "a"), entry("c", "C", "x.", "a"), entry("d", "D", "x.", "b", "c")}, nil, nil},
		{"forward reference is fine", []string{entry("a", "A", "x.", "b"), entry("b", "B", "x.")}, nil, nil},
		{"three cycle", []string{entry("a", "A", "x.", "c"), entry("b", "B", "x.", "a"), entry("c", "C", "x.", "b")}, []string{base + "[0].depends_on cycle", base + "[1].depends_on cycle", base + "[2].depends_on cycle"}, nil},
		{"entry behind a cycle is flagged, an independent one is not", []string{entry("a", "A", "x.", "b"), entry("b", "B", "x.", "a"), entry("c", "C", "x.", "b"), entry("d", "D", "x.")}, []string{base + "[0].depends_on cycle", base + "[1].depends_on cycle", base + "[2].depends_on cycle"}, []string{base + "[3].depends_on cycle"}},
		{"unknown and self are reported together", []string{entry("a", "A", "x.", "a", "ghost")}, []string{base + "[0].depends_on[0] self_reference", base + "[0].depends_on[1] reference"}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(setJSON(tt.entries...), graphContext())
			if len(tt.want) == 0 {
				if !d.Valid {
					t.Fatalf("refused: %v", diagnosticCodes(d))
				}
				return
			}
			if d.Valid {
				t.Fatal("accepted")
			}
			for _, w := range tt.want {
				path, code, _ := strings.Cut(w, " ")
				if !hasDiagnostic(d, path, code) {
					t.Errorf("missing %s: got %v", w, diagnosticCodes(d))
				}
			}
			for _, w := range tt.unwant {
				path, code, _ := strings.Cut(w, " ")
				if hasDiagnostic(d, path, code) {
					t.Errorf("unexpected %s", w)
				}
			}
		})
	}
}

// TEST-7: diagnostics are deterministic. The same bytes and the same binding give
// the same diagnostics in the same order, as preflight and submit both require.
func TestGraphPlan_ValidationIsDeterministicForIdenticalBytes(t *testing.T) {
	t.Parallel()
	inputs := map[string][]byte{
		"story request": graphDone(storyRequest(`,"changed_sections":["intent"],"expected_revision":"` + strings.Repeat("a", 64) + `"`)),
		"story set":     setJSON(entry("a", "A", "x.", "c"), entry("b", "B", "x.", "a"), entry("c", "C", "x.", "b", "ghost"), entry("c", "C", "")),
		"ask":           askEnvelope(`{"stories":[]}`),
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, first := ar.Create(data, graphContext())
			for range 5 {
				if _, again := ar.Create(data, graphContext()); !reflect.DeepEqual(first, again) {
					t.Fatalf("identical bytes gave different diagnostics:\n%v\n%v", diagnosticCodes(first), diagnosticCodes(again))
				}
			}
		})
	}
}

// AC-037, AC-008: a changed factory binding refuses bytes that were valid under the
// old one, and the fresh bytes keep their outcome while the binding is unchanged.
func TestGraphPlan_ChangedBindingRefusesUnchangedBytes(t *testing.T) {
	t.Parallel()
	data := graphDone(storyRequest(""))
	if _, d := ar.Create(data, graphContext()); !d.Valid {
		t.Fatalf("baseline refused: %v", diagnosticCodes(d))
	}
	cases := []struct {
		name   string
		change func(*ar.PlanGraphBinding)
		path   string
		code   string
	}{
		{"story selection moved", func(b *ar.PlanGraphBinding) { b.Story = "story-002" }, "$.result.payload.plan.storyrequest.draft.key", "binding"},
		{"story acquired an issue draft", func(b *ar.PlanGraphBinding) { b.ExistingRevision = strings.Repeat("c", 64) }, "$.result.payload.plan.storyrequest.operation", "existing_story"},
		{"stage advanced past story", func(b *ar.PlanGraphBinding) { b.Stage = "technical" }, "$.result.payload.plan.storyrequest.stage", "story_request"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := graphContext()
			tt.change(c.PlanGraph)
			_, d := ar.Create(data, c)
			if d.Valid || !hasDiagnostic(d, tt.path, tt.code) {
				t.Fatalf("want %s %s, got %v", tt.path, tt.code, diagnosticCodes(d))
			}
		})
	}
	// A moved shared revision or candidate never refuses fresh bytes: the factory
	// binds afresh, so it is not something an agent must match.
	for name, change := range map[string]func(*ar.PlanGraphBinding){
		"shared revision": func(b *ar.PlanGraphBinding) { b.SharedRevision = "later" },
		"candidate":       func(b *ar.PlanGraphBinding) { b.Candidate = "plan-9-9" },
		"upstream":        func(b *ar.PlanGraphBinding) { b.Upstream = []string{"z"} },
	} {
		c := graphContext()
		change(c.PlanGraph)
		if _, d := ar.Create(data, c); !d.Valid {
			t.Errorf("fresh bytes refused after the factory rebound %s: %v", name, diagnosticCodes(d))
		}
	}
}

// AC-037: every factory value the retained result recorded is checked against the
// current binding, so a result recorded under an earlier one is stale. Upstream
// order is not significant.
func TestGraphPlan_RetainedResultStaleAgainstEachFactoryValue(t *testing.T) {
	t.Parallel()
	c := graphContext()
	created, d := ar.Create(graphDone(storyRequest("")), c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	snapshot, d := ar.Assemble(created, ar.Metadata{SchemaVersion: 1, Task: "t", RunID: 1, AttemptID: "a", Mode: "plan", ResultID: "r", ResultRevision: 1, WorkflowVersion: 2}, c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	for field, change := range map[string]func(*ar.PlanGraphBinding){
		"request_id":      func(b *ar.PlanGraphBinding) { b.RequestID = "plan-request-9-9" },
		"stage":           func(b *ar.PlanGraphBinding) { b.Stage = "technical" },
		"candidate":       func(b *ar.PlanGraphBinding) { b.Candidate = "plan-9-9" },
		"shared_revision": func(b *ar.PlanGraphBinding) { b.SharedRevision = "later" },
		"upstream":        func(b *ar.PlanGraphBinding) { b.Upstream = []string{"a", "c"} },
	} {
		moved := graphContext()
		change(moved.PlanGraph)
		d := ar.ValidateResult(snapshot.Result, moved)
		if d.Valid || !hasDiagnostic(d, "$.result.payload.plan.storyrequest."+field, "stale_binding") {
			t.Errorf("%s: stale retained result accepted: %v", field, diagnosticCodes(d))
		}
	}
	reordered := graphContext()
	reordered.PlanGraph.Upstream = []string{"b", "a"}
	if d := ar.ValidateResult(snapshot.Result, reordered); !d.Valid {
		t.Fatalf("upstream order made a retained result stale: %v", diagnosticCodes(d))
	}
}

// AC-006: retire and update need the story to have an issue draft, and update
// checks the expected revision the factory binds; retire is a valid graph branch.
func TestGraphPlan_OperationsFollowTheStoryBinding(t *testing.T) {
	t.Parallel()
	rev := strings.Repeat("a", 64)
	request := func(op, extra string) []byte {
		return graphDone(`{"storyrequest":{"operation":"` + op + `"` + extra + `,"draft":` + graphDraft + `}}`)
	}
	existing := graphContext()
	existing.PlanGraph.ExistingRevision = rev
	cases := []struct {
		name string
		data []byte
		c    ar.Context
		path string
		code string
	}{
		{"retire an existing story", request("retire", `,"expected_revision":"`+rev+`"`), existing, "", ""},
		{"retire with a stale revision", request("retire", `,"expected_revision":"`+strings.Repeat("b", 64)+`"`), existing, "$.result.payload.plan.storyrequest.expected_revision", "stale_revision"},
		{"retire a story with no draft", request("retire", `,"expected_revision":"`+rev+`"`), graphContext(), "$.result.payload.plan.storyrequest.operation", "missing_story"},
		{"update a story with no draft", request("update", `,"expected_revision":"`+rev+`"`), graphContext(), "$.result.payload.plan.storyrequest.operation", "missing_story"},
		{"create over an existing draft", request("create", ""), existing, "$.result.payload.plan.storyrequest.operation", "existing_story"},
		{"unknown operation", request("promote", ""), graphContext(), "$.result.payload.plan.storyrequest.operation", "story_request"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := ar.Create(tt.data, tt.c)
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

// AC-006: an edit reaches the graph branches only under the graph grant. A legacy
// Plan grant does not include them, and an edit that breaks the set is refused.
func TestGraphPlan_EditGrantsAndSetEdits(t *testing.T) {
	t.Parallel()
	c := graphContext()
	created, d := ar.Create(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B.", "s1")), c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	snapshot, d := ar.Assemble(created, ar.Metadata{SchemaVersion: 1, Task: "t", RunID: 1, AttemptID: "a", Mode: "plan", ResultID: "r", ResultRevision: 1, WorkflowVersion: 2}, c)
	if !d.Valid {
		t.Fatal(diagnosticCodes(d))
	}
	good := `{"entries":[{"id":"s1","name":"One","intent":"A."},{"id":"s2","name":"Two","intent":"B.","depends_on":["s1"]},{"id":"s3","name":"Three","intent":"C."}]}`
	cyclic := `{"entries":[{"id":"s1","name":"One","intent":"A.","depends_on":["s2"]},{"id":"s2","name":"Two","intent":"B.","depends_on":["s1"]}]}`
	edit := func(value string, grant []ar.Target) ar.UpdateDiagnostics {
		_, diag := ar.ApplyUpdate(snapshot, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "payload.plan.storyset", "", value)}}, c, grant)
		return diag
	}
	if diag := edit(good, ar.PlanGraphEditTargets()); !diag.Valid {
		t.Fatalf("set edit under the graph grant refused: %v", diagnosticCodes(diag.Diagnostics))
	}
	if diag := edit(good, ar.EditTargets("plan")); diag.Valid {
		t.Fatal("set edit accepted under the legacy Plan grant")
	}
	if diag := edit(cyclic, ar.PlanGraphEditTargets()); diag.Valid || !hasDiagnostic(diag.Diagnostics, "$.result.payload.plan.storyset.entries[0].depends_on", "cycle") {
		t.Fatalf("cyclic set edit accepted: %v", diagnosticCodes(diag.Diagnostics))
	}
	if slices.ContainsFunc(ar.EditTargets("plan"), func(t ar.Target) bool {
		return t.Field == "payload.plan.storyrequest" || t.Field == "payload.plan.storyset"
	}) {
		t.Fatal("the legacy Plan grant names a graph branch")
	}
}

// AC-037: the factory-owned members of a retained result are copied, never shared,
// so binding one attempt cannot change another's.
func TestPlanGraphBinding_CloneAndEffectShareNoSlices(t *testing.T) {
	t.Parallel()
	var nilBinding *ar.PlanGraphBinding
	if nilBinding.Clone() != nil {
		t.Fatal("clone of nil is not nil")
	}
	b := &ar.PlanGraphBinding{Story: "s", Upstream: []string{"a", "b"}}
	c := b.Clone()
	c.Upstream[0] = "changed"
	if b.Upstream[0] != "a" {
		t.Fatal("clone shares the upstream slice")
	}
	q := ar.PlanStoryRequest{Operation: "create", Upstream: []string{"a"}, ChangedSections: []string{"intent"}, Replacements: []string{"r"}}
	e := q.Effect()
	e.Upstream[0], e.ChangedSections[0], e.Replacements[0] = "x", "x", "x"
	if q.Upstream[0] != "a" || q.ChangedSections[0] != "intent" || q.Replacements[0] != "r" {
		t.Fatal("effect shares slices with the retained request")
	}
	if got := (ar.PlanStoryRequest{}).Effect().Upstream; got == nil || len(got) != 0 {
		t.Fatalf("effect upstream must be an empty, non-nil list, got %#v", got)
	}
}

// The terminal comparison covers every application branch, not only the story
// request: a changed set entry, question or branch is a different result.
func TestSameResult_DetectsChangesInEveryBranch(t *testing.T) {
	t.Parallel()
	c := graphContext()
	build := func(data []byte) ar.Result {
		r, d := ar.Create(data, c)
		if !d.Valid {
			t.Fatalf("fixture invalid: %v", diagnosticCodes(d))
		}
		return r
	}
	set := build(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B.", "s1")))
	sameSet := build(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B.", "s1")))
	if !ar.SameResult(set, sameSet) {
		t.Fatal("equal sets differ")
	}
	for name, other := range map[string]ar.Result{
		"changed intent": build(setJSON(entry("s1", "One", "Changed."), entry("s2", "Two", "B.", "s1"))),
		"changed order":  build(setJSON(entry("s2", "Two", "B."), entry("s1", "One", "A."))),
		"changed deps":   build(setJSON(entry("s1", "One", "A."), entry("s2", "Two", "B."))),
		"story request":  build(graphDone(storyRequest(""))),
		"ask":            build(askEnvelope(`{}`)),
	} {
		if ar.SameResult(set, other) {
			t.Errorf("%s was accepted as the same result", name)
		}
	}
}

// Preflight and submit share Create, so a graph Plan ask without a shared question
// is refused there by name, agreeing with DecodePlanNode; a legacy Plan is unchanged.
func TestGraphPlan_UnfinishedResultWithoutQuestionsRefused(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"incomplete", "blocked"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			data := []byte(`{"result":{"outcome":"` + outcome + `","comment":"c","reason":"r","payload":{"plan":{}}}}`)
			_, d := ar.Create(data, graphContext())
			if d.Valid || !hasDiagnostic(d, "$.result.questions", "questions") {
				t.Fatalf("question-less %s ask: valid=%v %v", outcome, d.Valid, diagnosticCodes(d))
			}
		})
	}
	t.Run("with a question", func(t *testing.T) {
		t.Parallel()
		if _, d := ar.Create(askEnvelope(`{}`), graphContext()); !d.Valid {
			t.Fatalf("ask with a question refused: %v", diagnosticCodes(d))
		}
	})
}
