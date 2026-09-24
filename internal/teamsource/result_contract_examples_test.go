package teamsource

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// The Plan result contract's worked examples are executable (story-002 AC-040,
// AC-017, AC-020). Every marked example is extracted from node-result.md and run
// through the same validator preflight and submit reach, agentresult.Create over
// a factory-bound Context, so a broken example makes the test fail.

var (
	exampleMarked    = regexp.MustCompile("(?m)^<!-- example: (positive|negative) ([^>]*?) -->\n```json\n((?s:.*?))\n```")
	exampleMarkerLn  = regexp.MustCompile(`(?m)^<!-- example:`)
	walkthroughFence = regexp.MustCompile("(?m)^<!-- walkthrough: (?:id=)?(\\S+) -->\n```text\n((?s:.*?))\n```")
	walkthroughLn    = regexp.MustCompile(`(?m)^<!-- walkthrough:`)
	markerAttr       = regexp.MustCompile(`(\w+)=(\S+)`)
	jsonFence        = regexp.MustCompile("(?m)^```json\n")
)

const planContractPath = "plan/skills/plan-evidence/node-result.md"
const researchContractPath = "research/skills/research-evidence/node-result.md"

type contractExample struct {
	Kind  string // positive or negative
	ID    string
	Attrs map[string]string
	Body  []byte
}

func planContractDoc(t *testing.T) string {
	t.Helper()
	raw, err := planAssets.ReadFile(planContractPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// extractContractExamples returns every marked example plus the ways the markup
// itself is malformed: an unknown key or value, a duplicate id, or a marker that
// is not immediately followed by a json fence (which would silently skip it).
func extractContractExamples(doc string) ([]contractExample, []string) {
	var out []contractExample
	var problems []string
	ids := map[string]bool{}
	for _, m := range exampleMarked.FindAllStringSubmatch(doc, -1) {
		ex := contractExample{Kind: m[1], Attrs: map[string]string{}, Body: []byte(m[3])}
		for _, a := range markerAttr.FindAllStringSubmatch(m[2], -1) {
			ex.Attrs[a[1]] = a[2]
		}
		ex.ID = ex.Attrs["id"]
		var allowed []string
		if ex.Kind == "positive" {
			allowed = []string{"id", "stage", "response", "operation"}
		} else {
			allowed = []string{"id", "stage", "existing", "expect"}
		}
		for key := range ex.Attrs {
			if !slices.Contains(allowed, key) {
				problems = append(problems, fmt.Sprintf("%s %q: unknown marker key %q", ex.Kind, ex.ID, key))
			}
		}
		switch {
		case ex.ID == "":
			problems = append(problems, ex.Kind+" example without an id")
		case ids[ex.ID]:
			problems = append(problems, fmt.Sprintf("duplicate example id %q", ex.ID))
		}
		ids[ex.ID] = true
		if !slices.Contains([]string{"story", "technical", "acceptance", "set"}, ex.Attrs["stage"]) {
			problems = append(problems, fmt.Sprintf("%s %q: stage %q is not story, technical, acceptance or set", ex.Kind, ex.ID, ex.Attrs["stage"]))
		}
		if ex.Kind == "positive" {
			resp, op := ex.Attrs["response"], ex.Attrs["operation"]
			// The set stage has no story operation: ask and done both carry none.
			pair := (resp == "ask") == (op == "none")
			if ex.Attrs["stage"] == "set" {
				pair = op == "none"
			}
			if !slices.Contains([]string{"ask", "done"}, resp) || !slices.Contains([]string{"create", "update", "retire", "none"}, op) || !pair {
				problems = append(problems, fmt.Sprintf("positive %q: response=%s operation=%s is not a valid pair", ex.ID, resp, op))
			}
		} else {
			if code, path, ok := strings.Cut(ex.Attrs["expect"], "@"); !ok || code == "" || !strings.HasPrefix(path, "$") {
				problems = append(problems, fmt.Sprintf("negative %q: expect must be CODE@PATH", ex.ID))
			}
			if v, ok := ex.Attrs["existing"]; ok && v != "revision" {
				problems = append(problems, fmt.Sprintf("negative %q: existing must be revision", ex.ID))
			}
		}
		out = append(out, ex)
	}
	if markers := len(exampleMarkerLn.FindAllString(doc, -1)); markers != len(out) {
		problems = append(problems, fmt.Sprintf("%d example markers but %d extracted: a marker must sit on the line directly above a json fence", markers, len(out)))
	}
	return out, problems
}

func exampleExpectedRevision(body []byte) string {
	var in struct {
		Result struct {
			Payload struct {
				Plan struct {
					StoryRequest struct {
						ExpectedRevision string `json:"expected_revision"`
					} `json:"storyrequest"`
				} `json:"plan"`
			} `json:"payload"`
		} `json:"result"`
	}
	_ = json.Unmarshal(body, &in)
	return in.Result.Payload.Plan.StoryRequest.ExpectedRevision
}

// factoryContext fills what the factory binds for a graph Plan node, the way
// turn.planGraphBinding does, so the example is judged as the factory would.
func factoryContext(stage, existingRevision string) agentresult.Context {
	story := "story-001"
	if stage == "set" {
		// The set stage covers the whole parent: no single story is selected.
		story = ""
	}
	return agentresult.Context{
		SchemaVersion: 1, Mode: "plan", WorkflowVersion: 2, QuestionsAllowed: true,
		PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}, IssueRefs: []string{}},
		PlanGraph: &agentresult.PlanGraphBinding{
			Story: story, Stage: stage, Candidate: "plan-1-2", RequestID: "plan-request-1-2",
			Upstream: []string{"upstream-a", "upstream-b"}, SharedRevision: "shared-1", ExistingRevision: existingRevision,
		},
	}
}

