package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/gittest"
	"os/exec"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type graphDeliveryForge struct {
	binding                                             GraphDeliveryBinding
	pr                                                  github.DeliveryPR
	issue                                               github.Issue
	main, remote                                        string
	creates, merges, closes                             int
	loseMerge, raceMain, losePR, loseClose, refuseMerge bool
	beforeMerge                                         func() error
}

func (f *graphDeliveryForge) BaseURL() string { return "https://github.com" }
func (f *graphDeliveryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.issue, nil
}
func (f *graphDeliveryForge) GetDeliveryHead(_ context.Context, _ github.Repo, branch string) (string, error) {
	if branch == "main" {
		return f.main, nil
	}
	return f.remote, nil
}
func (f *graphDeliveryForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	if f.pr.ID == 0 {
		return nil, nil
	}
	return []github.DeliveryPR{f.pr}, nil
}
func (f *graphDeliveryForge) GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error) {
	return f.pr, nil
}
func (f *graphDeliveryForge) CreateDeliveryPR(_ context.Context, _ github.Repo, branch, target string, title, body redact.Body) (github.DeliveryPR, error) {
	f.creates++
	f.pr = github.DeliveryPR{ID: 80, Number: 4, CreatorID: f.binding.BotID, Head: f.remote, Branch: branch, Target: target, HeadRepo: "acme/acme-api", BaseRepo: "acme/acme-api", State: "open", Body: body.String()}
	if f.losePR {
		return f.pr, errors.New("lost create acknowledgment")
	}
	return f.pr, nil
}
func (f *graphDeliveryForge) MergeDeliveryPR(_ context.Context, _ github.Repo, _ int, head string) error {
	f.merges++
	if f.beforeMerge != nil {
		if err := f.beforeMerge(); err != nil {
			return err
		}
	}
	if f.refuseMerge {
		f.refuseMerge = false
		return &github.APIError{StatusCode: 405, Message: "Required status check is expected"}
	}
	if head != f.pr.Head {
		return errors.New("expected head guard failed")
	}
	f.pr.Merged = true
	f.pr.State = "closed"
	f.pr.MergeSHA = strings.Repeat("f", 40)
	f.main = f.pr.MergeSHA
	if f.loseMerge {
		return errors.New("lost merge acknowledgment")
	}
	return nil
}
func (f *graphDeliveryForge) GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error) {
	base := f.main
	if f.raceMain {
		base = strings.Repeat("e", 40)
	}
	return github.DeliveryCommit{SHA: f.pr.MergeSHA, Parents: []string{base, f.pr.Head}}, nil
}
func (f *graphDeliveryForge) CloseDeliveredParent(context.Context, github.Repo, int, int64) error {
	f.closes++
	if f.loseClose {
		f.loseClose = false
		return errors.New("lost closure acknowledgment")
	}
	f.issue.State = "closed"
	return nil
}

func graphFinalizationFixture(t *testing.T) (*fixture, GraphBugFinalization, ActivityRequest, *graphDeliveryForge, string) {
	t.Helper()
	f, _, instance := approvedGraphRepairFixture(t)
	ctx := t.Context()
	at := f.opts.MCP.Now()
	handoff, e := GraphBugApprovedHandoff(ctx, f.db, instance.ID)
	mustProgress(t, e)
	segment, e := f.db.GetOrCreateSegment(ctx, f.session.ID, 2, "fixer", store.AgentClaude)
	mustProgress(t, e)
	run, e := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 130, StartedAt: at, LogPath: func(int) string { return "fixer.log" }})
	mustProgress(t, e)
	attempt, e := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: at})
	mustProgress(t, e)
	head := strings.Repeat("a", 40)
	repair := bugrepair.Evidence{Summary: "Fix #123 from approved scope", Checks: []bugrepair.Check{{Command: "make check", Outcome: "passed"}}}
	eligibility := GraphBugRepairEligibility{Record: "repair_eligibility", InstanceID: instance.ID, AttemptID: attempt.ID, RunID: run.ID, ResultID: 42, ResultRevision: 1, Handoff: handoff, Repair: repair}
	raw, e := json.Marshal(eligibility)
	mustProgress(t, e)
	_, e = f.db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
	mustProgress(t, e)
	accepted, e := json.Marshal(bugRetainedNode{ResultID: 42, ResultRevision: 1, Node: agentresult.ResearchNode{Response: "succeeded", Result: agentresult.Result{Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "succeeded", Repair: &repair}}}}})
	mustProgress(t, e)
	_, e = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded})
	mustProgress(t, e)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "succeeded", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: accepted}}, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "fixer", ToNode: "finalization", OnOutcome: "succeeded", At: at}}))
	final, e := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "finalization", Limit: 16, StartedAt: at})
	mustProgress(t, e)
	req := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, NodeAttemptID: final.ID, Node: "finalization"}
	binding := GraphDeliveryBinding{Record: "delivery_binding", InstanceID: instance.ID, SessionID: f.session.ID, IssueID: 123, BotID: 99, IssueNumber: f.session.IssueNumber, Repository: github.Repo{Owner: "acme", Name: "acme-api"}, Branch: "lf/repair", Target: "main", Marker: "<!-- lf:bug-delivery exact -->", Eligibility: eligibility}
	forge := &graphDeliveryForge{binding: binding, main: strings.Repeat("c", 40), remote: strings.Repeat("0", 40), issue: github.Issue{ID: 123, Number: f.session.IssueNumber, State: "open"}}
	handler := GraphBugFinalization{SyncBase: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
		return syncBaseOutput{Result: "current", Commit: head}, nil
	}, Forge: forge, Redactor: redact.New(redact.Options{Environ: []string{}}), Resolve: func(context.Context, Options, ActivityRequest, GraphBugRepairEligibility) (GraphDeliveryBinding, error) {
		return binding, nil
	}, Save: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error) {
		return head, nil
	}, PublishSaved: func(context.Context, Options, ActivityRequest, string) error {
		forge.remote = head
		return nil
	}}
	return f, handler, req, forge, head
}

