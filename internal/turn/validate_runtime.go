package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// ValidateRuntimeConfig is what the host supplies to the Validate graph beyond
// the turn's own options: the forge of the owner steps, the resolved owner and
// the run-slot pool the reviewers take their slots from.
type ValidateRuntimeConfig struct {
	Forge   ValidateForge
	OwnerID int64
	Slots   ValidateSlots
}

// NewValidateRuntime builds the production host bindings of the Validate graph.
// Every binding is derived from factory state for the one entry it is bound to.
func NewValidateRuntime(ctx context.Context, opts Options, cfg ValidateRuntimeConfig) (*ValidateRuntime, error) {
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || opts.MCP == nil || opts.MCP.Redactor == nil || opts.MCP.BotID < 1 || opts.ResearchReporter == nil || cfg.Forge == nil || cfg.OwnerID < 1 || cfg.Slots == nil {
		return nil, errors.New("graph Validate requires concrete source, result, owner, reporter, forge and run-slot bindings")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := ValidateFlowSpecs(); err != nil {
		return nil, err
	}
	owner := cfg.OwnerID
	return &ValidateRuntime{
		OwnerID: func(context.Context, Options) (int64, error) { return owner, nil },
		Bind: func(ctx context.Context, opts Options, req Request, entry store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
			return bindValidate(ctx, opts, req, entry, cfg)
		},
	}, nil
}

// validateHost is one Validate run's view of its task: the session, its
// registered clone and the frozen approved stories under review.
type validateHost struct {
	opts      Options
	req       Request
	entry     store.ValidateEntry
	session   store.Session
	repo      store.Repo
	ref       naming.Ref
	clone     *git.Clone
	revisions []store.ApprovedStoryRevision
	feature   store.ManagedFeature

	mu       sync.Mutex
	worktree *git.Worktree
}

func bindValidate(ctx context.Context, opts Options, req Request, entry store.ValidateEntry, cfg ValidateRuntimeConfig) (ValidateSuiteBindings, ValidateFlowBindings, error) {
	h := &validateHost{opts: opts, req: req, entry: entry}
	var err error
	if h.session, err = opts.DB.GetSession(ctx, entry.SessionID); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	if h.ref, err = naming.ParseRef(h.session.TaskRef); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	if h.repo, err = opts.DB.GetRepo(ctx, h.session.RepoID); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	if h.clone, err = opts.Git.Open(ctx, h.repo.Path); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, fmt.Errorf("open the registered clone at %s: %w", h.repo.Path, err)
	}
	if h.feature, err = opts.DB.GetManagedFeature(ctx, h.session.TaskRef); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	if h.revisions, err = opts.DB.ListApprovedStoryRevisions(ctx, h.session.TaskRef, h.feature.Generation); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	definition, err := capturedValidate(ctx, opts, req)
	if err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	lockDir := opts.MCP.Paths.StoryProgressLocksDir()
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return ValidateSuiteBindings{}, ValidateFlowBindings{}, err
	}
	repo := GitValidateSuiteRepo{Clone: h.clone, ClonePath: h.repo.Path, Remote: opts.remote(), ScratchRoot: filepath.Join(opts.MCP.Paths.Home(), "validate-scratch"), Gate: opts.CloneGate}
	suite := ValidateSuiteBindings{
		DB: opts.DB, LockDir: lockDir, Repo: repo,
		Bind: func(ctx context.Context, _ Options, _ ActivityRequest) (ValidateSuiteAuthority, error) {
			commit, err := h.tested(ctx)
			return ValidateSuiteAuthority{Commit: commit}, err
		},
	}
	flow := ValidateFlowBindings{
		Worktree:   func() (*git.Worktree, error) { return h.openWorktree(ctx) },
		Session:    func() (string, error) { return h.sessionDir(ctx) },
		KeepChange: h.keepChange,
		Stories: func(ctx context.Context, _ Options, _ ActivityRequest) ([]ValidateStory, error) {
			return h.stories(ctx)
		},
		Repo:      repo,
		Reviewers: &validateReviewers{host: h, definition: definition},
		Slots:     cfg.Slots,
		Delivery: ValidateDelivery{
			Forge:    cfg.Forge,
			Redactor: opts.MCP.Redactor,
			Target: func(context.Context, Options, ActivityRequest) (ValidateDeliveryTarget, error) {
				return h.target(), nil
			},
			Head:    h.head,
			Publish: h.publish,
			OwnerID: func(context.Context, Options) (int64, error) { return cfg.OwnerID, nil },
		},
	}
	return suite, flow, nil
}

