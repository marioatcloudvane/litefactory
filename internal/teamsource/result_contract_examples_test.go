package teamsource

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// The Plan result contract's worked examples are executable (story-002 AC-040,
// AC-017, AC-020). Every marked example is extracted from node-result.md and run
// through the same validator preflight and submit reach, agentresult.Create over
// a factory-bound Context, so a broken example makes the test fail.

var (
	exampleMarked   = regexp.MustCompile("(?m)^<!-- example: (positive|negative) ([^>]*?) -->\n```json\n((?s:.*?))\n```")
	exampleMarkerLn = regexp.MustCompile(`(?m)^<!-- example:`)
	markerAttr      = regexp.MustCompile(`(\w+)=(\S+)`)
)

const planContractPath = "plan/skills/plan-evidence/node-result.md"

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
		// The factory derives the step's response from the accepted result alone
		// (#410): incomplete with questions is ask, completed without is done. The
		// marker's response must be the one the factory would derive.
		derived := ""
		switch {
		case result.Outcome == "incomplete" && len(result.Questions) > 0:
			derived = "ask"
		case result.Outcome == "completed" && len(result.Questions) == 0:
			derived = "done"
		}
		if derived != ex.Attrs["response"] {
			fails = append(fails, fmt.Sprintf("positive %q: the factory derives response %q, marker says %q", ex.ID, derived, ex.Attrs["response"]))
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
