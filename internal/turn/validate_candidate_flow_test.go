package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// This is the retained-repair main path: real Git objects and a reopened store,
// the real graph and authenticated owner reply, with only remote delivery,
// suite subprocesses and reviewers replaced by deterministic fixtures.
func TestValidateRetainedCandidateSurvivesRestartAndExactOwnerAdoption(t *testing.T) {
	o := newOwnerFixture(t)
	longText := strings.Repeat("complete retained repair evidence\n", 2600) + "last retained line\n"
	o.suiteExit = func(commit string) int {
		if commit == o.base {
			return 1
		}
		return 0
	}
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		f.write("repair-notes.txt", longText)
		if err := os.Symlink("repair-notes.txt", filepath.Join(f.wt.Path(), "repair-link")); err != nil {
			t.Fatal(err)
		}
		return handbackReport("The sum is wrong; this repair also includes retained notes requiring owner review."), ActivityResult{}
	}
	candidateRegister(t, o)
	o.toOwner()
	candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("one complete candidate: count=%d error=%v", len(candidates), err)
	}
	c := candidates[0]
	if c.Tree == "" || c.ReviewDigest == "" || !strings.Contains(c.Patch, "+last retained line") || !strings.Contains(c.Patch, "120000") {
		t.Fatalf("complete tree, long patch and symlink must survive: tree=%s digest=%s patch bytes=%d", c.Tree, c.ReviewDigest, len(c.Patch))
	}
	if o.head() != o.base {
		t.Fatal("pending repair changed the adopted branch")
	}
	if _, err := os.Lstat(filepath.Join(o.wt.Path(), "repair-link")); !os.IsNotExist(err) {
		t.Fatalf("pending symlink survived reset: %v", err)
	}
	if _, err := o.db.LatestKeptRepairCommit(t.Context(), o.instance.ID); err == nil {
		t.Fatal("pending candidate was selected as adopted work")
	}

	candidateRestart(t, o)
	// Inspect the exact retained review after restart before adopting it.
	o.say(fmt.Sprintf("/validate-repair %s %s %s inspect", o.state().Revision, c.ID, c.ReviewDigest))
	candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 || !candidates[0].ReviewComplete {
		t.Fatalf("review delivery remains complete after restart: count=%d error=%v notice=%s", len(candidates), err, o.notice())
	}
	c = candidates[0]
	o.forge.mu.Lock()
	parts := append([]github.Comment(nil), o.forge.comments...)
	o.forge.mu.Unlock()
	all := ""
	for _, part := range parts {
		all += part.Body
	}
	if !strings.Contains(all, "last retained line") || !strings.Contains(all, "repair-link") {
		t.Fatal("owner review omitted retained changes beyond preview limits")
	}
	nodes := o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate audit remains: %v", err)
	}
	adopted := candidates[0]
	if adopted.ResultCommit == "" || adopted.ResultCommit != o.head() {
		t.Fatalf("verified result commit missing or stale: result=%s head=%s notice=%s nodes=%s", adopted.ResultCommit, o.head(), o.notice(), joinNodes(nodes))
	}
	tree, err := o.wt.RevParse(t.Context(), "HEAD^{tree}", git.RevParseOptions{})
	if err != nil || tree != c.Tree {
		t.Fatalf("adopted exact reviewed tree: %s != %s: %v", tree, c.Tree, err)
	}
	if visits(nodes, "suite") != 1 || visits(nodes, "review") != 1 {
		t.Fatalf("adoption returns through suite and reviewers: %s", joinNodes(nodes))
	}
	if len(o.suiteCommands) != 4 {
		t.Fatalf("failed base stops at its first failure and adopted commit gets the full suite: %v", o.suiteCommands)
	}
	for _, commit := range o.suiteCommands[1:] {
		if commit != adopted.ResultCommit {
			t.Fatalf("suite used stale commit %s", commit)
		}
	}
	rounds, err := o.db.ListRepairRounds(t.Context(), o.instance.ID)
	if err != nil || len(rounds) != 1 || rounds[0].State != store.RepairHandback || rounds[0].ResultCommit != "" {
		t.Fatalf("historical round stays flagged and no new round is spent: %+v %v", rounds, err)
	}
	for _, review := range o.reviewers.started[3:] {
		if review.Commit != adopted.ResultCommit {
			t.Fatalf("review used stale commit %s", review.Commit)
		}
	}
	if len(o.forge.pushes) != 0 || len(o.forge.merges) != 0 {
		t.Fatal("candidate acceptance bypassed shipping gates")
	}
}

