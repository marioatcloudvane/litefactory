package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const deniedReason = "Read access to the checkout was denied"

func deniedReview(ValidateReviewRequest) (*agentresult.Result, error) {
	return &agentresult.Result{Outcome: "blocked", Reason: deniedReason}, nil
}

// A passed suite with every reviewer failed is one problem, not one owner task
// per item: each reviewer is named once with its reason, the items are listed
// once as not reviewed and the one next step is to run Validate again.
func TestValidateFailedReviewersAreReportedOnceNotPerItem(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = deniedReview
	o.toOwner()
	st := o.state()
	if st.Suite.Outcome != store.SuitePassed {
		t.Fatalf("the suite passed: %+v", st.Suite)
	}
	var ids []string
	for _, f := range st.Findings {
		if f.State != findingNotReviewed {
			t.Fatalf("every item is not reviewed: %+v", f)
		}
		ids = append(ids, f.ID)
	}
	if len(ids) != 5 {
		t.Fatalf("two criteria and three reviews: %v", ids)
	}
	// BC-3: one Open line per failed reviewer, push not offered.
	if len(st.Open) != 3 || st.Ready {
		t.Fatalf("one open line per reviewer: %v", st.Open)
	}
	for i, role := range agent.ReviewRoles {
		if !strings.Contains(st.Open[i], "The "+role+" reviewer has no usable result") || !strings.Contains(st.Open[i], deniedReason) {
			t.Errorf("open line %d: %q", i, st.Open[i])
		}
	}
	commands := joinCmds(st.NextCommands(nil))
	for _, c := range []string{ValidatePushCommand, ValidateCheckCommand, ValidateFindingCommand} {
		if strings.Contains(commands, c) {
			t.Errorf("no %s command for not-reviewed items: %s", c, commands)
		}
	}

	// BC-1, BC-2: the posted report.
	reports := o.forge.bodies("**Validate report**")
	if len(reports) != 1 {
		t.Fatalf("one report: %d", len(reports))
	}
	out := reports[0].Body
	// #571: each reviewer column of the story row names the reviewer's reason,
	// the row is red and the one Next line is the retry.
	if n := strings.Count(out, "not reviewed: failed: the reviewer could not finish: "+deniedReason); n != len(agent.ReviewRoles) {
		t.Errorf("each reviewer is named once with its reason (%d):\n%s", n, out)
	}
	if !strings.Contains(out, "| "+lightRed+" story-002") || !strings.HasPrefix(out, lightRed) {
		t.Errorf("the story row and the header are red:\n%s", out)
	}
	if n := strings.Count(out, "Run Validate again on commit `"+short(st.Commit)+"`"); n != 1 || !strings.Contains(out, "\nNext: `"+ValidateShipCommand) || !strings.Contains(out, "the passed suite is reused and only the reviews run again") {
		t.Errorf("exactly one retry step as the Next line (%d):\n%s", n, out)
	}
	if strings.Contains(out, findingAwaitingCheck) || strings.Contains(out, ValidateCheckCommand) || strings.Contains(out, ValidateFindingCommand) || strings.Contains(out, "review, optional") {
		t.Errorf("no owner task for a not-reviewed item:\n%s", out)
	}

	// BC-4: a check or a choice on a not-reviewed item is refused and records nothing.
	rev := st.Revision
	for _, body := range []string{
		o.command(ValidateCheckCommand, "story-002:AC-001", "pass", "-- looked at it"),
		o.command(ValidateFindingCommand, "story-002:code", "defer"),
		o.command(ValidateFindingCommand, "story-002:plan", "repair"),
	} {
		o.say(body)
		if got := o.notice(); !strings.Contains(got, "was not reviewed") || !strings.Contains(got, ValidateStartCommand) {
			t.Errorf("%s: notice %q", body, got)
		}
		if o.state().Revision != rev {
			t.Fatalf("%s changed the records", body)
		}
	}
	verdicts, err := o.db.ListValidateVerdicts(t.Context(), o.instance.ID, st.Commit, true)
	mustProgress(t, err)
	choices, err := o.db.ListValidateChoices(t.Context(), o.instance.ID, st.Commit, true)
	mustProgress(t, err)
	if len(verdicts)+len(choices) != 0 {
		t.Fatalf("nothing is recorded: %d verdicts, %d choices", len(verdicts), len(choices))
	}
}