// Delivery reconciles against GitHub: a lost reply is resolved by looking, a
// refused step parks and the owner's reply re-runs it from the top, and main
// moving on is never a reason to stop.
func TestGraphDeliveryReconcilesAndRetries(t *testing.T) {
	for _, variant := range []string{"complete", "lost merge reply", "lost PR reply", "lost close reply", "main moved", "merge refused", "already merged"} {
		t.Run(variant, func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			forge.loseMerge = variant == "lost merge reply"
			forge.losePR = variant == "lost PR reply"
			forge.loseClose = variant == "lost close reply"
			forge.refuseMerge = variant == "merge refused"
			if variant == "main moved" {
				forge.main = strings.Repeat("e", 40)
			}
			if variant == "already merged" {
				forge.remote = head
				forge.pr = github.DeliveryPR{ID: 80, Number: 4, Head: head, Branch: "lf/repair", Target: "main", State: "closed", Merged: true, MergeSHA: strings.Repeat("f", 40)}
			}
			out, e := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, e)
			retried := variant == "merge refused" || variant == "lost close reply"
			if retried {
				if out.Outcome != "blocked" || !strings.Contains(out.Diagnostic, "PR #4") {
					t.Fatalf("first attempt=%+v", out)
				}
				out, e = h.Execute(t.Context(), f.opts, req)
				mustProgress(t, e)
			}
			if out.Outcome != "completed" || forge.issue.State != "closed" || !forge.pr.Merged {
				t.Fatalf("outcome=%+v pr=%+v", out, forge.pr)
			}
			wantCreates, wantMerges := 1, 1
			if variant == "merge refused" {
				wantMerges = 2
			}
			if variant == "already merged" {
				wantCreates, wantMerges = 0, 0
			}
			if forge.creates != wantCreates || forge.merges != wantMerges {
				t.Fatalf("creates=%d merges=%d", forge.creates, forge.merges)
			}
		})
	}
}

func TestGraphMechanicalOwnerContinuationSpendsSubstantiveAllowanceOnce(t *testing.T) {
	f, _, instance := approvedGraphRepairFixture(t)
	ctx := t.Context()
	wait, e := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, e)
	if wait.State != "allocated" || !wait.AllowRunless {
		t.Fatalf("wait=%+v", wait)
	}
	segment, e := f.db.GetOrCreateSegment(ctx, f.session.ID, 2, "allowance-fixer", store.AgentClaude)
	mustProgress(t, e)
	run, e := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 180, StartedAt: f.opts.MCP.Now(), LogPath: func(int) string { return "allowance.log" }})
	mustProgress(t, e)
	start := store.GraphAttemptStart{InstanceID: instance.ID, Node: "fixer", RunID: run.ID, OwnerAllowanceSource: wait.ContinuationAttemptID, Limit: 1, StartedAt: f.opts.MCP.Now()}
	first, e := f.db.StartGraphNodeAttempt(ctx, start)
	mustProgress(t, e)
	mustProgress(t, f.db.FinishGraphNodeAttempt(ctx, first.ID, store.GraphFailed, "", f.opts.MCP.Now()))
	wait, e = f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, e)
	if wait.AllowanceAttemptID != first.ID || wait.ContinuationAttemptID == first.ID {
		t.Fatalf("separate mechanical/substantive facts lost: %+v", wait)
	}
	mustProgress(t, f.db.Close())
	db, e := store.Open(ctx, f.paths.DBPath())
	mustProgress(t, e)
	f.db = db
	f.opts.DB = db
	if _, e = db.StartGraphNodeAttempt(ctx, start); e == nil {
		t.Fatal("consumed reply authorized a second substantive invocation")
	}
	attempts, e := db.ListGraphNodeAttempts(ctx, instance.ID)
	mustProgress(t, e)
	if attempts[len(attempts)-1].ID != first.ID {
		t.Fatal("failed allowance claim allocated work")
	}
}