// tested is the commit the suite tests: the newest kept repair, or the commit
// the entry pinned.
func (h *validateHost) tested(ctx context.Context) (string, error) {
	commit, err := h.opts.DB.LatestKeptRepairCommit(ctx, h.entry.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return h.entry.Commit, nil
	}
	return commit, err
}

// openWorktree opens the task worktree on the feature branch. Implement merges
// on a scratch tree and pushes, so the task worktree can still stand on an older
// commit of the same branch; a clean worktree that is only behind the tested
// commit is moved forward to it, so a repair starts from, and the owner steps
// see, the commit under review. Anything else is left exactly as it is.
func (h *validateHost) openWorktree(ctx context.Context) (*git.Worktree, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.worktree == nil {
		wt, err := h.checkoutTaskWorktree(ctx)
		if err != nil {
			return nil, fmt.Errorf("open the task worktree: %w", err)
		}
		h.worktree = wt
	}
	wt := h.worktree
	tested, err := h.tested(ctx)
	if err != nil {
		return nil, err
	}
	head, err := wt.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil || head == tested {
		return wt, err
	}
	drift, err := wt.Divergence(ctx, tested)
	if err != nil || drift.Ahead != 0 || drift.Behind == 0 {
		return wt, nil //nolint:nilerr // a worktree that cannot be measured is left as it is
	}
	changes, err := wt.Status(ctx)
	if err != nil || len(changes) != 0 {
		return wt, nil //nolint:nilerr // uncommitted work is never discarded
	}
	session, err := h.sessionDirOf(ctx, wt)
	if err != nil {
		return nil, err
	}
	if err := wt.ResetHard(ctx, tested, session); err != nil {
		return nil, fmt.Errorf("move the task worktree to the tested commit: %w", err)
	}
	return wt, nil
}

// checkoutTaskWorktree creates or reuses the task worktree on the feature
// branch, in one acquisition of the clone gate.
func (h *validateHost) checkoutTaskWorktree(ctx context.Context) (*git.Worktree, error) {
	release, err := h.opts.CloneGate.Acquire(ctx, h.repo.Path)
	if err != nil {
		return nil, err
	}
	defer release()
	dir := h.session.WorktreePath
	if dir == "" {
		if dir, err = naming.WorktreePath(h.repo.WorktreeRoot, h.session.IssueNumber); err != nil {
			return nil, err
		}
	}
	ensured, err := git.EnsureWorktree(ctx, h.clone, git.EnsureWorktreeOptions{Dir: dir, Branch: h.session.Branch, BaseBranch: h.repo.BaseBranch})
	if err != nil {
		return nil, err
	}
	return ensured.Worktree, nil
}

func (h *validateHost) sessionDir(ctx context.Context) (string, error) {
	wt, err := h.openWorktree(ctx)
	if err != nil {
		return "", err
	}
	return h.sessionDirOf(ctx, wt)
}

// sessionDirOf is the session artifact directory Plan and Implement used on
// this branch, or the directory of this day when none exists yet.
func (h *validateHost) sessionDirOf(_ context.Context, wt *git.Worktree) (string, error) {
	now := spikeNow(h.opts)
	if dir, _ := existingSessionArtifacts(wt.Path(), h.session.Branch, h.ref.Issue, now, string(agent.ModeValidate), h.opts.Logger); dir != "" {
		return dir, nil
	}
	return naming.AITeamSessionDir(h.session.Branch, now, h.session.FeatureSlug)
}