// BC-5: only the failed reviewer's items are not reviewed; a real manual check
// and a real finding from reviewers that ran keep their own commands.
func TestValidateOneFailedReviewerLeavesTheOthersItems(t *testing.T) {
	o := newOwnerFixture(t)
	o.reviewers.run = func(req ValidateReviewRequest) (*agentresult.Result, error) {
		if req.Role == agent.ReviewRoleCode {
			return nil, errors.New(deniedReason)
		}
		return mixedReview(req)
	}
	o.toOwner()
	st := o.state()
	want := map[string]string{
		"story-002:AC-001":       findingSatisfied,
		"story-002:AC-002":       findingAwaitingCheck,
		"story-002:plan":         findingSatisfied,
		"story-002:code":         findingNotReviewed,
		"story-002:architecture": findingSatisfied,
	}
	for id, state := range want {
		if f, ok := st.finding(id); !ok || f.State != state {
			t.Errorf("%s: want %s, got %+v", id, state, f)
		}
	}
	if len(st.Open) != 2 || !strings.Contains(strings.Join(st.Open, "\n"), "The code reviewer has no usable result") || st.Ready {
		t.Fatalf("one line for the code reviewer and one for the check: %v", st.Open)
	}
	commands := joinCmds(st.NextCommands(nil))
	if !strings.Contains(commands, ValidateCheckCommand+" "+st.Revision+" story-002:AC-002 pass") || strings.Contains(commands, "story-002:code") {
		t.Fatalf("the manual check keeps its command, the code review has none: %s", commands)
	}
	out := renderValidateReport(st, nil, "")
	if !strings.Contains(out, "1 of 2 satisfied · 1 awaiting owner check") || strings.Count(out, "not reviewed: ") != 1 || !strings.Contains(out, "\nNext: `"+ValidateShipCommand) || strings.Contains(out, ValidateCheckCommand) {
		t.Fatalf("report:\n%s", out)
	}
}

// The step the report names works: an owner /validate on the same commit ends
// the waiting run whose reviews did not happen and starts a fresh one that reuses
// the passed suite, so only the reviews run again.
func TestValidateSameCommitStartReplacesARunWithoutUsableReviews(t *testing.T) {
	a := newAdoptFlow(t)
	a.reviewers.run = deniedReview
	if out, err := a.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	first := a.reached(t)
	if first.Wait != "waiting" || first.FullCommands != 3 || first.Reviewers != 3 || first.Outcome != store.ValidateAssessed {
		t.Fatalf("the first run waits for the owner after failed reviews: %+v", first)
	}
	old := a.entries(t)[0].InstanceID

	// The factory's own start never ends it.
	if out, err := a.auto(t); err != nil || !strings.Contains(out.Reason, "already been started") {
		t.Fatalf("%+v %v", out, err)
	}
	a.reviewers.run = nil
	out, err := a.manual(t, 6002)
	if err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	prior, err := a.f.db.GetGraphInstance(t.Context(), old)
	mustProgress(t, err)
	wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old)
	mustProgress(t, err)
	if prior.Status != store.GraphCancelled || wait.State != "cancelled" {
		t.Fatalf("the waiting run is cancelled with its wait: %s %s", prior.Status, wait.State)
	}
	got := a.reached(t)
	if got.Entries != 1 || got.Wait != "waiting" || got.Outcome != store.ValidateAssessed || got.Commit != validateHeadA {
		t.Fatalf("a fresh run on the same commit: %+v", got)
	}
	if got.FullCommands != 3 {
		t.Fatalf("the passed suite is reused, not run again: %d commands", got.FullCommands)
	}
	if got.Reviewers != 6 {
		t.Fatalf("the reviews run again: %d reviewer starts", got.Reviewers)
	}
	fresh := a.entries(t)[0].InstanceID
	if fresh == old {
		t.Fatal("a new instance")
	}
	if pred, _, err := a.f.db.WorkflowPredecessor(t.Context(), fresh); err != nil || pred != old {
		t.Fatalf("the fresh run links the ended one: %d %v", pred, err)
	}
}

