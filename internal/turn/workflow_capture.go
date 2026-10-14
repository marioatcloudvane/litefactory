package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// WorkflowSourceResolver returns owned linked-package bytes or a refusal.
// It must never execute a graph or grant package-owned authority.
type WorkflowSourceResolver func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error)

func capturedWorkflow(ctx context.Context, o Options, req Request, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	session, err := o.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	repo, err := executionRepository(ctx, o.DB, session)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*teamsource.WorkflowDefinition, error) {
		return nil, fmt.Errorf("repository %s requested workflow %s: %w", repo.Path, workflow, cause)
	}
	saved, err := o.DB.WorkflowCaptureForAdmission(ctx, req.SessionID, req.Trigger.CommentID, string(workflow))
	if err == nil {
		definition, restoreErr := restoreWorkflow(ctx, o.DB, saved, workflow, specs)
		if restoreErr != nil {
			return fail(restoreErr)
		}
		return definition, nil
	}
	if !errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrGraphInvariant) {
		return fail(fmt.Errorf("refuse retained workflow: %w", err))
	}
	if o.WorkflowSource == nil {
		return fail(errors.New("installed workflow source resolver is required"))
	}
	definition, err := o.WorkflowSource(ctx, repo, workflow, specs)
	if err != nil {
		return fail(err)
	}
	if definition == nil || definition.Profile == nil {
		return fail(errors.New("linked installed workflow definition is missing"))
	}
	if definition.Origin.Kind != "local" && definition.Origin.Kind != "fetched" {
		return fail(errors.New("fresh workflow requires local or fetched installed provenance"))
	}
	if definition.StackProfile != nil {
		if err := admitStackProfile(ctx, o, session, repo, definition); err != nil {
			return fail(err)
		}
	}
	if err := store.ValidateWorkflowProvenance(store.WorkflowProvenance(definition.Origin)); err != nil {
		return fail(err)
	}
	verified, err := teamsource.CaptureWorkflowIdentity(ctx, teamsource.RetainedFiles(definition.Files), definition.Origin.GraphFile, definition.Origin.Team, workflow, specs)
	if err != nil {
		return fail(err)
	}
	if verified.Digest != definition.Digest || verified.Profile.Digest != definition.Profile.Digest || len(verified.Files) != len(definition.Files) {
		return fail(errors.New("installed workflow bytes or identity do not match capture"))
	}
	verified.Origin = definition.Origin
	verified.StackFresh = definition.StackProfile != nil
	definition = verified
	total := len(definition.Origin.TeamConfig)
	for _, raw := range definition.Files {
		total += len(raw)
	}
	if total > teamsource.MaxSnapshotBytes {
		return fail(errors.New("workflow and team.config raw capture exceed bounds"))
	}
	// Check serialization before definition retention or graph/provider execution.
	raw, err := json.Marshal(definition.Files)
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return fail(errors.New("workflow serialized files exceed 16 MiB capture limit"))
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
	if definition.StackProfile != nil {
		if err := validateStackHost(definition.StackProfile.Effective.Requirements); err != nil {
			return fail(err)
		}
	}
	// Equivalent-topology launches retain the foundation graph identity.
	definition.Profile.Digest = pin.Digest
	return definition, nil
}

func admitStackProfile(ctx context.Context, o Options, session store.Session, repo store.Repo, definition *teamsource.WorkflowDefinition) error {
	if o.Git == nil {
		return errors.New("stack.revision: repository reader unavailable")
	}
	clone, err := o.Git.Open(ctx, repo.Path)
	if err != nil {
		return errors.New("stack.revision: repository cannot be opened")
	}
	ref := o.remote() + "/" + repo.BaseBranch
	if session.Branch != "" {
		if _, err := clone.RevParse(ctx, "refs/heads/"+session.Branch, git.RevParseOptions{}); err == nil {
			ref = "refs/heads/" + session.Branch
		}
	}
	revision, err := clone.RevParse(ctx, ref, git.RevParseOptions{})
	if err != nil {
		return errors.New("stack.revision: admitted repository revision unavailable")
	}
	override, err := clone.ReadStackOverrides(ctx, revision)
	if err != nil {
		return err
	}
	resolved, err := config.ResolveStackProfile(definition.StackProfile.BaseBytes, override)
	if err != nil {
		return err
	}
	if err := validateStackHost(resolved.Effective.Requirements); err != nil {
		return err
	}
	guidance, err := teamsource.CaptureStackGuidance(ctx, teamsource.RetainedFiles(definition.StackPackageFiles), resolved.Effective)
	if err != nil {
		return err
	}
	return teamsource.AttachStackProfile(ctx, definition, resolved, revision, guidance)
}

