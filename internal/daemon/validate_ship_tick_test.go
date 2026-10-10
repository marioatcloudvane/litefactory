package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

const shipTickRecipe = `version: 1
phases:
  regression:
    commands:
      - argv: ["check-regression"]
        cwd: "."
        timeout_seconds: 5
  focused:
    commands:
      - argv: ["check-focused"]
        cwd: "."
        timeout_seconds: 5
  full:
    commands:
      - argv: ["full-one"]
        cwd: "."
        timeout_seconds: 5
`

// shipTickRepo is a scratch checkout that only answers the recipe.
type shipTickRepo struct{}

func (shipTickRepo) Begin(context.Context, string) (turn.ValidateSuiteScratch, error) {
	return shipTickScratch{}, nil
}

type shipTickScratch struct{}

func (shipTickScratch) Path() string                                   { return "/scratch" }
func (shipTickScratch) Recipe(context.Context) ([]byte, error)         { return []byte(shipTickRecipe), nil }
func (shipTickScratch) Observe(context.Context) (string, error)        { return "tree-a", nil }
func (shipTickScratch) Close(context.Context) error                    { return nil }
func (shipTickSlots) TryAcquire(context.Context) (func(), bool, error) { return func() {}, true, nil }

type shipTickSlots struct{}

// shipTickReviewers satisfies every criterion from the passed suite commands.
type shipTickReviewers struct{}

func (shipTickReviewers) Run(_ context.Context, req turn.ValidateReviewRequest) (*agentresult.Result, error) {
	var passed []string
	for _, e := range req.Evidence {
		if e.State == store.SuitePassed {
			passed = append(passed, e.ID)
		}
	}
	finding := func(name, class string) agent.ContributionFinding {
		return agent.ContributionFinding{Name: name, Classification: class, Status: "satisfied", Evidence: []agent.EvidenceExplanation{{Summary: "A passed suite command covers it.", CheckIDs: passed[:1]}}}
	}
	var stories []agentresult.AssessmentStory
	for _, s := range req.Stories {
		a := agentresult.AssessmentStory{Story: s.Key, SnapshotID: req.SnapshotID, Criteria: []agent.ContributionFinding{}, Reviews: []agent.ContributionFinding{}}
		switch req.Role {
		case agent.ReviewRoleAcceptance:
			for _, c := range s.Criteria {
				a.Criteria = append(a.Criteria, finding(c, "blocking"))
			}
			a.Reviews = append(a.Reviews, finding("plan", "advisory"))
		case agent.ReviewRoleCode:
			a.Reviews = append(a.Reviews, finding("code", "advisory"))
		case agent.ReviewRoleArchitecture:
			a.Reviews = append(a.Reviews, finding("architecture", "advisory"))
		}
		stories = append(stories, a)
	}
	return &agentresult.Result{Outcome: "completed", Comment: "Reviewed " + req.Role + ".", Payload: agentresult.Payload{Validate: &agentresult.Validate{Stories: stories}}}, nil
}

func (shipTickReviewers) Recover(context.Context, string) (*agentresult.Result, bool, error) {
	return nil, false, nil
}

// shipTickForge answers the delivery reads the push step makes; every other
// forge call is outside this test and panics.
type shipTickForge struct {
	turn.ValidateForge
	mu      sync.Mutex
	remote  string
	publish []string
	prReads int
}

func (f *shipTickForge) GetDeliveryHead(context.Context, github.Repo, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remote == "" {
		return "", github.ErrNotFound
	}
	return f.remote, nil
}

// ListDeliveryPRs ends the test's forge at the pull request, past the push.
func (f *shipTickForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prReads++
	return nil, errors.New("this test stops at the pull request")
}

// noAgentRunner fails the test if any plain agent process is started.
type noAgentRunner struct{ t *testing.T }

func (r noAgentRunner) Run(_ context.Context, req agent.Request) (*agent.RunResult, error) {
	r.t.Errorf("no agent may run for a Validate owner control: mode=%s", req.Mode)
	return nil, errors.New("no agent")
}

