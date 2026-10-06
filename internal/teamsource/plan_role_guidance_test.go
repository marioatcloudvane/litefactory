package teamsource

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Story-002 AC-039, AC-015, AC-041: content-copy parity, the ask/done and
// request-recovery guidance in all three Plan roles, and the retained-result rule
// recorded in the project's constraints and context.

var planRoleFiles = []string{"plan-story.md", "plan-technical.md", "plan-acceptance.md", "plan-set.md"}

func squash(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// ---- AC-039: the source tree and the embedded copy are identical ----

const repoRoot = "../.."

func planSourceTree(t *testing.T) map[string][]byte {
	t.Helper()
	root := filepath.Join(repoRoot, "internal", "teamsource", "plan")
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func planEmbeddedTree(t *testing.T) map[string][]byte {
	t.Helper()
	sub, err := fs.Sub(planAssets, "plan")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	err = fs.WalkDir(sub, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(sub, name)
		out[name] = raw
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// compareTrees names every file that is missing from one side or differs.
func compareTrees(source, embedded map[string][]byte) []string {
	var problems []string
	names := map[string]bool{}
	for n := range source {
		names[n] = true
	}
	for n := range embedded {
		names[n] = true
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		s, inSource := source[n]
		e, inEmbedded := embedded[n]
		switch {
		case !inSource:
			problems = append(problems, n+" is embedded but has no source copy")
		case !inEmbedded:
			problems = append(problems, n+" has a source copy but is not embedded")
		case !bytes.Equal(s, e):
			problems = append(problems, n+" differs between source and embedded copies")
		}
	}
	return problems
}

func TestPlanSourceAndEmbeddedTreesAreByteIdentical(t *testing.T) {
	t.Parallel()
	source, embedded := planSourceTree(t), planEmbeddedTree(t)
	if len(embedded) < 12 {
		t.Fatalf("only %d embedded Plan files", len(embedded))
	}
	for _, p := range compareTrees(source, embedded) {
		t.Error(p)
	}
	// Roles and skills named by the guidance below are part of what is compared.
	for _, must := range append([]string{"plan.dot", "skills/plan-evidence/node-result.md", "skills/plan-evidence/SKILL.md"}, planRoleFiles...) {
		if _, ok := source[must]; !ok {
			t.Errorf("source tree lacks %s", must)
		}
	}
}

func TestCompareTreesReportsEveryKindOfDrift(t *testing.T) {
	t.Parallel()
	base := map[string][]byte{"a.md": []byte("a"), "skills/x/SKILL.md": []byte("x")}
	clone := func() map[string][]byte {
		c := map[string][]byte{}
		for k, v := range base {
			c[k] = append([]byte(nil), v...)
		}
		return c
	}
	if got := compareTrees(base, clone()); len(got) != 0 {
		t.Fatalf("identical trees differ: %v", got)
	}
	changed := clone()
	changed["a.md"] = []byte("a\n")
	extraSource := clone()
	extraSource["new.md"] = []byte("n")
	missing := clone()
	delete(missing, "skills/x/SKILL.md")
	for name, tt := range map[string]struct {
		source, embedded map[string][]byte
		want             string
	}{
		"one byte changed":       {base, changed, "a.md differs"},
		"source-only file":       {extraSource, base, "new.md has a source copy but is not embedded"},
		"embedded-only file":     {base, extraSource, "new.md is embedded but has no source copy"},
		"nested skill file lost": {base, missing, "skills/x/SKILL.md has a source copy but is not embedded"},
	} {
		if got := strings.Join(compareTrees(tt.source, tt.embedded), "; "); !strings.Contains(got, tt.want) {
			t.Errorf("%s: %q does not contain %q", name, got, tt.want)
		}
	}
}

// ---- AC-015: ask/done and request recovery in every Plan role ----

// planRoleObligations are the phrases each role must carry in the named section.
// They are matched on lowercase, whitespace-normalised text.
var planRoleObligations = []struct {
	section string
	terms   []string
}{
	{"How You Are Invoked", []string{"`ask` publishes your questions", "`done` carries", "only the factory-bound owner path promotes"}},
	{"Must Do", []string{
		// Shared route and same-attempt correction.
		"deliver through the shared result route", "preflight your result", "read every diagnostic and correct it in the same attempt", "submit, then read the matching receipt", "do it even when you have nothing to correct",
		// Lost-reply recovery and correction identity.
		"keep the request identity and the exact bytes you submitted if a submit reply is lost", "look up the receipt for that identity before any retry", "resend identical bytes only when no receipt exists",
		"the corrected content is a new request with a new identity", "never change accepted content just to force a retry",
		// The accepted submission is the result; nothing is printed (#410).
		"do not print a json result", "the accepted submission is this step's result", "terminal text is never read as a result",
	}},
	{"Must Not Do", []string{
		"do not return `approved`",
		"do not supply stage, candidate, request identity, upstream references or shared-decision revision", "supplying one is refused",
		"both a story request and a story set", "any legacy summary, context or stories fields", "do not attach a story branch to an ask",
		"do not treat a receipt as approval or publication", "it only acknowledges input",
	}},
	{"Output", []string{
		"preflight, submit and receipt only", "do not print a json result", "the factory derives `response` `ask` or `done` from the result you submitted",
		"`ask` carries an incomplete result, a non-blank reason and at least one shared question", "neither a story request nor a story set", "authorizes no story effect",
		"`done` carries a completed result, no questions and exactly one branch", "`approved` is never role output",
		"the accepted submission is the step's result", "an attempt without one fails", "terminal text is never read as a result",
	}},
}

// planRoleBannedTerms are the printed-result instructions a Plan role must no
// longer carry anywhere (#410).
var planRoleBannedTerms = []string{"strict json object", "final json object", "confirming object", "final object only confirms", "echoes the result the factory accepted"}

// checkPlanRoleGuidance reports every obligation a Plan role file does not meet.
func checkPlanRoleGuidance(content string) []string {
	names, bodies := roleSectionBodies(content)
	sections := map[string]string{}
	for i, n := range names {
		sections[n] = squash(bodies[i])
	}
	var fails []string
	for _, o := range planRoleObligations {
		body, ok := sections[o.section]
		if !ok {
			fails = append(fails, fmt.Sprintf("no %s section", o.section))
			continue
		}
		for _, term := range o.terms {
			if !strings.Contains(body, term) {
				fails = append(fails, fmt.Sprintf("%s does not say %q", o.section, term))
			}
		}
	}
	whole := squash(content)
	for _, banned := range planRoleBannedTerms {
		if strings.Contains(whole, banned) {
			fails = append(fails, fmt.Sprintf("role still asks for a printed result: %q", banned))
		}
	}
	// A role may name approved only to refuse it: it is never a value to return.
	out := sections["Output"]
	if strings.Contains(strings.ReplaceAll(out, "`approved` is never role output", ""), "approved") {
		fails = append(fails, "Output mentions approved other than to refuse it")
	}
	return fails
}

func TestPlanRolesCarryAskDoneAndRecoveryGuidance(t *testing.T) {
	t.Parallel()
	for _, file := range planRoleFiles {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			raw, err := planAssets.ReadFile("plan/" + file)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range checkPlanRoleGuidance(string(raw)) {
				t.Error(f)
			}
		})
	}
}

// Both copies of every Plan role and of the node-result skill direct the agent
// to the shared result route only and never ask for a printed JSON result (#410).
// The byte-identity test covers parity; this reads the source copy directly so a
// regression in either tree names its own file.
func TestPlanGuidanceNeverAsksForAPrintedResultInEitherCopy(t *testing.T) {
	t.Parallel()
	source, embedded := planSourceTree(t), planEmbeddedTree(t)
	for tree, files := range map[string]map[string][]byte{"internal/teamsource/plan-files": source, "internal/teamsource/plan": embedded} {
		for _, name := range append([]string{"skills/plan-evidence/node-result.md"}, planRoleFiles...) {
			raw, ok := files[name]
			if !ok {
				t.Errorf("%s lacks %s", tree, name)
				continue
			}
			text := squash(string(raw))
			for _, banned := range planRoleBannedTerms {
				if strings.Contains(text, banned) {
					t.Errorf("%s/%s still asks for a printed result: %q", tree, name, banned)
				}
			}
			for _, want := range []string{"do not print a json result", "preflight", "submit", "receipt"} {
				if !strings.Contains(text, want) {
					t.Errorf("%s/%s does not say %q", tree, name, want)
				}
			}
		}
	}
}

// Each role's branch for done is the one the contract gives that stage.
func TestPlanRolesNameTheirStageBranchAndForbiddenContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file, section string
		terms         []string
	}{
		{"plan-story.md", "Output", []string{"story request: create, update or retire the selected story", "omits architecture, design, guidance, tasks and criteria"}},
		{"plan-technical.md", "Output", []string{"story request with the draft, its architecture", "never tasks or criteria", "`expected_revision` and `changed_sections`"}},
		{"plan-acceptance.md", "Output", []string{"story request whose draft adds criteria", "tasks", "`expected_revision` and `changed_sections`"}},
		{"plan-story.md", "Must Not Do", []string{"never write architecture, tasks, criteria"}},
		{"plan-technical.md", "Must Not Do", []string{"do not author tasks or criteria"}},
		{"plan-acceptance.md", "Must Not Do", []string{"do not add a fourth stage"}},
	}
	for _, tt := range cases {
		t.Run(tt.file+"/"+tt.section, func(t *testing.T) {
			t.Parallel()
			raw, err := planAssets.ReadFile("plan/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			names, bodies := roleSectionBodies(string(raw))
			for i, n := range names {
				if n != tt.section {
					continue
				}
				body := squash(bodies[i])
				for _, term := range tt.terms {
					if !strings.Contains(body, term) {
						t.Errorf("%s does not say %q", tt.section, term)
					}
				}
			}
		})
	}
}

// The contract states the same rule from the factory's side: five members are
// never supplied and are refused by name.
func TestPlanContractNamesTheFactoryProvidedFields(t *testing.T) {
	t.Parallel()
	text := squash(planContractDoc(t))
	for _, term := range []string{
		"supplying any of stage, candidate, request_id, upstream or shared_revision is refused by name",
		"the factory fills in the stage, candidate, request identity, upstream references and shared-decision revision",
		"a receipt only acknowledges input", "acknowledgment is not acceptance",
		"new request identity",
	} {
		if !strings.Contains(text, term) {
			t.Errorf("contract does not say %q", term)
		}
	}
}

func TestPlanRoleGuidanceCheckerCatchesDrift(t *testing.T) {
	t.Parallel()
	raw, err := planAssets.ReadFile("plan/plan-story.md")
	if err != nil {
		t.Fatal(err)
	}
	role := string(raw)
	if got := checkPlanRoleGuidance(role); len(got) != 0 {
		t.Fatalf("shipped role fails: %v", got)
	}
	cases := []struct{ name, old, new, want string }{
		{"receipt lookup dropped", "Look up the receipt for that identity before any retry.", "", "look up the receipt for that identity before any retry"},
		{"same-attempt correction dropped", "correct it in the same attempt", "correct it later", "correct it in the same attempt"},
		{"identity reuse on corrected content", "the corrected content is a new request with a new identity", "the corrected content reuses the request", "new request with a new identity"},
		{"factory fields become authorable", "Do not supply stage, candidate, request identity, upstream references or shared-decision revision.", "Supply stage and candidate.", "do not supply stage, candidate"},
		{"approved becomes allowed", "`approved` is never role output.", "`approved` is allowed when the owner agreed.", "approved"},
		{"ask may carry a branch", "It carries neither a story request nor a story set, and it authorizes no story effect.", "It may carry a story request.", "neither a story request nor a story set"},
		{"done may carry two branches", "exactly one branch", "one or two branches", "exactly one branch"},
		{"receipt treated as approval", "Do not treat a receipt as approval or publication. It only acknowledges input.", "", "do not treat a receipt as approval"},
		{"printed result asked for again", "Deliver your result through preflight, submit and receipt only, and do not print a JSON result.", "Deliver your result through preflight, submit and receipt first. Then return one strict JSON object.", "strict json object"},
		{"no-print rule dropped", " - Do not print a JSON result. The accepted submission is this step's result", " - The accepted submission is this step's result", "do not print a json result"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mutated := strings.Replace(role, tt.old, tt.new, 1)
			if mutated == role {
				t.Fatalf("mutation target %q not found in the shipped role", tt.old)
			}
			if got := strings.Join(checkPlanRoleGuidance(mutated), "; "); !strings.Contains(got, tt.want) {
				t.Fatalf("failures %q do not contain %q", got, tt.want)
			}
		})
	}
}

