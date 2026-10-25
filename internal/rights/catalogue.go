package rights

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Entry is one grantable action: the workflow, the stage (graph node name)
// and the human action allowed there.
type Entry struct {
	Workflow    string `json:"workflow"`
	Stage       string `json:"stage"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

// String renders the entry as "workflow | stage | action".
func (e Entry) String() string { return e.Workflow + " | " + e.Stage + " | " + e.Action }

// Source lists the grantable actions of one repository.
//
// The repository source below reads the repository's selected installed team.
type Source interface {
	Catalogue(ctx context.Context, repo store.Repo) ([]Entry, error)
}

// Resolver captures one workflow of the repository's selected installed team.
type Resolver func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error)

// RepositorySource lists the lf_rights actions of the repository's selected
// installed team. A repository with no selected team has no actions.
type RepositorySource struct {
	Resolve Resolver
}

// NewRepositorySource reads workflows through resolve, the same repository
// workflow source the daemon uses.
func NewRepositorySource(resolve Resolver) RepositorySource {
	return RepositorySource{Resolve: resolve}
}

// Catalogue implements Source.
func (s RepositorySource) Catalogue(ctx context.Context, repo store.Repo) ([]Entry, error) {
	type workflowSpecs struct {
		workflow agent.Workflow
		specs    func() (graphexec.Registry, error)
	}
	list := []workflowSpecs{
		{agent.WorkflowBug, turn.SimplifiedBugSpecs},
		{agent.WorkflowPlan, turn.PlanSpecs},
		{agent.WorkflowResearch, turn.ResearchSpecs},
		{agent.WorkflowImplement, turn.ImplementSpecs},
		{agent.WorkflowSpike, func() (graphexec.Registry, error) { return turn.WorkflowSpecs(turn.SpikeWorkflowPolicy()) }},
	}
	var profiles []*graphexec.Profile
	for _, w := range list {
		specs, err := w.specs()
		if err != nil {
			return nil, fmt.Errorf("rights catalogue: %w", err)
		}
		d, err := s.Resolve(ctx, repo, w.workflow, specs)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil // no team is selected for this repository
		}
		if err != nil {
			return nil, fmt.Errorf("rights catalogue: %w", err)
		}
		profiles = append(profiles, d.Profile)
	}
	return EntriesFromProfiles(profiles...)
}

// EntriesFromProfiles collects one entry per tagged action, in graph order.
// Untagged nodes are omitted.
func EntriesFromProfiles(profiles ...*graphexec.Profile) ([]Entry, error) {
	var out []Entry
	for _, p := range profiles {
		if p == nil {
			return nil, errors.New("rights catalogue: nil graph profile")
		}
		workflow, err := agent.ResolveWorkflow(p.Workflow, p.Mode)
		if err != nil {
			return nil, fmt.Errorf("rights catalogue: %w", err)
		}
		for _, node := range p.Nodes {
			tag, tagged := node.Attrs[graphexec.RightsAttribute]
			if !tagged {
				continue
			}
			for action := range strings.SplitSeq(tag, ",") {
				action = strings.TrimSpace(action)
				if !slices.Contains(graphexec.RightsActions, action) {
					return nil, fmt.Errorf("rights catalogue: node %q has unknown right %q", node.Name, action)
				}
				out = append(out, Entry{Workflow: string(workflow), Stage: node.Name, Action: action, Description: node.Attrs["label"]})
			}
		}
	}
	return out, nil
}

func findEntry(entries []Entry, workflow, stage, action string) bool {
	return slices.ContainsFunc(entries, func(e Entry) bool {
		return e.Workflow == workflow && e.Stage == stage && e.Action == action
	})
}

func findStage(entries []Entry, workflow, stage string) bool {
	return slices.ContainsFunc(entries, func(e Entry) bool { return e.Workflow == workflow && e.Stage == stage })
}
