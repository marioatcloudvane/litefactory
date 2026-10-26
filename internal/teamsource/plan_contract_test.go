package teamsource

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

func planPackageRegistry(t *testing.T) graphexec.Registry {
	t.Helper()
	var r graphexec.Registry
	policy := graphexec.PlanControlPolicy()
	specs := []graphexec.ActionSpec{
		{Name: "lf_agent", Mode: agent.ModePlan, Outcomes: []string{"success", "failed"}},
		graphexec.ControlSpec("lf_if", policy), graphexec.ControlSpec("lf_loop", policy),
		{Name: "lf_ask", Mode: agent.ModePlan, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "review"}}}},
		{Name: "lf_comment", Mode: agent.ModePlan, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"failure", "final"}}}}, graphexec.PlanStorySpec(), graphexec.PlanGateSpec(),
	}
	for _, s := range specs {
		if err := r.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func planPackageFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	source, err := fs.Sub(teamFS(t, goTeam), "plan")
	if err != nil {
		t.Fatal(err)
	}
	result := fstest.MapFS{}
	err = fs.WalkDir(source, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		result[name] = &fstest.MapFile{Data: raw}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPlanPackageClosureAndDigest(t *testing.T) {
	t.Parallel()
	definition, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	agents := 0
	for _, node := range definition.Profile.Nodes {
		if node.Action == "lf_agent" {
			agents++
		}
	}
	if agents == 0 || len(definition.Nodes) != agents {
		t.Fatalf("closure: %d roles for %d agent nodes", len(definition.Nodes), agents)
	}
	fixture := planPackageFixture(t)
	for name := range fixture {
		if strings.HasPrefix(name, "skills/plan-go-guidance/") || strings.HasPrefix(name, "skills/plan-agent-setup-guidance/") || strings.HasPrefix(name, "skills/owner-report/") {
			if _, captured := definition.Files[name]; captured {
				t.Fatal("unselected stack guidance leaked into neutral graph capture", name)
			}
			continue
		}
		if _, captured := definition.Files[name]; !captured {
			t.Fatal("required embedded resource outside graph closure", name)
		}
	}
	var keys []string
	for name, raw := range definition.Files {
		keys = append(keys, name)
		if !bytes.Equal(raw, fixture[name].Data) {
			t.Fatalf("captured bytes differ for %s", name)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(raw))
		if definition.Hashes[name] != want {
			t.Fatalf("hash missing/wrong for %s", name)
		}
	}
	sort.Strings(keys)
	var manifest strings.Builder
	for _, name := range keys {
		fmt.Fprintf(&manifest, "%s\x00%s\n", name, definition.Hashes[name])
	}
	if definition.Digest != fmt.Sprintf("%x", sha256.Sum256([]byte(manifest.String()))) {
		t.Fatal("digest does not cover all resource hashes")
	}
	changed := planPackageFixture(t)
	changed["skills/plan-evidence/projection.md"].Data = append(changed["skills/plan-evidence/projection.md"].Data, []byte("\nScoped clarification.\n")...)
	next, err := CaptureWorkflow(t.Context(), changed, "plan.dot", "go-development", agent.ModePlan, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if next.Digest == definition.Digest {
		t.Fatal("changed transitive resource did not alter digest")
	}
}

func TestPlanPackageRejectsInvalidClosure(t *testing.T) {
	cases := []struct {
		name   string
		change func(fstest.MapFS)
	}{
		{"missing role", func(f fstest.MapFS) { delete(f, "plan-story.md") }},
		{"missing shared result", func(f fstest.MapFS) { delete(f, "skills/plan-evidence/node-result.md") }},
		{"missing required technical skill", func(f fstest.MapFS) { delete(f, "skills/plan-technical-shaping/SKILL.md") }},
		{"resource traversal", func(f fstest.MapFS) {
			f["skills/plan-evidence/SKILL.md"].Data = bytes.ReplaceAll(f["skills/plan-evidence/SKILL.md"].Data, []byte("projection.md"), []byte("../../escape.md"))
		}},
		{"oversize resource", func(f fstest.MapFS) { f["skills/plan-evidence/projection.md"].Data = make([]byte, MaxSourceBytes+1) }},
		{"invalid tier", func(f fstest.MapFS) {
			f["plan-technical.md"].Data = bytes.ReplaceAll(f["plan-technical.md"].Data, []byte("tier: flagship"), []byte("tier: arbitrary"))
		}},
		{"unknown action", func(f fstest.MapFS) {
			f["plan.dot"].Data = bytes.ReplaceAll(f["plan.dot"].Data, []byte("lf_plan_story"), []byte("lf_plan_delete"))
		}},
		{"unsupported condition", func(f fstest.MapFS) {
			f["plan.dot"].Data = bytes.ReplaceAll(f["plan.dot"].Data, []byte(`lf_condition="approved"`), []byte(`lf_condition="implicit_approval"`))
		}},
		{"zero loop bound", func(f fstest.MapFS) {
			f["plan.dot"].Data = bytes.ReplaceAll(f["plan.dot"].Data, []byte(`lf_limit="8"`), []byte(`lf_limit="0"`))
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := planPackageFixture(t)
			tt.change(fixture)
			got, err := CaptureWorkflow(t.Context(), fixture, "plan.dot", "go-development", agent.ModePlan, planPackageRegistry(t))
			if err == nil || got != nil {
				t.Fatalf("invalid closure admitted: %v", got)
			}
		})
	}
}

func TestPlanPackagePositiveGatesAndStoppedExhaustion(t *testing.T) {
	t.Parallel()
	d, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"set", "story", "technical", "acceptance"} {
		t.Run(stage, func(t *testing.T) {
			// A story review hands on to the creation cycle's pick; every other
			// review is a durable owner wait.
			afterReview := "waiting"
			if stage == "story" {
				afterReview = "story_pick"
			}
			draftTarget := stage + "_review"
			if stage == "set" {
				draftTarget = "set_gate"
			}
			cases := []struct{ node, outcome, want string }{{stage + "_loop", "false", "stopped"}, {stage + "_question", "true", stage + "_ask"}, {stage + "_draft", "applied", draftTarget}, {stage + "_draft", "uncertain", "failed"}, {stage + "_draft", "conflict", "failed"}, {stage + "_draft", "failed", "failed"}, {stage + "_agent", "failed", "failed"}, {stage + "_review", "published", afterReview}, {stage + "_ask", "published", "waiting"}}
			for _, tt := range cases {
				got, ok := graphexec.Decide(d.Profile, tt.node, tt.outcome)
				if !ok || got != tt.want {
					t.Errorf("%s/%s=%q,%v want %s", tt.node, tt.outcome, got, ok, tt.want)
				}
			}
			if stage == "set" {
				for _, c := range []struct{ outcome, want string }{{"false", "set_review"}, {"true", "promoted"}} {
					if got, ok := graphexec.Decide(d.Profile, "set_gate", c.outcome); !ok || got != c.want {
						t.Errorf("set_gate/%s=%q,%v want %s", c.outcome, got, ok, c.want)
					}
				}
				gate, _ := d.Profile.Node("set_gate")
				if gate.Attrs["lf_condition"] != "approved" {
					t.Fatal("promotion must require policy-owned approval, not agent output")
				}
				if _, ok := graphexec.Decide(d.Profile, "set_gate", "review_ready"); ok {
					t.Fatal("review_ready directly promotes")
				}
				return
			}
			// Story, technical and acceptance stop at a generic owner gate: no
			// lf_if approval pair is left, and the gate names the closed command.
			if _, ok := d.Profile.Node(stage + "_gate"); ok {
				t.Fatalf("%s_gate must be replaced by the lf_gate review", stage)
			}
			review, _ := d.Profile.Node(stage + "_review")
			if review.Action != graphexec.PlanGateAction || review.Attrs["lf_command"] != graphexec.PlanGateCommand || review.Attrs["lf_args"] != graphexec.PlanGateArgs || review.Attrs["lf_source"] != stage+"_agent" {
				t.Fatalf("%s_review is not the generic gate: %v", stage, review.Attrs)
			}
		})
	}
	for _, node := range d.Profile.Nodes {
		if !node.Terminal && node.Limit <= 0 {
			t.Errorf("unbounded activity %s", node.Name)
		}
	}
}

// The set stage, its owner wait and the story creation cycle are declared in the
// graph definition file: the definition alone says where a session starts, how a
// session that already has stories skips the set stage and when the cycle ends.
func TestPlanPackageDeclaresSetStageAndCreationCycle(t *testing.T) {
	t.Parallel()
	d, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if d.Profile.Start != "select_set" {
		t.Fatalf("the set stage does not come first: start %q", d.Profile.Start)
	}
	for _, c := range []struct{ node, outcome, want string }{
		{"select_set", "true", "set_loop"},
		{"select_set", "false", "select_story"},
		{"set_review", "published", "waiting"},
		{"story_review", "published", "story_pick"},
		{"story_pick", "true", "story_agent"},
		{"story_pick", "false", "waiting"},
	} {
		if got, ok := graphexec.Decide(d.Profile, c.node, c.outcome); !ok || got != c.want {
			t.Errorf("%s/%s=%q,%v want %s", c.node, c.outcome, got, ok, c.want)
		}
	}
	for _, name := range []string{"select_set", "set_loop", "set_agent", "set_question", "set_draft", "set_gate", "set_ask", "set_review", "story_pick"} {
		if _, ok := d.Profile.Node(name); !ok {
			t.Errorf("node %s is not declared", name)
		}
	}
	set, _ := d.Profile.Node("set_agent")
	if _, captured := d.Nodes["set_agent"]; set.Attrs["lf_agent_markdown"] != "plan-set" || !captured {
		t.Errorf("set stage lacks its captured role: %v", set.Attrs)
	}
	// Every node of the creation cycle carries the bound that covers a full set.
	for _, name := range []string{"story_pick", "story_agent", "story_question", "story_draft", "story_review"} {
		n, _ := d.Profile.Node(name)
		if n.Limit < graphexec.PlanCreationBound {
			t.Errorf("%s bound %d does not cover %d stories", name, n.Limit, planbrief.MaxPlanStorySetEntries)
		}
	}
	if err := graphexec.ValidatePlanControlGraph(d.Profile); err != nil {
		t.Fatalf("shipped graph fails the declared-structure check: %v", err)
	}
}

func TestPlanPackageNativeContracts(t *testing.T) {
	t.Parallel()
	d, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex"} {
		for _, stage := range []string{"story", "technical", "acceptance"} {
			t.Run(harness+"/"+stage, func(t *testing.T) {
				t.Parallel()
				node := d.Nodes[stage+"_agent"]
				files, err := NativeWorkflow(harness, node, "test-model", []string{"Read"}, "workspace-write")
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				for _, file := range files {
					if seen[file.Path] || len(file.Bytes) == 0 {
						t.Fatalf("invalid native file %s", file.Path)
					}
					seen[file.Path] = true
				}
				for _, name := range []string{"node-result.md", "projection.md"} {
					if !seen["plugin/skills/plan-evidence/"+name] {
						t.Errorf("missing native shared resource %s", name)
					}
				}
			})
		}
	}
}

func TestPlanPackageNeutralGuidanceAndReuseFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		guidance []string
		frontend bool
	}{{"Go-only", []string{"go"}, false}, {"agent-only", []string{"agent-setup"}, false}, {"mixed", []string{"go", "agent-setup"}, false}, {"frontend", []string{}, true}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			draft := planbrief.PlanDraft{Key: "fixture", Name: "Review scope", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "a guided plan", Benefit: "retained decisions"}, Problem: "Avoid losing decisions", InScope: []string{"Review"}, NotInScope: []string{}, Architecture: &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Scope", Decision: "Reuse decisions", Rationale: "Keep settled answers"}}}, Guidance: tt.guidance, Frontend: tt.frontend}
			if tt.frontend {
				draft.Design = []string{"https://example.test/design"}
			}
			next := planbrief.PlanDraft{Key: draft.Key, Questions: []string{"Clarify the new feedback"}}
			merged, err := planbrief.MergePlanDraft(draft, next, []string{"conversation"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(merged.Architecture, draft.Architecture) || !reflect.DeepEqual(merged.Design, draft.Design) || !slices.Equal(merged.Guidance, draft.Guidance) {
				t.Fatal("feedback reset settled applicable content")
			}
			_, body, err := report.RenderPlanDraft(362, 1, merged, "technical", planbrief.ContentScope{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, "## Architecture - General") {
				t.Fatal("missing mandatory General")
			}
			for _, optional := range []string{"## Data Model", "## Service Design", "## Solution Design"} {
				if strings.Contains(body, optional) {
					t.Errorf("invented irrelevant section %s", optional)
				}
			}
			if strings.Contains(body, "# Design\n") != tt.frontend {
				t.Fatal("frontend Design applicability mismatch")
			}
		})
	}
}

func TestPlanCaptureLeavesTheResearchClosureUnchanged(t *testing.T) {
	t.Parallel()
	before, err := captureResearchGo(t.Context(), t, researchTestRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = capturePlan(t.Context(), t, planPackageRegistry(t)); err != nil {
		t.Fatal(err)
	}
	after, err := captureResearchGo(t.Context(), t, researchTestRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest != after.Digest || !reflect.DeepEqual(before.Files, after.Files) {
		t.Fatal("Plan capture changed Research closure")
	}
	for path := range after.Files {
		if strings.HasPrefix(path, "skills/plan-") || strings.HasPrefix(path, "plan-") {
			t.Errorf("Plan contract leaked into Research: %s", path)
		}
	}
}

func TestNeutralPlanGraphDoesNotRequireUnselectedStackGuidance(t *testing.T) {
	fixture := planPackageFixture(t)
	delete(fixture, "skills/plan-go-guidance/SKILL.md")
	delete(fixture, "skills/plan-agent-setup-guidance/SKILL.md")
	if _, err := CaptureWorkflow(t.Context(), fixture, "plan.dot", "go-development", agent.ModePlan, planPackageRegistry(t)); err != nil {
		t.Fatal(err)
	}
}
