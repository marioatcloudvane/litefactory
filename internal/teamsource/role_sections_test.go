package teamsource

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The nine-section guard (story-002 AC-013, AC-019). It starts from the shipped
// graph definitions, reads which roles their agent entries name, and checks the
// section structure of exactly those roles. A workflow added later is covered
// because the walk is driven by the graph files, not by a list of role names.

// roleSectionOrder is the required level-1 heading order of every role that a
// shipped graph invokes. A role may append further sections (Plan technical adds
// Skills); the nine must appear once each, in this order.
var roleSectionOrder = []string{
	"Role", "Voice", "Goal", "How You Are Invoked", "When You Are Done",
	"Must Do", "Must Not Do", "Writing the Report", "Output",
}

const (
	// minSectionRunes and minSectionWords make a heading with a stub body
	// ("TODO", "TBD") fail while every shipped section, the shortest of which is
	// a single sentence, passes.
	minSectionRunes = 40
	minSectionWords = 7
)

// shippedWorkflow is one embedded workflow package.
type shippedWorkflow struct {
	Name string // directory under internal/teamsource
	FS   fs.FS  // the embedded bytes that ship
	Dot  string // graph definition file inside FS
}

// embeddedWorkflowRoots maps every workflow directory to the embed.FS that ships
// it. A new workflow directory with a graph but no entry here fails the walk, so
// it cannot ship uncovered.
func embeddedWorkflowRoots() map[string]fs.FS {
	return map[string]fs.FS{"plan": planAssets, "research": researchAssets, "bug": bugAssets, "spike": spikeAssets}
}

