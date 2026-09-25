package agentresult_test

import (
	"embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func updateSnapshot(mode, outcome string) (ar.Snapshot, ar.Context) {
	r, c := createFixture(mode, outcome)
	return ar.Snapshot{Meta: ar.Metadata{SchemaVersion: 1, Mode: mode, ResultID: "result-1", ResultRevision: 1, RunID: 1, AttemptID: "attempt-1", Task: "task-1"}, Result: r}, c
}
func operation(op, field, id, value string) ar.Operation {
	return ar.Operation{Op: op, Target: ar.Target{Field: field, ID: id}, Value: json.RawMessage(value)}
}
func update(t *testing.T, s ar.Snapshot, h ar.RetiredIDs, c ar.Context, ops ...ar.Operation) ar.PreparedUpdate {
	t.Helper()
	got, d := ar.ApplyUpdate(s, h, ar.UpdateInput{ExpectedRevision: s.Meta.ResultRevision, Operations: ops}, c, ar.EditTargets(c.Mode))
	if !d.Valid {
		t.Fatalf("update rejected: %+v", d)
	}
	return got
}
func TestUpdate_AllModeOutcomeRoundTrips(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, outcome := range []string{"completed", "incomplete", "blocked"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				t.Parallel()
				s, c := updateSnapshot(mode, outcome)
				before := authored(t, s.Result)
				got := update(t, s, nil, c, operation("set", "comment", "", `"Amended commentary"`))
				if got.Snapshot.Meta.ResultRevision != 2 || got.Snapshot.Result.Comment != "Amended commentary" {
					t.Fatal("revision/comment not updated")
				}
				got.Snapshot.Result.Comment = s.Result.Comment
				if !reflect.DeepEqual(authored(t, got.Snapshot.Result), before) {
					t.Fatal("untouched content changed")
				}
				if !reflect.DeepEqual(authored(t, s.Result), before) {
					t.Fatal("baseline changed")
				}
			})
		}
	}
}
func TestUpdate_OperationsAndRetirement(t *testing.T) {
	s, c := updateSnapshot("research", "incomplete")
	got := update(t, s, nil, c,
		operation("add_item", "payload.research.findings", "F-2", `{"id":"F-2","text":"Second","evidence":[{"path":"file.go"}]}`),
		operation("update_item", "payload.research.findings", "F-2", `{"text":"Amended"}`),
		ar.Operation{Op: "reorder", Target: ar.Target{Field: "payload.research.findings"}, Order: []string{"F-2", "F-1"}},
		operation("set", "recommendation", "", `{"text":"Continue","action":"action.moveto.planmode"}`),
		operation("set", "recommendation", "", `{"text":"Updated recommendation"}`))
	f := got.Snapshot.Result.Payload.Research.Findings
	if len(f) != 2 || f[0].ID != "F-2" || f[0].Text != "Amended" || len(f[0].Evidence) != 1 || got.Snapshot.Result.Recommendation.Action != "action.moveto.planmode" {
		t.Fatal("merge/reorder lost untouched data")
	}
	got = update(t, got.Snapshot, got.Retired, c, operation("remove_item", "payload.research.findings", "F-1", ""), operation("remove", "recommendation", "", ""))
	if got.Snapshot.Result.Recommendation != nil || len(got.Snapshot.Result.Payload.Research.Findings) != 1 || len(got.Retired) == 0 {
		t.Fatal("removal was not retained")
	}
	for _, op := range []ar.Operation{operation("add_item", "payload.research.findings", "F-1", `{"id":"F-1","text":"Reuse"}`), operation("set", "payload.research.findings", "", `[{"id":"F-1","text":"Reuse"}]`)} {
		_, d := ar.ApplyUpdate(got.Snapshot, got.Retired, ar.UpdateInput{ExpectedRevision: 3, Operations: []ar.Operation{op}}, c, ar.EditTargets(c.Mode))
		if d.Valid {
			t.Fatal("retired ID reuse accepted")
		}
	}
	cleared := update(t, got.Snapshot, got.Retired, c, operation("set", "payload.research.findings", "", `[]`))
	if len(cleared.Snapshot.Result.Payload.Research.Findings) != 0 {
		t.Fatal("explicit empty collection did not clear")
	}
	update(t, cleared.Snapshot, cleared.Retired, c, ar.Operation{Op: "reorder", Target: ar.Target{Field: "payload.research.findings"}, Order: []string{}})
}
func TestUpdate_RejectedOperationsAreAtomic(t *testing.T) {
	cases := []struct {
		name string
		op   ar.Operation
	}{
		{"unknown op", operation("replace", "comment", "", `"change"`)},
		{"unknown member", operation("set", "recommendation", "", `{"text":"ok","approval":true}`)},
		{"wrong type", operation("set", "comment", "", `4`)},
		{"null", operation("set", "recommendation", "", `{"text":null}`)},
		{"pointer", operation("set", "/payload/research/request", "", `"change"`)},
		{"index", operation("set", "payload.research.findings.0.text", "", `"change"`)},
		{"meta", operation("set", "meta.run_id", "", `3`)},
		{"wrong mode", operation("set", "payload.plan.summary", "", `"change"`)},
		{"remove required", operation("remove", "comment", "", "")},
		{"remove absent", operation("remove", "recommendation", "", "")},
		{"identity change", operation("update_item", "payload.research.findings", "F-1", `{"id":"F-2"}`)},
		{"identity mismatch", operation("add_item", "payload.research.findings", "F-3", `{"id":"F-2","text":"new"}`)},
		{"missing item", operation("update_item", "payload.research.findings", "F-9", `{"text":"missing"}`)},
		{"duplicate item", operation("add_item", "payload.research.findings", "F-1", `{"id":"F-1","text":"new"}`)},
		{"duplicate array", operation("set", "payload.research.findings", "", `[{"id":"F-1","text":"a"},{"id":"F-1","text":"b"}]`)},
		{"incomplete reorder", ar.Operation{Op: "reorder", Target: ar.Target{Field: "payload.research.findings"}, Order: []string{}}},
		{"unknown reorder", ar.Operation{Op: "reorder", Target: ar.Target{Field: "payload.research.findings"}, Order: []string{"F-9"}}},
		{"empty action", operation("set", "recommendation", "", `{"text":"next","action":""}`)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s, c := updateSnapshot("research", "incomplete")
			before := authored(t, s.Result)
			h := ar.RetiredIDs{"prior": []string{"old"}}
			got, d := ar.ApplyUpdate(s, h, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("set", "comment", "", `"private change"`), tt.op}}, c, ar.EditTargets(c.Mode))
			if d.Valid || got.Snapshot.Meta.ResultRevision != 0 || got.Retired != nil {
				t.Fatal("invalid operation returned candidate")
			}
			if !reflect.DeepEqual(authored(t, s.Result), before) || !reflect.DeepEqual(h, ar.RetiredIDs{"prior": []string{"old"}}) {
				t.Fatal("failed update mutated baseline/history")
			}
		})
	}
}
func TestUpdate_PlanEnrichmentGraphsAndRetiredChildren(t *testing.T) {
	s, c := updateSnapshot("plan", "incomplete")
	s.Result.Payload.Plan.Stories = append(s.Result.Payload.Plan.Stories, ar.PlanStory{Key: "US-002", Title: "Untouched", UserStory: ar.UserStory{Want: "remain intact"}})
	criterion := operation("add_item", "payload.plan.stories.acceptance_criteria", "AC-001", `{"id":"AC-001","text":"Validate"}`)
	criterion.Target.Story = "US-001"
	first := update(t, s, nil, c, criterion)
	architecture := operation("set", "payload.plan.stories.architecture", "", `{"summary":"Pure validation","components":[],"interfaces":[],"constraints":[],"decisions":[]}`)
	architecture.Target.Story = "US-001"
	next := update(t, first.Snapshot, first.Retired, c, architecture)
	if !reflect.DeepEqual(next.Snapshot.Result.Payload.Plan.Stories[1], s.Result.Payload.Plan.Stories[1]) || next.Snapshot.Result.Payload.Plan.Stories[0].AcceptanceCriteria[0].ID != "AC-001" {
		t.Fatal("enrichment changed identities/sibling")
	}
	dep := operation("set", "payload.plan.stories.dependencies", "", `{"story_keys":["US-003"],"issue_refs":[]}`)
	dep.Target.Story = "US-001"
	_, d := ar.ApplyUpdate(next.Snapshot, next.Retired, ar.UpdateInput{ExpectedRevision: 3, Operations: []ar.Operation{dep}}, c, ar.EditTargets("plan"))
	if d.Valid {
		t.Fatal("dangling graph accepted")
	}
	coordinated := update(t, next.Snapshot, next.Retired, c, dep, operation("add_item", "payload.plan.stories", "US-003", `{"key":"US-003","title":"Dependency","user_story":{"want":"support"}}`))
	cycle := operation("set", "payload.plan.stories.dependencies", "", `{"story_keys":["US-001"],"issue_refs":[]}`)
	cycle.Target.Story = "US-003"
	_, d = ar.ApplyUpdate(coordinated.Snapshot, coordinated.Retired, ar.UpdateInput{ExpectedRevision: 4, Operations: []ar.Operation{cycle}}, c, ar.EditTargets("plan"))
	if d.Valid {
		t.Fatal("cycle accepted")
	}
	remove := operation("remove_item", "payload.plan.stories.acceptance_criteria", "AC-001", "")
	remove.Target.Story = "US-001"
	retired := update(t, next.Snapshot, next.Retired, c, remove)
	restore := operation("set", "payload.plan.stories", "", `[{"key":"US-001","title":"Restore","user_story":{"want":"restore"},"acceptance_criteria":[{"id":"AC-001","text":"forbidden reuse"}]}]`)
	_, d = ar.ApplyUpdate(retired.Snapshot, retired.Retired, ar.UpdateInput{ExpectedRevision: 4, Operations: []ar.Operation{restore}}, c, ar.EditTargets("plan"))
	if d.Valid {
		t.Fatal("parent replacement reused retired child")
	}
}
func TestUpdate_FullFinalValidation(t *testing.T) {
	for _, fault := range []string{"rough completion", "invalid sibling", "missing evidence", "invalid completed merge"} {
		t.Run(fault, func(t *testing.T) {
			s, c := updateSnapshot("plan", "incomplete")
			ops := []ar.Operation{operation("set", "comment", "", `"amended"`)}
			switch fault {
			case "rough completion":
				ops = append(ops, operation("set", "outcome", "", `"completed"`), operation("remove", "reason", "", ""))
			case "invalid sibling":
				s.Result.Payload.Plan.Stories = append(s.Result.Payload.Plan.Stories, ar.PlanStory{Key: "US-002", Title: "Invalid", UserStory: ar.UserStory{Want: "dependency"}, Dependencies: &planbrief.DependencyContent{StoryKeys: []string{"US-999"}}})
			case "missing evidence":
				s, c = updateSnapshot("implement", "completed")
				s.Result.Payload.Implement.Stories[0].Engineering.CheckIDs = []string{"unretained"}
			case "invalid completed merge":
				s, c = updateSnapshot("plan", "completed")
				ops = append(ops, operation("set", "payload.plan.summary", "", `""`))
			}
			got, d := ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: ops}, c, ar.EditTargets(c.Mode))
			if d.Valid || got.Snapshot.Meta.ResultRevision != 0 || s.Meta.ResultRevision != 1 {
				t.Fatal("invalid final state accepted")
			}
		})
	}
	// An actually complete draft can transition atomically without intermediate
	// outcome/reason validation demanding invented proof.
	s, c := updateSnapshot("plan", "completed")
	s.Result.Outcome = "incomplete"
	s.Result.Reason = "Awaiting finalization"
	got := update(t, s, nil, c, operation("set", "outcome", "", `"completed"`), operation("remove", "reason", "", ""))
	if got.Snapshot.Result.Outcome != "completed" {
		t.Fatal("complete draft could not finalize")
	}
}
func TestUpdate_StaleAndProjectionAuthorization(t *testing.T) {
	s, c := updateSnapshot("plan", "incomplete")
	s.Meta.ResultRevision = 7
	target := ar.Target{Field: "payload.plan.stories", ID: "US-001"}
	grants := []ar.Target{target}
	ops := []ar.Operation{operation("update_item", target.Field, target.ID, `{"title":"new"}`), operation("set", "comment", "", `"not granted"`)}
	got, d := ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: 6, Operations: ops}, c, grants)
	if d.Valid || d.ExpectedRevision != 6 || d.CurrentRevision != 7 || len(d.Operations) != 1 || d.Operations[0].Target != target || got.Snapshot.Meta.ResultRevision != 0 {
		t.Fatalf("stale diagnostic not bounded: %+v", d)
	}
	p, diag := ar.Project(s, []ar.Target{target}, grants, c)
	if !diag.Valid || p.Revision != 7 || p.CompleteSnapshot || p.ResultID != "result-1" || len(p.Selections) != 1 {
		t.Fatalf("bad projection: %+v %+v", p, diag)
	}
	p.Selections[0].Value[0] = '!'
	if s.Result.Payload.Plan.Stories[0].Key != "US-001" {
		t.Fatal("projection aliases baseline")
	}
	for _, selection := range []ar.Target{{Field: target.Field}, {Field: target.Field, ID: "US-002"}, {Field: "meta"}, {Field: "payload.plan.stories.title", Story: "US-001"}} {
		if _, diag = ar.Project(s, []ar.Target{selection}, grants, c); diag.Valid {
			t.Fatalf("selection broadened scope: %+v", selection)
		}
	}
	if _, diag = ar.Project(s, []ar.Target{target}, nil, c); diag.Valid {
		t.Fatal("nil grant permitted read")
	}
	_, d = ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: 7, Operations: ops[:1]}, c, nil)
	if d.Valid {
		t.Fatal("nil grant permitted write")
	}
}
func TestUpdate_DeepCopiesAndPriorAssessments(t *testing.T) {
	s, c := updateSnapshot("validate", "completed")
	before := authored(t, s.Result)
	h := ar.RetiredIDs{"prior": []string{"ID-1"}}
	got := update(t, s, h, c, operation("update_item", "payload.validate.stories", "US-001", `{"criteria":[{"name":"AC-001","status":"unverified","classification":"blocking","evidence":[{"check_ids":[],"summary":"No supporting evidence"}]}]}`))
	got.Snapshot.Result.Payload.Validate.Stories[0].Criteria[0].Evidence[0].Summary = "mutated"
	got.Retired["prior"][0] = "changed"
	if !reflect.DeepEqual(authored(t, s.Result), before) || h["prior"][0] != "ID-1" {
		t.Fatal("new assessment overwrote baseline or history")
	}
}
func TestDecodeUpdate_StrictAndLimits(t *testing.T) {
	valid := `{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"new"}]}`
	for _, bad := range []string{`null`, `{}`, valid + ` {}`, strings.Replace(valid, `"expected_revision":1`, `"expected_revision":1,"expected_revision":1`, 1), strings.Replace(valid, `"field":"comment"`, `"field":"comment","pointer":"/meta"`, 1), strings.Replace(valid, `"new"`, `null`, 1), strings.Replace(valid, `"new"`, `{"text":"a","text":"b"}`, 1), strings.Replace(valid, `"expected_revision":1`, `"expected_revision":"1"`, 1), strings.Replace(valid, `"operations":`, `"meta":{},"operations":`, 1)} {
		if _, d := ar.DecodeUpdate([]byte(bad)); d.Valid {
			t.Fatalf("invalid update accepted: %s", bad)
		}
	}
	for _, n := range []int{256, 257} {
		ops := make([]ar.Operation, n)
		for i := range ops {
			ops[i] = operation("set", "comment", "", `"new"`)
		}
		b, err := json.Marshal(ar.UpdateInput{ExpectedRevision: 1, Operations: ops})
		if err != nil {
			t.Fatal(err)
		}
		_, d := ar.DecodeUpdate(b)
		if d.Valid != (n == 256) {
			t.Fatalf("operation bound %d: %+v", n, d)
		}
	}
	for _, n := range []int{ar.MaxBytes, ar.MaxBytes + 1} {
		b := []byte(valid + strings.Repeat(" ", n-len(valid)))
		_, d := ar.DecodeUpdate(b)
		if d.Valid != (n == ar.MaxBytes) {
			t.Fatalf("byte bound %d: %+v", n, d)
		}
	}
	// root, operations array and operation object occupy three containers.
	for _, n := range []int{29, 30} {
		raw := strings.Repeat("[", n) + `"leaf"` + strings.Repeat("]", n)
		b := strings.Replace(valid, `"new"`, raw, 1)
		_, d := ar.DecodeUpdate([]byte(b))
		if d.Valid != (n == 29) {
			t.Fatalf("depth %d: %+v", n+3, d)
		}
	}
	s, c := updateSnapshot("spike", "incomplete")
	ops := make([]ar.Operation, 256)
	for i := range ops {
		ops[i] = operation("set", "comment", "", fmt.Sprintf(`"comment %d"`, i))
	}
	got := update(t, s, nil, c, ops...)
	if got.Snapshot.Result.Comment != "comment 255" || got.Snapshot.Meta.ResultRevision != 2 {
		t.Fatal("operation boundary did not execute sequentially once")
	}
}