func validateStackHost(requirements config.StackRequirements) error {
	if len(requirements.OS) != 0 && !slices.Contains(requirements.OS, runtime.GOOS) {
		return errors.New("stack.requirements.os: incompatible host")
	}
	if len(requirements.Arch) != 0 && !slices.Contains(requirements.Arch, runtime.GOARCH) {
		return errors.New("stack.requirements.arch: incompatible host")
	}
	for i, name := range requirements.Executables {
		if _, err := config.ResolveExecutable("stack.requirements", name, os.Getenv("PATH")); err != nil {
			return fmt.Errorf("stack.requirements.executables[%d]: required host executable unavailable", i)
		}
	}
	return nil
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
	return capturedWorkflow(ctx, o, req, agent.WorkflowImplement, specs)
}
func capturedValidate(ctx context.Context, o Options, req Request) (*teamsource.WorkflowDefinition, error) {
	specs, err := ValidateFlowSpecs()
	if err != nil {
		return nil, err
	}
	return capturedWorkflow(ctx, o, req, agent.WorkflowValidate, specs)
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

// workflowRequestError keeps admission and persistence refusals actionable even
// when they happen after capture, while retaining the original wrapped cause.
func workflowRequestError(ctx context.Context, opts Options, req Request, session store.Session, cause error) error {
	if cause == nil {
		return nil
	}
	workflow := req.Workflow
	if workflow == "" {
		workflow, _ = agent.WorkflowForMode(req.Mode)
	}
	repo, err := executionRepository(context.WithoutCancel(ctx), opts.DB, session)
	if err != nil {
		return fmt.Errorf("repository %d requested workflow %s: %w", session.RepoID, workflow, cause)
	}
	return fmt.Errorf("repository %s requested workflow %s: %w", repo.Path, workflow, cause)
}

// ValidateGraphDispatchRecovery checks the interrupted dispatch's exact retained
// instructions with the same registry and integrity checks used at execution.
// It grants no approval and performs no recovery or external effects.
func ValidateGraphDispatchRecovery(ctx context.Context, db *store.DB, dispatch store.GraphDispatch) error {
	saved, err := db.GetWorkflowCapture(ctx, dispatch.LaunchID)
	if err != nil {
		return err
	}
	if saved.InstanceID != dispatch.InstanceID || saved.SessionID != dispatch.SessionID {
		return errors.New("dispatch recovery capture identity mismatch")
	}
	instance, err := db.GetGraphInstance(ctx, dispatch.InstanceID)
	if err != nil {
		return err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	workflow, err := agent.ResolveWorkflow(agent.Workflow(pin.Workflow), agent.Mode(pin.Mode))
	if err != nil {
		return err
	}
	builders := map[agent.Workflow]func() (graphexec.Registry, error){agent.WorkflowResearch: ResearchSpecs, agent.WorkflowPlan: PlanSpecs, agent.WorkflowImplement: ImplementSpecs, agent.WorkflowValidate: ValidateFlowSpecs, agent.WorkflowSpike: SpikeSpecs, agent.WorkflowBug: SimplifiedBugSpecs}
	builder, ok := builders[workflow]
	if !ok {
		return errors.New("dispatch recovery workflow is unsupported")
	}
	specs, err := builder()
	if err != nil {
		return err
	}
	definition, err := restoreWorkflow(ctx, db, saved, workflow, specs)
	if err != nil {
		return err
	}
	if _, ok := definition.Profile.Node(instance.CurrentNode); !ok {
		return errors.New("dispatch recovery node is absent from retained capture")
	}
	return nil
}