// keepChange commits the measured repair as the factory, under the newest
// Validate run of the task.
func (h *validateHost) keepChange(ctx context.Context, summary string) (string, error) {
	wt, err := h.openWorktree(ctx)
	if err != nil {
		return "", err
	}
	author, err := resolveAuthor(ctx, h.opts, h.clone)
	if err != nil {
		return "", err
	}
	runs, err := h.opts.DB.ListRuns(ctx, h.session.ID)
	if err != nil {
		return "", err
	}
	ordinal := 0
	for _, r := range runs {
		if r.Mode == string(agent.ModeValidate) && r.Ordinal > ordinal {
			ordinal = r.Ordinal
		}
	}
	if ordinal < 1 {
		return "", errors.New("a kept repair needs the Validate run that made it")
	}
	release, err := h.opts.CloneGate.Acquire(ctx, h.repo.Path)
	if err != nil {
		return "", err
	}
	defer release()
	result, err := wt.Commit(ctx, git.CommitOptions{Summary: summary, Task: h.session.TaskRef, Mode: string(agent.ModeValidate), Run: ordinal, Author: author})
	if err != nil {
		return "", err
	}
	if !result.Committed {
		return "", errors.New("the repair left nothing to commit")
	}
	return result.Head, nil
}

// stories are the frozen approved stories Implement built, with the criterion
// IDs and the text of their approved revision.
func (h *validateHost) stories(ctx context.Context) ([]ValidateStory, error) {
	var out []ValidateStory
	for _, r := range h.revisions {
		if r.State == "completed" || r.EngineeringComplete {
			continue
		}
		criteria, err := storyCriterionIDs(ctx, h.opts.DB, h.session.TaskRef, h.feature.Generation, r.Key, r.Hash, r.Body)
		if err != nil {
			return nil, fmt.Errorf("criteria of %s: %w", r.Key, err)
		}
		out = append(out, ValidateStory{Key: r.Key, Title: r.Title, Revision: r.Hash, Criteria: criteria, Text: r.Body})
	}
	if len(out) == 0 {
		return nil, errors.New("no approved story is under review")
	}
	return out, nil
}

// target names the delivery: the feature branch onto the base branch, the
// parent issue and the story issues a close completes.
func (h *validateHost) target() ValidateDeliveryTarget {
	t := ValidateDeliveryTarget{Repo: github.Repo{Owner: h.repo.Owner, Name: h.repo.Name}, Branch: h.session.Branch, Base: h.repo.BaseBranch, BotID: h.opts.MCP.BotID, Parent: ValidateIssueRef{Number: h.session.IssueNumber}}
	for _, r := range h.revisions {
		if t.Parent.ID == 0 {
			t.Parent.ID = r.ParentID
		}
		if r.State == "completed" || r.EngineeringComplete {
			continue
		}
		t.Stories = append(t.Stories, ValidateIssueRef{Number: r.IssueNumber, ID: r.IssueID})
	}
	return t
}

// head is the current head of the feature branch in the task worktree.
func (h *validateHost) head(ctx context.Context) (string, error) {
	wt, err := h.openWorktree(ctx)
	if err != nil {
		return "", err
	}
	return wt.RevParse(ctx, "HEAD", git.RevParseOptions{})
}

// publish pushes the tested commit to the feature branch with the guarded push
// Implement uses: no force, and only while the remote is still at expectedHead.
func (h *validateHost) publish(ctx context.Context, commit, expectedHead string) error {
	wt, err := h.openWorktree(ctx)
	if err != nil {
		return err
	}
	head, err := wt.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("the task worktree is at %s, not at the tested commit %s", shortSHA(head), shortSHA(commit))
	}
	if expectedHead == "" {
		return errors.New("the feature branch is missing on the remote; nothing is pushed")
	}
	release, err := h.opts.CloneGate.Acquire(ctx, h.repo.Path)
	if err != nil {
		return err
	}
	defer release()
	return wt.PushHeadTo(ctx, h.opts.remote(), h.session.Branch, expectedHead)
}

// validateReviewers starts each reviewer as its own read-only agent run of the
// task, through the same delivery, result route and metering every graph agent
// node uses, on the scratch checkout of the tested commit.
//
// The shared result route grants one active writer per task, so reviewers of
// one task run one after another even when the review step starts them side by
// side; each still holds its own run slot.
type validateReviewers struct {
	host       *validateHost
	definition *teamsource.WorkflowDefinition
	mu         sync.Mutex
}