func candidateRegister(t *testing.T, o *ownerFixture) {
	t.Helper()
	o.registry = ActivityRegistry{}
	o.register()
	o.b.RepairAuthor = func(context.Context) (git.Author, error) {
		return git.Author{Name: "Factory", Email: "factory@example.com"}, nil
	}
	// Replace the apply binding with the fixture author after common registration.
	o.registry.handlers[graphexec.ValidateRepairApplyAction] = ValidateRepairApplyActivity{o.b}
}

func candidateRestart(t *testing.T, o *ownerFixture) {
	t.Helper()
	dbpath := filepath.Join(filepath.Dir(o.wt.Path()), "litefactory.db")
	if err := o.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), dbpath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	o.db, o.opts.DB = db, db
	bodies, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(ReporterOptions{BotUserID: ownerBotID, DB: db, Forge: o.forge, Renderer: bodies, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	o.opts.ResearchReporter = reporter
	candidateRegister(t, o)
}

func TestValidateRetainedCandidateRejectsChangedDigestAndKeepsDiscardTerminal(t *testing.T) {
	o := newOwnerFixture(t)
	o.suiteExit = func(string) int { return 1 }
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return handbackReport("The fix requires owner review."), ActivityResult{}
	}
	candidateRegister(t, o)
	o.toOwner()
	candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate: count=%d error=%v", len(candidates), err)
	}
	c := candidates[0]
	o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, strings.Repeat("0", 64)))
	after, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || after[0].Resolution != "pending" || o.head() != o.base {
		t.Fatalf("changed review digest authorized work: count=%d error=%v", len(after), err)
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s discard", o.state().Revision, c.ID, c.ReviewDigest))
	candidateRestart(t, o)
	after, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(after) != 1 || after[0].Resolution != "discard" || o.head() != o.base {
		t.Fatalf("discard must persist without adoption: count=%d error=%v notice=%s", len(after), err, o.notice())
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	after, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || after[0].Resolution != "discard" || after[0].ResultCommit != "" || o.head() != o.base {
		t.Fatalf("discard was reversed: count=%d error=%v notice=%s", len(after), err, o.notice())
	}
}

func TestValidateFrozenScopeRepairRemainsIneligibleForExactOwnerCommand(t *testing.T) {
	o := newOwnerFixture(t)
	o.suiteExit = func(string) int { return 1 }
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write(flowSessionDir+"/plan_brief.md", "changed acceptance criterion\n")
		return handbackReport("The approved criterion would need changing."), ActivityResult{}
	}
	candidateRegister(t, o)
	o.toOwner()
	candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate: count=%d error=%v", len(candidates), err)
	}
	c := candidates[0]
	if c.Eligible {
		t.Fatal("frozen acceptance edits became eligible")
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	after, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || after[0].Resolution != "pending" || after[0].ResultCommit != "" || o.head() != o.base {
		t.Fatalf("owner command adopted frozen scope edits: count=%d error=%v notice=%s", len(after), err, o.notice())
	}
}

