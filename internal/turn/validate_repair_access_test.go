package turn

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// validateModeRequest is the request the daemon builds from the read-only
// validate mode profile: the only request a Validate graph node starts from.
func validateModeRequest(kind agent.Kind) Request {
	if kind == agent.Kind(store.AgentCodex) {
		return Request{Agent: kind, ToolProfile: "validate", CodexSandbox: "read-only"}
	}
	return Request{Agent: kind, ToolProfile: "validate", Tools: []string{"Read", "Glob", "Grep", "Task"}, AllowedTools: []string{"Read", "Glob", "Grep"}}
}

func validateRepairResolver(t *testing.T) func(agent.Kind, string) (BugRolePermissions, error) {
	t.Helper()
	return func(kind agent.Kind, role string) (BugRolePermissions, error) {
		if role != config.ValidateRepairProfile {
			t.Fatalf("Validate asked for role %q", role)
		}
		if kind == agent.Kind(store.AgentCodex) {
			return BugRolePermissions{Sandbox: "workspace-write"}, nil
		}
		return BugRolePermissions{Tools: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, AllowedTools: []string{"Read", "Bash(go test:*)"}}, nil
	}
}

// The repair agent starts from the read-only validate profile, which alone can
// never grant it write access (#553); its validate-repair grant does.
func TestValidateRepairAgentRunsUnderItsOwnWriteGrant(t *testing.T) {
	policy := ValidateWorkflowPolicy()
	repair, _ := captureValidate(t).Profile.Node("repair_agent")
	codexKind, claudeKind := agent.Kind(store.AgentCodex), agent.Kind(store.AgentClaude)

	// Without the grant the read-only mode ceiling refuses the node on both providers.
	for _, kind := range []agent.Kind{codexKind, claudeKind} {
		if _, err := applyNodeAccess(policy, repair, validateModeRequest(kind)); err == nil {
			t.Fatalf("%s: the read-only validate profile granted write access", kind)
		}
	}

	codexReq, err := validateRepairGrant(validateRepairResolver(t), validateModeRequest(codexKind))
	if err == nil {
		codexReq, err = applyNodeAccess(policy, repair, codexReq)
	}
	if err != nil || codexReq.CodexSandbox != "workspace-write" || codexReq.ToolProfile != config.ValidateRepairProfile {
		t.Fatalf("Codex repair: %+v %v", codexReq, err)
	}
	perm := requestPermissions(codexReq)
	if sandbox, ok := perm.Payload.(codex.Sandbox); !ok || perm.Profile != config.ValidateRepairProfile || sandbox.Mode != "workspace-write" {
		t.Fatalf("Codex repair permissions: %+v", perm)
	}

	claudeReq, err := validateRepairGrant(validateRepairResolver(t), validateModeRequest(claudeKind))
	if err == nil {
		claudeReq, err = applyNodeAccess(policy, repair, claudeReq)
	}
	if err != nil || !slices.Equal(claudeReq.Tools, []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}) || claudeReq.ToolProfile != config.ValidateRepairProfile {
		t.Fatalf("Claude repair: %+v %v", claudeReq, err)
	}
	if !slices.Equal(claudeReq.AllowedTools, []string{"Read", "Bash(go test:*)"}) {
		t.Fatalf("Claude repair allowed tools: %v", claudeReq.AllowedTools)
	}
	surface := requestPermissions(claudeReq)
	if tools, ok := surface.Payload.(harness.ToolSurface); !ok || surface.Profile != config.ValidateRepairProfile || !slices.Contains(tools.Tools, "Bash") {
		t.Fatalf("Claude repair permissions: %+v", surface)
	}
}

// A missing grant refuses the repair agent explicitly instead of widening it.
func TestValidateRepairWithoutGrantIsRefused(t *testing.T) {
	if _, err := validateRepairGrant(nil, validateModeRequest(agent.Kind(store.AgentCodex))); err == nil || !strings.Contains(err.Error(), "missing validate-repair permission profile") {
		t.Fatalf("missing resolver: %v", err)
	}
	missing := errors.New("missing explicit validate-repair permission profile for codex")
	_, err := validateRepairGrant(func(agent.Kind, string) (BugRolePermissions, error) { return BugRolePermissions{}, missing }, validateModeRequest(agent.Kind(store.AgentCodex)))
	if !errors.Is(err, missing) {
		t.Fatalf("missing profile: %v", err)
	}
}

// Reviewers stay read-only on the unchanged validate profile.
func TestValidateReviewersStayReadOnly(t *testing.T) {
	policy := ValidateWorkflowPolicy()
	reviewer := graphexec.Node{Name: "reviewer", Action: "lf_agent", Attrs: map[string]string{graphexec.AccessAttribute: ValidateReviewAccess}}
	codexReq, err := applyNodeAccess(policy, reviewer, validateModeRequest(agent.Kind(store.AgentCodex)))
	if err != nil || codexReq.CodexSandbox != "read-only" || codexReq.ToolProfile != "validate" {
		t.Fatalf("Codex reviewer: %+v %v", codexReq, err)
	}
	claudeReq, err := applyNodeAccess(policy, reviewer, validateModeRequest(agent.Kind(store.AgentClaude)))
	if err != nil || !slices.Equal(claudeReq.Tools, []string{"Read", "Glob", "Grep"}) || claudeReq.ToolProfile != "validate" {
		t.Fatalf("Claude reviewer: %+v %v", claudeReq, err)
	}
}

// Spike's fixer keeps its own surface: write access never adds a shell there.
func TestSpikeWriteAccessKeepsTheSpikeFixerSurface(t *testing.T) {
	node := graphexec.Node{Name: "fix_agent", Attrs: map[string]string{graphexec.AccessAttribute: "write"}}
	req, err := applyWriteAccess(node, Request{Agent: agent.Kind(store.AgentClaude), ToolProfile: config.SpikeFixerProfile, Tools: []string{"Read", "Edit", "Write", "Bash", "Task"}})
	if err != nil || !slices.Equal(req.Tools, []string{"Read", "Edit", "Write"}) {
		t.Fatalf("Spike fixer surface: %+v %v", req.Tools, err)
	}
}