// validateReviewNode is the captured node of one reviewer role.
func validateReviewNode(role string) string {
	return "review:validate-" + role
}

// Run starts the reviewer and returns its retained result.
func (r *validateReviewers) Run(ctx context.Context, vr ValidateReviewRequest) (out *agentresult.Result, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.host
	opts := h.opts
	if vr.Access != ValidateReviewAccess || vr.Checkout == "" || vr.InstanceID < 1 || vr.NodeAttemptID < 1 || vr.Attempt == "" {
		return nil, errors.New("a reviewer needs a read-only checkout and its review step")
	}
	nodeName := validateReviewNode(vr.Role)
	if _, ok := r.definition.Nodes[nodeName]; !ok {
		return nil, fmt.Errorf("no reviewer role is captured for %q", vr.Role)
	}
	policy := ValidateWorkflowPolicy()
	nodeReq := h.req
	nodeReq.researchDefinition, nodeReq.researchNode, nodeReq.workflowPolicy = r.definition, nodeName, &policy
	nodeReq.noQuestions = true
	if nodeReq, err = applyNodeAccess(policy, graphexec.Node{Name: nodeName, Attrs: map[string]string{graphexec.AccessAttribute: ValidateReviewAccess}}, nodeReq); err != nil {
		return nil, err
	}
	plan, err := planWorkflowDelivery(opts, nodeReq, r.definition, nodeName, policy)
	if err != nil {
		return nil, err
	}
	firstRef, err := naming.SessionRef(h.ref, store.FirstSegmentOrdinal)
	if err != nil {
		return nil, err
	}
	segment, delivery, err := prepareDeliverySegment(ctx, opts, nodeReq, h.session, h.ref, firstRef, plan)
	if err != nil {
		return nil, fmt.Errorf("prepare the %s reviewer's instructions: %w", vr.Role, err)
	}
	runner, ok := opts.Runners.runner(agent.Kind(segment.Agent))
	if !ok {
		return nil, fmt.Errorf("no coding-agent-tool implementation is registered for %q", segment.Agent)
	}
	trigger, err := opts.DB.NextValidateReviewTrigger(ctx, h.session.ID)
	if err != nil {
		return nil, err
	}
	var pathErr error
	run, err := opts.DB.AllocateRun(ctx, store.AllocateRunParams{SessionID: h.session.ID, SegmentID: segment.ID, Mode: string(agent.ModeValidate), Agent: segment.Agent, TriggerCommentID: trigger, LogPath: func(ordinal int) string {
		path, err := naming.RunLogPath(opts.Paths.Home(), h.ref, ordinal)
		pathErr = err
		return path
	}})
	if err = errors.Join(pathErr, err); err != nil {
		return nil, fmt.Errorf("allocate the %s reviewer's run: %w", vr.Role, err)
	}
	var process *agent.RunResult
	defer func() {
		outcome := store.RunOutcome{Status: store.RunFailed, FinishedAt: spikeNow(opts)}
		if process != nil {
			outcome = runOutcome(process)
		}
		if out == nil {
			outcome.Status = store.RunFailed
			if err != nil {
				outcome.Error = clipText(err.Error(), 500)
			}
		}
		_, finishErr := opts.DB.FinishRun(context.WithoutCancel(ctx), run.ID, outcome)
		err = errors.Join(err, finishErr)
	}()

	request := agent.Request{
		Delivery:       delivery,
		Ref:            h.ref,
		RunOrdinal:     run.Ordinal,
		SegmentOrdinal: segment.Ordinal,
		Session:        agent.SessionRef{Ref: segment.SessionRef},
		Mode:           string(agent.ModeValidate),
		Path:           agent.PathCold,
		WorktreePath:   vr.Checkout,
		Permissions:    requestPermissions(nodeReq),
		Invocation:     agent.InvocationResearchNode,
		Graph:          &agent.GraphInvocation{InstanceID: vr.InstanceID, AttemptID: vr.NodeAttemptID, RunID: run.ID, Node: nodeName},
	}
	prompt, err := validateReviewPrompt(vr)
	if err != nil {
		return nil, err
	}
	if delivery != nil {
		prompt = "Instruction bundle: " + delivery.Root + "\n\n" + prompt
	}
	if delivery == nil || delivery.MainRole == "" {
		prompt += policy.Guidance
	}
	request.Prompt = prompt
	prep := &preparation{mcpRuntime: opts.MCP, meterDB: opts.DB, repo: h.repo, session: h.session, ref: h.ref, segment: segment, run: run, runner: runner, request: request}
	w := &researchWorkflow{opts: opts, req: nodeReq, policy: policy, prep: prep}
	scope := cloneResultContext(vr.Scope)
	route, err := w.openResultRoute(ctx, &request, &resultOverride{
		Resolve: func(check context.Context) (agentresult.Context, error) {
			return cloneResultContext(scope), check.Err()
		},
		Targets: agentresult.EditTargets(string(agent.ModeValidate)),
		Pass:    vr.Attempt,
		Attempt: vr.Attempt,
	})
	if err != nil {
		return nil, fmt.Errorf("open the %s reviewer's result route: %w", vr.Role, err)
	}
	transportsOpen := true
	defer func() {
		bookkeeping := context.WithoutCancel(ctx)
		if transportsOpen {
			err = errors.Join(err, route.prepared.closeTransports(bookkeeping))
		}
		err = errors.Join(err, opts.DB.EndResultAttempt(bookkeeping, route.prepared.service.Binding()))
	}()
	request.Prompt += route.guidance
	if err := route.prepared.bindFinalRoute(ctx, &request, nil); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	process, runErr := runMeteredWith(ctx, prep, opts.Logger, request, runner.Run)
	// The helper transports are joined before the result is read, as for every
	// graph agent node.
	transportsOpen = false
	if closeErr := route.prepared.closeTransports(context.WithoutCancel(ctx)); closeErr != nil {
		runErr = errors.Join(runErr, closeErr)
	}
	// An accepted submission is the reviewer's answer, however the process
	// ended afterwards; a factory shutdown is not the reviewer's.
	if ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), runErr)
	}
	snapshot, _, readErr := route.prepared.service.retainedCurrent(ctx)
	if readErr != nil {
		reason := "the reviewer ended without submitting a result"
		if process != nil && strings.TrimSpace(process.Reason) != "" {
			reason += ": " + process.Reason
		}
		return nil, errors.Join(errors.New(reason), runErr, readErr)
	}
	result := snapshot.Result
	return &result, nil
}

