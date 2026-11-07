package turn

import (
	"context"
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
	return Request{Agent: kind, ToolProfile: "validate", Tools: config.AgentTools(), CodexSandbox: "read-only"}
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
		return BugRolePermissions{Tools: config.AgentTools(), Sandbox: "workspace-write"}, nil
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
	if err != nil || !slices.Equal(claudeReq.Tools, config.AgentTools()) || claudeReq.ToolProfile != config.ValidateRepairProfile {
		t.Fatalf("Claude repair: %+v %v", claudeReq, err)
	}
	surface := requestPermissions(claudeReq)
	if tools, ok := surface.Payload.(harness.ToolSurface); !ok || surface.Profile != config.ValidateRepairProfile || !slices.Contains(tools.Tools, "Bash") || tools.ReadOnly {
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

// Reviewers keep the full tool set and run in the read-only sandbox.
func TestValidateReviewersStayReadOnly(t *testing.T) {
	policy := ValidateWorkflowPolicy()
	reviewer := graphexec.Node{Name: "reviewer", Action: "lf_agent", Attrs: map[string]string{graphexec.AccessAttribute: ValidateReviewAccess}}
	codexReq, err := applyNodeAccess(policy, reviewer, validateModeRequest(agent.Kind(store.AgentCodex)))
	if err != nil || codexReq.CodexSandbox != "read-only" || codexReq.ToolProfile != "validate" {
		t.Fatalf("Codex reviewer: %+v %v", codexReq, err)
	}
	claudeReq, err := applyNodeAccess(policy, reviewer, validateModeRequest(agent.Kind(store.AgentClaude)))
	if err != nil || !slices.Equal(claudeReq.Tools, config.AgentTools()) || claudeReq.ToolProfile != "validate" {
		t.Fatalf("Claude reviewer: %+v %v", claudeReq, err)
	}
	if surface, ok := requestPermissions(claudeReq).Payload.(harness.ToolSurface); !ok || !surface.ReadOnly {
		t.Fatalf("Claude reviewer is not in the read-only sandbox: %+v", surface)
	}

	// A Spike investigator read node starts from the fixer's writable
	// sandbox and narrows to read-only; its fixer write node stays writable.
	spike := Request{Agent: agent.Kind(store.AgentClaude), ToolProfile: string(agent.ModeSpike), Tools: config.AgentTools(), CodexSandbox: "workspace-write"}
	got, err := applyNodeAccess(SpikeWorkflowPolicy(), graphexec.Node{Name: "probe_agent", Attrs: map[string]string{graphexec.AccessAttribute: "read"}}, spike)
	if surface, _ := requestPermissions(got).Payload.(harness.ToolSurface); err != nil || !surface.ReadOnly || !slices.Equal(surface.Tools, config.AgentTools()) {
		t.Fatalf("investigator: %+v %v", surface, err)
	}
	got, err = applyNodeAccess(SpikeWorkflowPolicy(), graphexec.Node{Name: "fix_agent", Attrs: map[string]string{graphexec.AccessAttribute: "write"}}, spike)
	if surface, _ := requestPermissions(got).Payload.(harness.ToolSurface); err != nil || surface.ReadOnly || !got.nodeWrite {
		t.Fatalf("fixer: %+v %v", surface, err)
	}
}

// On a managed parent the repair agent keeps its validate-repair grant at the
// request the runner receives: the managed read-only Validate context, built
// for reviewers, must not narrow it or tell it not to edit (#1396).
func TestValidateRepairOnManagedParentKeepsItsWriteSurface(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var seen []agent.Request
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				seen = append(seen, req)
				return nil, errors.New("stop after recording the request")
			})
			f := newFixture(t, runner)
			setupVersionedProgressScope(t, f, 0)
			f.opts.Runners = RunnerSet{kind: runner}
			grant, err := validateRepairGrant(validateRepairResolver(t), validateModeRequest(kind))
			if err != nil {
				t.Fatal(err)
			}
			req := f.request()
			req.Mode, req.Agent = agent.ModeValidate, kind
			req.Tools, req.AllowedTools, req.ToolProfile, req.CodexSandbox = grant.Tools, grant.AllowedTools, grant.ToolProfile, grant.CodexSandbox
			req.Command = ParseCommand("/validate assess")
			req.Trigger.Body = "/validate assess"
			_, _ = Execute(t.Context(), f.opts, req)
			if len(seen) == 0 {
				t.Fatalf("repair agent never started:\n%s", f.logs)
			}
			got := seen[0]
			if got.RetainedValidation {
				t.Fatal("repair run carries the managed read-only Validate narrowing")
			}
			if strings.Contains(got.Prompt, "Managed read-only Validate contract") || strings.Contains(got.Prompt, "Factory-retained validation context") {
				t.Fatal("repair prompt carries the managed read-only Validate contract")
			}
			switch payload := got.Permissions.Payload.(type) {
			case harness.ToolSurface:
				if !slices.Contains(payload.Tools, "Edit") || !slices.Contains(payload.Tools, "Bash") {
					t.Fatalf("Claude repair surface: %+v", payload)
				}
			case codex.Sandbox:
				if payload.Mode != "workspace-write" {
					t.Fatalf("Codex repair sandbox: %+v", payload)
				}
			default:
				t.Fatalf("repair permissions: %+v", got.Permissions)
			}
		})
	}
}

// A reviewer on the same managed parent keeps the read-only Validate context.
func TestValidateReviewerOnManagedParentKeepsRetainedValidation(t *testing.T) {
	var seen []agent.Request
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		seen = append(seen, req)
		return nil, errors.New("stop after recording the request")
	})
	f := newFixture(t, runner)
	setupVersionedProgressScope(t, f, 0)
	kind := agent.Kind(store.AgentClaude)
	f.opts.Runners = RunnerSet{kind: runner}
	reviewer := validateModeRequest(kind)
	req := f.request()
	req.Mode, req.Agent = agent.ModeValidate, kind
	req.Tools, req.AllowedTools, req.ToolProfile = reviewer.Tools, reviewer.AllowedTools, reviewer.ToolProfile
	req.Command = ParseCommand("/validate assess")
	req.Trigger.Body = "/validate assess"
	_, _ = Execute(t.Context(), f.opts, req)
	if len(seen) == 0 || !seen[0].RetainedValidation || !strings.Contains(seen[0].Prompt, "Managed read-only Validate contract") {
		t.Fatalf("managed reviewer lost the read-only Validate context: %d", len(seen))
	}
}