// #591 c-ship-tick: a Validate instance waits for the owner at a published
// report; the owner's /validate-ship goes through the real tick (claim) and the
// real turn.Execute with Validate wiring, and the same instance continues past
// owner_resolve to the push step. No plain agent run is started.
func TestValidateShipCommentContinuesTheWaitingInstanceThroughTheTick(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
	f.settle()
	session := f.sessionOf(testIssue)
	parent := session.TaskRef

	// A managed parent whose approved story Implement merged.
	story := store.ManagedStory{ParentTask: parent, Generation: 1, Key: "story-001", IssueNumber: 201, IssueID: 7001, State: "planning"}
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	check(f.db.PutManagedStory(ctx, story))
	check(f.db.SetManagedFeatureReady(ctx, parent, 1, true))
	check(f.db.ApproveManagedStories(ctx, parent, 1, []store.ApprovedStoryRevision{{ManagedStory: story, ParentID: 9, Title: "Health", Body: "## Acceptance criteria\n\n- AC-001: the endpoint answers\n", Hash: strings.Repeat("cd", 32), FetchedAt: f.clock.peek()}}))
	check(f.db.DeliverStoryScopeNotice(ctx, parent, 1, story.Key, "freeze"))
	check(f.db.ActivateManagedScope(ctx, parent, 1))
	_, err := f.db.FreezeSessionSlug(ctx, session.ID, "feature", "lf/123-feature")
	check(err)
	if session, err = f.db.GetSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	// The story's own comments are already swept; this test is about the parent.
	check(f.db.AdvanceStoryDiscovery(ctx, session.ID, story.IssueNumber, 1))
	check(f.db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{{Key: story.Key, Branch: "lf/201-a"}}, f.clock.peek()))
	row, err := f.db.GetImplementStory(ctx, parent, 1, story.Key)
	check(err)
	from := store.ImplementWaiting
	for _, to := range []string{store.ImplementRunning, store.ImplementDone, store.ImplementMerged} {
		tr := store.ImplementTransition{Parent: parent, Generation: 1, Key: story.Key, From: from, To: to, ExpectedRevision: row.Revision, At: f.clock.peek()}
		if to == store.ImplementMerged {
			tr.MergeCommit = strings.Repeat("c", 40)
		}
		row, err = f.db.TransitionImplementStory(ctx, tr)
		check(err)
		from = to
	}
	implementPin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("b", 64)})
	check(err)
	implement, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: implementPin.ID, CurrentNode: "start", StartedAt: f.clock.peek()})
	check(err)
	check(f.db.SetGraphInstanceStatus(ctx, implement.ID, store.GraphSucceeded, time.Time{}, f.clock.peek()))

	// The real turn.Execute with Validate wiring; only the check runner, the
	// reviewers and the delivery forge are fakes.
	const head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	binary, err := git.Resolve(ctx)
	check(err)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { check(authority.Close(context.WithoutCancel(ctx))) })
	specs, err := turn.ValidateFlowSpecs()
	check(err)
	captured, err := teamsource.CaptureValidate(ctx, specs)
	check(err)
	forge := &shipTickForge{}
	lockDir := t.TempDir()
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "validate-ship-tick", Logger: f.logger,
		CloneGate: unusedRecoveryCloneGate{}, Runners: turn.RunnerSet{agent.Kind(store.AgentClaude): noAgentRunner{t}}, ResearchReporter: f.reporter,
		MCP: &turn.MCPRuntime{Authority: authority, Paths: f.paths, Executable: "/factory/litefactory", Now: f.clock.Now}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		if workflow != agent.WorkflowValidate {
			t.Errorf("only Validate runs here, not %s", workflow)
		}
		definition, err := teamsource.CaptureValidate(ctx, specs)
		if err != nil {
			return nil, err
		}
		config := []byte("validate: validate/validate.dot\n")
		digest := sha256.Sum256(config)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "validate/validate.dot", GraphFile: "validate.dot", Team: definition.Profile.Team, TeamConfig: config, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	var bound []int64
	opts.Validate = &turn.ValidateRuntime{
		OwnerID: func(context.Context, turn.Options) (int64, error) { return ownerID, nil },
		Head:    func(context.Context, turn.Options, store.Session) (string, error) { return head, nil },
		Bind: func(_ context.Context, _ turn.Options, _ turn.Request, entry store.ValidateEntry) (turn.ValidateSuiteBindings, turn.ValidateFlowBindings, error) {
			bound = append(bound, entry.InstanceID)
			suite := turn.ValidateSuiteBindings{
				DB: f.db, LockDir: lockDir, Repo: shipTickRepo{}, Now: f.clock.Now,
				Bind: func(context.Context, turn.Options, turn.ActivityRequest) (turn.ValidateSuiteAuthority, error) {
					return turn.ValidateSuiteAuthority{Commit: entry.Commit}, nil
				},
				Stopped: func(context.Context, string) (bool, error) { return true, nil },
				Run: func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
					code := 0
					res := bugchecks.CommandResult{Started: f.clock.Now(), Ended: f.clock.Now(), CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "id-" + c.Argv[0], Output: "ok"}
					_ = started(res)
					res.Exit, res.Outcome = &code, "passed"
					return res
				},
			}
			flow := turn.ValidateFlowBindings{
				Worktree:   func() (*git.Worktree, error) { return nil, io.ErrClosedPipe },
				Session:    func() (string, error) { return ".aiteam/validate-session", nil },
				KeepChange: func(context.Context, string) (string, error) { return "", io.ErrClosedPipe },
				Scope: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
					return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "validate", WorkflowVersion: store.WorkflowSeparated}, nil
				},
				Stories: func(context.Context, turn.Options, turn.ActivityRequest) ([]turn.ValidateStory, error) {
					return []turn.ValidateStory{{Key: story.Key, Title: "Health", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001"}}}, nil
				},
				Repo:      shipTickRepo{},
				Reviewers: shipTickReviewers{},
				Slots:     shipTickSlots{},
				Delivery: turn.ValidateDelivery{
					Forge:    forge,
					Redactor: redact.New(redact.Options{Environ: []string{}}),
					Target: func(context.Context, turn.Options, turn.ActivityRequest) (turn.ValidateDeliveryTarget, error) {
						return turn.ValidateDeliveryTarget{Repo: github.Repo{Owner: testOwnerName, Name: testRepoName}, Branch: session.Branch, Base: "main", BotID: botID, Parent: turn.ValidateIssueRef{Number: testIssue, ID: 1230}}, nil
					},
					Head: func(context.Context) (string, error) { return entry.Commit, nil },
					Publish: func(_ context.Context, commit, _ string) error {
						forge.mu.Lock()
						defer forge.mu.Unlock()
						forge.publish = append(forge.publish, commit)
						forge.remote = commit
						return nil
					},
					OwnerID: func(context.Context, turn.Options) (int64, error) { return ownerID, nil },
				},
			}
			return suite, flow, nil
		},
	}
	var requests []turn.Request
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		requests = append(requests, req)
		return turn.Execute(ctx, opts, req)
	})

	// The owner starts Validate; the run rests at the owner wait.
	f.forge.addComment(testIssue, 42, owner(), "/validate check the finished work")
	f.settle()
	instance, err := f.db.LatestWorkflowInstance(ctx, session.ID, "validate")
	if err != nil {
		t.Fatalf("Validate started: %v\n%s", err, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("the run waits for the owner: %+v %v\n%s", wait, err, f.logs)
	}
	state, err := turn.LoadValidateState(ctx, opts, captured.Profile, instance.ID)
	check(err)
	if state.Revision == "" {
		t.Fatal("the report has a revision")
	}

	// The owner ships the current revision.
	const shipComment = 900_000
	f.forge.addComment(testIssue, shipComment, owner(), turn.ValidateShipCommand+" "+state.Revision+" -- All good")
	f.settle()

	if len(requests) != 2 || requests[1].Trigger.CommentID != shipComment {
		t.Fatalf("the ship comment reached turn.Execute once: %+v\n%s", requests, f.logs)
	}
	after, err := f.db.LatestWorkflowInstance(ctx, session.ID, "validate")
	check(err)
	if after.ID != instance.ID {
		t.Fatalf("the same instance continues, no successor: %d, was %d", after.ID, instance.ID)
	}
	if pred, _, err := f.db.WorkflowPredecessor(ctx, after.ID); err != nil || pred != 0 {
		t.Fatalf("no successor link: %d %v", pred, err)
	}
	if len(bound) != 2 || bound[1] != instance.ID {
		t.Fatalf("the ship ran the waiting instance's graph: %v", bound)
	}
	attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	check(err)
	var pushed, shipRun bool
	for _, a := range attempts {
		pushed = pushed || a.Node == "owner_push"
	}
	run, found, err := f.db.GetRunByTrigger(ctx, session.ID, shipComment)
	check(err)
	if found {
		if shipRun, err = f.db.HasWorkflowGraphRun(ctx, run.ID, "validate"); err != nil || !shipRun {
			t.Fatalf("the run for the ship comment is a graph run: %+v %v %v", run, shipRun, err)
		}
	}
	if !pushed {
		t.Fatalf("the ship reached the push step: node %s attempts %+v\n%s", after.CurrentNode, attempts, f.logs)
	}
	forge.mu.Lock()
	published := append([]string(nil), forge.publish...)
	forge.mu.Unlock()
	if len(published) != 1 || published[0] != head {
		t.Fatalf("the push published the tested commit once: %v", published)
	}
	if wait, err = f.db.GetGraphOwnerWait(ctx, instance.ID); err == nil && wait.State == "claimed" && wait.ReplyID == shipComment {
		t.Fatalf("the claimed ship reply was consumed: %+v", wait)
	}
}
