package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// WorkflowSourceResolver returns owned linked-package bytes, or nil for an
// unlinked clone. It must never execute a graph or grant package-owned authority.
type WorkflowSourceResolver func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error)

func capturedWorkflow(ctx context.Context, o Options, req Request, workflow agent.Workflow, specs graphexec.Registry, builtin func(context.Context, graphexec.Registry) (*teamsource.WorkflowDefinition, error)) (*teamsource.WorkflowDefinition, error) {
	saved, err := o.DB.WorkflowCaptureForAdmission(ctx, req.SessionID, req.Trigger.CommentID, string(workflow))
	if err == nil {
		return restoreWorkflow(ctx, o.DB, saved, workflow, specs)
	}
	if !errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrGraphInvariant) {
		return nil, fmt.Errorf("refuse retained %s workflow: %w", workflow, err)
	}
	session, err := o.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	repo, err := o.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		return nil, err
	}
	var definition *teamsource.WorkflowDefinition
	if o.WorkflowSource != nil {
		definition, err = o.WorkflowSource(ctx, repo, workflow, specs)
		if err != nil {
			return nil, err
		}
	} else {
		_, bindingErr := o.DB.GetRepositoryTeamBinding(ctx, repo.ID)
		if bindingErr == nil {
			return nil, errors.New("linked repository requires workflow source resolver")
		}
		if !errors.Is(bindingErr, store.ErrNotFound) {
			return nil, bindingErr
		}
	}
	if definition == nil {
		definition, err = builtin(ctx, specs)
		if err != nil {
			return nil, err
		}
		definition.Origin = teamsource.WorkflowOrigin{Kind: "builtin", GraphPath: string(workflow) + "/" + string(workflow) + ".dot", GraphFile: string(workflow) + ".dot", Team: definition.Profile.Team}
	}
	// Check serialization before definition retention or graph/provider execution.
	raw, err := json.Marshal(definition.Files)
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("workflow serialized files exceed 16 MiB capture limit")
	}
	return definition, nil
}

func restoreWorkflow(ctx context.Context, db *store.DB, saved store.WorkflowCapture, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	fail := func(e error) (*teamsource.WorkflowDefinition, error) {
		return nil, fmt.Errorf("refuse retained %s capture %d; original work preserved: %w", workflow, saved.ID, e)
	}
	instance, err := db.GetGraphInstance(ctx, saved.InstanceID)
	if err != nil {
		return fail(err)
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return fail(err)
	}
	stored, err := agent.ResolveWorkflow(agent.Workflow(pin.Workflow), agent.Mode(pin.Mode))
	if err != nil || stored != workflow {
		return fail(errors.New("retained workflow identity mismatch"))
	}
	var files map[string][]byte
	if err = json.Unmarshal(saved.Files, &files); err != nil {
		return fail(err)
	}
	total := 0
	for name, raw := range files {
		if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:\x00") || int64(len(raw)) > teamsource.MaxSourceBytes {
			return fail(errors.New("invalid retained resource path or size"))
		}
		total += len(raw)
		if total > teamsource.MaxSnapshotBytes {
			return fail(errors.New("retained raw capture exceeds bounds"))
		}
	}
	graphFile := saved.Provenance.GraphFile
	team := saved.Provenance.Team
	if saved.Provenance.Kind == "" {
		graphFile = path.Base(pin.Path)
		team = pin.Team
	}
	definition, err := teamsource.CaptureWorkflowIdentity(ctx, teamsource.RetainedFiles(files), graphFile, team, workflow, specs)
	if err != nil {
		return fail(err)
	}
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return fail(err)
	}
	if definition.Digest != saved.ContentDigest || topology != saved.TopologyDigest || len(definition.Files) != len(files) || pin.Team != team || pin.ProfileVersion != definition.Profile.ProfileVersion {
		return fail(errors.New("retained bytes, digests or contract do not match admission"))
	}
	definition.Origin = teamsource.WorkflowOrigin(saved.Provenance)
	// Equivalent-topology launches retain the foundation graph identity.
	definition.Profile.Digest = pin.Digest
	return definition, nil
}

// ValidateTeamWorkflows validates every map entry against factory registries.
func ValidateTeamWorkflows(ctx context.Context, source fs.FS, workflows map[string]string) error {
	return validateMappedWorkflows(ctx, source, workflows)
}

func capturedImplement(ctx context.Context, o Options, req Request) (*teamsource.WorkflowDefinition, error) {
	specs, err := ImplementSpecs()
	if err != nil {
		return nil, err
	}
	return capturedWorkflow(ctx, o, req, agent.WorkflowImplement, specs, teamsource.CaptureImplement)
}
func capturedValidate(ctx context.Context, o Options, req Request) (*teamsource.WorkflowDefinition, error) {
	specs, err := ValidateFlowSpecs()
	if err != nil {
		return nil, err
	}
	return capturedWorkflow(ctx, o, req, agent.WorkflowValidate, specs, teamsource.CaptureValidate)
}

func retainedAdmission(ctx context.Context, db *store.DB, req Request, instance store.GraphInstance, workflow string) store.WorkflowAdmission {
	// The exact capture has just been loaded and validated under the task lock.
	saved, err := db.WorkflowCaptureForAdmission(ctx, req.SessionID, req.Trigger.CommentID, workflow)
	if err != nil {
		return store.WorkflowAdmission{Instance: instance}
	}
	return store.WorkflowAdmission{Instance: instance, LaunchID: saved.ID}
}

func workflowGraphPath(d *teamsource.WorkflowDefinition) string {
	if d.Origin.GraphPath != "" {
		return d.Origin.GraphPath
	}
	workflow, _ := agent.ResolveWorkflow(d.Profile.Workflow, d.Profile.Mode)
	return string(workflow) + "/" + string(workflow) + ".dot"
}

// retainedGraphFile follows the captured graph mapping without opening a package.
func retainedGraphFile(ctx context.Context, db *store.DB, instanceID int64) (string, error) {
	instance, err := db.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return "", err
	}
	return path.Base(pin.Path), nil
}
