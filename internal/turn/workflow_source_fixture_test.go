package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
)

// Test packages use owned embedded bytes as an offline installed-source fixture.
func fixtureInstalled(def *teamsource.WorkflowDefinition, err error) (*teamsource.WorkflowDefinition, error) {
	if err != nil {
		return nil, err
	}
	config := []byte("fixture installed workflow mapping")
	digest := sha256.Sum256(config)
	workflow, err := agent.ResolveWorkflow(def.Profile.Workflow, def.Profile.Mode)
	if err != nil {
		return nil, err
	}
	def.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: string(workflow) + "/" + string(workflow) + ".dot", GraphFile: string(workflow) + ".dot", Team: def.Profile.Team, TeamConfig: config, ConfigDigest: hex.EncodeToString(digest[:])}
	return def, nil
}

func fixtureWorkflowSource(ctx context.Context, _ store.Repo, w agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	var capture func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error)
	switch w {
	case agent.WorkflowResearch:
		capture = teamsourcetest.CaptureResearch
	case agent.WorkflowPlan:
		capture = teamsourcetest.CapturePlan
	case agent.WorkflowImplement:
		capture = teamsourcetest.CaptureImplement
	case agent.WorkflowValidate:
		capture = teamsourcetest.CaptureValidate
	case agent.WorkflowSpike:
		capture = teamsourcetest.CaptureSpike
	case agent.WorkflowBug:
		capture = teamsourcetest.CaptureBug
	default:
		return nil, fmt.Errorf("unexpected fixture workflow %s", w)
	}
	return fixtureInstalled(capture(ctx, specs))
}
