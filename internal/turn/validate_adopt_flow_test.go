package turn

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// adoptFlow wires the real Validate graph, behind the real admission, to a fake
// check runner, fake reviewers and an in-memory forge. No test below starts an
// agent: a passing suite needs no repair.
type adoptFlow struct {
	*validateAdmissionFixture
	forge     *ownerForge
	reviewers *flowReviewers
	suiteRepo *suiteRepoFake
	mu        sync.Mutex
	commands  []string // commit under test, one per full-phase command run
	// interrupt, when set, is called as the named command starts, once; calling
	// its cancel function is a restart in the middle of the suite.
	interrupt string
	cancel    context.CancelFunc
}

func newAdoptFlow(t *testing.T) *adoptFlow {
	t.Helper()
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	a := &adoptFlow{validateAdmissionFixture: v, forge: newOwnerForge(), reviewers: &flowReviewers{retained: map[string]*agentresult.Result{}}, suiteRepo: &suiteRepoFake{recipe: []byte(suiteRecipe), trees: []string{"tree-a"}}}
	bodies, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mustProgress(t, err)
	reporter, err := NewReporter(ReporterOptions{BotUserID: ownerBotID, DB: v.f.db, Forge: a.forge, Renderer: bodies, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mustProgress(t, err)
	v.f.opts.ResearchReporter = reporter
	slots := &flowSlots{free: 3}
	lockDir := t.TempDir()
	v.f.opts.Validate.Bind = func(_ context.Context, _ Options, _ Request, entry store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
		v.binds++
		suite := ValidateSuiteBindings{
			DB: v.f.db, LockDir: lockDir, Repo: a.suiteRepo, Now: time.Now,
			Bind: func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
				return ValidateSuiteAuthority{Commit: entry.Commit}, nil
			},
			Stopped: func(context.Context, string) (bool, error) { return true, nil },
			Run: func(ctx context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
				run, err := v.f.db.RunningSuiteRun(ctx)
				mustProgress(t, err)
				a.mu.Lock()
				a.commands = append(a.commands, run.Commit)
				a.mu.Unlock()
				res := bugchecks.CommandResult{Started: time.Now(), Ended: time.Now(), CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "id-" + c.Argv[0], Output: "ok"}
				_ = started(res)
				if a.cancel != nil && c.Argv[0] == a.interrupt {
					cancel := a.cancel
					a.cancel = nil
					cancel()
					res.Outcome, res.Reason = "blocked", "the factory stopped"
					return res
				}
				code := 0
				res.Exit, res.Outcome = &code, "passed"
				return res
			},
		}
		flow := ValidateFlowBindings{
			Worktree: func() (*git.Worktree, error) { return nil, io.ErrClosedPipe },
			Session:  func() (string, error) { return flowSessionDir, nil },
			KeepChange: func(context.Context, string) (string, error) {
				return "", io.ErrClosedPipe
			},
			Scope: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
				return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "validate", WorkflowVersion: store.WorkflowSeparated}, nil
			},
			Stories:   func(context.Context, Options, ActivityRequest) ([]ValidateStory, error) { return a.stories(), nil },
			Repo:      a.suiteRepo,
			Reviewers: a.reviewers,
			Slots:     slots,
			Delivery: ValidateDelivery{
				Forge:    a.forge,
				Redactor: redact.New(redact.Options{Environ: []string{}}),
				Target: func(context.Context, Options, ActivityRequest) (ValidateDeliveryTarget, error) {
					return ValidateDeliveryTarget{Repo: github.Repo{Owner: "acme", Name: "api"}, Branch: "lf/497-validate", Base: "main", BotID: ownerBotID, Parent: ValidateIssueRef{Number: ownerParent, ID: 4970}}, nil
				},
				Head:    func(context.Context) (string, error) { return entry.Commit, nil },
				Publish: func(context.Context, string, string) error { return nil },
				OwnerID: func(context.Context, Options) (int64, error) { return validateOwnerID, nil },
			},
		}
		return suite, flow, nil
	}
	return a
}

