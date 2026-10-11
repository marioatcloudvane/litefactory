package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// ValidateAutoPayload is the instruction of a run the factory starts by itself
// when validate.auto_start is on. Everything the run needs comes from saved
// state, never from this text.
const ValidateAutoPayload = "/validate Start Validate for the finished Implement result."

// ValidateRuntime supplies what the Validate graph needs from the host: the
// resolved owner and the bindings of the suite and of every later step. A
// factory built without it keeps Validate on its legacy path.
type ValidateRuntime struct {
	// OwnerID is the resolved numeric owner identity.
	OwnerID func(ctx context.Context, opts Options) (int64, error)
	// Bind builds the suite and flow bindings of one Validate run pinned to the
	// entry's commit. The suite step must test exactly entry.Commit (or the
	// newest kept repair). The profile is added by the caller.
	Bind func(ctx context.Context, opts Options, req Request, entry store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error)
	// Head overrides the integration-branch head read; nil reads the remote.
	Head func(ctx context.Context, opts Options, session store.Session) (string, error)
	// Permissions resolves the operator's validate-repair grant, the only
	// write grant a Validate node can receive. Nil refuses the repair node.
	Permissions func(agent.Kind, string) (BugRolePermissions, error)
}

// validateRepairGrant replaces the read-only validate profile of a write node
// with the operator's validate-repair grant, the way a Bug node receives its
// role's grant. A missing grant refuses the node instead of widening the run.
func validateRepairGrant(resolve func(agent.Kind, string) (BugRolePermissions, error), req Request) (Request, error) {
	if resolve == nil {
		return req, fmt.Errorf("missing %s permission profile", config.ValidateRepairProfile)
	}
	permissions, err := resolve(req.Agent, config.ValidateRepairProfile)
	if err != nil {
		return req, err
	}
	req.Tools, req.AllowedTools, req.CodexSandbox, req.ToolProfile = permissions.Tools, permissions.AllowedTools, permissions.Sandbox, config.ValidateRepairProfile
	return req, nil
}

// IntegrationHead reads the full commit the parent's integration branch is at on
// the remote. It fetches, so it sees what Implement last pushed.
func IntegrationHead(ctx context.Context, opts Options, session store.Session) (string, error) {
	if opts.Validate != nil && opts.Validate.Head != nil {
		return opts.Validate.Head(ctx, opts, session)
	}
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || strings.TrimSpace(session.Branch) == "" {
		return "", errors.New("the integration branch cannot be read: no git, clone gate or branch")
	}
	repo, err := opts.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		return "", err
	}
	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return "", fmt.Errorf("open the registered clone at %s: %w", repo.Path, err)
	}
	release, err := opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	defer release()
	remote := opts.remote()
	if err := clone.Fetch(ctx, remote); err != nil {
		return "", fmt.Errorf("fetch %s: %w", remote, err)
	}
	head, err := clone.RevParse(ctx, "refs/remotes/"+remote+"/"+session.Branch, git.RevParseOptions{})
	if err != nil {
		return "", fmt.Errorf("integration branch %s: %w", session.Branch, err)
	}
	if !isFullSHA(head) {
		return "", fmt.Errorf("integration branch %s has no full commit", session.Branch)
	}
	return head, nil
}

// ValidateGate says whether Validate may start on a parent, with the plain
// reason when it may not.
type ValidateGate struct {
	Open   bool
	Reason string
	// NotMerged names the approved stories that are not merged yet.
	NotMerged []string
}

