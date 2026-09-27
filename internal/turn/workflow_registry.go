package turn

import (
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
)

// RegisteredWorkflowPolicies is the closed registry of every shipped mode policy.
// A new workflow registers here or it is not admitted; a registry-wide guard
// walks this list, so a policy that skips the shared result route fails before
// any dispatch and is caught by the same test that covers Research and Plan.
func RegisteredWorkflowPolicies() []WorkflowPolicy {
	return []WorkflowPolicy{ResearchWorkflowPolicy(), PlanWorkflowPolicy(), BugWorkflowPolicy()}
}

// Admit is the policy-level requirement of every workflow: results are judged by
// the mode's decoder after they travel the shared result route, and preparation
// binds that route. A policy that could not judge a result, or whose handlers
// prepare no invocation to bind one, is refused here rather than discovered at
// launch. Admission proves the policy is well formed; it is never the guard that
// keeps a terminal-only result out, because a nominal decoder satisfies it. That
// guard is [AgentActivity.acceptRetained], which refuses any attempt without a
// retained accepted result whatever the decoder says.
func (p WorkflowPolicy) Admit() error {
	var faults []error
	workflow, err := agent.ResolveWorkflow(p.Workflow, p.Mode)
	if err != nil {
		faults = append(faults, err)
	}
	control, err := agent.ResolveWorkflow(p.Control.Workflow, p.Control.Mode)
	if err != nil || control != workflow {
		faults = append(faults, fmt.Errorf("policy control mode %q differs from its mode %q", p.Control.Mode, p.Mode))
	}
	if p.Decode == nil {
		faults = append(faults, errors.New("policy has no result decoder: every workflow result travels the shared result route and is judged by its decoder"))
	}
	if p.Handlers != nil && p.Handlers.Prepare == nil {
		faults = append(faults, errors.New("policy handlers prepare no invocation, so nothing binds the shared result service"))
	}
	if strings.TrimSpace(p.Guidance) == "" {
		faults = append(faults, errors.New("policy carries no agent guidance for the shared result route"))
	}
	if p.Handlers != nil && (p.Failure == nil || p.Render == nil) {
		faults = append(faults, errors.New("policy with its own handlers defines no failure comment content builder and renderer, so its failures would not reach the owner"))
	}
	if len(faults) == 0 {
		return nil
	}
	return fmt.Errorf("workflow policy refused: %w", errors.Join(faults...))
}