// Recover finds the result a reviewer already submitted under its attempt. It
// starts nothing.
func (r *validateReviewers) Recover(ctx context.Context, attempt string) (*agentresult.Result, bool, error) {
	db := r.host.opts.DB
	bindings, err := db.ResultBindingsByAttempt(ctx, attempt)
	if err != nil {
		return nil, false, err
	}
	for _, b := range bindings {
		revision, err := db.GetResultRevision(ctx, b, 0)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		snapshot, _, err := decodeRetainedResult(revision)
		if err != nil {
			return nil, false, err
		}
		if snapshot.Meta.AttemptID != attempt || snapshot.Meta.RunID != b.RunID {
			continue
		}
		result := snapshot.Result
		return &result, true, nil
	}
	return nil, false, nil
}

// validateReviewInput is the factory data one reviewer is given.
type validateReviewInput struct {
	Role       string             `json:"role"`
	Commit     string             `json:"commit"`
	SnapshotID string             `json:"snapshot_id"`
	Stories    []ValidateStory    `json:"stories"`
	Evidence   []ValidateEvidence `json:"evidence"`
}

func validateReviewPrompt(vr ValidateReviewRequest) (string, error) {
	data, err := json.Marshal(validateReviewInput{Role: vr.Role, Commit: vr.Commit, SnapshotID: vr.SnapshotID, Stories: vr.Stories, Evidence: vr.Evidence})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Factory invocation: Validate %s review of commit %s. The working directory is a read-only checkout of exactly that commit; read it with your file tools or read-only shell commands, never run tests or builds. Report one entry under stories for every story below, with the snapshot ID exactly as given, and cite only the check IDs of the evidence list; only a passed command proves a finding.\n<validate-review>%s</validate-review>\n", vr.Role, vr.Commit, data), nil
}