func hasDiag(d agentresult.Diagnostics, code, path string) bool {
	return slices.ContainsFunc(d.Diagnostics, func(f agentresult.Diagnostic) bool { return f.Code == code && f.Path == path })
}

func diagList(d agentresult.Diagnostics) string {
	var parts []string
	for _, f := range d.Diagnostics {
		parts = append(parts, f.Code+"@"+f.Path)
	}
	return strings.Join(parts, ", ")
}

// checkContractExample runs one extracted example and returns its failures.
func checkContractExample(ex contractExample) []string {
	var fails []string
	stage := ex.Attrs["stage"]
	revision := exampleExpectedRevision(ex.Body)
	if ex.Kind == "positive" {
		existing := ""
		if op := ex.Attrs["operation"]; op == "update" || op == "retire" {
			existing = revision
			if len(existing) != 64 {
				return []string{fmt.Sprintf("positive %q: %s needs a 64 character expected_revision", ex.ID, op)}
			}
		}
		ctx := factoryContext(stage, existing)
		result, d := agentresult.Create(ex.Body, ctx)
		if !d.Valid {
			return []string{fmt.Sprintf("positive %q refused: %s", ex.ID, diagList(d))}
		}
		want := map[string]string{"ask": "incomplete", "done": "completed"}[ex.Attrs["response"]]
		if result.Outcome != want {
			fails = append(fails, fmt.Sprintf("positive %q: response=%s but outcome is %q", ex.ID, ex.Attrs["response"], result.Outcome))
		}
		plan := result.Payload.Plan
		if plan == nil {
			return append(fails, fmt.Sprintf("positive %q: no plan payload", ex.ID))
		}
		branches := 0
		if plan.StoryRequest != nil {
			branches++
			if got := plan.StoryRequest.Operation; got != ex.Attrs["operation"] {
				fails = append(fails, fmt.Sprintf("positive %q: marker operation=%s but request operation is %s", ex.ID, ex.Attrs["operation"], got))
			}
		}
		if plan.StorySet != nil {
			branches++
		}
		if stage == "set" && ex.Attrs["response"] == "done" && (plan.StorySet == nil || plan.StoryRequest != nil) {
			fails = append(fails, fmt.Sprintf("positive %q: a set-stage done carries exactly a storyset", ex.ID))
		}
		if (ex.Attrs["response"] == "ask") != (branches == 0) || branches > 1 {
			fails = append(fails, fmt.Sprintf("positive %q: %d story branches for response=%s", ex.ID, branches, ex.Attrs["response"]))
		}
		// The confirming object the role returns afterwards must also decode over
		// the same context: the example's result inside the version-1 envelope.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(ex.Body, &fields); err != nil {
			return append(fails, fmt.Sprintf("positive %q: %v", ex.ID, err))
		}
		envelope, _ := json.Marshal(map[string]any{"version": 1, "response": ex.Attrs["response"], "result": fields["result"]})
		if node, nd := agentresult.DecodePlanNode(envelope, ctx); !nd.Valid || node.Response != ex.Attrs["response"] {
			fails = append(fails, fmt.Sprintf("positive %q: confirming object refused: %s", ex.ID, diagList(nd)))
		}
		return fails
	}
	existing := ""
	if ex.Attrs["existing"] == "revision" {
		existing = revision
	}
	_, d := agentresult.Create(ex.Body, factoryContext(stage, existing))
	code, path, _ := strings.Cut(ex.Attrs["expect"], "@")
	switch {
	case d.Valid:
		fails = append(fails, fmt.Sprintf("negative %q was accepted; it must be refused with %s", ex.ID, ex.Attrs["expect"]))
	case !hasDiag(d, code, path):
		fails = append(fails, fmt.Sprintf("negative %q: want %s, got %s", ex.ID, ex.Attrs["expect"], diagList(d)))
	}
	return fails
}