// ValidateEntryGate checks the entry gate: the latest Implement coordinator
// instance of the parent reached its finished terminal and every approved story
// is merged. It reads saved state only.
func ValidateEntryGate(ctx context.Context, db *store.DB, session store.Session) (ValidateGate, error) {
	instance, err := db.LatestWorkflowInstance(ctx, session.ID, string(agent.ModeImplement))
	if err != nil {
		return ValidateGate{}, err
	}
	feature, err := db.GetManagedFeature(ctx, session.TaskRef)
	if err != nil {
		return ValidateGate{}, err
	}
	revisions, err := db.ListApprovedStoryRevisions(ctx, session.TaskRef, feature.Generation)
	if err != nil {
		return ValidateGate{}, err
	}
	rows, err := db.ListImplementStories(ctx, session.TaskRef, int64(feature.Generation))
	if err != nil {
		return ValidateGate{}, err
	}
	state := map[string]string{}
	for _, row := range rows {
		state[row.Key] = row.State
	}
	var gate ValidateGate
	parked := false
	for _, r := range revisions {
		if r.State == "completed" || r.EngineeringComplete {
			continue
		}
		got, ok := state[r.Key]
		if !ok {
			got = store.ImplementWaiting
		}
		if got == store.ImplementMerged {
			continue
		}
		if got == store.ImplementParked {
			parked = true
		}
		gate.NotMerged = append(gate.NotMerged, fmt.Sprintf("%s (%s)", r.Key, got))
	}
	switch {
	case instance.Status == store.GraphRunning && parked:
		gate.Reason = "Implement is waiting for you: some stories are parked."
	case instance.Status == store.GraphRunning:
		gate.Reason = "Implement is still running."
	case instance.Status != store.GraphSucceeded:
		gate.Reason = "The latest Implement run did not finish."
	case len(gate.NotMerged) > 0:
		gate.Reason = "Not every approved story is merged."
	default:
		gate.Open = true
		return gate, nil
	}
	return gate, nil
}

func validateRefusalText(gate ValidateGate) string {
	var b strings.Builder
	b.WriteString("Validate was not started. " + gate.Reason)
	if len(gate.NotMerged) > 0 {
		b.WriteString(" Not merged yet: " + strings.Join(gate.NotMerged, ", ") + ".")
	}
	b.WriteString(" Finish Implement first, then send /validate again.")
	return b.String()
}

// validateRuntimeState is one Validate launch on a managed parent.
type validateRuntimeState struct {
	opts    Options
	req     Request
	result  Result
	session store.Session
	entry   store.ValidateEntry
}

// admitValidateExecution takes a /validate launch on a managed parent whose
// Implement ran on the graph onto the Validate graph. Anything else, such as a
// separated (legacy) feature, keeps the legacy path.
func admitValidateExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != string(agent.ModeValidate) || req.Workflow == agent.WorkflowBug || opts.Validate == nil || opts.DB == nil {
		return result, false, nil
	}
	if _, err := opts.DB.GetManagedFeature(ctx, result.Ref.String()); errors.Is(err, store.ErrNotFound) {
		return result, false, nil
	} else if err != nil {
		return result, true, err
	}
	session, err := opts.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return result, true, err
	}
	if _, err := opts.DB.LatestWorkflowInstance(ctx, session.ID, string(agent.ModeImplement)); errors.Is(err, store.ErrNotFound) {
		return result, false, nil
	} else if err != nil {
		return result, true, err
	}
	graph, err := opts.DB.HasWorkflowGraphSession(ctx, session.ID, string(agent.ModeValidate))
	if err != nil {
		return result, true, err
	}
	if req.Continuation != nil {
		if !graph {
			return result, false, nil
		}
		return result, true, errors.New("validate graphs require explicit owner continuation")
	}
	if opts.MCP == nil || opts.ResearchReporter == nil {
		return result, false, nil
	}
	rt := &validateRuntimeState{opts: opts, req: req, result: result, session: session}
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		out, err := rt.resumeDispatch(ctx)
		return out, true, err
	}
	// An owner control command continues the waiting Validate instance whose
	// owner wait the daemon claimed with this very comment; it never runs as a
	// plain agent.
	if req.Command.Kind == KindValidateControl {
		out, err := rt.continueOwner(ctx)
		return out, true, err
	}
	// Only the /validate instruction command starts Validate. Owner replies
	// inside a running Validate belong to its own owner steps.
	if req.Command.Kind != KindSpike || req.Command.Name != "/validate" {
		return result, false, nil
	}
	out, err := rt.admit(ctx)
	return out, true, err
}