// #621: a run whose reviews were all usable, with a blocking finding among
// them, is also replaced by the owner's /validate on the same commit. The new
// run is captured from the currently linked team release, reuses the passed
// suite and runs the reviews again.
func TestValidateSameCommitStartReplacesARunWithUsableReviews(t *testing.T) {
	a := newAdoptFlow(t)
	a.reviewers.run = mixedReview
	if out, err := a.manual(t, 6001); err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	first := a.reached(t)
	if first.Wait != "waiting" || first.FullCommands != 3 || first.Reviewers != 3 || first.Outcome != store.ValidateAssessed {
		t.Fatalf("the first run waits for the owner after usable reviews: %+v", first)
	}
	old := a.entries(t)[0].InstanceID
	oldDigest := validateCaptureDigest(t, a, 6001, old)

	// The factory's own start never ends it.
	if out, err := a.auto(t); err != nil || !strings.Contains(out.Reason, "already been started") {
		t.Fatalf("%+v %v", out, err)
	}
	// A newer team release is linked before the owner starts Validate again.
	a.f.opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return fixtureInstalled(captureValidateVariant(ctx, t, specs))
	}
	out, err := a.manual(t, 6002)
	if err != nil {
		t.Fatalf("%+v %v", out, err)
	}
	prior, err := a.f.db.GetGraphInstance(t.Context(), old)
	mustProgress(t, err)
	wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old)
	mustProgress(t, err)
	if prior.Status != store.GraphCancelled || wait.State != "cancelled" {
		t.Fatalf("the waiting run is cancelled with its wait: %s %s", prior.Status, wait.State)
	}
	got := a.reached(t)
	if got.Entries != 1 || got.Wait != "waiting" || got.Outcome != store.ValidateAssessed || got.Commit != validateHeadA {
		t.Fatalf("a fresh run on the same commit: %+v", got)
	}
	if got.FullCommands != 3 {
		t.Fatalf("the passed suite is reused, not run again: %d commands", got.FullCommands)
	}
	if got.Reviewers != 6 {
		t.Fatalf("the reviews run again: %d reviewer starts", got.Reviewers)
	}
	entry := a.entries(t)[0]
	fresh := entry.InstanceID
	if fresh == old || entry.State != store.ValidateEntryAdmitted {
		t.Fatalf("a new admitted instance: %+v", entry)
	}
	if pred, _, err := a.f.db.WorkflowPredecessor(t.Context(), fresh); err != nil || pred != old {
		t.Fatalf("the fresh run links the ended one: %d %v", pred, err)
	}
	if digest := validateCaptureDigest(t, a, 6002, fresh); digest == oldDigest {
		t.Fatalf("the fresh run is captured from the newly linked release: %s", digest)
	}
}

// #621: while a node of the waiting run is in progress, the owner's /validate
// on the same commit is refused as before and changes nothing.
func TestValidateSameCommitStartIsRefusedWhileTheRunExecutes(t *testing.T) {
	a := newAdoptFlow(t)
	a.reviewers.run = mixedReview
	old, _ := a.waitingValidate(t)
	// An earlier start comment took the wait and the waiting node runs again.
	a.claimOwnerReply(t, old.ID, "/validate check the finished work")
	wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old.ID)
	mustProgress(t, err)
	cur, err := a.f.db.GetGraphInstance(t.Context(), old.ID)
	mustProgress(t, err)
	node := wait.OriginNode
	if node == "" {
		node = cur.CurrentNode
	}
	_, err = a.f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: old.ID, Node: node, Limit: 64, StartedAt: time.Now()})
	mustProgress(t, err)
	before := a.entries(t)
	binds := a.binds
	out, err := a.manual(t, 6002)
	if err != nil || !strings.Contains(out.Reason, "already been started") || a.binds != binds {
		t.Fatalf("%+v %v binds=%d", out, err, a.binds)
	}
	after, err := a.f.db.GetGraphInstance(t.Context(), old.ID)
	mustProgress(t, err)
	if after.Status != store.GraphRunning {
		t.Fatalf("the executing run is untouched: %+v", after)
	}
	latest, err := a.f.db.LatestWorkflowInstance(t.Context(), a.f.session.ID, "validate")
	mustProgress(t, err)
	if latest.ID != old.ID {
		t.Fatalf("no new instance: %d", latest.ID)
	}
	if entries := a.entries(t); len(entries) != len(before) || entries[0].InstanceID != old.ID {
		t.Fatalf("no new entry: %+v", entries)
	}
}

