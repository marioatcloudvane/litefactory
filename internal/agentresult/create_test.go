package agentresult_test

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func createFixture(mode, outcome string) (ar.Result, ar.Context) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("deterministic implementation bytes")))
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte("frozen US-001 requirements")))
	ctx := ar.Context{SchemaVersion: 1, Mode: mode, WorkflowVersion: 2, QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, Stories: map[string]agent.ContributionStory{"US-001": {Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, SnapshotRetained: true, Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}, Checks: map[string]agent.RetainedContributionCheck{"check-1": {Story: "US-001", Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, Executed: true, Passed: true, Retained: true}}}}}
	r := ar.Result{Outcome: outcome, Comment: "Recorded meaningful work."}
	if outcome != "completed" {
		r.Reason = "Further work needs the authorized input."
	}
	switch mode {
	case "research":
		r.Payload.Research = &ar.Research{Request: "Describe the existing boundary", Findings: []ar.Finding{{ID: "F-1", Text: "The boundary is explicit."}}}
	case "spike":
		r.Payload.Spike = &ar.Spike{Request: "Try a bounded alternative", WorkDone: []string{"Exercised the alternative."}}
	case "plan":
		r.Payload.Plan = &ar.Plan{Stories: []ar.PlanStory{{Key: "US-001", Title: "Create result", UserStory: ar.UserStory{Want: "create a valid result"}}}}
		if outcome == "completed" {
			p := r.Payload.Plan
			p.Summary = "Deliver a pure result boundary"
			s := &p.Stories[0]
			s.Intent = "create"
			s.UserStory.Actor = "adapter author"
			s.UserStory.Benefit = "consistent results"
			s.UserStory.Notes = []string{"Offline synthetic contract fixture."}
			s.AcceptanceCriteria = []planbrief.CriterionContent{{ID: "AC-001", Text: "Reject invalid input"}}
			s.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Implement validation", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}
			s.Architecture = &planbrief.ArchitectureContent{Summary: "Pure boundary", Components: []planbrief.ComponentContent{{Name: "validator", Responsibility: "Validate input"}}, Interfaces: []planbrief.InterfaceContent{{Name: "Create", Input: "authored input", Output: "validated result"}}, Constraints: []string{"No I/O"}, Decisions: []planbrief.DecisionContent{{Decision: "Typed input", Reason: "Explicit shape"}}}
			s.Dependencies = &planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}
		}
	case "implement":
		r.Payload.Implement = &ar.Implement{}
		if outcome == "completed" {
			r.Payload.Implement.Stories = []ar.ImplementationStory{{Story: "US-001", Outcome: "engineering_complete", Engineering: &agent.EngineeringContribution{SnapshotID: "snapshot-1", CheckIDs: []string{"check-1"}}}}
		}
	case "validate":
		r.Payload.Validate = &ar.Validate{}
		if outcome == "completed" {
			a := assessmentFixture()
			r.Payload.Validate.Stories = []ar.AssessmentStory{{Story: "US-001", SnapshotID: a.SnapshotID, Criteria: a.Criteria, Reviews: a.Reviews}}
		}
	}
	return r, ctx
}
func assessmentFixture() *agent.AssessmentContribution {
	a := &agent.AssessmentContribution{SnapshotID: "snapshot-1", Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{"check-1"}, Summary: "Retained synthetic execution supports this fixture."}}}}, Reviews: []agent.ContributionFinding{}}
	for _, name := range []string{"code", "architecture", "plan"} {
		a.Reviews = append(a.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Source-only fixture finding."}}})
	}
	return a
}
func authored(t *testing.T, r ar.Result) []byte {
	t.Helper()
	b, e := json.Marshal(ar.CreateInput{Result: r})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func assertValid(t *testing.T, r ar.Result, c ar.Context) {
	t.Helper()
	got, d := ar.Create(authored(t, r), c)
	if !d.Valid || !d.Complete || !reflect.DeepEqual(got, r) {
		t.Fatalf("creation mismatch: diagnostics=%+v got=%+v", d, got)
	}
}
func TestCreate_ModeOutcomeCorpus(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, outcome := range []string{"completed", "incomplete", "blocked"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) { t.Parallel(); r, c := createFixture(mode, outcome); assertValid(t, r, c) })
		}
	}
}
func TestCreate_CommonRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ar.Result, *ar.Context)
	}{
		{"blank comment", func(r *ar.Result, _ *ar.Context) { r.Comment = " \n" }},
		{"unfinished without reason", func(r *ar.Result, _ *ar.Context) { r.Reason = "" }},
		{"completed with reason", func(r *ar.Result, _ *ar.Context) { r.Outcome = "completed" }},
		{"unknown outcome", func(r *ar.Result, _ *ar.Context) { r.Outcome = "accepted" }},
		{"unknown schema", func(_ *ar.Result, c *ar.Context) { c.SchemaVersion = 2 }},
		{"unknown workflow", func(_ *ar.Result, c *ar.Context) { c.WorkflowVersion = 3 }},
		{"wrong mode", func(_ *ar.Result, c *ar.Context) { c.Mode = "spike" }},
		{"multiple payloads", func(r *ar.Result, _ *ar.Context) { r.Payload.Spike = &ar.Spike{Request: "Try"} }},
		{"missing payload", func(r *ar.Result, _ *ar.Context) { r.Payload = ar.Payload{} }},
		{"blank recommendation", func(r *ar.Result, _ *ar.Context) { r.Recommendation = &ar.Recommendation{} }},
		{"unauthorized recommendation action", func(r *ar.Result, _ *ar.Context) {
			r.Recommendation = &ar.Recommendation{Text: "Proceed", Action: "publish"}
		}},
		{"duplicate finding", func(r *ar.Result, _ *ar.Context) {
			r.Payload.Research.Findings = append(r.Payload.Research.Findings, r.Payload.Research.Findings[0])
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, c := createFixture("research", "incomplete")
			tt.mutate(&r, &c)
			if d := ar.ValidateResult(r, c); d.Valid || len(d.Diagnostics) == 0 {
				t.Fatalf("invalid input accepted: %+v", d)
			}
		})
	}
}
func TestCreate_Questions(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, outcome := range []string{"completed", "incomplete", "blocked"} {
			for _, permission := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/permission=%t", mode, outcome, permission), func(t *testing.T) {
					r, c := createFixture(mode, outcome)
					c.QuestionsAllowed = permission
					r.Questions = []ar.Question{{ID: "Q-1", Text: "Which option?", Options: []ar.Option{{ID: "O-1", Text: "Proceed"}}, RecommendedOption: "O-1"}}
					d := ar.ValidateResult(r, c)
					want := permission && (mode == "research" || mode == "plan") && outcome != "completed"
					if d.Valid != want {
						t.Fatalf("valid=%t want=%t: %+v", d.Valid, want, d)
					}
				})
			}
		}
	}
	for _, kind := range []string{"duplicate question", "duplicate option", "missing option", "invalid ID"} {
		t.Run(kind, func(t *testing.T) {
			r, c := createFixture("research", "incomplete")
			r.Questions = []ar.Question{{ID: "Q-1", Text: "Choose", Options: []ar.Option{{ID: "O-1", Text: "Option"}}}}
			switch kind {
			case "duplicate question":
				r.Questions = append(r.Questions, r.Questions[0])
			case "duplicate option":
				r.Questions[0].Options = append(r.Questions[0].Options, r.Questions[0].Options[0])
			case "missing option":
				r.Questions[0].RecommendedOption = "O-2"
			case "invalid ID":
				r.Questions[0].ID = "../Q"
			}
			if ar.ValidateResult(r, c).Valid {
				t.Fatal("invalid question accepted")
			}
		})
	}
}
func TestCreate_ArtifactPaths(t *testing.T) {
	for _, p := range []string{"", ".", "..", "../escape", "/absolute", "a/../b", "a//b", "a\\b", "C:drive", "a\x00b"} {
		t.Run(fmt.Sprintf("%q", p), func(t *testing.T) {
			r, c := createFixture("research", "completed")
			r.Artifacts = []ar.Artifact{{ID: "ART-1", Kind: "brief", Path: p}}
			if ar.ValidateResult(r, c).Valid {
				t.Fatal("unsafe artifact path accepted")
			}
		})
	}
	r, c := createFixture("research", "completed")
	r.Artifacts = []ar.Artifact{{ID: "ART-1", Kind: "brief", Path: "session/brief.md"}}
	r.Recommendation = &ar.Recommendation{Text: "Plan next", Action: "action.moveto.planmode"}
	assertValid(t, r, c)
}
func TestCreate_StrictDecoding(t *testing.T) {
	r, c := createFixture("research", "completed")
	base := string(authored(t, r))
	secret := "PRIVATE_CAPABILITY_TEST_VALUE"
	cases := []struct{ name, input, code string }{
		{"malformed", `{"result":`, "json"}, {"trailing", base + ` {}`, "json"}, {"null root", `null`, "null"},
		{"duplicate", strings.Replace(base, `"comment":`, `"comment":"first","comment":`, 1), "duplicate"},
		{"unknown", strings.Replace(base, `"comment":`, `"`+secret+`":"`+secret+`","comment":`, 1), "unknown"},
		{"meta", `{"meta":{"capability":"` + secret + `"},` + base[1:], "authorization"},
		{"wrong type", strings.Replace(base, `"outcome":"completed"`, `"outcome":false`, 1), "type"},
		{"null optional", strings.Replace(base, `"comment":`, `"artifacts":null,"comment":`, 1), "null"},
	}
	for _, field := range []string{"owner_answers", "approval", "acceptance", "publication", "capability", "execution", "passed"} {
		cases = append(cases, struct{ name, input, code string }{field, strings.Replace(base, `"comment":`, `"`+field+`":true,"comment":`, 1), "unknown"})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, d := ar.Create([]byte(tt.input), c)
			if d.Valid || !reflect.DeepEqual(got, ar.Result{}) {
				t.Fatal("invalid input returned candidate")
			}
			found := false
			last := ""
			for _, v := range d.Diagnostics {
				found = found || v.Code == tt.code
				order := v.Path + "\x00" + v.Code
				if order < last {
					t.Fatal("unsorted diagnostics")
				}
				last = order
				if v.Message == "" || v.Path == "" {
					t.Fatal("unactionable diagnostic")
				}
				if v.Code == "type" && (v.Expected == "" || v.Actual == "") {
					t.Fatal("missing types")
				}
			}
			if !found {
				t.Fatalf("missing %s: %+v", tt.code, d)
			}
			b, _ := json.Marshal(d)
			if strings.Contains(string(b), secret) {
				t.Fatal("diagnostic leaked authored secret")
			}
		})
	}
}
func TestDecode_Boundaries(t *testing.T) {
	for _, n := range []int{ar.MaxBytes, ar.MaxBytes + 1} {
		t.Run(fmt.Sprintf("bytes/%d", n), func(t *testing.T) {
			var dst string
			d := ar.Decode([]byte(`"`+strings.Repeat("x", n-2)+`"`), &dst)
			if d.Valid != (n == ar.MaxBytes) {
				t.Fatalf("boundary: %+v", d)
			}
			if n > ar.MaxBytes && (d.Complete || len(d.UncheckedPaths) == 0) {
				t.Fatal("incomplete inspection claimed complete")
			}
		})
	}
	type node struct {
		Child *node `json:"child,omitempty"`
	}
	for _, depth := range []int{32, 33} {
		t.Run(fmt.Sprintf("depth/%d", depth), func(t *testing.T) {
			data := strings.Repeat(`{"child":`, depth-1) + `{}` + strings.Repeat(`}`, depth-1)
			var dst node
			d := ar.Decode([]byte(data), &dst)
			if d.Valid != (depth == 32) {
				t.Fatalf("depth boundary: %+v", d)
			}
			if depth > 32 && (d.Complete || len(d.UncheckedPaths) == 0) {
				t.Fatal("depth inspection falsely complete")
			}
		})
	}
	var s string
	if d := ar.Decode([]byte{'"', 0xff, '"'}, &s); d.Valid || d.Complete {
		t.Fatalf("invalid UTF-8: %+v", d)
	}
}
func TestCreate_PlanCompletionAndGraphs(t *testing.T) {
	for _, fault := range []string{"summary", "actor", "benefit", "criteria", "tasks", "architecture", "unknown role", "unknown criterion", "cycle", "dangling", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			r, c := createFixture("plan", "completed")
			p := r.Payload.Plan
			s := &p.Stories[0]
			switch fault {
			case "summary":
				p.Summary = ""
			case "actor":
				s.UserStory.Actor = ""
			case "benefit":
				s.UserStory.Benefit = ""
			case "criteria":
				s.AcceptanceCriteria = nil
			case "tasks":
				s.Tasks = nil
			case "architecture":
				s.Architecture = nil
			case "unknown role":
				s.Tasks[0].OwnerRole = "unrostered"
			case "unknown criterion":
				s.Tasks[0].CriterionIDs = []string{"AC-999"}
			case "cycle":
				s.Dependencies.StoryKeys = []string{"US-001"}
			case "dangling":
				s.Dependencies.StoryKeys = []string{"US-999"}
			case "duplicate":
				p.Stories = append(p.Stories, *s)
			}
			if ar.ValidateResult(r, c).Valid {
				t.Fatal("incomplete or invalid Plan accepted as completed")
			}
		})
	}
	for _, dep := range []string{"US-001", "US-999"} {
		t.Run("rough graph/"+dep, func(t *testing.T) {
			r, c := createFixture("plan", "incomplete")
			r.Payload.Plan.Stories[0].Dependencies = &planbrief.DependencyContent{StoryKeys: []string{dep}}
			if ar.ValidateResult(r, c).Valid {
				t.Fatal("invalid draft graph accepted")
			}
		})
	}
}
func TestCreate_EvidenceAuthority(t *testing.T) {
	for _, fault := range []string{"unknown story", "unretained snapshot", "unexecuted", "failed", "wrong revision", "wrong story", "wrong digest", "assessment in separated", "missing aggregate"} {
		t.Run(fault, func(t *testing.T) {
			r, c := createFixture("implement", "completed")
			s := c.Stories["US-001"]
			check := s.Checks["check-1"]
			switch fault {
			case "unknown story":
				r.Payload.Implement.Stories[0].Story = "US-999"
			case "unretained snapshot":
				s.SnapshotRetained = false
			case "unexecuted":
				check.Executed = false
			case "failed":
				check.Passed = false
			case "wrong revision":
				check.Revision = "wrong"
			case "wrong story":
				check.Story = "US-999"
			case "wrong digest":
				check.ImplementationDigest = "wrong"
			case "assessment in separated":
				r.Payload.Implement.Stories[0].Assessment = assessmentFixture()
			case "missing aggregate":
				c.RequireAggregate = true
			}
			s.Checks["check-1"] = check
			c.Stories["US-001"] = s
			if ar.ValidateResult(r, c).Valid {
				t.Fatal("unauthorized proof accepted")
			}
		})
	}
	t.Run("unresolved aggregate incomplete", func(t *testing.T) {
		r, c := createFixture("implement", "completed")
		r.Payload.Implement.AggregateChecks = &agent.EngineeringContribution{SnapshotID: "aggregate", CheckIDs: []string{"aggregate-check"}}
		d := ar.ValidateResult(r, c)
		if d.Valid || d.Complete || len(d.UncheckedPaths) == 0 {
			t.Fatalf("unresolved evidence: %+v", d)
		}
	})
	t.Run("legacy retains complete obligations", func(t *testing.T) {
		r, c := createFixture("implement", "completed")
		c.WorkflowVersion = 1
		s := &r.Payload.Implement.Stories[0]
		s.Outcome = "verified"
		s.Assessment = assessmentFixture()
		assertValid(t, r, c)
		s.Assessment = nil
		if ar.ValidateResult(r, c).Valid {
			t.Fatal("legacy missing assessment accepted")
		}
	})
	for _, status := range []string{"unmet", "unverified"} {
		t.Run("complete assessment/"+status, func(t *testing.T) {
			r, c := createFixture("validate", "completed")
			f := &r.Payload.Validate.Stories[0].Criteria[0]
			f.Status = status
			f.Evidence = []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Required behavior has not been demonstrated."}}
			assertValid(t, r, c)
		})
	}
}
func TestAssemble_CopiesBoundaryAndChecksMetadata(t *testing.T) {
	r, c := createFixture("research", "completed")
	m := ar.Metadata{SchemaVersion: 1, Task: "task", RunID: 1, AttemptID: "attempt", Mode: "research", ResultID: "result", ResultRevision: 1, WorkflowVersion: 2, SubmissionContractVersion: 2}
	s, d := ar.Assemble(r, m, c)
	if !d.Valid {
		t.Fatal(d)
	}
	s.Result.Payload.Research.Findings[0].Text = "Changed copy"
	if r.Payload.Research.Findings[0].Text == "Changed copy" {
		t.Fatal("snapshot aliases input")
	}
	if s.Meta != m {
		t.Fatal("metadata changed")
	}
	for _, revision := range []int64{0, -1} {
		m.ResultRevision = revision
		if _, d = ar.Assemble(r, m, c); d.Valid {
			t.Fatal("invalid revision accepted")
		}
	}
}
func TestJSONSchema_AuthoredBoundary(t *testing.T) {
	b, e := ar.JSONSchema()
	if e != nil {
		t.Fatal(e)
	}
	var s map[string]interface{}
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	if s["$id"] != "urn:litefactory:agent-result:create:1" || s["additionalProperties"] != false {
		t.Fatal("schema identity or closed boundary missing")
	}
	p, ok := s["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema properties missing")
	}
	if len(p) != 1 || p["result"] == nil {
		t.Fatal("authored schema permits factory envelope")
	}
}

