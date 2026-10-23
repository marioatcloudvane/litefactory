package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

var standardsToolNames = []string{"standards_categories_list", "standards_select", "standards_read"}

func hasStandardsTool(tools []string) bool {
	for _, name := range standardsToolNames {
		if slices.Contains(tools, name) {
			return true
		}
	}
	return false
}

// An ordinary run gets no standards tools, no standards text in its prompt and
// no retained book: nothing is captured or asked of it.
func TestOrdinaryRunGetsNoStandardsToolsOrPreamble(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan", "validate"} {
		t.Run(mode, func(t *testing.T) {
			prep, db := lifecycleMCPPreparation(t, mode)
			publishRunStandards(t, db, "ordinary-"+mode, 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}}})
			prepared, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: mode, Invocation: agent.InvocationResearchNode}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := prepared.close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			}()
			if hasStandardsTool(spec.PermittedTools()) {
				t.Fatalf("ordinary %s run was given standards tools: %v", mode, spec.PermittedTools())
			}
			guidance, err := mcpGuidance(t.Context(), spec, prepared.service, nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(guidance), "standards") {
				t.Fatalf("ordinary %s run guidance mentions standards", mode)
			}
			if _, err := db.GetStandardsRunBinding(t.Context(), fmt.Sprint(prep.run.ID)); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("ordinary %s run retained a standards binding: %v", mode, err)
			}
		})
	}

	prep, _ := lifecycleMCPPreparation(t)
	var prompt string
	var tools []string
	prep.runner = lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		prompt = req.Prompt
		if req.MCPLaunch != nil {
			tools = req.MCPLaunch.PermittedTools()
		}
		return nil, nil
	})
	_, _ = runMCPBound(t.Context(), prep, agent.Request{Mode: "research", Prompt: "Investigate."}, nil)
	if prompt == "" {
		t.Fatal("provider was not launched")
	}
	if strings.Contains(strings.ToLower(prompt), "standards") || hasStandardsTool(tools) {
		t.Fatal("an ordinary launch carried a standards preamble or tools")
	}
}

// A granted role gets the one read-only tool, but no preamble and no gate: the
// book is pinned only when the role chooses to look, and an empty book is
// reported in the tool reply rather than stopping the run.
func TestGrantedRoleGetsOptionalStandardsTools(t *testing.T) {
	for _, mode := range []string{"plan", "validate", "implement"} {
		t.Run(mode, func(t *testing.T) {
			prep, db := lifecycleMCPPreparation(t, mode)
			prepared, spec, err := prepareMCPWith(t.Context(), prep, agent.Request{Mode: mode, Invocation: agent.InvocationResearchNode}, nil, nil, "story-worker")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := prepared.close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			}()
			for _, name := range standardsToolNames {
				if slices.Contains(spec.PermittedTools(), name) != (name == "standards_read") {
					t.Fatalf("granted %s role has the wrong standards tools: %v", mode, spec.PermittedTools())
				}
			}
			root := fmt.Sprint(prep.run.ID)
			if _, err := db.GetStandardsRunBinding(t.Context(), root); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("book was captured before the role asked: %v", err)
			}
			client := submission.Client{DescriptorPath: spec.DescriptorPath()}
			reply, err := client.Call(t.Context(), "standards_read", json.RawMessage(`{}`))
			if err != nil || reply.Error != "" {
				t.Fatalf("empty book refused the call: %v %s", err, reply.Error)
			}
			var page struct {
				Status standards.RuntimeStatus `json:"status"`
				Text   string                  `json:"text"`
			}
			if err := json.Unmarshal(reply.Result, &page); err != nil {
				t.Fatal(err)
			}
			if page.Status.Integrity == "verified" || page.Text != "" {
				t.Fatalf("empty book reported content: %+v", page)
			}
		})
	}

	prep, db := lifecycleMCPPreparation(t, "plan")
	publishRunStandards(t, db, "granted-book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "errors", Rule: "MUST wrap errors", Description: "Wrap errors with context.\n", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "design", Rule: "MUST name the seams", Description: "Name the seams.\n", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"plan-technical"}}},
	})
	prepared, spec, err := prepareMCPWith(t.Context(), prep, agent.Request{Mode: "plan", Invocation: agent.InvocationResearchNode}, nil, nil, "plan-technical")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	}()
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	reply, err := client.Call(t.Context(), "standards_read", json.RawMessage(`{}`))
	if err != nil || reply.Error != "" {
		t.Fatalf("read: %v %s", err, reply.Error)
	}
	var page standards.RolePage
	if err := json.Unmarshal(reply.Result, &page); err != nil {
		t.Fatal(err)
	}
	if page.Status.Integrity != "verified" || !slices.Equal(page.StandardIDs, []string{"design"}) || !strings.Contains(page.Text, "(design)") || strings.Contains(page.Text, "(errors)") {
		t.Fatalf("published book not offered by role: %+v", page)
	}
}

