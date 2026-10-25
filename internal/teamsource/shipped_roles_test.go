package teamsource

import (
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// The shipped-role guard starts from the shipped graph definitions, reads which
// roles their agent entries name, and checks that each of those roles ships a
// file. A workflow added later is covered because the walk is driven by the
// graph files, not by a list of role names.

// shippedTeams are the team sources whose workflows the guard covers.
var shippedTeams = []string{goTeam, "python-fastapi", "swift-ios"}

// shippedWorkflow is one resolved workflow package of a team source.
type shippedWorkflow struct {
	Name string // team/workflow
	FS   fs.FS  // the resolved bytes of the workflow folder
	Dot  string // graph definition file inside FS
}

// discoverShippedWorkflows finds workflow packages from the resolved team files
// (any top-level folder holding a .dot graph) of every shipped team.
func discoverShippedWorkflows(t *testing.T) []shippedWorkflow {
	t.Helper()
	var out []shippedWorkflow
	for _, team := range shippedTeams {
		files := teamFiles(t, team)
		dirs := map[string][]string{}
		for name := range files {
			dir, file, ok := strings.Cut(name, "/")
			if ok && !strings.Contains(file, "/") && strings.HasSuffix(file, ".dot") {
				dirs[dir] = append(dirs[dir], file)
			}
		}
		if len(dirs) == 0 {
			t.Errorf("team %s has no workflow with a graph definition", team)
		}
		for dir, dots := range dirs {
			for _, dot := range dots {
				out = append(out, shippedWorkflow{Name: team + "/" + dir, FS: teamWorkflowFS(t, team, dir), Dot: dot})
			}
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
		// A step that starts agents itself names them in lf_roles.
		for _, named := range strings.Split(attrs["lf_roles"], ",") {
			if named = strings.TrimSpace(named); named != "" && !seen[named] {
				seen[named] = true
				roles = append(roles, named)
			}
		}
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

// TestShippedWorkflowRolesHaveShippedFiles walks every shipped graph.
func TestShippedWorkflowRolesHaveShippedFiles(t *testing.T) {
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
			if len(roles) == 0 {
				t.Fatalf("%s/%s names no agent role: the guard would pass vacuously", w.Name, w.Dot)
			}
			for _, role := range roles {
				if _, err := fs.ReadFile(w.FS, role+".md"); err != nil {
					t.Errorf("%s: graph names role %q but its file is not shipped: %v", w.Name, role, err)
				}
			}
		})
		dot, _ := fs.ReadFile(w.FS, w.Dot)
		for _, role := range dotAgentRoles(string(dot)) {
			roleCount[w.Name+"/"+role]++
		}
	}
	for _, want := range []string{"research/research", "research/shaping", "plan/plan-story", "plan/plan-technical", "plan/plan-acceptance", "bug/bug-analyst", "bug/bug-fixer", "spike/spike-investigator", "spike/spike-fixer", "implement/story-worker", "validate/validate-repair", "validate/validate-acceptance", "validate/validate-code", "validate/validate-architecture"} {
		if roleCount[goTeam+"/"+want] == 0 {
			t.Errorf("shipped graphs no longer name role %s", want)
		}
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

// Every shipped team covers the six workflows, so a workflow cannot ship
// outside the guard.
func TestShippedTeamsCoverEveryWorkflow(t *testing.T) {
	t.Parallel()
	covered := map[string]bool{}
	for _, w := range discoverShippedWorkflows(t) {
		covered[w.Name] = true
	}
	for _, team := range shippedTeams {
		for _, name := range []string{"plan", "research", "bug", "spike", "implement", "validate"} {
			if !covered[team+"/"+name] {
				t.Errorf("%s/%s ships no graph", team, name)
			}
		}
	}
}