func TestGraphDeliverySyncOutcomesNeverMergeUncheckedSource(t *testing.T) {
	for _, state := range []string{"synced", "retry", "conflict", "drift", "error"} {
		t.Run(state, func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			h.SyncBase = func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
				if forge.remote != head {
					t.Fatal("sync ran before retained repair was saved and published")
				}
				if state == "error" {
					return syncBaseOutput{}, errors.New("fetch failed")
				}
				result := state
				if state == "drift" {
					result = "current"
				}
				return syncBaseOutput{Result: result, Commit: "changed", Conflicts: []string{"shared_test.go"}, Reason: "controlled refusal"}, nil
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			want := "revalidate"
			if state == "conflict" || state == "error" {
				want = "blocked"
			}
			if out.Outcome != want || forge.merges != 0 || forge.closes != 0 {
				t.Fatalf("unchecked delivery: %+v merges=%d closes=%d", out, forge.merges, forge.closes)
			}
			if state == "conflict" && (!strings.Contains(out.Diagnostic, "shared_test.go") || !strings.Contains(out.Diagnostic, "controlled refusal")) {
				t.Fatalf("not actionable: %s", out.Diagnostic)
			}
		})
	}
}

func TestGraphDeliveryRechecksBaseOnlyOnConflictRefusal(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			calls := 0
			h.SyncBase = func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
				calls++
				if calls == 1 {
					return syncBaseOutput{Result: "current", Commit: head}, nil
				}
				return syncBaseOutput{Result: "synced", Commit: "new base merge"}, nil
			}
			forge.beforeMerge = func() error {
				message := "Required status check is expected"
				if conflict {
					message = "Pull Request has merge conflicts"
				}
				return &github.APIError{StatusCode: 405, Message: message}
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			want, wantCalls := "blocked", 1
			if conflict {
				want, wantCalls = "revalidate", 2
			}
			if out.Outcome != want || calls != wantCalls || forge.closes != 0 {
				t.Fatalf("out=%+v syncs=%d closes=%d", out, calls, forge.closes)
			}
		})
	}
}