// verifyContractExamples is the runner over document text: extraction problems
// plus every example's failures.
func verifyContractExamples(doc string) ([]contractExample, []string) {
	examples, fails := extractContractExamples(doc)
	for _, ex := range examples {
		fails = append(fails, checkContractExample(ex)...)
	}
	return examples, fails
}

// checkContractCoverage reports what the example set fails to demonstrate.
func checkContractCoverage(examples []contractExample) []string {
	var fails []string
	seen := map[string]bool{}
	codes := map[string]bool{}
	for _, ex := range examples {
		if ex.Kind == "positive" {
			seen[ex.Attrs["stage"]+"/"+ex.Attrs["response"]] = true
			seen["done/"+ex.Attrs["operation"]] = seen["done/"+ex.Attrs["operation"]] || ex.Attrs["response"] == "done"
		} else {
			code, _, _ := strings.Cut(ex.Attrs["expect"], "@")
			codes[code] = true
		}
	}
	for _, stage := range []string{"story", "technical", "acceptance", "set"} {
		for _, resp := range []string{"ask", "done"} {
			if !seen[stage+"/"+resp] {
				fails = append(fails, fmt.Sprintf("no positive example for stage %s, response %s", stage, resp))
			}
		}
	}
	for _, op := range []string{"create", "update", "retire"} {
		if !seen["done/"+op] {
			fails = append(fails, "no positive done example for operation "+op)
		}
	}
	for _, code := range []string{"branch", "legacy_field", "factory_owned", "story_request", "draft", "section", "duplicate", "blank", "cycle", "limit", "self_reference", "reference"} {
		if !codes[code] {
			fails = append(fails, "no negative example expecting "+code)
		}
	}
	return fails
}

func TestPlanContractExamplesRunThroughTheSharedValidator(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	examples, fails := verifyContractExamples(doc)
	for _, f := range fails {
		t.Error(f)
	}
	var positives, negatives int
	for _, ex := range examples {
		if ex.Kind == "positive" {
			positives++
		} else {
			negatives++
		}
	}
	if positives < 9 || negatives < 5 {
		t.Fatalf("only %d positive and %d negative examples extracted", positives, negatives)
	}
	for _, f := range checkContractCoverage(examples) {
		t.Error(f)
	}
}

// Per example subtests give a failing example its own name in test output.
func TestPlanContractExamplesEachExample(t *testing.T) {
	t.Parallel()
	examples, problems := extractContractExamples(planContractDoc(t))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	for _, ex := range examples {
		t.Run(ex.Kind+"/"+ex.ID, func(t *testing.T) {
			t.Parallel()
			for _, f := range checkContractExample(ex) {
				t.Error(f)
			}
		})
	}
}

// An unmarked json fence is illustrative and never run; a text fence, even one
// carrying a marker-like comment, is never extracted as an example.
func TestPlanContractUnmarkedAndTextFencesAreNotExtracted(t *testing.T) {
	t.Parallel()
	doc := "text\n```json\n{\"not\":\"an example\"}\n```\n\n<!-- walkthrough: id=w -->\n```text\n{\"result\":1}\n```\n"
	examples, problems := extractContractExamples(doc)
	if len(examples) != 0 || len(problems) != 0 {
		t.Fatalf("examples=%v problems=%v", examples, problems)
	}
	// The real document keeps every walkthrough out of the examples.
	found, _ := extractContractExamples(planContractDoc(t))
	walks, _ := extractWalkthroughs(planContractDoc(t))
	for _, ex := range found {
		for _, w := range walks {
			if ex.ID == w.ID || strings.Contains(string(ex.Body), w.Text) {
				t.Fatalf("walkthrough %q was extracted as %s %q", w.ID, ex.Kind, ex.ID)
			}
		}
	}
}