// auto reports whether this launch is the factory's own automatic start.
func (rt *validateRuntimeState) auto(ctx context.Context) (bool, error) {
	if !store.IsScheduleTrigger(rt.req.Trigger.CommentID) {
		return false, nil
	}
	scheduled, err := rt.opts.DB.GetScheduleByTrigger(ctx, rt.req.Trigger.CommentID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return scheduled.Payload == ValidateAutoPayload, nil
}

func (rt *validateRuntimeState) refuse(ctx context.Context, reason string) (Result, error) {
	return prelaunchRefused(ctx, rt.opts, rt.result, reason)
}

func (rt *validateRuntimeState) admit(ctx context.Context) (Result, error) {
	opts, req := rt.opts, rt.req
	auto, err := rt.auto(ctx)
	if err != nil {
		return rt.result, err
	}
	source := store.ValidateSourceOwner
	if auto {
		source = store.ValidateSourceAuto
	} else {
		if opts.Validate.OwnerID == nil {
			return rt.result, errors.New("validate requires the resolved owner")
		}
		owner, err := opts.Validate.OwnerID(ctx, opts)
		if err != nil {
			return rt.result, err
		}
		if owner < 1 || req.Trigger.AuthorID != owner || req.Trigger.CommentID < 1 {
			return rt.refuse(ctx, "Only the owner can start Validate. Nothing was started.")
		}
	}
	// The very start that admitted an entry, replayed after a restart or a lost
	// reply, goes on with that entry and its pinned commit.
	if entry, ok, err := rt.replayedEntry(ctx); err != nil {
		return rt.result, err
	} else if ok {
		inst, err := opts.DB.GetGraphInstance(ctx, entry.InstanceID)
		if err != nil {
			return rt.result, err
		}
		if inst.Status != store.GraphRunning {
			return rt.refuse(ctx, fmt.Sprintf("The Validate run on commit %s stopped before it finished. Send /validate again to start it anew.", shortSHA(entry.Commit)))
		}
		rt.entry = entry
		return rt.launch(ctx, spikeNow(opts))
	}
	// Pin the commit once, at entry. A later push never changes this run.
	head, err := IntegrationHead(ctx, opts, rt.session)
	if err != nil {
		return rt.result, fmt.Errorf("read the integration branch head: %w", err)
	}
	now := spikeNow(opts)
	in := store.ValidateEntry{SessionID: rt.session.ID, Commit: head, Branch: rt.session.Branch, Source: source, CommentID: req.Trigger.CommentID, AuthorID: req.Trigger.AuthorID, Note: strings.TrimSpace(req.Command.Text)}
	if auto {
		in.CommentID, in.AuthorID, in.Note = 0, 0, ""
	}
	existing, err := opts.DB.GetValidateEntry(ctx, rt.session.ID, head)
	reopen := false
	switch {
	case err == nil && existing.State == store.ValidateEntryAdmitted:
		inst, ierr := opts.DB.GetGraphInstance(ctx, existing.InstanceID)
		if ierr != nil {
			return rt.result, ierr
		}
		// A run that ended without finishing is started anew only by an explicit
		// owner command; the factory's own start never invents that authority.
		ended := inst.Status == store.GraphFailed || inst.Status == store.GraphCancelled
		if !auto && !ended {
			// A run that only waits for the owner after a passed suite is ended
			// by the owner's /validate, so the reviews run again on the same commit.
			if ended, err = rt.endWaitingSameCommit(ctx, existing, now); err != nil {
				return rt.result, err
			}
		}
		if auto || !ended {
			return rt.alreadyStarted(ctx, existing)
		}
		reopen = true
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return rt.result, err
	}
	gate, err := ValidateEntryGate(ctx, opts.DB, rt.session)
	if err != nil {
		return rt.result, err
	}
	// The entry is saved before admission is attempted.
	if reopen {
		in.ID = existing.ID
		rt.entry, err = opts.DB.ReopenValidateEntry(ctx, in, now)
	} else {
		rt.entry, _, err = opts.DB.EnsureValidateEntry(ctx, in, now)
	}
	if err != nil {
		return rt.result, err
	}
	if !gate.Open {
		text := validateRefusalText(gate)
		if err := opts.DB.RefuseValidateEntry(ctx, rt.entry.ID, text, now); err != nil {
			return rt.result, err
		}
		return rt.refuse(ctx, text)
	}
	// A new commit gets a successor instance, but never beside a run that is
	// still going: two runs would move the same branch.
	if prior, err := opts.DB.LatestWorkflowInstance(ctx, rt.session.ID, string(agent.ModeValidate)); err == nil && prior.Status == store.GraphRunning {
		if e, err := rt.entryOf(ctx, prior.ID); err == nil && e.Commit != head {
			// An owner /validate on a newer commit ends a run on an older
			// commit that only waits for the owner; a run that is executing
			// keeps refusing.
			superseded := false
			if !auto {
				if superseded, err = rt.supersedeWaiting(ctx, prior.ID, now); err != nil {
					return rt.result, err
				}
			}
			if superseded {
				return rt.launch(ctx, now)
			}
			text := fmt.Sprintf("Validate was not started. A Validate run on commit %s is still going. Wait for it to finish, then send /validate again.", shortSHA(e.Commit))
			if err := opts.DB.RefuseValidateEntry(ctx, rt.entry.ID, text, now); err != nil {
				return rt.result, err
			}
			return rt.refuse(ctx, text)
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return rt.result, err
	}
	return rt.launch(ctx, now)
}

// endWaitingSameCommit cancels a run on this commit that only waits for the
// owner after a passed suite (an assessed outcome), whatever its reviews
// found. A new run on the same commit then reuses the passed suite and only
// the reviews run again. A run with a node still in progress, or any other
// run, is left alone and false is returned.
func (rt *validateRuntimeState) endWaitingSameCommit(ctx context.Context, entry store.ValidateEntry, now time.Time) (bool, error) {
	db := rt.opts.DB
	wait, err := db.GetGraphOwnerWait(ctx, entry.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	claimedReply, open := openOwnerWait(wait)
	if !open {
		return false, nil
	}
	outcome, err := db.GetValidateOutcome(ctx, entry.InstanceID, entry.Commit)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if outcome.Outcome != store.ValidateAssessed {
		return false, nil
	}
	gate, err := ValidateEntryGate(ctx, db, rt.session)
	if err != nil || !gate.Open {
		return false, err
	}
	return db.CancelIdleGraphOwnerWait(ctx, entry.InstanceID, claimedReply, "the owner started Validate again on the same commit", now)
}

// openOwnerWait reports whether an owner wait still counts as open. A wait
// claimed by a /validate start comment can never be applied, so it counts as
// open and its claiming reply is returned; any other claim keeps the run.
func openOwnerWait(wait store.GraphOwnerWait) (int64, bool) {
	switch {
	case wait.State == "waiting":
		return 0, true
	case wait.State == "claimed" && ParseCommand(wait.ReplyBody).Name == "/validate":
		return wait.ReplyID, true
	default:
		return 0, false
	}
}

// supersedeWaiting cancels a run on an older commit that is idle at its owner
// wait. It returns false when the run has no open owner wait or is executing.
func (rt *validateRuntimeState) supersedeWaiting(ctx context.Context, instanceID int64, now time.Time) (bool, error) {
	db := rt.opts.DB
	wait, err := db.GetGraphOwnerWait(ctx, instanceID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	claimedReply, open := openOwnerWait(wait)
	if !open {
		return false, nil
	}
	return db.CancelIdleGraphOwnerWait(ctx, instanceID, claimedReply, "superseded by a newer commit", now)
}

// replayedEntry finds the admitted entry this very trigger launched.
func (rt *validateRuntimeState) replayedEntry(ctx context.Context) (store.ValidateEntry, bool, error) {
	entries, err := rt.opts.DB.ListValidateEntries(ctx, rt.session.ID)
	if err != nil {
		return store.ValidateEntry{}, false, err
	}
	for _, e := range entries {
		if e.State != store.ValidateEntryAdmitted {
			continue
		}
		launched, err := rt.opts.DB.ValidateLaunchedBy(ctx, rt.session.ID, rt.req.Trigger.CommentID, e.InstanceID)
		if err != nil || launched {
			return e, launched, err
		}
	}
	return store.ValidateEntry{}, false, nil
}

// launch admits the saved entry to the Validate graph through the standard
// pattern and runs it. Repeating it with the same trigger yields the same
// instance.
func (rt *validateRuntimeState) launch(ctx context.Context, now time.Time) (Result, error) {
	opts, req := rt.opts, rt.req
	if _, err := opts.DB.AdmitWorkflowPath(ctx, req.SessionID, "graph"); err != nil {
		return rt.result, err
	}
	definition, err := capturedValidate(ctx, opts, req)
	if err != nil {
		return rt.result, err
	}
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return rt.result, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return rt.result, err
	}
	pin, err := opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: string(agent.ModeValidate), ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return rt.result, err
	}
	// The same trigger always yields the same instance: the owner's comment id,
	// or the private identity of the saved automatic schedule.
	admitted, err := opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: string(agent.ModeValidate), SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body})
	if err != nil {
		return rt.result, err
	}
	if err := opts.DB.AdmitValidateEntry(ctx, rt.entry.ID, admitted.Instance.ID, now); err != nil {
		return rt.result, err
	}
	rt.entry.State, rt.entry.InstanceID = store.ValidateEntryAdmitted, admitted.Instance.ID
	actual, err := opts.DB.GetGraphDefinition(ctx, admitted.Instance.DefinitionID)
	if err != nil {
		return rt.result, err
	}
	definition.Profile.Digest = actual.Digest
	return rt.execute(ctx, definition, admitted)
}