// validateCaptureDigest is the content digest of the team release captured
// for the instance the trigger comment started.
func validateCaptureDigest(t *testing.T, a *adoptFlow, trigger, instanceID int64) string {
	t.Helper()
	c, err := a.f.db.WorkflowCaptureForAdmission(t.Context(), a.f.session.ID, trigger, "validate")
	mustProgress(t, err)
	if c.InstanceID != instanceID || c.ContentDigest == "" {
		t.Fatalf("the capture belongs to instance %d: %+v", instanceID, c)
	}
	return c.ContentDigest
}

// captureValidateVariant captures the shipped Validate package with changed
// reviewer guidance, as a newer linked release would carry.
func captureValidateVariant(ctx context.Context, t *testing.T, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	t.Helper()
	root, err := teamsourcetest.Workflow(ctx, teamsourcetest.Go, "validate")
	mustProgress(t, err)
	files := fstest.MapFS{}
	mustProgress(t, fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(root, p)
		if p == "validate-code.md" {
			raw = append(raw, []byte("\nA newer release.\n")...)
		}
		files[p] = &fstest.MapFile{Data: raw}
		return err
	}))
	return teamsource.CaptureWorkflow(ctx, files, "validate.dot", "go-development", agent.ModeValidate, specs)
}

// #577: a run whose wait an earlier /validate start already claimed is ended by
// the owner's next /validate; a wait claimed by a control command keeps the run.
func TestValidateSameCommitStartEndsARunClaimedByAnEarlierStart(t *testing.T) {
	for _, tc := range []struct {
		name  string
		claim string
		ends  bool
	}{
		{"claimed by a start", "/validate check the finished work", true},
		{"claimed by a check", ValidateCheckCommand + " r1 story-001:AC-001 pass", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdoptFlow(t)
			a.reviewers.run = deniedReview
			if out, err := a.manual(t, 6001); err != nil {
				t.Fatalf("%+v %v", out, err)
			}
			first := a.reached(t)
			if first.Wait != "waiting" || first.Reviewers != 3 {
				t.Fatalf("the first run waits for the owner after failed reviews: %+v", first)
			}
			old := a.entries(t)[0].InstanceID
			wait, err := a.f.db.GetGraphOwnerWait(t.Context(), old)
			mustProgress(t, err)
			delivery, err := a.f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			ref := a.f.session.TaskRef
			_, err = a.f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: old, OwnerID: validateOwnerID, AuthorID: validateOwnerID, CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: ref, SourceRef: ref, Mode: "validate", Workflow: "validate", DeliveryKey: wait.DeliveryKey, Body: tc.claim, At: time.Now()})
			mustProgress(t, err)

			a.reviewers.run = nil
			out, err := a.manual(t, delivery.CommentID+2)
			if err != nil {
				t.Fatalf("%+v %v", out, err)
			}
			prior, err := a.f.db.GetGraphInstance(t.Context(), old)
			mustProgress(t, err)
			wait, err = a.f.db.GetGraphOwnerWait(t.Context(), old)
			mustProgress(t, err)
			if !tc.ends {
				if prior.Status != store.GraphRunning || wait.State != "claimed" || !strings.Contains(out.Reason, "already been started") {
					t.Fatalf("a control-command claim keeps the run: %s %s %q", prior.Status, wait.State, out.Reason)
				}
				return
			}
			if prior.Status != store.GraphCancelled || wait.State != "cancelled" {
				t.Fatalf("the claimed run is cancelled with its wait: %s %s", prior.Status, wait.State)
			}
			got := a.reached(t)
			if got.Wait != "waiting" || got.Commit != validateHeadA || got.FullCommands != 3 || got.Reviewers != 6 {
				t.Fatalf("a fresh run on the same commit reuses the suite and reviews again: %+v", got)
			}
			if fresh := a.entries(t)[0].InstanceID; fresh == old {
				t.Fatal("a new instance")
			}
		})
	}
}