func (a *adoptFlow) stories() []ValidateStory {
	return []ValidateStory{{Key: "story-001", Title: "Health", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001"}}}
}

// reached is the state a start leaves: it must not depend on how it began.
type adoptReached struct {
	Node, Status, Wait string
	Entries            int
	Instances          int
	FullCommands       int
	Reviewers          int
	Outcome            string
	Commit             string
}

func (a *adoptFlow) reached(t *testing.T) adoptReached {
	t.Helper()
	var r adoptReached
	inst, err := a.f.db.LatestWorkflowInstance(t.Context(), a.f.session.ID, "validate")
	mustProgress(t, err)
	r.Node, r.Status = inst.CurrentNode, inst.Status
	if wait, err := a.f.db.GetGraphOwnerWait(t.Context(), inst.ID); err == nil {
		r.Wait = wait.State
	}
	entries := a.entries(t)
	r.Entries = len(entries)
	for _, e := range entries {
		if e.InstanceID > 0 {
			r.Instances++
		}
		r.Commit = e.Commit
	}
	a.mu.Lock()
	r.FullCommands = len(a.commands)
	a.mu.Unlock()
	r.Reviewers = len(a.reviewers.roles())
	if o, err := a.f.db.GetValidateOutcome(t.Context(), inst.ID, r.Commit); err == nil {
		r.Outcome = o.Outcome
	}
	return r
}

func (a *adoptFlow) manual(t *testing.T, comment int64) (Result, error) {
	t.Helper()
	return Execute(t.Context(), a.f.opts, a.request(comment, validateOwnerID, "/validate check the finished work"))
}

func (a *adoptFlow) auto(t *testing.T) (Result, error) {
	t.Helper()
	stored, _, err := a.f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: a.f.session.ID, SourceCommentID: 1<<56 | 7, OwnerHandle: "owner", Payload: ValidateAutoPayload, DueAt: implementClock})
	mustProgress(t, err)
	req := a.request(stored.TriggerID(), 0, ValidateAutoPayload)
	req.Trigger.AuthorID = 0
	return Execute(t.Context(), a.f.opts, req)
}

func TestValidateManualStartRunsSuiteReviewAndOutcomeToTheOwnerWait(t *testing.T) {
	a := newAdoptFlow(t)
	out, err := a.manual(t, 6001)
	if err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	got := a.reached(t)
	t.Logf("reached: %+v", got)
	if got.Wait != "waiting" || got.Instances != 1 || got.FullCommands != 3 || got.Reviewers != 3 || got.Outcome == "" || got.Commit != validateHeadA {
		t.Fatalf("a manual start ends at the owner wait with the suite, reviews and outcome done once: %+v", got)
	}
}

func TestValidateAutomaticStartReachesTheSameStateAsAManualStart(t *testing.T) {
	m := newAdoptFlow(t)
	if out, err := m.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	a := newAdoptFlow(t)
	if out, err := a.auto(t); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	manual, automatic := m.reached(t), a.reached(t)
	if manual != automatic {
		t.Fatalf("the result must not depend on how Validate began:\nmanual    %+v\nautomatic %+v", manual, automatic)
	}
	if automatic.Wait != "waiting" || automatic.Instances != 1 || automatic.Outcome == "" {
		t.Fatalf("%+v", automatic)
	}
	if e := a.entries(t)[0]; e.Source != store.ValidateSourceAuto || e.CommentID != 0 {
		t.Fatalf("the automatic start is recorded as automatic: %+v", e)
	}
}

