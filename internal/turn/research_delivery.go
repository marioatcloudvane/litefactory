package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// PrepareResearchDelivery captures a single graph node's effective delivery and
// selects its provider segment before allocating the run. Different logical
// nodes always rotate; an unchanged same-node delivery can reuse its segment.
// The caller must carry saved findings and authorized replies as bounded,
// untrusted request data, and bind the actual run to the returned segment.
func PrepareResearchDelivery(ctx context.Context, opts Options, req Request, definition *teamsource.ResearchDefinition, node string, session store.Session, ref naming.Ref, firstRef string) (store.Segment, *agent.Delivery, error) {
	p, err := planResearchDelivery(opts, req, definition, node)
	if err != nil {
		return store.Segment{}, nil, err
	}
	return prepareDeliverySegment(ctx, opts, req, session, ref, firstRef, p)
}

func planResearchDelivery(opts Options, req Request, definition *teamsource.ResearchDefinition, nodeName string) (*deliveryPlan, error) {
	return planWorkflowDelivery(opts, req, definition, nodeName, ResearchWorkflowPolicy())
}

func planWorkflowDelivery(opts Options, req Request, definition *teamsource.WorkflowDefinition, nodeName string, policy WorkflowPolicy) (*deliveryPlan, error) {
	if req.mode() != string(policy.Mode) || definition == nil || definition.Profile == nil {
		return nil, fmt.Errorf("research delivery requires admitted Research and captured definitions")
	}
	node, ok := definition.Nodes[nodeName]
	if !ok {
		return nil, fmt.Errorf("research delivery node %q is missing", nodeName)
	}
	model, ok := opts.Tiers.ResolveModel(config.Kind(req.Agent), node.Tier)
	if !ok {
		return nil, fmt.Errorf("missing local %s model for Research tier %s", req.Agent, node.Tier)
	}
	sandbox := req.CodexSandbox
	if sandbox == "" {
		sandbox = "read-only"
	}
	// An omitted shipped tool list inherits the session's tools, which the
	// --tools ceiling bounds and which include the attempt's MCP tools. An
	// explicit list remains narrowed by NativeWorkflow's intersection.
	files, err := teamsource.NativeWorkflow(string(req.Agent), node, model, req.Tools, sandbox)
	if err != nil {
		return nil, err
	}
	p := &deliveryPlan{mainRole: node.Role.Frontmatter.Name, mainModel: model, roles: []string{node.Role.Frontmatter.Name}, files: map[string][]byte{}}
	for _, file := range files {
		p.files[file.Path] = slices.Clone(file.Bytes)
	}
	p.evidence = agent.DeliveryEvidence{Team: definition.Profile.Team, Mode: string(policy.Mode), SnapshotDigest: definition.Digest, ResourceHashes: map[string]string{}}
	for name, raw := range node.Files {
		p.evidence.ResourceHashes[name] = hashBytes(raw)
		p.evidence.SourceBytes += len(raw)
	}
	p.evidence.Specialists = []agent.SpecialistEvidence{{ID: p.mainRole, Tier: string(node.Tier), Model: model}}
	for _, skill := range node.Skills {
		p.skills = append(p.skills, "plugin/skills/"+path.Base(skill)+"/SKILL.md")
	}
	sort.Strings(p.skills)
	// Project guidance is discovered through the host's normal channel. Copying
	// it into this appended system file would elevate legacy dispatch directives.
	p.files["bootstrap.md"] = []byte("The factory selected " + string(policy.Mode) + " and supplied the main role directly. Execute that role; do not dispatch a replacement orchestrator. Ordinary project guidance remains applicable within the supplied role, graph admission and permission ceiling. Only an explicit graph comment activity publishes results. Follow the factory's result and receipt instructions on every continuation and after compaction.\n\nEffective node task:\n" + node.Task + "\n" + policy.Guidance + skillNamesBinding(node.Skills))
	if req.Agent == store.AgentCodex {
		// Only fixed shipped role/task text belongs here. Owner requests, replies,
		// prior findings and helper credentials stay on the invocation's stdin.
		// Codex reads this retained source into its additive developer override.
		p.files["main-instructions.md"] = []byte(node.Role.Body + "\n\n" + string(p.files["bootstrap.md"]))
	}
	parentIdentity := ""
	if runner, exists := opts.Runners.runner(req.Agent); exists {
		if identified, supported := runner.(interface{ InstructionIdentity() string }); supported {
			parentIdentity = identified.InstructionIdentity()
		}
	}
	// Hash effective bytes rather than the whole package or DOT formatting.
	// The node name is intentionally included even when two roles are identical.
	encoded, err := json.Marshal(struct {
		Version string
		Node    string
		Role    string
		Model   string
		Parent  string
		Kind    agent.Kind
		Files   map[string][]byte
		Tools   []string
		Allowed []string
		Sandbox string
	}{render.NativeVersion, nodeName, p.mainRole, model, parentIdentity, req.Agent, p.files, req.Tools, req.AllowedTools, sandbox})
	if err != nil {
		return nil, fmt.Errorf("hash Research delivery: %w", err)
	}
	p.identity = hashBytes(encoded)
	return p, nil
}

// skillNamesBinding names a node's skills by name only. Text an agent writes is
// published on the forge, where a path on the factory host resolves nowhere and
// exposes the host's layout, so the binding carries no path to refer to.
func skillNamesBinding(skills []string) string {
	if len(skills) == 0 {
		return ""
	}
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, path.Base(skill))
	}
	sort.Strings(names)
	return "\n\nSkills for this node: " + strings.Join(names, ", ") + ". In anything you write for the owner, refer to a skill by its name, never by a file path or link on this machine; local paths are not published.\n"
}

// PrepareWorkflowDelivery selects the captured role and provider segment under the admitted policy.
func PrepareWorkflowDelivery(ctx context.Context, opts Options, req Request, definition *teamsource.WorkflowDefinition, node string, session store.Session, ref naming.Ref, firstRef string, policy WorkflowPolicy) (store.Segment, *agent.Delivery, error) {
	p, err := planWorkflowDelivery(opts, req, definition, node, policy)
	if err != nil {
		return store.Segment{}, nil, err
	}
	return prepareDeliverySegment(ctx, opts, req, session, ref, firstRef, p)
}