// alreadyStarted answers a start whose key already has a Validate instance. It
// gets a pointer to the report and starts nothing. (A replay of the very start
// that admitted the entry never gets here: it goes on with its instance.)
func (rt *validateRuntimeState) alreadyStarted(ctx context.Context, entry store.ValidateEntry) (Result, error) {
	text := fmt.Sprintf("Validate has already been started for commit %s. No second run was started.", shortSHA(entry.Commit))
	if report, err := rt.opts.DB.GetValidateReport(ctx, entry.InstanceID); err == nil && report.CommentID > 0 {
		text += fmt.Sprintf(" Its report: https://github.com/%s/%s/issues/%d#issuecomment-%d", rt.result.Ref.Owner, rt.result.Ref.Repo, rt.result.Ref.Issue, report.CommentID)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return rt.result, err
	} else {
		text += " Its report will appear on this issue."
	}
	return rt.refuse(ctx, text)
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// continueOwner hands an owner control comment to the running Validate
// instance whose owner wait it holds, so the instance's own resolve step
// interprets it. A control that holds no claim is refused and starts nothing.
func (rt *validateRuntimeState) continueOwner(ctx context.Context) (Result, error) {
	opts, req := rt.opts, rt.req
	instance, err := opts.DB.LatestWorkflowInstance(ctx, req.SessionID, string(agent.ModeValidate))
	if errors.Is(err, store.ErrNotFound) {
		return rt.refuse(ctx, validateNoWaitText)
	}
	if err != nil {
		return rt.result, err
	}
	if instance.Status != store.GraphRunning {
		return rt.refuse(ctx, validateNoWaitText)
	}
	held := false
	if wait, err := opts.DB.GetGraphOwnerWait(ctx, instance.ID); err == nil {
		held = wait.State == "claimed" && wait.ReplyID == req.Trigger.CommentID
	} else if !errors.Is(err, store.ErrNotFound) {
		return rt.result, err
	}
	if !held {
		// The same comment replayed after it already continued this instance.
		if held, err = opts.DB.ValidateLaunchedBy(ctx, req.SessionID, req.Trigger.CommentID, instance.ID); err != nil {
			return rt.result, err
		}
	}
	if !held {
		return rt.refuse(ctx, validateNoWaitText)
	}
	entry, err := rt.entryOf(ctx, instance.ID)
	if err != nil {
		return rt.result, err
	}
	rt.entry = entry
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return rt.result, err
	}
	definition, err := capturedValidate(ctx, opts, req)
	if err != nil {
		return rt.result, err
	}
	if definition.Profile.Digest != pin.Digest {
		return rt.result, errors.New("retained validate graph does not match the waiting instance; the owner reply cannot continue it")
	}
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return rt.result, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return rt.result, err
	}
	admitted, err := opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: string(agent.ModeValidate), SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: instance.DefinitionID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: req.Trigger.Body})
	if err != nil {
		return rt.result, err
	}
	if admitted.Instance.ID != instance.ID || admitted.PredecessorID != 0 {
		return rt.result, errors.New("the owner reply must continue the waiting Validate instance, not start another")
	}
	return rt.execute(ctx, definition, admitted)
}

