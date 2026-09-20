package teamsource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func researchTestRegistry(t *testing.T) graphexec.Registry {
	t.Helper()
	var r graphexec.Registry
	for name, outcomes := range map[string][]string{"agent": {"success", "failed"}, "loop": {"true", "false"}, "if": {"true", "false"}, "comment": {"published"}, "ask": {"published"}, "research_brief": {"success", "failed"}} {
		if err := r.Register(graphexec.ActionSpec{Name: "lf_" + name, Mode: "research", Outcomes: outcomes}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func researchTestSource() fstest.MapFS {
	return fstest.MapFS{
		"research.dot": {Data: []byte(`digraph research {
 graph [team="builtin", mode="research", profile_version="1", start="work", lf_task="package task", lf_model_tier="fast"];
 work [lf_action="lf_agent", lf_outcomes="success", lf_agent_markdown="research", lf_skills="reader", lf_limit="2"];
 end [lf_terminal="true"];
 work -> end [lf_on="success"];
 }`)},
		"research.md":                 {Data: []byte("---\nname: research\ndescription: Research role\ntier: balanced\ntask: role task\n---\nInvestigate the request.\n")},
		"skills/reader/SKILL.md":      {Data: []byte("---\nname: reader\ndescription: Read evidence\nrequires: [shared/base]\nresources: [guide.md]\n---\nRead [guide](guide.md).\n")},
		"skills/reader/guide.md":      {Data: []byte("Evidence guide")},
		"skills/shared/base/SKILL.md": {Data: []byte("---\nname: base\ndescription: Base evidence\n---\nPreserve evidence.\n")},
		"unselected.md":               {Data: []byte("Not part of the closure")},
	}
}

func replaceResearchFixture(f fstest.MapFS, name, old, next string) {
	f[name].Data = []byte(strings.ReplaceAll(string(f[name].Data), old, next))
}

func TestCaptureResearchValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
		want   string
	}{
		{"external namespace", func(f fstest.MapFS) { replaceResearchFixture(f, "research.dot", `team="builtin"`, `team="external"`) }, "admitted team or mode"},
		{"agent extension", func(f fstest.MapFS) {
			replaceResearchFixture(f, "research.dot", `lf_agent_markdown="research"`, `lf_agent_markdown="research.md"`)
		}, "agent"},
		{"agent traversal", func(f fstest.MapFS) {
			replaceResearchFixture(f, "research.dot", `lf_agent_markdown="research"`, `lf_agent_markdown="../research"`)
		}, "agent"},
		{"duplicate skill", func(f fstest.MapFS) {
			replaceResearchFixture(f, "research.dot", `lf_skills="reader"`, `lf_skills="reader,reader"`)
		}, "duplicate"},
		{"skill traversal", func(f fstest.MapFS) {
			replaceResearchFixture(f, "research.dot", `lf_skills="reader"`, `lf_skills="../reader"`)
		}, "skill"},
		{"missing role", func(f fstest.MapFS) { delete(f, "research.md") }, "research.md"},
		{"missing resource", func(f fstest.MapFS) { delete(f, "skills/reader/guide.md") }, "guide.md"},
		{"missing dependency", func(f fstest.MapFS) { delete(f, "skills/shared/base/SKILL.md") }, "shared/base"},
		{"dependency cycle", func(f fstest.MapFS) {
			replaceResearchFixture(f, "skills/shared/base/SKILL.md", "description: Base evidence", "description: Base evidence\nrequires: [reader]")
		}, "cycle"},
		{"native collision", func(f fstest.MapFS) {
			f["skills/base/SKILL.md"] = &fstest.MapFile{Data: append([]byte(nil), f["skills/shared/base/SKILL.md"].Data...)}
			replaceResearchFixture(f, "research.dot", `lf_skills="reader"`, `lf_skills="reader,base"`)
		}, "collide"},
		{"resource escape", func(f fstest.MapFS) { replaceResearchFixture(f, "skills/reader/SKILL.md", "guide.md", "../outside.md") }, "unsafe"},
		{"symlink resource", func(f fstest.MapFS) { f["skills/reader/guide.md"].Mode = fs.ModeSymlink }, "guide.md"},
		{"resource bounds", func(f fstest.MapFS) { f["skills/reader/guide.md"].Data = make([]byte, MaxSourceBytes+1) }, "at most"},
		{"invalid tier", func(f fstest.MapFS) { replaceResearchFixture(f, "research.md", "tier: balanced", "tier: vendor-model") }, "tier"},
		{"infinite limit", func(f fstest.MapFS) { replaceResearchFixture(f, "research.dot", `lf_limit="2"`, `lf_limit="Inf"`) }, "limit"},
		{"zero limit", func(f fstest.MapFS) { replaceResearchFixture(f, "research.dot", `lf_limit="2"`, `lf_limit="0"`) }, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := researchTestSource()
			tt.mutate(f)
			got, err := captureResearch(t.Context(), f, researchTestRegistry(t))
			if err == nil || !strings.Contains(err.Error(), tt.want) || got != nil {
				t.Fatalf("capture = %v, %v; want no definition and diagnostic %q", got, err, tt.want)
			}
		})
	}
}

