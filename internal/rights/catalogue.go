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
// SWAP POINT FOR #317: the built-in source below is temporary. It reads the
// hardcoded built-in workflows and returns the same list for every
// repository. Issue #317 replaces it with a source that reads each
// repository's own graphs. Nothing outside this interface has to change.
type Source interface {
	Catalogue(ctx context.Context, repo store.Repo) ([]Entry, error)
}

// Capture loads one built-in workflow graph.
type Capture func(ctx context.Context) (*graphexec.Profile, error)

// BuiltinSource is the temporary catalogue source (swap point for #317). It
// walks the hardcoded built-in workflows and collects their lf_rights nodes,
// ignoring the repository.
type BuiltinSource struct {
	Captures []Capture
}

// NewBuiltinSource wires the shipped bug, plan, research, implement and spike
// workflows. The validate workflow joins this list when it exists.
func NewBuiltinSource() BuiltinSource {
	return BuiltinSource{Captures: []Capture{
		func(ctx context.Context) (*graphexec.Profile, error) {
			specs, err := turn.SimplifiedBugSpecs()
			if err != nil {
				return nil, err
			}
			d, err := teamsource.CaptureBugParent(ctx, specs)
			if err != nil {
				return nil, err
			}
			return d.Profile, nil
		},
		func(ctx context.Context) (*graphexec.Profile, error) {
			specs, err := turn.PlanSpecs()
			if err != nil {
				return nil, err
			}
			d, err := teamsource.CapturePlan(ctx, specs)
			if err != nil {
				return nil, err
			}
			return d.Profile, nil
		},
		func(ctx context.Context) (*graphexec.Profile, error) {
			specs, err := turn.ResearchSpecs()
			if err != nil {
				return nil, err
			}
			d, err := teamsource.CaptureResearch(ctx, specs)
			if err != nil {
				return nil, err
			}
			return d.Profile, nil
		},
		func(ctx context.Context) (*graphexec.Profile, error) {
			specs, err := turn.ImplementSpecs()
			if err != nil {
				return nil, err
			}
			d, err := teamsource.CaptureImplement(ctx, specs)
			if err != nil {
				return nil, err
			}
			return d.Profile, nil
		},
		func(ctx context.Context) (*graphexec.Profile, error) {
			specs, err := turn.WorkflowSpecs(turn.SpikeWorkflowPolicy())
			if err != nil {
				return nil, err
			}
			d, err := teamsource.CaptureSpike(ctx, specs)
			if err != nil {
				return nil, err
			}
			return d.Profile, nil
		},
	}}
}

// Catalogue implements Source. The repository is ignored.
func (s BuiltinSource) Catalogue(ctx context.Context, _ store.Repo) ([]Entry, error) {
	var profiles []*graphexec.Profile
	for _, capture := range s.Captures {
		p, err := capture(ctx)
		if err != nil {
			return nil, fmt.Errorf("rights catalogue: %w", err)
		}
		profiles = append(profiles, p)
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
