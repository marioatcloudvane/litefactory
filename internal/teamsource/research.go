package teamsource

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/skillclosure"
)

//go:embed research
var researchAssets embed.FS

// ResearchNode contains one node's effective instructions and selected closure.
// Its resources retain package-relative paths; callers own all returned bytes.
type ResearchNode struct {
	Role   render.ResolvedSource
	Task   string
	Tier   config.Tier
	Skills []string
	Files  map[string][]byte
}

// ResearchDefinition captures actual shipped bytes, independent of installed
// team catalogs. Digest includes DOT formatting as well as effective resources;
// topology identity is a separate concern at the launch boundary.
type ResearchDefinition struct {
	StackProfile  *config.ResolvedStackProfile
	StackRevision string
	// StackFresh marks a newly resolved admission, never a retained replay.
	StackFresh bool
	// StackPackageFiles is fresh-admission input; selected retained resources
	// live in Files and never need current disk during resume or fallback.
	StackPackageFiles map[string][]byte
	Origin            WorkflowOrigin
	Profile           *graphexec.Profile
	Nodes             map[string]ResearchNode
	Files             map[string][]byte
	Hashes            map[string]string
	Digest            string
}

// CaptureResearch selects only the application's embedded Research package.
// The registry is factory-owned; no source path, team or owner override is taken.
// Effective task and tier use explicit node overrides, then role frontmatter,
// then graph package defaults. An explicit empty override is invalid, not absent.
func CaptureResearch(ctx context.Context, registry graphexec.Registry) (*ResearchDefinition, error) {
	source, err := fs.Sub(researchAssets, "research")
	if err != nil {
		return nil, fmt.Errorf("open built-in Research: %w", err)
	}
	return captureResearch(ctx, source, registry)
}

type researchReader struct {
	ctx      context.Context
	source   fs.FS
	files    map[string][]byte
	total    int
	maxFiles int // Zero preserves the historical reader's byte-only bounds.
	states   map[string]int
	native   map[string]string
	skills   []string
}