const validateNoWaitText = "No Validate run is waiting for this command, so nothing was recorded."

// resumeDispatch enters the graph at the instance's current node with the
// definition it was admitted under. Nothing here is a fresh admission.
func (rt *validateRuntimeState) resumeDispatch(ctx context.Context) (Result, error) {
	instance, err := rt.opts.DB.LatestWorkflowInstance(ctx, rt.req.SessionID, string(agent.ModeValidate))
	if err != nil {
		return rt.result, fmt.Errorf("resume validate dispatch: %w", err)
	}
	if instance.Status != store.GraphRunning {
		return rt.result, errors.New("validate graph dispatch targets an instance that is no longer running")
	}
	pin, err := rt.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return rt.result, err
	}
	definition, err := capturedValidate(ctx, rt.opts, rt.req)
	if err != nil {
		return rt.result, err
	}
	if definition.Profile.Digest != pin.Digest {
		return rt.result, errors.New("retained validate graph does not match the running instance; the resting instance cannot resume")
	}
	entry, err := rt.entryOf(ctx, instance.ID)
	if err != nil {
		return rt.result, err
	}
	rt.entry = entry
	return rt.execute(ctx, definition, retainedAdmission(ctx, rt.opts.DB, rt.req, instance, "validate"))
}

func (rt *validateRuntimeState) entryOf(ctx context.Context, instanceID int64) (store.ValidateEntry, error) {
	entries, err := rt.opts.DB.ListValidateEntries(ctx, rt.req.SessionID)
	if err != nil {
		return store.ValidateEntry{}, err
	}
	for _, e := range entries {
		if e.InstanceID == instanceID {
			return e, nil
		}
	}
	return store.ValidateEntry{}, errors.New("the Validate instance has no saved entry")
}

