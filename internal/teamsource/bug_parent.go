package teamsource

import (
	"context"
	_ "embed"
	"errors"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// historicalBug retains the factory-pinned pre-consolidation topology for saved
// builtin captures. It is immutable compatibility evidence, not current source.
//
//go:embed history/bug.profile
var historicalBug []byte

// CaptureBugParent pins the simplified parent and its focused role resources.
func CaptureBugParent(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(bugAssets, "bug")
	if err != nil {
		return nil, err
	}
	out, err := CaptureWorkflowIdentity(ctx, source, "bug.dot", "go-development", agent.WorkflowBug, registry)
	if err != nil {
		return nil, err
	}
	if err = ValidateBugParent(out.Profile); err != nil {
		return nil, err
	}

	return out, nil
}

// ValidateBugParent admits exactly the shipped topology, role selection and
// finite bounds. User package text cannot select a new entry or capability.
func ValidateBugParent(p *graphexec.Profile) error {
	if p == nil || p.Workflow != agent.WorkflowBug || p.Mode != "" || p.ProfileVersion != 2 || (p.Team != "builtin" && p.Team != "go-development") {
		return errors.New("invalid Bug parent identity")
	}
	raw, err := fs.ReadFile(bugAssets, "bug/bug.dot")
	if err != nil {
		return err
	}
	if p.Team == "builtin" {
		raw = historicalBug
	}
	expected, err := graphexec.ParseProfile(raw)
	if err != nil {
		return err
	}
	if p.Digest != expected.Digest {
		return errors.New("bug parent differs from factory-pinned topology and bounds")
	}
	return graphexec.ValidateOwnerConversationGraph(p)
}
