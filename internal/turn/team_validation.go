package turn

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func validateMappedWorkflows(ctx context.Context, source fs.FS, workflows map[string]string) error {
	builders := map[string]func() (graphexec.Registry, error){"research": ResearchSpecs, "plan": PlanSpecs, "implement": ImplementSpecs, "validate": ValidateFlowSpecs, "spike": SpikeSpecs, "bug": SimplifiedBugSpecs}
	modes := make([]string, 0, len(workflows))
	for mode := range workflows {
		modes = append(modes, mode)
	}
	slices.Sort(modes)
	var faults []error
	for _, mode := range modes {
		graph := workflows[mode]
		raw, err := fs.ReadFile(source, graph)
		if err != nil {
			faults = append(faults, fmt.Errorf("%s: %w", graph, err))
			continue
		}
		profile, err := graphexec.ParseProfile(raw)
		if err != nil {
			faults = append(faults, fmt.Errorf("%s: %w", graph, err))
			continue
		}
		builder, ok := builders[mode]
		if !ok {
			faults = append(faults, fmt.Errorf("unknown workflow %s", mode))
			continue
		}
		registry, err := builder()
		if err != nil {
			faults = append(faults, err)
			continue
		}
		root, err := fs.Sub(source, path.Dir(graph))
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if _, err := teamsource.CaptureWorkflowIdentity(ctx, root, path.Base(graph), profile.Team, agent.Workflow(mode), registry); err != nil {
			faults = append(faults, fmt.Errorf("%s: %w", graph, err))
		}
	}
	return errors.Join(faults...)
}