//go:embed testdata/v1/updates/*.json
var updateCorpus embed.FS

func TestUpdate_VersionedCorpus(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		t.Run(mode, func(t *testing.T) {
			data, err := updateCorpus.ReadFile("testdata/v1/updates/" + mode + ".json")
			if err != nil {
				t.Fatal(err)
			}
			input, d := ar.DecodeUpdate(data)
			if !d.Valid {
				t.Fatalf("fixture rejected: %+v", d)
			}
			s, c := updateSnapshot(mode, "incomplete")
			got, diag := ar.ApplyUpdate(s, nil, input, c, ar.EditTargets(mode))
			if !diag.Valid || got.Snapshot.Meta.ResultRevision != 2 {
				t.Fatalf("fixture did not produce revision: %+v", diag)
			}
		})
	}
}

func TestUpdate_SchemaParity(t *testing.T) {
	var schema struct {
		Properties struct {
			Operations struct {
				MaxItems int `json:"maxItems"`
				Items    struct {
					Properties struct {
						Op struct {
							Enum []string `json:"enum"`
						} `json:"op"`
						Target struct {
							Properties struct {
								Field struct {
									Enum []string `json:"enum"`
								} `json:"field"`
							} `json:"properties"`
						} `json:"target"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	data := ar.UpdateJSONSchema()
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties.Operations.MaxItems != ar.MaxOperations {
		t.Fatal("schema operation limit differs")
	}
	documented := schema.Properties.Operations.Items.Properties.Target.Properties.Field.Enum
	seen := map[string]bool{}
	for _, f := range documented {
		seen[f] = true
	}
	expected := map[string]bool{}
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, target := range ar.EditTargets(mode) {
			expected[target.Field] = true
		}
	}
	for _, target := range ar.PlanGraphEditTargets() {
		expected[target.Field] = true
	}
	if !reflect.DeepEqual(seen, expected) {
		t.Fatal("schema edit matrix differs from Go")
	}
	if !reflect.DeepEqual(schema.Properties.Operations.Items.Properties.Op.Enum, []string{"set", "remove", "add_item", "update_item", "remove_item", "reorder"}) {
		t.Fatal("schema operation vocabulary differs")
	}
	data[0] = '!'
	if ar.UpdateJSONSchema()[0] != '{' {
		t.Fatal("schema bytes alias")
	}
}

func TestUpdate_RetirementRollbackAndTaskCoordination(t *testing.T) {
	s, c := updateSnapshot("research", "incomplete")
	h := ar.RetiredIDs{"previous": []string{"F-9"}}
	got, d := ar.ApplyUpdate(s, h, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{operation("remove_item", "payload.research.findings", "F-1", ""), operation("set", "comment", "", `""`)}}, c, ar.EditTargets("research"))
	if d.Valid || got.Retired != nil || len(h) != 1 || len(s.Result.Payload.Research.Findings) != 1 {
		t.Fatal("failed final validation leaked retirement")
	}
	s, c = updateSnapshot("plan", "incomplete")
	task := operation("add_item", "payload.plan.stories.tasks", "T-001", `{"id":"T-001","description":"Implement","owner_role":"engineer","criterion_ids":["AC-001"],"depends_on":[]}`)
	task.Target.Story = "US-001"
	criterion := operation("add_item", "payload.plan.stories.acceptance_criteria", "AC-001", `{"id":"AC-001","text":"Observable result"}`)
	criterion.Target.Story = "US-001"
	result := update(t, s, nil, c, task, criterion)
	if result.Snapshot.Result.Payload.Plan.Stories[0].Tasks[0].CriterionIDs[0] != "AC-001" {
		t.Fatal("coordinated criterion/task graph not retained")
	}
	remove := operation("remove_item", "payload.plan.stories.acceptance_criteria", "AC-001", "")
	remove.Target.Story = "US-001"
	_, d = ar.ApplyUpdate(result.Snapshot, result.Retired, ar.UpdateInput{ExpectedRevision: 2, Operations: []ar.Operation{remove}}, c, ar.EditTargets("plan"))
	if d.Valid {
		t.Fatal("dangling unchanged task was not revalidated")
	}
}
func TestUpdate_ImmutableTargetsAndBoundProof(t *testing.T) {
	s, c := updateSnapshot("implement", "completed")
	for _, op := range []ar.Operation{
		operation("set", "payload.implement.stories.engineering", "", `{}`),
		operation("update_item", "payload.implement.stories", "US-001", `{"capture":{"files":[]}}`),
		operation("update_item", "payload.implement.stories", "US-001", `{"approval":true}`),
		operation("update_item", "payload.implement.stories", "US-001", `{"engineering":{"check_ids":["missing"]}}`),
	} {
		got, d := ar.ApplyUpdate(s, nil, ar.UpdateInput{ExpectedRevision: 1, Operations: []ar.Operation{op}}, c, ar.EditTargets("implement"))
		if d.Valid || got.Snapshot.Meta.ResultRevision != 0 {
			t.Fatal("immutable or unretained proof edit accepted")
		}
	}
}