// The factory alone binds these five members, and each is refused by name at its
// own path however it is supplied, so no positive example can teach an agent to
// send one.
func TestPlanFactoryOwnedFieldsAreRefusedByName(t *testing.T) {
	t.Parallel()
	examples, _ := extractContractExamples(planContractDoc(t))
	var create contractExample
	for _, ex := range examples {
		if ex.ID == "story-create" {
			create = ex
		}
	}
	if create.ID == "" {
		t.Fatal("no story-create example")
	}
	for _, field := range []string{"request_id", "stage", "candidate", "upstream", "shared_revision"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			var value any = "supplied"
			if field == "upstream" {
				value = []string{"x"}
			}
			body := mutateBody(t, create.Body, func(m map[string]any) {
				storyRequestOf(m)[field] = value
			})
			_, d := agentresult.Create(body, factoryContext("story", ""))
			if d.Valid || !hasDiag(d, "factory_owned", "$.result.payload.plan.storyrequest."+field) {
				t.Fatalf("%s: %s", field, diagList(d))
			}
		})
	}
}

func mutateBody(t *testing.T, body []byte, f func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// mutateDocExample rewrites one example's json in the document text.
func mutateDocExample(t *testing.T, doc, id string, f func(map[string]any)) string {
	t.Helper()
	for _, loc := range exampleMarked.FindAllStringSubmatchIndex(doc, -1) {
		if !slices.Contains(strings.Fields(doc[loc[4]:loc[5]]), "id="+id) {
			continue
		}
		next := mutateBody(t, []byte(doc[loc[6]:loc[7]]), f)
		return doc[:loc[6]] + string(next) + doc[loc[7]:]
	}
	t.Fatalf("no example %q", id)
	return ""
}

// objectAt walks nested json objects by key, panicking (as the bare type
// assertions it replaces would) when a step is not an object.
func objectAt(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			panic(fmt.Sprintf("example json: %q is not an object", k))
		}
		cur = next
	}
	return cur
}

func planOf(m map[string]any) map[string]any {
	return objectAt(m, "result", "payload", "plan")
}

func storyRequestOf(m map[string]any) map[string]any {
	return objectAt(m, "result", "payload", "plan", "storyrequest")
}