// execute advances the captured graph under the task lock through the shared
// driver, with the host's bindings for this entry.
func (rt *validateRuntimeState) execute(ctx context.Context, definition *teamsource.WorkflowDefinition, admission store.WorkflowAdmission) (Result, error) {
	policy := ValidateWorkflowPolicy()
	policy.WriteGrant = func(req Request) (Request, error) {
		return validateRepairGrant(rt.opts.Validate.Permissions, req)
	}
	policy.RegisterEffects = func(specs *graphexec.Registry, activities *ActivityRegistry, profile *graphexec.Profile, scope func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), _ func() (string, string, error), _ WorkflowEnv) error {
		suite, flow, err := rt.opts.Validate.Bind(ctx, rt.opts, rt.req, rt.entry)
		if err != nil {
			return err
		}
		suite.StackProfile = definition.StackProfile
		flow.Profile = profile
		if flow.Scope == nil {
			// A repair result is judged in the same trusted scope the shared
			// workflow gave its attempt.
			flow.Scope = scope
		}
		if err := RegisterValidateSuiteActivity(specs, activities, suite); err != nil {
			return err
		}
		return RegisterValidateFlow(specs, activities, flow)
	}
	return ExecuteWorkflow(ctx, rt.opts, rt.req, rt.result, definition, admission, policy)
}