//go:embed testdata/v1/*.json
var creationCorpus embed.FS

func TestCreate_VersionedFixtures(t *testing.T) {
	entries, err := creationCorpus.ReadDir("testdata/v1")
	if err != nil {
		t.Fatal(err)
	}
	validCount := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			mode := strings.Split(name, "-")[0]
			if strings.HasPrefix(name, "invalid-") {
				mode = "research"
				if name == "invalid-spike-question.json" {
					mode = "spike"
				}
			}
			_, ctx := createFixture(mode, "incomplete")
			if mode == "validate" {
				s := ctx.Stories["US-001"]
				s.SnapshotID = ""
				s.SnapshotRetained = false
				s.Checks = nil
				ctx.Stories["US-001"] = s
			}
			b, e := creationCorpus.ReadFile("testdata/v1/" + name)
			if e != nil {
				t.Fatal(e)
			}
			_, d := ar.Create(b, ctx)
			want := !strings.HasPrefix(name, "invalid-")
			if d.Valid != want {
				t.Fatalf("fixture valid=%t want=%t: %+v", d.Valid, want, d)
			}
			if want {
				validCount++
				schemaBytes, schemaErr := ar.JSONSchema()
				if schemaErr != nil {
					t.Fatal(schemaErr)
				}
				var schema map[string]interface{}
				var value interface{}
				if e = json.Unmarshal(schemaBytes, &schema); e != nil {
					t.Fatal(e)
				}
				if e = json.Unmarshal(b, &value); e != nil {
					t.Fatal(e)
				}
				assertSchemaShape(t, schema, value, "$")
				var shape ar.CreateInput
				if d := ar.Decode(b, &shape); !d.Valid {
					t.Fatalf("fixture structural parity: %+v", d)
				}
			}
		})
	}
	if validCount != 16 {
		t.Fatalf("valid fixture count=%d want=16 (15 mode/outcome plus questioning)", validCount)
	}
}
func TestCreate_CompletedPlanOptionalNotes(t *testing.T) {
	r, c := createFixture("plan", "completed")
	r.Payload.Plan.Stories[0].UserStory.Notes = nil
	assertValid(t, r, c)
}