// The grant is declared in the DOT, for the story worker, the technical planner
// and the architecture reviewer only, in every canonical team source.
func TestStandardsGrantDeclaredOnlyForThreeRoles(t *testing.T) {
	want := map[string][]string{"implement": {"story-worker"}, "plan": {"plan-technical"}, "validate": {"validate-architecture"}}
	var graphs []string
	for _, root := range []string{"../teamsource", "../../teams/source"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".dot") {
				graphs = append(graphs, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(graphs) < 20 {
		t.Fatalf("found only %d graphs", len(graphs))
	}
	for _, path := range graphs {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		profile, err := graphexec.ParseProfile(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var granted []string
		for _, node := range profile.Nodes {
			roles := strings.Split(node.Attrs["lf_roles"], ",")
			roles = append(roles, node.Attrs["lf_agent_markdown"])
			for _, role := range roles {
				if graphexec.StandardsRole(node.Attrs, role) != "" {
					granted = append(granted, role)
				}
			}
		}
		mode := strings.TrimSuffix(filepath.Base(path), ".dot")
		if !slices.Equal(granted, want[mode]) {
			t.Fatalf("%s grants standards to %v, want %v", path, granted, want[mode])
		}
	}
}

// Go only honours the node's declaration: the pinned graph decides which agent
// node of a run gets the tools, and which reviewer of the shared review step.
func TestStandardsGrantFollowsPinnedNode(t *testing.T) {
	profileOf := func(mode string) *graphexec.Profile {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("../teamsource", mode, mode+".dot"))
		if err != nil {
			t.Fatal(err)
		}
		profile, err := graphexec.ParseProfile(raw)
		if err != nil {
			t.Fatal(err)
		}
		return profile
	}
	for mode, nodes := range map[string]map[string]bool{
		"implement": {"worker_agent": true, "sync_start_resolve": false, "sync_done_resolve": false},
		"plan":      {"technical_agent": true, "set_agent": false, "story_agent": false, "acceptance_agent": false},
		"research":  {"investigate": false, "shape": false},
		"bug":       {"analyst": false, "fixer": false},
	} {
		w := &researchWorkflow{definition: &teamsource.ResearchDefinition{Profile: profileOf(mode)}}
		for node, granted := range nodes {
			if _, ok := w.definition.Profile.Node(node); !ok {
				t.Fatalf("%s graph has no node %s", mode, node)
			}
			if got := w.nodeStandardsRole(&agent.Request{Graph: &agent.GraphInvocation{Node: node}}) != ""; got != granted {
				t.Fatalf("%s node %s granted=%v, want %v", mode, node, got, granted)
			}
		}
	}
	review, ok := profileOf("validate").Node("review")
	if !ok {
		t.Fatal("validate review node missing")
	}
	for role, granted := range map[string]bool{agent.ReviewRoleArchitecture: true, agent.ReviewRoleCode: false, agent.ReviewRoleAcceptance: false} {
		if got := graphexec.StandardsRole(review.Attrs, "validate-"+role) != ""; got != granted {
			t.Fatalf("validate %s reviewer granted=%v, want %v", role, got, granted)
		}
	}
}