func TestCaptureResearchEffectiveDefaults(t *testing.T) {
	tests := []struct {
		name, roleTask, roleTier, nodeAttrs, wantTask, wantTier string
		fail                                                    bool
	}{
		{name: "role defaults", wantTask: "role task", wantTier: "balanced"},
		{name: "node overrides", nodeAttrs: `, lf_task="node task", lf_model_tier="flagship"`, wantTask: "node task", wantTier: "flagship"},
		{name: "package defaults", roleTask: "remove", roleTier: "remove", wantTask: "package task", wantTier: "fast"},
		{name: "empty explicit task", nodeAttrs: `, lf_task=""`, fail: true},
		{name: "empty explicit tier", nodeAttrs: `, lf_model_tier=""`, fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := researchTestSource()
			if tt.roleTask != "" {
				replaceResearchFixture(f, "research.md", "task: role task\n", "")
			}
			if tt.roleTier != "" {
				replaceResearchFixture(f, "research.md", "tier: balanced\n", "")
			}
			replaceResearchFixture(f, "research.dot", `lf_limit="2"`, `lf_limit="2"`+tt.nodeAttrs)
			got, err := captureResearch(t.Context(), f, researchTestRegistry(t))
			if tt.fail {
				if err == nil {
					t.Fatal("accepted empty override")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			node := got.Nodes["work"]
			if node.Task != tt.wantTask || string(node.Tier) != tt.wantTier {
				t.Fatalf("effective task/tier = %q/%q, want %q/%q", node.Task, node.Tier, tt.wantTask, tt.wantTier)
			}
		})
	}
}

func TestCaptureResearchOwnedClosure(t *testing.T) {
	t.Parallel()
	f := researchTestSource()
	r := researchTestRegistry(t)
	got, err := captureResearch(t.Context(), f, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 5 || len(got.Nodes["work"].Skills) != 2 {
		t.Fatalf("closure has %d files and %v skills", len(got.Files), got.Nodes["work"].Skills)
	}
	for name, raw := range got.Files {
		if got.Hashes[name] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			t.Errorf("wrong content hash for %s", name)
		}
	}
	before := string(got.Files["skills/reader/guide.md"])
	f["skills/reader/guide.md"].Data[0] = 'X'
	if string(got.Files["skills/reader/guide.md"]) != before {
		t.Fatal("captured bytes alias source")
	}
	changed, err := captureResearch(t.Context(), f, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest == changed.Digest {
		t.Fatal("resource change did not change identity")
	}
}

func TestCaptureResearchIndependentDiagnostics(t *testing.T) {
	t.Parallel()
	f := researchTestSource()
	delete(f, "research.md")
	delete(f, "skills/reader/guide.md")
	_, err := captureResearch(t.Context(), f, researchTestRegistry(t))
	if err == nil {
		t.Fatal("accepted missing resources")
	}
	for _, want := range []string{"research.md", "guide.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s diagnostic: %v", want, err)
		}
	}
}

func TestCaptureResearchBuiltin(t *testing.T) {
	t.Parallel()
	r := researchTestRegistry(t)
	got, err := CaptureResearch(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile.Team != "builtin" || len(got.Nodes) == 0 || got.Digest == "" {
		t.Fatalf("invalid built-in definition: %+v", got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CaptureResearch(ctx, r); err == nil {
		t.Fatal("accepted cancelled capture")
	}
}

func TestCaptureResearchAggregateBounds(t *testing.T) {
	t.Parallel()
	f := researchTestSource()
	var resources []string
	for i := 0; i < 17; i++ {
		name := fmt.Sprintf("part-%d.md", i)
		resources = append(resources, name)
		f["skills/reader/"+name] = &fstest.MapFile{Data: make([]byte, MaxSourceBytes)}
	}
	replaceResearchFixture(f, "skills/reader/SKILL.md", "resources: [guide.md]", "resources: ["+strings.Join(resources, ",")+"]")
	got, err := captureResearch(t.Context(), f, researchTestRegistry(t))
	if err == nil || got != nil || !strings.Contains(err.Error(), "bounds") {
		t.Fatalf("capture = %v, %v; want aggregate bound refusal", got, err)
	}
}

func TestCaptureResearchMissingEffectiveDefaults(t *testing.T) {
	t.Parallel()
	f := researchTestSource()
	replaceResearchFixture(f, "research.md", "task: role task\n", "")
	replaceResearchFixture(f, "research.md", "tier: balanced\n", "")
	replaceResearchFixture(f, "research.dot", `, lf_task="package task", lf_model_tier="fast"`, "")
	got, err := captureResearch(t.Context(), f, researchTestRegistry(t))
	if err == nil || got != nil {
		t.Fatal("accepted missing effective task and tier")
	}
	for _, want := range []string{"task", "tier"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %s diagnostic: %v", want, err)
		}
	}
}