func TestValidateRestartAfterTheEntryWasSavedStillStartsOnce(t *testing.T) {
	a := newAdoptFlow(t)
	// The factory stopped after it saved the entry and before it admitted it.
	_, created, err := a.f.db.EnsureValidateEntry(t.Context(), store.ValidateEntry{SessionID: a.f.session.ID, Commit: validateHeadA, Branch: "lf/123-feature", Source: store.ValidateSourceOwner, CommentID: 6001, AuthorID: validateOwnerID}, implementClock)
	mustProgress(t, err)
	if !created {
		t.Fatal("entry not saved")
	}
	if out, err := a.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if got := a.reached(t); got.Entries != 1 || got.Instances != 1 || got.Wait != "waiting" || got.FullCommands != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestValidateRestartAfterAdmissionLosesNoStartAndMakesNoSecondRun(t *testing.T) {
	a := newAdoptFlow(t)
	realBind := a.f.opts.Validate.Bind
	failOnce := true
	a.f.opts.Validate.Bind = func(ctx context.Context, o Options, r Request, e store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
		if failOnce {
			failOnce = false
			return ValidateSuiteBindings{}, ValidateFlowBindings{}, errValidateStop
		}
		return realBind(ctx, o, r, e)
	}
	// The factory stopped after admission, before the graph ran a step.
	if _, err := a.manual(t, 6001); err == nil {
		t.Fatal("the first start must stop")
	}
	if got := a.entries(t); len(got) != 1 || got[0].State != store.ValidateEntryAdmitted {
		t.Fatalf("admitted before the stop: %+v", got)
	}
	// The same comment is read again, with a duplicate /validate in between.
	dup, err := a.manual(t, 6002)
	if err != nil || !strings.Contains(dup.Reason, "already been started") {
		t.Fatalf("a duplicate starts nothing: %+v %v", dup, err)
	}
	if out, err := a.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if got := a.reached(t); got.Entries != 1 || got.Instances != 1 || got.Wait != "waiting" || got.FullCommands != 3 || got.Reviewers != 3 {
		t.Fatalf("exactly one run, nothing lost: %+v", got)
	}
}

func TestValidateRestartMidSuiteNeedsOneOwnerStartAndNeverTwoLiveRuns(t *testing.T) {
	a := newAdoptFlow(t)
	ctx, cancel := context.WithCancel(t.Context())
	a.interrupt, a.cancel = "full-two", cancel
	_, _ = Execute(ctx, a.f.opts, a.request(6001, validateOwnerID, "/validate check the finished work"))
	if got := a.reached(t); got.Wait == "waiting" || got.Instances != 1 || got.FullCommands != 2 {
		t.Fatalf("the run stopped inside the suite: %+v", got)
	}
	// The factory restarts: startup settles the interrupted step, as the daemon does.
	n, err := a.f.db.ReconcileWorkflowGraphs(t.Context(), "validate", time.Now())
	mustProgress(t, err)
	if n != 1 {
		t.Fatalf("one interrupted step settled: %d", n)
	}
	// Replaying the old comment does not start anything; it says what happened.
	if again, err := a.manual(t, 6001); err != nil || !strings.Contains(again.Reason, "stopped before it finished") {
		t.Fatalf("%+v %v", again, err)
	}
	// The owner's new /validate starts the run anew, once; a duplicate repeats nothing.
	if out, err := a.manual(t, 6002); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	dup, err := a.manual(t, 6003)
	if err != nil || !strings.Contains(dup.Reason, "already been started") {
		t.Fatalf("%+v %v", dup, err)
	}
	got := a.reached(t)
	if got.Entries != 1 || got.Instances != 1 || got.Wait != "waiting" || got.Outcome == "" || got.Reviewers != 3 {
		t.Fatalf("one entry, finished through the owner wait: %+v", got)
	}
	live := 0
	for _, id := range []int64{a.entries(t)[0].InstanceID} {
		inst, err := a.f.db.GetGraphInstance(t.Context(), id)
		mustProgress(t, err)
		if inst.Status == store.GraphRunning {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("exactly one live Validate run: %d", live)
	}
	pred, _, err := a.f.db.WorkflowPredecessor(t.Context(), a.entries(t)[0].InstanceID)
	mustProgress(t, err)
	if pred == 0 {
		t.Fatal("the new run links the interrupted one as its predecessor")
	}
}