// The offline fixture executes real package checks after a real base merge.
// Store transitions stand in for the surrounding scheduler; no provider or
// remote forge is used. Existing fixer tests cover its shared result acceptance.
func TestGraphDeliveryGitSyncThenFreshChecks(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			g := newSyncGit(t)
			const seed = "package fixture\nimport \"testing\"\nfunc TestSeed(t *testing.T) {}\n"
			g.advance(gittest.DefaultBranch, "go.mod", "module fixture\ngo 1.24\n")
			g.advance(gittest.DefaultBranch, "shared_test.go", seed)
			_, _, err := g.syncBase()
			mustProgress(t, err)
			// Update the local fixture branch to the initial synchronized tip.
			g.repo.Git("fetch", "--quiet", "origin")
			g.repo.Git("branch", "-f", implementFeature, "origin/"+implementFeature)
			g.advance(implementFeature, "shared_test.go", seed+"func TestRepair(t *testing.T) {}\n")
			baseFile := "base_test.go"
			baseText := "package fixture\nimport \"testing\"\nfunc TestBase(t *testing.T) {}\n"
			if conflict {
				baseFile, baseText = "shared_test.go", seed+"func TestBase(t *testing.T) {}\n"
			}
			base := g.advance(gittest.DefaultBranch, baseFile, baseText)
			f, h, req, forge, _ := graphFinalizationFixture(t)
			h.Resolve = func(_ context.Context, _ Options, _ ActivityRequest, eligible GraphBugRepairEligibility) (GraphDeliveryBinding, error) {
				b := forge.binding
				b.Branch, b.Target, b.Eligibility = implementFeature, gittest.DefaultBranch, eligible
				return b, nil
			}
			h.Save = func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error) {
				return g.remoteFeature(), nil
			}
			h.PublishSaved = func(_ context.Context, _ Options, _ ActivityRequest, head string) error {
				forge.remote = head
				forge.pr.Head = head
				return nil
			}
			gate := newTestGate()
			advice := 0
			h.SyncBase = func(ctx context.Context, opts Options, a ActivityRequest, b GraphDeliveryBinding, head string) (syncBaseOutput, error) {
				binding := SyncBaseBinding{Repo: g.bindings.Repo, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
					return SyncBaseTarget{Working: b.Branch, Base: b.Target, ExpectedHead: head, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}, nil
				}, ResolveConflict: func(ctx context.Context, scratch ImplementScratch, target SyncBaseTarget) (string, error) {
					advice++
					return resolveBugDeliveryConflict(ctx, gatedScratch{ImplementScratch: scratch, gate: gate, clone: g.repo.Dir}, target, resolvingAdvisor("func TestRepair(t *testing.T) {}\nfunc TestBase(t *testing.T) {}\n", ""), f.session.TaskRef, req.NodeAttemptID, "fixture", "", 65536)
				}}
				result, err := (SyncBaseActivity{binding}).Execute(ctx, opts, a)
				var out syncBaseOutput
				if err == nil {
					err = json.Unmarshal(result.Output, &out)
				}
				return out, err
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if out.Outcome != "revalidate" || forge.merges != 0 || forge.closes != 0 || (conflict && advice != 1) {
				t.Fatalf("sync did not revalidate: %+v advice=%d", out, advice)
			}
			at := f.opts.MCP.Now()
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), req.NodeAttemptID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "revalidate", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: out.Output}}, Transition: &store.GraphTransition{InstanceID: req.InstanceID, FromNode: "finalization", ToNode: "fixer", OnOutcome: "revalidate", At: at}}))
			if _, err := CurrentGraphBugRepairEligibility(t.Context(), f.db, req.InstanceID); err == nil {
				t.Fatal("pre-sync result remained eligible")
			}
			out, err = h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if out.Outcome != "blocked" || forge.merges != 0 {
				t.Fatal("stale success merged")
			}
			checked := g.remoteFeature()
			g.repo.Git("merge-base", "--is-ancestor", base, checked)
			scratch, err := g.bindings.Repo.Begin(t.Context(), implementFeature)
			mustProgress(t, err)
			cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
			cmd.Dir = scratch.Path()
			output, checkErr := cmd.CombinedOutput()
			mustProgress(t, scratch.Close(t.Context()))
			if checkErr != nil {
				t.Fatalf("synchronized package check: %s %v", output, checkErr)
			}
			if !strings.Contains(g.remoteFile("shared_test.go"), "TestRepair") || !strings.Contains(g.remoteFile(baseFile), "TestBase") {
				t.Fatal("lost an addition")
			}
			// A later failed/blocked fixer attempt cannot use the old success.
			for _, outcome := range []string{"blocked", "succeeded"} {
				completeDeliveryRecheck(t, f, req.InstanceID, forge.binding.Eligibility, outcome)
				out, err = h.Execute(t.Context(), f.opts, req)
				mustProgress(t, err)
				if outcome == "blocked" && (out.Outcome != "blocked" || forge.merges != 0) {
					t.Fatal("blocked checks merged")
				}
			}
			if out.Outcome != "completed" || forge.merges != 1 || forge.closes != 1 || forge.pr.Head != checked {
				t.Fatalf("fresh head not delivered: %+v %+v", out, forge)
			}
		})
	}
}

func completeDeliveryRecheck(t *testing.T, f *fixture, instance int64, old GraphBugRepairEligibility, outcome string) {
	t.Helper()
	ctx, at := t.Context(), f.opts.MCP.Now()
	segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 2, "recheck", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: int64(200 + len(outcome)), StartedAt: at, LogPath: func(int) string { return "recheck.log" }})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: at})
	mustProgress(t, err)
	if outcome == "succeeded" {
		old.AttemptID, old.RunID, old.ResultID = attempt.ID, run.ID, old.ResultID+1
		old.Repair.Checks = []bugrepair.Check{{Command: "go test ./...", Outcome: "passed"}}
		raw, err := json.Marshal(old)
		mustProgress(t, err)
		_, err = f.db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
		mustProgress(t, err)
	}
	completion := store.GraphCompletion{Status: store.GraphSucceeded, Outcome: outcome, FinishedAt: at, InstanceStatus: store.GraphRunning}
	if outcome == "succeeded" {
		completion.Transition = &store.GraphTransition{InstanceID: instance, FromNode: "fixer", ToNode: "finalization", OnOutcome: outcome, At: at}
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, completion))
}
