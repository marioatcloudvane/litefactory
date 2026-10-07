package teamsource

import (
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
)

// The shipped-role guard starts from the shipped graph definitions, reads which
// roles their agent entries name, and checks that each of those roles ships a
// file. A workflow added later is covered because the walk is driven by the
// graph files, not by a list of role names.

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
	return map[string]fs.FS{"plan": planAssets, "research": researchAssets, "bug": bugAssets, "spike": spikeAssets, "implement": implementAssets, "validate": validateAssets}
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
		if roleCount[want] == 0 {
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