// discoverShippedWorkflows finds workflow packages from the source layout (any
// directory of this package holding a .dot graph), then resolves each to its
// embedded bytes.
func discoverShippedWorkflows(t *testing.T) []shippedWorkflow {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	roots := embeddedWorkflowRoots()
	var out []shippedWorkflow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dots, err := fs.Glob(os.DirFS(e.Name()), "*.dot")
		if err != nil {
			t.Fatal(err)
		}
		if len(dots) == 0 {
			continue
		}
		root, ok := roots[e.Name()]
		if !ok {
			t.Errorf("workflow directory %q has a graph definition but is not embedded: add it to embeddedWorkflowRoots", e.Name())
			continue
		}
		sub, err := fs.Sub(root, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, dot := range dots {
			out = append(out, shippedWorkflow{Name: e.Name(), FS: sub, Dot: dot})
		}
	}
	for name := range roots {
		found := false
		for _, w := range out {
			found = found || w.Name == name
		}
		if !found {
			t.Errorf("embedded workflow %q has no graph definition on disk", name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// dotAgentRoles returns the role file stems named by the agent entries of a
// graph definition: every node that declares lf_agent_markdown. Roles come from
// the graph's own attributes, never from a naming convention.
func dotAgentRoles(dot string) []string {
	seen := map[string]bool{}
	var roles []string
	for _, match := range researchDotNode.FindAllStringSubmatch(dot, -1) {
		attrs := map[string]string{}
		for _, attr := range researchDotAttr.FindAllStringSubmatch(match[2], -1) {
			attrs[attr[1]] = attr[2]
		}
		role := attrs["lf_agent_markdown"]
		if role == "" && attrs["lf_action"] != "lf_agent" {
			continue
		}
		if role != "" && !seen[role] {
			seen[role] = true
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	return roles
}

var roleHeading = regexp.MustCompile(`^# (\S.*?)\s*$`)

// roleSectionBodies splits a role file into its level-1 sections, in order.
// Front matter is skipped and lines inside a fenced block never open a section,
// so an example that contains "# Voice" is not mistaken for a heading. A heading
// repeated in the file appears repeatedly in names.
func roleSectionBodies(content string) (names []string, bodies []string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if strings.HasPrefix(content, "---\n") {
		if _, rest, ok := strings.Cut(content[4:], "\n---\n"); ok {
			content = rest
		}
	}
	var cur []string
	inFence := false
	flush := func() {
		if len(names) > 0 {
			bodies = append(bodies, strings.TrimSpace(strings.Join(cur, "\n")))
		}
		cur = nil
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence {
			if m := roleHeading.FindStringSubmatch(line); m != nil {
				flush()
				names = append(names, m[1])
				continue
			}
		}
		cur = append(cur, line)
	}
	flush()
	return names, bodies
}

// checkRoleSections reports every way a role file departs from the nine-section
// structure. The nine must each appear exactly once, in order, each with a
// non-trivial body; extra sections are allowed anywhere after Role.
func checkRoleSections(content string) []string {
	names, bodies := roleSectionBodies(content)
	var problems []string
	index := map[string]int{}
	for i, n := range names {
		if _, dup := index[n]; dup {
			for _, want := range roleSectionOrder {
				if want == n {
					problems = append(problems, fmt.Sprintf("section %q appears more than once", n))
				}
			}
			continue
		}
		index[n] = i
	}
	last := -1
	for _, want := range roleSectionOrder {
		i, ok := index[want]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing section %q", want))
			continue
		}
		if i < last {
			problems = append(problems, fmt.Sprintf("section %q is out of order", want))
		}
		last = max(last, i)
		body := bodies[i]
		if len([]rune(body)) < minSectionRunes || len(strings.Fields(body)) < minSectionWords {
			problems = append(problems, fmt.Sprintf("section %q is empty or a stub (%d characters)", want, len([]rune(body))))
		}
	}
	return problems
}

// TestShippedWorkflowRolesHaveTheNineSections walks every shipped graph.
func TestShippedWorkflowRolesHaveTheNineSections(t *testing.T) {
	t.Parallel()
	workflows := discoverShippedWorkflows(t)
	if len(workflows) < 2 {
		t.Fatalf("expected the Research and Plan graphs, found %d workflows", len(workflows))
	}
	roleCount := map[string]int{}
	for _, w := range workflows {
		t.Run(w.Name, func(t *testing.T) {
			t.Parallel()
			dot, err := fs.ReadFile(w.FS, w.Dot)
			if err != nil {
				t.Fatal(err)
			}
			roles := dotAgentRoles(string(dot))
			if len(roles) == 0 && w.Name == "bug" && w.Dot == "bug_tests.dot" {
				// The sole approved test child executes factory checks, never agents.
				return
			}
			if len(roles) == 0 {
				t.Fatalf("%s/%s names no agent role: the guard would pass vacuously", w.Name, w.Dot)
			}
			for _, role := range roles {
				raw, err := fs.ReadFile(w.FS, role+".md")
				if err != nil {
					t.Errorf("%s: graph names role %q but its file is not shipped: %v", w.Name, role, err)
					continue
				}
				for _, problem := range checkRoleSections(string(raw)) {
					t.Errorf("%s/%s.md: %s", w.Name, role, problem)
				}
			}
		})
		dot, _ := fs.ReadFile(w.FS, w.Dot)
		for _, role := range dotAgentRoles(string(dot)) {
			roleCount[w.Name+"/"+role]++
		}
	}
	for _, want := range []string{"research/research", "research/shaping", "plan/plan-story", "plan/plan-technical", "plan/plan-acceptance", "bug/bug-analyst", "bug/bug-fixer", "spike/spike-investigator", "spike/spike-fixer"} {
		if roleCount[want] == 0 {
			t.Errorf("shipped graphs no longer name role %s", want)
		}
	}
}

// The technical role's extra Skills section is a tenth section and is allowed;
// it must come after the nine and does not disturb them.
func TestPlanTechnicalRoleKeepsSkillsAsTenthSection(t *testing.T) {
	t.Parallel()
	raw, err := planAssets.ReadFile("plan/plan-technical.md")
	if err != nil {
		t.Fatal(err)
	}
	names, _ := roleSectionBodies(string(raw))
	if len(names) != 10 || names[9] != "Skills" {
		t.Fatalf("headings=%v", names)
	}
	if got := checkRoleSections(string(raw)); len(got) != 0 {
		t.Fatalf("Skills section broke the nine-section check: %v", got)
	}
}

func TestDotAgentRolesComeFromGraphAttributesNotNames(t *testing.T) {
	t.Parallel()
	dot := `digraph x {
 graph [team="t", start="a"];
 a [lf_action="lf_agent", lf_agent_markdown="odd-name"];
 b [lf_action="lf_agent", lf_agent_markdown="another"];
 c [lf_action="lf_ask", lf_kind="question"];
 d [lf_action="lf_agent", lf_agent_markdown="odd-name"];
 a -> b [lf_on="success"];
 role_named_like_an_agent [lf_terminal="true"];
}`
	got := strings.Join(dotAgentRoles(dot), ",")
	if got != "another,odd-name" {
		t.Fatalf("roles=%q", got)
	}
}

func goodRole() string {
	body := func(title string) string {
		return "# " + title + "\n\nThis section carries a full sentence describing the " + strings.ToLower(title) + " content of the role.\n\n"
	}
	var b strings.Builder
	b.WriteString("---\nname: r\ntier: balanced\n---\n")
	for _, s := range roleSectionOrder {
		b.WriteString(body(s))
	}
	return b.String()
}

// The checker is itself tested: a guard that cannot fail proves nothing.
func TestCheckRoleSectionsCatchesDefects(t *testing.T) {
	t.Parallel()
	replaceSection := func(content, title, with string) string {
		start := strings.Index(content, "# "+title+"\n")
		end := strings.Index(content[start+1:], "\n# ")
		if end < 0 {
			return content[:start] + with
		}
		return content[:start] + with + content[start+1+end+1:]
	}
	cases := []struct {
		name    string
		content string
		want    string // substring of a problem; empty means no problem
	}{
		{"complete role/no problem", goodRole(), ""},
		{"extra tenth section/allowed", goodRole() + "# Skills\n\nSelect the applicable guidance entries from the actual story artifacts.\n", ""},
		{"extra section in the middle/allowed", strings.Replace(goodRole(), "# Goal\n", "# Extra\n\nAnother section with plenty of words to be considered content.\n\n# Goal\n", 1), ""},
		{"missing section/reported", replaceSection(goodRole(), "Voice", ""), `missing section "Voice"`},
		{"missing last section/reported", replaceSection(goodRole(), "Output", ""), `missing section "Output"`},
		{"swapped sections/out of order", strings.Replace(strings.Replace(strings.Replace(goodRole(), "# Must Do\n", "# @@\n", 1), "# Must Not Do\n", "# Must Do\n", 1), "# @@\n", "# Must Not Do\n", 1), "out of order"},
		{"empty body/stub", replaceSection(goodRole(), "Goal", "# Goal\n\n"), `section "Goal" is empty`},
		{"one-word body/stub", replaceSection(goodRole(), "Goal", "# Goal\n\nTODO\n\n"), `section "Goal" is empty`},
		{"short sentence/stub", replaceSection(goodRole(), "Goal", "# Goal\n\nBe helpful.\n\n"), `section "Goal" is empty`},
		{"duplicate heading/reported", goodRole() + "# Goal\n\nA second goal section repeating the heading with enough words.\n", `"Goal" appears more than once`},
		{"level-2 heading/is not a section", strings.Replace(goodRole(), "# Voice\n", "## Voice\n", 1), `missing section "Voice"`},
		{"heading only inside a fence/is not a section", strings.Replace(goodRole(), "# Voice\n", "```\n# Voice\n```\n", 1), `missing section "Voice"`},
		{"empty file/every section missing", "", `missing section "Role"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := strings.Join(checkRoleSections(tt.content), "; ")
			if tt.want == "" {
				if got != "" {
					t.Fatalf("unexpected problems: %s", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("problems %q do not contain %q", got, tt.want)
			}
		})
	}
}

// A workflow directory with a graph that is not embedded is an error, so a new
// workflow cannot ship outside the guard.
func TestEmbeddedWorkflowRootsCoverTheShippedPackages(t *testing.T) {
	t.Parallel()
	roots := embeddedWorkflowRoots()
	for _, name := range []string{"plan", "research"} {
		if roots[name] == nil {
			t.Fatalf("no embedded root for %s", name)
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if dots, _ := fs.Glob(os.DirFS(e.Name()), "*.dot"); len(dots) > 0 && roots[e.Name()] == nil {
			t.Errorf("%s ships a graph but has no embedded root", e.Name())
		}
	}
}