func TestValidatePrivacyRedactedRepairReviewCannotAuthorizeAdoption(t *testing.T) {
	o := newOwnerFixture(t)
	privatePath := "/Users/private/.litefactory/retained-private.txt"
	o.suiteExit = func(string) int { return 1 }
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("repair-notes.txt", strings.Repeat("retained note\n", 1300)+"private path: "+privatePath+"\n")
		return handbackReport("The review contains a private factory path."), ActivityResult{}
	}
	candidateRegister(t, o)
	o.toOwner()
	candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate count=%d error=%v", len(candidates), err)
	}
	c := candidates[0]
	if !strings.Contains(c.Patch, privatePath) {
		t.Fatal("protected candidate lost its complete original bytes")
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s inspect", o.state().Revision, c.ID, c.ReviewDigest))
	candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || candidates[0].ReviewComplete {
		t.Fatalf("privacy-redacted review became complete: error=%v", err)
	}
	for _, comment := range o.forge.bodies(privatePath) {
		t.Errorf("private path escaped outbound review in comment %d", comment.ID)
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || candidates[0].Resolution != "pending" || o.head() != o.base {
		t.Fatalf("incomplete review authorized adoption: error=%v notice=%s", err, o.notice())
	}
}

// candidateReviewForge interrupts only retained review publication; owner
// reports and questions still use the same in-memory remote.
type candidateReviewForge struct {
	*ownerForge
	unavailable bool
	lostReply   bool
}

func (f *candidateReviewForge) PostComment(ctx context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	review := strings.Contains(body.String(), "Retained Validate repair candidate")
	if review && f.unavailable {
		return github.Comment{}, errors.New("review delivery unavailable")
	}
	posted, err := f.ownerForge.PostComment(ctx, repo, issue, body)
	if err == nil {
		posted.HTMLURL = fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", repo.Owner, repo.Name, issue, posted.ID)
		f.mu.Lock()
		for i := range f.comments {
			if f.comments[i].ID == posted.ID {
				f.comments[i] = posted
			}
		}
		f.mu.Unlock()
	}
	if review && f.lostReply && err == nil {
		f.lostReply = false
		return github.Comment{}, errors.New("review post reply was lost")
	}
	return posted, err
}

func TestValidateRetainedReviewDeliveryFailureAndLostReplyRecoverWithoutDuplicate(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost-reply=%t", lost), func(t *testing.T) {
			o := newOwnerFixture(t)
			remote := &candidateReviewForge{ownerForge: o.forge, unavailable: !lost, lostReply: lost}
			o.opts.ResearchReporter.forge = remote
			o.suiteExit = func(string) int { return 1 }
			o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
				f.write("repair-notes.txt", strings.Repeat("complete delivery content\n", 2600))
				return handbackReport("This retained repair needs owner review."), ActivityResult{}
			}
			candidateRegister(t, o)
			o.toOwner()
			candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidate count=%d error=%v", len(candidates), err)
			}
			c := candidates[0]
			if !lost {
				if c.ReviewComplete {
					t.Fatal("failed delivery enabled acceptance")
				}
				o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
				candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
				if err != nil || candidates[0].Resolution != "pending" || o.head() != o.base {
					t.Fatal("failed review delivery authorized adoption")
				}
			}
			remote.unavailable = false
			o.say(fmt.Sprintf("/validate-repair %s %s %s inspect", o.state().Revision, c.ID, c.ReviewDigest))
			candidates, err = o.db.ListRepairCandidates(t.Context(), o.instance.ID)
			if !lost {
				if err != nil || candidates[0].ReviewComplete {
					t.Fatalf("absent remote receipt must remain uncertain: error=%v", err)
				}
				if len(o.forge.bodies("Retained Validate repair candidate "+c.ID)) != 0 {
					t.Fatal("uncertain delivery was blindly resent")
				}
				return
			}
			if err != nil || !candidates[0].ReviewComplete {
				t.Fatalf("review did not recover: error=%v notice=%s", err, o.notice())
			}
			if firstParts := o.forge.bodies("Retained Validate repair candidate " + c.ID); len(firstParts) != 1 {
				t.Fatalf("uncertain delivery duplicated first review part: %d", len(firstParts))
			}
			if o.head() != o.base {
				t.Fatal("review delivery adopted pending candidate")
			}
		})
	}
}

func candidateRecoveryFixture(t *testing.T) (*ownerFixture, store.RepairCandidate) {
	t.Helper()
	o := newOwnerFixture(t)
	o.suiteExit = func(commit string) int {
		if commit == o.base {
			return 1
		}
		return 0
	}
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return handbackReport("This exact repair needs owner review."), ActivityResult{}
	}
	candidateRegister(t, o)
	o.toOwner()
	candidates, err := o.db.ListRepairCandidates(t.Context(), o.instance.ID)
	if err != nil || len(candidates) != 1 || !candidates[0].ReviewComplete {
		t.Fatalf("reviewed recovery fixture: count=%d error=%v", len(candidates), err)
	}
	return o, candidates[0]
}