// assertSchemaShape independently checks the structural subset emitted by the
// versioned schema; Go creation separately exercises semantic mode/scope rules.
func assertSchemaShape(t *testing.T, s map[string]interface{}, v interface{}, path string) {
	t.Helper()
	switch s["type"] {
	case "object":
		obj, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("%s: expected object", path)
		}
		props, ok := s["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("schema properties missing")
		}
		if required, ok := s["required"].([]interface{}); ok {
			for _, key := range required {
				keyName, ok := key.(string)
				if !ok {
					t.Fatal("invalid required field")
				}
				if _, ok := obj[keyName]; !ok {
					t.Fatalf("%s: missing %s", path, key)
				}
			}
		}
		for key, value := range obj {
			child, ok := props[key]
			if !ok {
				t.Fatalf("%s: unknown %s", path, key)
			}
			childSchema, ok := child.(map[string]interface{})
			if !ok {
				t.Fatal("invalid child schema")
			}
			assertSchemaShape(t, childSchema, value, path+"."+key)
		}
	case "array":
		items, ok := v.([]interface{})
		if !ok {
			t.Fatalf("%s: expected array", path)
		}
		for i, item := range items {
			itemSchema, ok := s["items"].(map[string]interface{})
			if !ok {
				t.Fatal("invalid item schema")
			}
			assertSchemaShape(t, itemSchema, item, fmt.Sprintf("%s[%d]", path, i))
		}
	case "string":
		if _, ok := v.(string); !ok {
			t.Fatalf("%s: expected string", path)
		}
	case "integer":
		n, ok := v.(float64)
		if !ok || n != float64(int64(n)) {
			t.Fatalf("%s: expected integer", path)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			t.Fatalf("%s: expected boolean", path)
		}
	default:
		t.Fatalf("%s: unsupported schema type %v", path, s["type"])
	}
	if values, ok := s["enum"].([]interface{}); ok {
		found := false
		for _, allowed := range values {
			found = found || reflect.DeepEqual(v, allowed)
		}
		if !found {
			t.Fatalf("%s: value outside schema enum", path)
		}
	}
}
func TestSnapshotJSONSchema_MetadataVersions(t *testing.T) {
	b, err := ar.SnapshotJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]interface{}
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	r, c := createFixture("research", "completed")
	m := ar.Metadata{SchemaVersion: 1, Task: "task", RunID: 1, AttemptID: "attempt", Mode: "research", ResultID: "result", ResultRevision: 1, WorkflowVersion: 2, SubmissionContractVersion: 2}
	snapshot, d := ar.Assemble(r, m, c)
	if !d.Valid {
		t.Fatal(d)
	}
	b, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var value interface{}
	if err = json.Unmarshal(b, &value); err != nil {
		t.Fatal(err)
	}
	assertSchemaShape(t, schema, value, "$")
	for _, bad := range []ar.Metadata{{SchemaVersion: 2, Task: "task", RunID: 1, AttemptID: "attempt", Mode: "research", ResultID: "result", ResultRevision: 1}, {SchemaVersion: 1, Task: "task", RunID: 1, AttemptID: "attempt", Mode: "plan", ResultID: "result", ResultRevision: 1}} {
		if _, d := ar.Assemble(r, bad, c); d.Valid {
			t.Fatal("unsupported metadata accepted")
		}
	}
}