// ---- AC-041: the retained-result rule in the project's records ----

// projectRecord reads a .aiteam file, skipping cleanly where the checkout has none:
// they are project records, not part of the shipped product.
func projectRecord(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".aiteam", name))
	if os.IsNotExist(err) {
		t.Skipf(".aiteam/%s is not present in this checkout", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func linesWith(doc, marker string) []string {
	var out []string
	for _, l := range strings.Split(doc, "\n") {
		if strings.Contains(l, marker) {
			out = append(out, l)
		}
	}
	return out
}

func missingTerms(text string, terms []string) []string {
	text = squash(text)
	var out []string
	for _, term := range terms {
		if !strings.Contains(text, squash(term)) {
			out = append(out, term)
		}
	}
	return out
}

var retainedRuleConstraintTerms = []string{
	"every mode and graph-node application result must use the shared communication route",
	"communication_preflight", "communication_submit", "communication_receipt", "turn.AgentResultService",
	"terminal text must not be a source of application data or of effect inputs",
	"no missing result falls back to it",
	"terminal-envelope parsing may only check completion and consistency against the retained accepted result",
	"a terminal-only result and one that differs from the retained result must each fail with their own cause",
	"a nominal decoder must not satisfy this rule",
	"WorkflowPolicy.Admit", "RegisteredWorkflowPolicies", "acceptRetained",
	"research and graph plan", "planned bug workflow", "graph implement, validate and spike",
	"must not ship a private result protocol or a terminal-parsing path",
	"compatibility routes are preserved", "legacy plan (v1 full-body proposals)", "submission-v1 scopes", "unmanaged implement and validate",
	"held back",
}

func TestConstraintsRecordTheRetainedResultRule(t *testing.T) {
	t.Parallel()
	doc := projectRecord(t, "APP_CONSTRAINTS.md")
	rule := linesWith(doc, "**CONC-24")
	if len(rule) != 1 {
		t.Fatalf("expected exactly one CONC-24 rule, found %d", len(rule))
	}
	if !strings.Contains(rule[0], "[enforced]") {
		t.Error("CONC-24 is not marked enforced")
	}
	if got := missingTerms(rule[0], retainedRuleConstraintTerms); len(got) != 0 {
		t.Errorf("CONC-24 does not state %q", got)
	}
	// The rule's evidence table names test files that exist.
	for _, row := range linesWith(doc, "(CONC-24)") {
		for _, file := range []string{"workflow_registry_test.go", "plan_graph_retained_test.go", "plan_graph_lost_reply_test.go", "workflow_admission_gap_test.go", "workflow_registry_structure_test.go"} {
			if !strings.Contains(row, file) {
				continue
			}
			if _, err := os.Stat(filepath.Join(repoRoot, "internal", "turn", file)); err != nil {
				t.Errorf("CONC-24 evidence names %s, which does not exist: %v", file, err)
			}
		}
	}
	if len(linesWith(doc, "(CONC-24)")) == 0 {
		t.Error("no evidence row cites CONC-24")
	}
}

func TestContextRecordsTheRetainedResultCapability(t *testing.T) {
	t.Parallel()
	doc := projectRecord(t, "APP_CONTEXT.md")
	rows := linesWith(doc, "return, validate and retain an agent's mode or graph-node result")
	if len(rows) != 1 {
		t.Fatalf("expected one capability row, found %d", len(rows))
	}
	terms := []string{
		"communication_preflight", "communication_submit", "communication_receipt", "turn.AgentResultService",
		"agentresult", "never parse terminal text for application data or effect inputs",
		"WorkflowPolicy.Admit", "RegisteredWorkflowPolicies", "AgentActivity.acceptRetained",
		"only to confirm completion/consistency", "research and graph plan", "bug and graph implement/validate/spike", "conc-24",
		"compatibility routes", "legacy plan v1", "submission-v1 scopes", "unmanaged implement/validate", "held back",
	}
	if got := missingTerms(rows[0], terms); len(got) != 0 {
		t.Errorf("capability row does not state %q", got)
	}
	// The owning component paths in the row exist.
	for _, path := range []string{"turn/agent_results.go", "agentresult/plan_graph.go", "turn/workflow_policy.go", "turn/workflow_registry.go", "turn/research_agent.go"} {
		if !strings.Contains(rows[0], path) {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "internal", filepath.FromSlash(path))); err != nil {
			t.Errorf("capability row names %s, which does not exist", path)
		}
	}
}

func TestRetainedResultRecordCheckerCatchesOmissions(t *testing.T) {
	t.Parallel()
	rule := "Every mode and graph-node application result MUST use the shared communication route (`communication_preflight`, `communication_submit`, `communication_receipt`, `turn.AgentResultService`). Terminal text MUST NOT be a source of application data or of effect inputs, and no missing result falls back to it."
	if got := missingTerms(rule, retainedRuleConstraintTerms[:7]); len(got) != 0 {
		t.Fatalf("baseline missing %v", got)
	}
	for name, mutated := range map[string]string{
		"fallback allowed":   strings.Replace(rule, "no missing result falls back to it", "a missing result falls back to terminal text", 1),
		"terminal a source":  strings.Replace(rule, "MUST NOT be a source", "MAY be a source", 1),
		"receipt route lost": strings.Replace(rule, "`communication_receipt`, ", "", 1),
		"owner unnamed":      strings.Replace(rule, "`turn.AgentResultService`", "the service", 1),
	} {
		if len(missingTerms(mutated, retainedRuleConstraintTerms[:7])) == 0 {
			t.Errorf("%s: omission was not detected", name)
		}
	}
}