// AC-017: a deliberately broken example must make the runner fail. Each case
// mutates a real example in the document text and asserts the runner notices.
func TestContractRunnerFailsOnBrokenExamples(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	if _, fails := verifyContractExamples(doc); len(fails) != 0 {
		t.Fatalf("baseline must be clean: %v", fails)
	}
	cases := []struct {
		name   string
		mutate func(t *testing.T, doc string) string
		want   string
	}{
		{"positive gains a supplied stage field", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "story-create", func(m map[string]any) { storyRequestOf(m)["stage"] = "story" })
		}, `positive "story-create" refused: factory_owned@$.result.payload.plan.storyrequest.stage`},
		{"positive gains an extra branch", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "story-create", func(m map[string]any) {
				planOf(m)["storyset"] = map[string]any{"entries": []any{}}
			})
		}, `positive "story-create" refused`},
		{"positive draft names another story", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "story-update", func(m map[string]any) { objectAt(storyRequestOf(m), "draft")["key"] = "story-999" })
		}, `binding@$.result.payload.plan.storyrequest.draft.key`},
		{"story stage selects a later section", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "story-update", func(m map[string]any) { storyRequestOf(m)["changed_sections"] = []any{"architecture"} })
		}, `section@`},
		{"ask example gains a legacy field", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "ask-story", func(m map[string]any) {
				planOf(m)["summary"] = "legacy"
			})
		}, `legacy_field@`},
		{"positive drops the questions from an ask", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "ask-technical", func(m map[string]any) { delete(objectAt(m, "result"), "questions") })
		}, `positive "ask-technical"`},
		{"positive marker claims the wrong response", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story response=done operation=create", "id=story-create stage=story response=ask operation=none", 1)
		}, `positive "story-create"`},
		{"positive marker claims the wrong operation", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story response=done operation=create", "id=story-create stage=story response=done operation=retire", 1)
		}, `story-create`},
		{"negative that is actually valid", func(t *testing.T, d string) string {
			return mutateDocExample(t, d, "story-stage-architecture", func(m map[string]any) {
				delete(objectAt(storyRequestOf(m), "draft"), "architecture")
			})
		}, `negative "story-stage-architecture" was accepted`},
		{"negative expects a code it does not produce", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=done-no-branch stage=story expect=branch@", "id=done-no-branch stage=story expect=cycle@", 1)
		}, `negative "done-no-branch": want cycle@`},
		{"negative expects the wrong path", func(t *testing.T, d string) string {
			return strings.Replace(d, "expect=legacy_field@$.result.payload.plan.summary", "expect=legacy_field@$.result.payload.plan.context", 1)
		}, `negative "legacy-summary": want`},
		{"marker separated from its fence by a blank line", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story response=done operation=create -->\n```json", "id=story-create stage=story response=done operation=create -->\n\n```json", 1)
		}, `example markers but`},
		{"marker above a text fence", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story response=done operation=create -->\n```json", "id=story-create stage=story response=done operation=create -->\n```text", 1)
		}, `example markers but`},
		{"marker with an unknown key", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story response=done operation=create", "id=story-create stage=story response=done operation=create branch=set", 1)
		}, `unknown marker key "branch"`},
		{"marker with an unknown stage", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-create stage=story", "id=story-create stage=discovery", 1)
		}, `stage "discovery"`},
		{"duplicate example id", func(t *testing.T, d string) string {
			return strings.Replace(d, "id=story-update ", "id=story-create ", 1)
		}, `duplicate example id "story-create"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, fails := verifyContractExamples(tt.mutate(t, doc))
			if len(fails) == 0 {
				t.Fatal("the runner accepted a broken example")
			}
			if !strings.Contains(strings.Join(fails, "\n"), tt.want) {
				t.Fatalf("failures %q do not mention %q", fails, tt.want)
			}
		})
	}
}

func TestContractCoverageCheckerFailsOnGaps(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	examples, _ := extractContractExamples(doc)
	if got := checkContractCoverage(examples); len(got) != 0 {
		t.Fatalf("shipped set has gaps: %v", got)
	}
	without := func(pred func(contractExample) bool) []contractExample {
		var out []contractExample
		for _, ex := range examples {
			if !pred(ex) {
				out = append(out, ex)
			}
		}
		return out
	}
	cases := []struct {
		name string
		set  []contractExample
		want string
	}{
		{"no technical ask", without(func(e contractExample) bool { return e.ID == "ask-technical" }), "stage technical, response ask"},
		{"no acceptance done", without(func(e contractExample) bool { return e.Attrs["stage"] == "acceptance" && e.Attrs["response"] == "done" }), "stage acceptance, response done"},
		{"no retire", without(func(e contractExample) bool { return e.Attrs["operation"] == "retire" }), "operation retire"},
		{"no create", without(func(e contractExample) bool { return e.Attrs["operation"] == "create" }), "operation create"},
		{"no factory-owned negative", without(func(e contractExample) bool { return strings.HasPrefix(e.Attrs["expect"], "factory_owned") }), "expecting factory_owned"},
		{"no examples", nil, "stage story, response ask"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(checkContractCoverage(tt.set), "; "); !strings.Contains(got, tt.want) {
				t.Fatalf("gaps %q do not contain %q", got, tt.want)
			}
		})
	}
}

// docSection returns the text under a level-2 heading up to the next one.
func docSection(doc, heading string) string {
	_, rest, ok := strings.Cut(doc, "\n"+heading+"\n")
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "\n## "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

var planSections = []string{"intent", "architecture", "criteria", "tasks", "dependencies", "design", "guidance", "conversation"}

// checkSectionRestrictions compares what the document says each stage may select
// with what the validator enforces, so the prose and the code cannot drift.
func checkSectionRestrictions(doc string) []string {
	body := docSection(doc, "## Sections and stage restrictions")
	if body == "" {
		return []string{"no Sections and stage restrictions section"}
	}
	var fails []string
	for _, name := range planSections {
		if !strings.Contains(body, name) {
			fails = append(fails, fmt.Sprintf("section name %q is not listed", name))
		}
	}
	mentioned := func(text string) []string {
		var out []string
		for _, name := range planSections {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(text) {
				out = append(out, name)
			}
		}
		return out
	}
	for _, stage := range []string{"story", "technical", "acceptance"} {
		var line string
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, "- "+stage+" may select") {
				line = l
			}
		}
		if line == "" {
			fails = append(fails, fmt.Sprintf("no restriction line for stage %s", stage))
			continue
		}
		var docAllowed []string
		switch {
		case strings.Contains(line, "any section except"):
			_, after, _ := strings.Cut(line, "except")
			// Advice that follows the restriction ("; intent changes belong to
			// the story stage") is not part of what is excluded.
			after, _, _ = strings.Cut(after, ";")
			excluded := mentioned(after)
			for _, name := range planSections {
				if !slices.Contains(excluded, name) {
					docAllowed = append(docAllowed, name)
				}
			}
		case strings.Contains(line, "any section"):
			docAllowed = slices.Clone(planSections)
		default:
			docAllowed = mentioned(line)
		}
		var actual []string
		for _, name := range planSections {
			if len(graphexec.PlanSectionFaults(stage, []string{name})) == 0 {
				actual = append(actual, name)
			}
		}
		sort.Strings(docAllowed)
		sort.Strings(actual)
		if !slices.Equal(docAllowed, actual) {
			fails = append(fails, fmt.Sprintf("stage %s: document allows %v, validator allows %v", stage, docAllowed, actual))
		}
	}
	return fails
}

func TestPlanContractSectionRestrictionsMatchTheValidator(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	for _, f := range checkSectionRestrictions(doc) {
		t.Error(f)
	}
	for name, mutate := range map[string]func(string) string{
		"story loosened": func(d string) string {
			return strings.Replace(d, "- story may select intent, dependencies and conversation only.", "- story may select intent, architecture, dependencies and conversation only.", 1)
		},
		"technical loosened": func(d string) string { return strings.Replace(d, "except criteria and tasks", "except criteria", 1) },
		"acceptance narrowed": func(d string) string {
			return strings.Replace(d, "- acceptance may select any section.", "- acceptance may select any section except tasks.", 1)
		},
		"a section name lost":   func(d string) string { return strings.ReplaceAll(d, "guidance", "hints") },
		"restriction line lost": func(d string) string { return strings.Replace(d, "- technical may select", "- technical selects", 1) },
	} {
		if got := checkSectionRestrictions(mutate(doc)); len(got) == 0 {
			t.Errorf("%s: the checker accepted a document that disagrees with the validator", name)
		}
	}
}

type walkthrough struct{ ID, Text string }

func extractWalkthroughs(doc string) ([]walkthrough, []string) {
	var out []walkthrough
	var problems []string
	for _, m := range walkthroughFence.FindAllStringSubmatch(doc, -1) {
		out = append(out, walkthrough{ID: m[1], Text: m[2]})
	}
	if n := len(walkthroughLn.FindAllString(doc, -1)); n != len(out) {
		problems = append(problems, fmt.Sprintf("%d walkthrough markers but %d text fences extracted", n, len(out)))
	}
	return out, problems
}

// checkWalkthroughs verifies each required walkthrough exists by id and says the
// things a reader needs. Terms are matched on whitespace-normalised, lowercase
// text so a re-wrapped paragraph does not fail it.
func checkWalkthroughs(doc string) []string {
	walks, fails := extractWalkthroughs(doc)
	byID := map[string]string{}
	for _, w := range walks {
		byID[w.ID] = strings.ToLower(strings.Join(strings.Fields(w.Text), " "))
	}
	required := map[string][]string{
		"delivery":          {"communication_preflight", "communication_submit", "communication_receipt", "request_id", "confirming object"},
		"lost-reply":        {"communication_receipt", "do not change p", "same request identity", "identical, unchanged bytes", "do not resend"},
		"corrected-content": {"refused", "communication_preflight", "new request identity", "never resend the refused bytes"},
		"authority-changed": {"stale_revision", "stale_binding", "bytes are unchanged", "nothing is retained", "new request identity"},
	}
	for id, terms := range required {
		text, ok := byID[id]
		if !ok {
			fails = append(fails, fmt.Sprintf("walkthrough %q is missing", id))
			continue
		}
		for _, term := range terms {
			if !strings.Contains(text, term) {
				fails = append(fails, fmt.Sprintf("walkthrough %q does not mention %q", id, term))
			}
		}
	}
	// Order inside the delivery walkthrough is the contract: preflight, then
	// submit, then receipt, then the confirming object.
	if text := byID["delivery"]; text != "" {
		last := -1
		for _, term := range []string{"communication_preflight(p)", "communication_submit(p)", "communication_receipt(", "confirming object"} {
			i := strings.Index(text, term)
			if i < 0 || i < last {
				fails = append(fails, fmt.Sprintf("delivery walkthrough is out of order at %q", term))
			}
			last = max(last, i)
		}
	}
	return fails
}

// checkTerminalConsistencyOnly verifies the document states that terminal content
// is consistency evidence and never the source of application data.
func checkTerminalConsistencyOnly(doc string) []string {
	text := strings.ToLower(strings.Join(strings.Fields(doc), " "))
	var fails []string
	for _, term := range []string{
		"terminal content is consistency evidence only",
		"retained accepted result is the sole source",
		"a terminal object cannot replace a missing one",
		"one with no accepted submission, or with a different result, fails the attempt",
		"never supplies a result",
	} {
		if !strings.Contains(text, term) {
			fails = append(fails, fmt.Sprintf("the contract does not state %q", term))
		}
	}
	return fails
}

func TestPlanContractWalkthroughsAndTerminalRule(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	for _, f := range checkWalkthroughs(doc) {
		t.Error(f)
	}
	for _, f := range checkTerminalConsistencyOnly(doc) {
		t.Error(f)
	}
	walks, _ := extractWalkthroughs(doc)
	if len(walks) < 4 {
		t.Fatalf("%d walkthroughs", len(walks))
	}
}

// The document's own convention names the marker `<!-- walkthrough: id=ID -->`;
// the markers must follow it so a tool that relies on the stated form finds them.
func TestPlanContractWalkthroughMarkersFollowTheDocumentedConvention(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	if !strings.Contains(doc, "`<!-- walkthrough: id=ID -->`") {
		t.Skip("the document no longer states the id= form")
	}
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "<!-- walkthrough:") && !strings.HasPrefix(line, "<!-- walkthrough: id=") {
			t.Errorf("marker %q does not use the documented id=ID form", line)
		}
	}
}

func TestContractWalkthroughCheckerFailsOnGaps(t *testing.T) {
	t.Parallel()
	doc := planContractDoc(t)
	cases := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{"lost-reply drops the receipt", func(d string) string {
			return strings.Replace(d, "3. A matching receipt exists: the submission was received. Read its state and go on;\n   do not resend.\n4. No receipt exists: resend the identical, unchanged bytes of P with the same\n   request identity. Identical bytes never create a second result.", "3. Resend.", 1)
		}, `"lost-reply" does not mention`},
		{"corrected content keeps the old identity", func(d string) string {
			return regexp.MustCompile(`new\s+request\s+identity`).ReplaceAllString(d, "same request")
		}, `"corrected-content" does not mention "new request identity"`},
		{"authority walkthrough removed", func(d string) string {
			return strings.Replace(d, "<!-- walkthrough: id=authority-changed -->", "<!-- note -->", 1)
		}, `"authority-changed" is missing`},
		{"delivery lost", func(d string) string {
			return strings.Replace(d, "<!-- walkthrough: id=delivery -->\n```text", "<!-- walkthrough: id=delivery -->\n```json", 1)
		}, `walkthrough "delivery" is missing`},
		{"delivery order swapped", func(d string) string {
			return strings.Replace(strings.Replace(d, "communication_submit(P): the reply", "communication_receipt(P): the reply", 1), "3. communication_receipt({", "3. communication_submit(P) ({", 1)
		}, `delivery walkthrough is out of order`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mutated := tt.mutate(doc)
			if mutated == doc {
				t.Fatal("mutation did not change the document")
			}
			if got := strings.Join(checkWalkthroughs(mutated), "; "); !strings.Contains(got, tt.want) {
				t.Fatalf("failures %q do not contain %q", got, tt.want)
			}
		})
	}
	if got := checkTerminalConsistencyOnly(strings.ReplaceAll(doc, "consistency evidence only", "authoritative")); len(got) == 0 {
		t.Fatal("terminal-content checker accepted a document that dropped the rule")
	}
}

// ---- AC-020: every shipped workflow's result contract ----

// checkResultContract is the generic shared-route rule for a workflow's node
// result contract: at least one worked example and a description of the shared
// result route (preflight, submit and receipt).
func checkResultContract(content string) []string {
	var fails []string
	if !jsonFence.MatchString(content) {
		fails = append(fails, "has no worked example (json fence)")
	}
	lower := strings.ToLower(content)
	for _, term := range []string{"preflight", "submit", "receipt"} {
		if !strings.Contains(lower, term) {
			fails = append(fails, fmt.Sprintf("does not mention the shared result route step %q", term))
		}
	}
	return fails
}

// workflowResultContracts finds node-result.md files under a workflow package.
func workflowResultContracts(w shippedWorkflow) ([]string, error) {
	var out []string
	err := fs.WalkDir(w.FS, ".", func(name string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "node-result.md" {
			out = append(out, name)
		}
		return err
	})
	return out, err
}

func TestEveryShippedWorkflowResultContractDescribesTheSharedRoute(t *testing.T) {
	t.Parallel()
	workflows := discoverShippedWorkflows(t)
	checked := map[string]bool{}
	for _, w := range workflows {
		contracts, err := workflowResultContracts(w)
		if err != nil {
			t.Fatal(err)
		}
		if len(contracts) == 0 {
			t.Errorf("workflow %s ships no node-result.md: an agent workflow needs a result contract", w.Name)
		}
		for _, path := range contracts {
			raw, err := fs.ReadFile(w.FS, path)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range checkResultContract(string(raw)) {
				t.Errorf("%s/%s %s", w.Name, path, f)
			}
			checked[w.Name+"/"+path] = true
		}
	}
	for _, want := range []string{"plan/" + strings.TrimPrefix(planContractPath, "plan/"), "research/" + strings.TrimPrefix(researchContractPath, "research/")} {
		if !checked[want] {
			t.Errorf("contract %s was not discovered", want)
		}
	}
}

func TestResultContractCheckerFailsOnBadContracts(t *testing.T) {
	t.Parallel()
	good := "Deliver: preflight, then submit, then read the receipt.\n\n```json\n{\"result\":{}}\n```\n"
	if got := checkResultContract(good); len(got) != 0 {
		t.Fatalf("good contract refused: %v", got)
	}
	cases := []struct{ name, content, want string }{
		{"no example", strings.Replace(good, "```json", "```text", 1), "no worked example"},
		{"an inline brace is not an example", "preflight submit receipt {\"a\":1}", "no worked example"},
		{"no preflight", strings.Replace(good, "preflight", "check", 1), `"preflight"`},
		{"no submit", strings.Replace(good, "submit", "send", 1), `"submit"`},
		{"no receipt", strings.Replace(good, "receipt", "reply", 1), `"receipt"`},
		{"private terminal-only format", "Return this JSON as your final answer:\n```json\n{}\n```\n", `"preflight"`},
		{"empty", "", "no worked example"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(checkResultContract(tt.content), "; "); !strings.Contains(got, tt.want) {
				t.Fatalf("failures %q do not contain %q", got, tt.want)
			}
		})
	}
}

// A workflow package with agent roles and no node-result contract fails the walk.
func TestWorkflowWithoutResultContractIsDetected(t *testing.T) {
	t.Parallel()
	for _, w := range discoverShippedWorkflows(t) {
		contracts, err := workflowResultContracts(w)
		if err != nil || len(contracts) == 0 {
			t.Fatalf("%s: contracts=%v err=%v", w.Name, contracts, err)
		}
	}
	bare := shippedWorkflow{Name: "bare", FS: fstest.MapFS{"bare.dot": {Data: []byte("digraph b {}")}, "role.md": {Data: []byte("# Role")}}}
	if got, err := workflowResultContracts(bare); err != nil || len(got) != 0 {
		t.Fatalf("a package with no contract must report none: %v %v", got, err)
	}
	withOne := shippedWorkflow{Name: "one", FS: fstest.MapFS{"skills/x/node-result.md": {Data: []byte("x")}}}
	if got, err := workflowResultContracts(withOne); err != nil || len(got) != 1 {
		t.Fatalf("a nested contract must be discovered: %v %v", got, err)
	}
}