func candidateReserveCurrent(t *testing.T, o *ownerFixture) ActivityRequest {
	t.Helper()
	node := o.currentNode()
	attempt, err := o.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: o.instance.ID, Node: node, Limit: 64, StartedAt: o.clock})
	if err != nil {
		t.Fatal(err)
	}
	return ActivityRequest{Mode: agent.ModeValidate, Node: node, InstanceID: o.instance.ID, NodeAttemptID: attempt.ID}
}

func candidateCompleteReserved(t *testing.T, o *ownerFixture, req ActivityRequest, result ActivityResult) {
	t.Helper()
	next, ok := graphexec.Decide(o.profile, req.Node, result.Outcome)
	if !ok {
		t.Fatalf("no declared recovery transition: %s/%s", req.Node, result.Outcome)
	}
	err := o.db.CompleteGraphNodeAttempt(t.Context(), req.NodeAttemptID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: result.Outcome, FinishedAt: o.clock, InstanceStatus: store.GraphRunning, NextDueAt: o.clock, Outputs: []store.GraphOutput{{Kind: result.Kind, Payload: result.Output}}, Transition: &store.GraphTransition{InstanceID: o.instance.ID, FromNode: req.Node, ToNode: next, OnOutcome: result.Outcome, At: o.clock}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateCandidateSavedAuthorizationReplaysOriginalSourceAfterRestart(t *testing.T) {
	o, c := candidateRecoveryFixture(t)
	originalRevision := o.state().Revision
	o.reply(o.owner, fmt.Sprintf("/validate-repair %s %s %s accept", originalRevision, c.ID, c.ReviewDigest))
	req := candidateReserveCurrent(t, o)
	result, err := (ValidateResolveActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || result.Outcome != "apply_repair" {
		t.Fatalf("saved acceptance: outcome=%s error=%v", result.Outcome, err)
	}
	// The decision reached durable storage, but its graph output did not.
	candidateRestart(t, o)
	if o.state().Revision == originalRevision {
		t.Fatal("saved resolution did not change report identity")
	}
	replay, err := (ValidateResolveActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || replay.Outcome != "apply_repair" {
		t.Fatalf("original authorized source was made stale by its own decision: outcome=%s error=%v", replay.Outcome, err)
	}
	candidateCompleteReserved(t, o, req, replay)
	o.toOwner()
	saved, err := o.db.GetRepairCandidate(t.Context(), c.ID)
	if err != nil || saved.ResultCommit == "" || saved.ResultCommit != o.head() {
		t.Fatalf("replayed authorization did not adopt exact candidate: result=%s error=%v notice=%s", saved.ResultCommit, err, o.notice())
	}
	if count := o.repo.GitIn(o.wt.Path(), "rev-list", "--count", o.base+"..HEAD"); count != "1" {
		t.Fatalf("authorization replay created %s commits", count)
	}
}

func TestValidateCandidateCompletedApplyReplaysLostGraphResultWithoutSecondAdoption(t *testing.T) {
	o, c := candidateRecoveryFixture(t)
	o.reply(o.owner, fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	for range 12 {
		if o.currentNode() == "owner_repair_apply" {
			break
		}
		if _, err := o.step(); err != nil {
			t.Fatal(err)
		}
	}
	if o.currentNode() != "owner_repair_apply" {
		t.Fatalf("did not reach declared adoption node: %s", o.currentNode())
	}
	req := candidateReserveCurrent(t, o)
	result, err := (ValidateRepairApplyActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || result.Outcome != "applied" {
		t.Fatalf("first application: outcome=%s error=%v output=%s", result.Outcome, err, result.Output)
	}
	head := o.head()
	before, err := o.db.ListRepairCandidateJournal(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Git and the apply journal are complete; the graph result was lost.
	candidateRestart(t, o)
	replay, err := (ValidateRepairApplyActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || replay.Outcome != "applied" || o.head() != head {
		t.Fatalf("completed adoption replay: outcome=%s error=%v output=%s", replay.Outcome, err, replay.Output)
	}
	after, err := o.db.ListRepairCandidateJournal(t.Context(), c.ID)
	if err != nil || len(after) != len(before) {
		t.Fatalf("replay wrote another apply operation: before=%d after=%d error=%v", len(before), len(after), err)
	}
	if count := o.repo.GitIn(o.wt.Path(), "rev-list", "--count", o.base+"..HEAD"); count != "1" {
		t.Fatalf("application replay created %s commits", count)
	}
	candidateCompleteReserved(t, o, req, replay)
	o.toOwner()
	if o.state().Commit != head {
		t.Fatalf("recovered graph did not validate adopted commit: %s", o.state().Commit)
	}
}

func TestValidateCandidateBranchDriftStillAllowsInspectAndRefusesAcceptance(t *testing.T) {
	o, c := candidateRecoveryFixture(t)
	o.write("branch-drift.txt", "independent branch change\n")
	committed, err := o.wt.Commit(t.Context(), gitCommitOptions())
	if err != nil {
		t.Fatal(err)
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s inspect", o.state().Revision, c.ID, c.ReviewDigest))
	if !strings.Contains(o.notice(), "Complete retained repair review:") {
		t.Fatalf("branch drift hid retained inspection: %s", o.notice())
	}
	o.say(fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	saved, err := o.db.GetRepairCandidate(t.Context(), c.ID)
	if err != nil || saved.Resolution != "pending" || saved.ResultCommit != "" || o.head() != committed.Head {
		t.Fatalf("branch drift authorized adoption: resolution=%s result=%s error=%v notice=%s", saved.Resolution, saved.ResultCommit, err, o.notice())
	}
}

func candidateBeforeCheck(t *testing.T) *ownerFixture {
	t.Helper()
	o := newOwnerFixture(t)
	o.suiteExit = func(string) int { return 1 }
	o.agent = func(_ int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("repair-notes.txt", "original repair bytes\n")
		return handbackReport("Retain this candidate for owner review."), ActivityResult{}
	}
	candidateRegister(t, o)
	for range 12 {
		if o.currentNode() == "repair_check" {
			return o
		}
		if _, err := o.step(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("did not reach repair check: %s", o.currentNode())
	return nil
}

func TestValidateCandidateInterruptedCaptureCompletesBeforeResetAndPreservesOutcome(t *testing.T) {
	o := candidateBeforeCheck(t)
	round, err := o.db.RunningRepairRound(t.Context(), o.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := o.wt.CaptureRepairCandidate(t.Context(), round.PreCommit, fmt.Sprintf("%d:%d", round.InstanceID, round.ID))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := o.b.repairScope(t.Context(), o.opts, ActivityRequest{InstanceID: o.instance.ID})
	if err != nil {
		t.Fatal(err)
	}
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	source, err := o.db.LatestGraphNodeAttempt(t.Context(), o.instance.ID, "repair_agent")
	if err != nil {
		t.Fatal(err)
	}
	finish := store.RepairFinish{State: store.RepairHandback, Response: "handback", Reason: "original owner-review handback", NodeAttemptID: source.ID, Files: []string{"repair-notes.txt"}, At: o.clock}
	evidence, _ := json.Marshal(candidateEvidence{Finish: finish, Complete: true})
	manifest, _ := json.Marshal(snapshot.Changes)
	patchDigest := sha256.Sum256([]byte(snapshot.Patch))
	c := store.RepairCandidate{TaskID: session.TaskRef, InstanceID: o.instance.ID, RoundID: round.ID, NodeAttemptID: source.ID, BaseCommit: snapshot.Base, Tree: snapshot.Tree, Commit: snapshot.Commit, Ref: snapshot.Ref, Patch: snapshot.Patch, PatchDigest: hex.EncodeToString(patchDigest[:]), Manifest: manifest, Evidence: evidence, ScopeRevision: scope, ReportRevision: "original-report", Eligible: true, CreatedAt: o.clock}
	c.ReviewDigest = candidateHash(c)
	c.ID = candidateHash([]int64{o.instance.ID, round.ID})
	if err := o.db.JournalRepairCandidate(t.Context(), c.ID, store.RepairCandidateJournal{InstanceID: o.instance.ID, RoundID: round.ID, Phase: "capture", State: "intent", ExpectedBase: o.base, At: o.clock}); err != nil {
		t.Fatal(err)
	}
	if err := o.db.PutRepairCandidate(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	candidateRestart(t, o)
	if err := (ValidateRepairBudgetActivity{o.b}).settleInterrupted(t.Context(), o.opts, round, o.wt, flowSessionDir); err != nil {
		t.Fatal(err)
	}
	saved, err := o.db.GetRepairRound(t.Context(), round.ID)
	if err != nil || saved.State != store.RepairHandback || saved.Reason != finish.Reason {
		t.Fatalf("capture recovery changed historical outcome: state=%s reason=%s error=%v", saved.State, saved.Reason, err)
	}
	journal, err := o.db.ListRepairCandidateJournal(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	captureComplete, resetComplete := -1, -1
	for i, entry := range journal {
		if entry.Phase == "capture" && entry.State == "complete" {
			captureComplete = i
		}
		if entry.Phase == "reset" && entry.State == "complete" {
			resetComplete = i
		}
	}
	if captureComplete < 0 || resetComplete <= captureComplete {
		t.Fatalf("reset preceded complete capture: %+v", journal)
	}
	if _, err := os.Stat(filepath.Join(o.wt.Path(), "repair-notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("verified reset did not remove rejected working edit: %v", err)
	}
	retained, err := o.db.GetRepairCandidate(t.Context(), c.ID)
	if err != nil || retained.Tree != snapshot.Tree || !strings.Contains(retained.Patch, "original repair bytes") {
		t.Fatalf("reset lost retained source: error=%v", err)
	}
	if err := o.db.CheckRepairWorktree(t.Context(), o.instance.SessionID, 0); err != nil {
		t.Fatalf("verified settlement left worktree held: %v", err)
	}
}

func TestValidateCandidateCaptureFailurePreservesSourceAndHoldsWorktree(t *testing.T) {
	o := candidateBeforeCheck(t)
	pipe := filepath.Join(o.wt.Path(), "unsupported-pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	req := candidateReserveCurrent(t, o)
	_, err := (ValidateRepairCheckActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err == nil {
		t.Fatal("unsupported input was silently omitted before reset")
	}
	if body, readErr := os.ReadFile(filepath.Join(o.wt.Path(), "repair-notes.txt")); readErr != nil || string(body) != "original repair bytes\n" {
		t.Fatalf("failed capture destroyed source: error=%v", readErr)
	}
	if info, statErr := os.Lstat(pipe); statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("failed capture removed unsupported input: %v", statErr)
	}
	if _, err := o.db.RunningRepairRound(t.Context(), o.instance.ID); err != nil {
		t.Fatalf("failed capture settled running repair: %v", err)
	}
	if err := o.db.CheckRepairWorktree(t.Context(), o.instance.SessionID, 0); !errors.Is(err, store.ErrRepairWorktreeHeld) {
		t.Fatalf("failed capture released other workers: %v", err)
	}
}

func TestValidateCandidateCompletedApplyReplayPreservesDirtyCheckout(t *testing.T) {
	o, c := candidateRecoveryFixture(t)
	o.reply(o.owner, fmt.Sprintf("/validate-repair %s %s %s accept", o.state().Revision, c.ID, c.ReviewDigest))
	for range 12 {
		if o.currentNode() == "owner_repair_apply" {
			break
		}
		if _, err := o.step(); err != nil {
			t.Fatal(err)
		}
	}
	if o.currentNode() != "owner_repair_apply" {
		t.Fatalf("did not reach adoption: %s", o.currentNode())
	}
	req := candidateReserveCurrent(t, o)
	first, err := (ValidateRepairApplyActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || first.Outcome != "applied" {
		t.Fatalf("first adoption: outcome=%s error=%v", first.Outcome, err)
	}
	head := o.head()
	candidateRestart(t, o)
	dirty := "independent dirty edit must survive\n"
	o.write("app/app.go", dirty)
	replay, err := (ValidateRepairApplyActivity{o.b}).Execute(t.Context(), o.opts, req)
	if err != nil || replay.Outcome != "blocked" {
		t.Fatalf("dirty completed adoption was reapplied: outcome=%s error=%v output=%s", replay.Outcome, err, replay.Output)
	}
	body, err := os.ReadFile(filepath.Join(o.wt.Path(), "app/app.go"))
	if err != nil || string(body) != dirty || o.head() != head {
		t.Fatalf("replay destroyed dirty checkout: error=%v head=%s", err, o.head())
	}
}