func (r *researchReader) read(name string) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if !safeResearchPath(name) {
		return nil, fmt.Errorf("unsafe Research resource %q", name)
	}
	if raw, ok := r.files[name]; ok {
		return raw, nil
	}
	if r.maxFiles > 0 && len(r.files) >= r.maxFiles {
		return nil, errors.New("captured resource count exceeds bounds")
	}
	file, err := r.source.Open(name)
	if err != nil {
		return nil, fmt.Errorf("read Research %s: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSourceBytes {
		return nil, fmt.Errorf("research %s must be a regular file of at most %d bytes", name, MaxSourceBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxSourceBytes || len(raw) > MaxSnapshotBytes-r.total {
		return nil, fmt.Errorf("research source bounds exceeded at %s", name)
	}
	r.total += len(raw)
	r.files[name] = raw
	return raw, nil
}

func safeResearchPath(name string) bool {
	return skillclosure.SafePath(name)
}

func captureResearch(ctx context.Context, source fs.FS, registry graphexec.Registry) (*ResearchDefinition, error) {
	return CaptureWorkflow(ctx, source, "research.dot", "go-development", agent.ModeResearch, registry)
}

// WorkflowDefinition and WorkflowNode are captured packages and roles, independent of mode.
type WorkflowDefinition = ResearchDefinition

// WorkflowNode is the captured role and resource closure used by shared workflow delivery.
type WorkflowNode = ResearchNode

// CaptureWorkflow reads a bounded factory-owned package against an admitted registry.
func CaptureWorkflow(ctx context.Context, source fs.FS, graphPath, teamName string, mode agent.Mode, registry graphexec.Registry) (*WorkflowDefinition, error) {
	workflow, ok := agent.WorkflowForMode(mode)
	if !ok {
		return nil, fmt.Errorf("invalid workflow compatibility mode %q", mode)
	}
	return CaptureWorkflowIdentity(ctx, source, graphPath, teamName, workflow, registry)
}

// CaptureWorkflowIdentity captures a package using its distinct workflow identity.
func CaptureWorkflowIdentity(ctx context.Context, source fs.FS, graphPath, teamName string, workflow agent.Workflow, registry graphexec.Registry, instructionPaths ...config.Paths) (*WorkflowDefinition, error) {
	r := researchReader{ctx: ctx, source: source, files: map[string][]byte{}}
	raw, err := r.read(graphPath)
	if err != nil {
		return nil, err
	}
	profile, err := graphexec.ParseProfile(raw)
	if err != nil {
		return nil, err
	}
	var faults []error
	owner, bindingErr := agent.ResolveWorkflow(profile.Workflow, profile.Mode)
	if profile.Team != teamName || bindingErr != nil || owner != workflow || !workflow.Valid() {
		faults = append(faults, errors.New("workflow package differs from admitted team or mode"))
	}
	faults = append(faults, graphexec.Validate(profile, registry))
	result := &ResearchDefinition{Profile: profile, Nodes: map[string]ResearchNode{}, Files: r.files, Hashes: map[string]string{}}
	for _, declared := range profile.Nodes {
		// A step that starts agents itself names them in lf_roles; each is
		// captured as a node of its own, keyed "<step>:<role>", with the step's
		// skills and limit.
		for _, node := range capturedAgentNodes(declared) {
			captured, err := captureResearchNode(ctx, source, node, profile.Attrs)
			if err != nil {
				faults = append(faults, fmt.Errorf("research node %s: %w", node.Name, err))
				continue
			}
			result.Nodes[node.Name] = captured
			for name, content := range captured.Files {
				if _, exists := result.Files[name]; exists {
					continue
				}
				if len(content) > MaxSnapshotBytes-r.total {
					faults = append(faults, errors.New("research aggregate source bounds exceeded"))
					break
				}
				r.total += len(content)
				result.Files[name] = slices.Clone(content)
			}
		}
	}
	for _, declared := range profile.Nodes {
		if declared.Action != graphexec.JudgeAction {
			continue
		}
		if err := captureWorkflowJudge(ctx, source, result, &r.total, instructionPaths); err != nil {
			faults = append(faults, err)
		}
		break
	}
	if len(result.Nodes) == 0 {
		faults = append(faults, errors.New("research definition has no resolved agent nodes"))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	if err := restoreStackProfile(ctx, source, result); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(result.Files))
	for name, content := range result.Files {
		keys = append(keys, name)
		result.Hashes[name] = hashBytes(content)
	}
	sort.Strings(keys)
	var manifest strings.Builder
	for _, name := range keys {
		manifest.WriteString(name + "\x00" + result.Hashes[name] + "\n")
	}
	result.Digest = hashBytes([]byte(manifest.String()))
	return result, nil
}

// capturedAgentNodes lists the agent nodes a declared node needs captured: the
// node itself when it is an agent, or one node per role a step names in lf_roles.
func capturedAgentNodes(node graphexec.Node) []graphexec.Node {
	if node.Action == "lf_agent" {
		return []graphexec.Node{node}
	}
	if node.Terminal || node.Attrs["lf_roles"] == "" {
		return nil
	}
	var out []graphexec.Node
	for _, role := range strings.Split(node.Attrs["lf_roles"], ",") {
		attrs := map[string]string{"lf_agent_markdown": strings.TrimSpace(role)}
		if skills := node.Attrs["lf_skills"]; skills != "" {
			attrs["lf_skills"] = skills
		}
		out = append(out, graphexec.Node{Name: node.Name + ":" + strings.TrimSpace(role), Action: "lf_agent", Limit: node.Limit, Attrs: attrs})
	}
	return out
}

func captureResearchNode(ctx context.Context, source fs.FS, node graphexec.Node, defaults map[string]string) (ResearchNode, error) {
	var result ResearchNode
	name, selected, refErr := graphexec.ResearchReferences(node.Attrs)
	r := researchReader{ctx: ctx, source: source, files: map[string][]byte{}, states: map[string]int{}, native: map[string]string{}}
	faults := []error{refErr}
	if graphexec.ResearchName(name, false) {
		raw, err := r.read(name + ".md")
		if err == nil {
			result.Role, err = parseAgentSource(raw)
		}
		if err != nil {
			faults = append(faults, err)
		} else {
			fm := &result.Role.Frontmatter
			if fm.Name != name || strings.TrimSpace(fm.Description) == "" || strings.TrimSpace(result.Role.Body) == "" || fm.Model != "" {
				faults = append(faults, fmt.Errorf("research role %q has invalid metadata/body", name))
			}
			task, taskOK := fm.Extra["task"].(string)
			tier, tierOK := fm.Extra["tier"].(string)
			if _, exists := fm.Extra["task"]; exists && !taskOK {
				faults = append(faults, errors.New("role task must be a string"))
			}
			if _, exists := fm.Extra["tier"]; exists && !tierOK {
				faults = append(faults, errors.New("role tier must be a string"))
			}
			result.Task = inherited(node.Attrs, "lf_task", task, defaults["lf_task"])
			result.Tier = config.Tier(inherited(node.Attrs, "lf_model_tier", tier, defaults["lf_model_tier"]))
			delete(fm.Extra, "task")
			delete(fm.Extra, "tier")
			if len(fm.Extra) != 0 {
				faults = append(faults, errors.New("research role contains unsupported metadata"))
			}
		}
	}
	faults = append(faults, graphexec.ValidateResearchEffective(result.Task, result.Tier, node.Limit))
	for _, skill := range selected {
		faults = append(faults, r.skill(skill))
	}
	if err := errors.Join(faults...); err != nil {
		return ResearchNode{}, err
	}
	result.Skills = slices.Clone(r.skills)
	sort.Strings(result.Skills)
	result.Files = r.files
	return result, nil
}

func inherited(attrs map[string]string, key, role, fallback string) string {
	if value, ok := attrs[key]; ok {
		return value
	}
	if role != "" {
		return role
	}
	return fallback
}

func (r *researchReader) skill(id string) error {
	if !graphexec.ResearchName(id, true) {
		return fmt.Errorf("invalid Research skill %q", id)
	}
	if r.states[id] == 1 {
		return fmt.Errorf("research skill dependency cycle at %s", id)
	}
	if r.states[id] == 2 {
		return nil
	}
	r.states[id] = 1
	native := path.Base(id)
	if previous, exists := r.native[native]; exists && previous != id {
		return fmt.Errorf("research skills %s and %s collide as %s", previous, id, native)
	}
	r.native[native] = id
	name := "skills/" + id + "/SKILL.md"
	raw, err := r.read(name)
	if err != nil {
		return err
	}
	header, _, err := skillclosure.Parse(raw, id)
	if err != nil {
		return err
	}
	var faults []error
	seen := map[string]bool{}
	for _, dep := range header.Requires {
		if seen[dep] {
			faults = append(faults, fmt.Errorf("duplicate Research dependency %q", dep))
			continue
		}
		seen[dep] = true
		faults = append(faults, r.skill(dep))
	}
	for _, resource := range header.Resources {
		if !safeResearchPath(resource) {
			faults = append(faults, fmt.Errorf("unsafe Research resource %q", resource))
			continue
		}
		_, err := r.read(path.Join(path.Dir(name), resource))
		faults = append(faults, err)
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	r.states[id] = 2
	r.skills = append(r.skills, id)
	return nil
}
