package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/git"
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
	if f.refuseMerge {
		f.refuseMerge = false
		return &github.APIError{StatusCode: 403}
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
	base := f.binding.Eligibility.Inputs.TargetMain
	if f.raceMain {
		base = strings.Repeat("e", 40)
	}
	return github.DeliveryCommit{SHA: f.pr.MergeSHA, Parents: []string{base, f.pr.Head}}, nil
}
func (f *graphDeliveryForge) CloseDeliveredParent(context.Context, github.Repo, int, int64) error {
	f.closes++
	f.issue.State = "closed"
	if f.loseClose {
		return errors.New("lost closure acknowledgment")
	}
	return nil
}

func graphFinalizationFixture(t *testing.T) (*fixture, GraphBugFinalization, ActivityRequest, *graphDeliveryForge, *GraphBugRepairInputs) {
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
	root, e := filepath.EvalSymlinks(t.TempDir())
	mustProgress(t, e)
	mustProgress(t, os.WriteFile(filepath.Join(root, "check.log"), []byte("controlled executed checks\n"), 0600))
	hash, e := git.ObserveEvidenceLog(ctx, root, "check.log")
	mustProgress(t, e)
	commands := []bugrepair.ValidationRequirement{{ID: "check", Command: "make check", Directory: "."}}
	manifest := []bugrepair.ValidationInput{{Name: "toolchain", SHA256: strings.Repeat("d", 64)}}
	digest, e := bugrepair.ValidationInputsDigest(commands, manifest)
	mustProgress(t, e)
	inputs := GraphBugRepairInputs{Source: git.SourceIdentity{Head: strings.Repeat("a", 40), Digest: strings.Repeat("b", 64)}, TargetMain: strings.Repeat("c", 40), InputsDigest: digest, Commands: commands, InputManifest: manifest}
	zero := 0
	repair := bugrepair.Evidence{Summary: "Fix #123 from approved scope", Validation: &bugrepair.Validation{Generation: "final", SourceDigest: inputs.Source.Digest, TargetMain: inputs.TargetMain, InputsDigest: digest, Commands: []bugrepair.CommandOutcome{{ID: "check", Command: "make check", Directory: ".", ExitCode: &zero, Outcome: "passed", Log: bugrepair.LogReference{Path: "check.log", SHA256: hash}}}, Conditions: []bugrepair.ConditionOutcome{{ID: "C1", Outcome: "passed", Log: bugrepair.LogReference{Path: "check.log", SHA256: hash}}}}}
	eligibility := GraphBugRepairEligibility{Record: "repair_eligibility", InstanceID: instance.ID, AttemptID: attempt.ID, RunID: run.ID, ResultID: 42, ResultRevision: 1, Handoff: handoff, Inputs: inputs, Repair: repair}
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
	forge := &graphDeliveryForge{binding: binding, main: inputs.TargetMain, remote: strings.Repeat("0", 40), issue: github.Issue{ID: 123, Number: f.session.IssueNumber, State: "open"}}
	handler := GraphBugFinalization{Forge: forge, Redactor: redact.New(redact.Options{Environ: []string{}}), Resolve: func(context.Context, Options, ActivityRequest, GraphBugRepairEligibility) (GraphDeliveryBinding, error) {
		return binding, nil
	}, Observe: func(context.Context, Options, ActivityRequest) (GraphBugRepairInputs, error) { return inputs, nil }, LogRoot: func(ActivityRequest) string { return root }, Save: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error) {
		return inputs.Source.Head, nil
	}, PublishSaved: func(context.Context, Options, ActivityRequest, string) error {
		forge.remote = inputs.Source.Head
		return nil
	}}
	return f, handler, req, forge, &inputs
}

func TestGraphDeliveryObservedEffectsAndMergeAcknowledgmentRecovery(t *testing.T) {
	for _, variant := range []string{"complete", "lost merge reply", "lost PR reply", "lost close reply", "main race", "stale", "definitive merge refusal"} {
		t.Run(variant, func(t *testing.T) {
			f, h, req, forge, inputs := graphFinalizationFixture(t)
			original := h.Observe
			h.Observe = func(ctx context.Context, o Options, a ActivityRequest) (GraphBugRepairInputs, error) {
				if variant == "stale" {
					v, e := original(ctx, o, a)
					v.Source.Digest = strings.Repeat("9", 64)
					return v, e
				}
				return original(ctx, o, a)
			}
			forge.loseMerge = variant == "lost merge reply"
			forge.losePR = variant == "lost PR reply"
			forge.loseClose = variant == "lost close reply"
			forge.raceMain = variant == "main race"
			forge.refuseMerge = variant == "definitive merge refusal"
			out, e := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, e)
			if variant == "stale" {
				if out.Outcome != "stale" || forge.creates+forge.merges+forge.closes != 0 {
					t.Fatalf("stale=%+v", out)
				}
				return
			}
			if variant == "lost merge reply" || variant == "lost PR reply" || variant == "lost close reply" || variant == "definitive merge refusal" {
				if variant == "lost merge reply" && (out.Outcome != "blocked" || forge.merges != 1 || forge.closes != 0) {
					t.Fatalf("lost=%+v", out)
				}
				mustProgress(t, f.db.Close())
				db, e := store.Open(t.Context(), f.paths.DBPath())
				mustProgress(t, e)
				f.db = db
				f.opts.DB = db
				out, e = h.Execute(t.Context(), f.opts, req)
				mustProgress(t, e)
			}
			if variant == "main race" {
				if out.Outcome != "blocked" || forge.closes != 0 {
					t.Fatalf("race=%+v", out)
				}
				return
			}
			expectedMerges := 1
			if variant == "definitive merge refusal" {
				expectedMerges = 2
			}
			if out.Outcome != "completed" || forge.creates != 1 || forge.merges != expectedMerges || forge.closes != 1 {
				t.Fatalf("progress=%+v counts=%d,%d,%d", out, forge.creates, forge.merges, forge.closes)
			}
			_ = inputs
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
