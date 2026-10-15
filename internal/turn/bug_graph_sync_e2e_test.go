package turn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// The canonical captured graph, authenticated approval, durable reservations and
// reusable Git synchronization run together. Only the provider and finalizer
// responses are controlled; each fixer observes the real retained checkout.
func TestCanonicalBugEachFixerReceivesFreshBaseSnapshot(t *testing.T) {
	f, _, profile, instance := conversationSourceFixture(t, true)
	ctx := t.Context()
	g := newSyncGit(t)
	retained, err := git.EnsureWorktree(ctx, g.clone, git.EnsureWorktreeOptions{Dir: filepath.Join(t.TempDir(), "bug-retained"), Branch: implementFeature, BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	binding := SyncBaseBinding{Repo: g.bindings.Repo, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		return SyncBaseTarget{Working: implementFeature, Base: gittest.DefaultBranch, Checkout: GitSyncBaseCheckout{Worktree: retained.Worktree, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}, nil
	}}
	var activities ActivityRegistry
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: profile, Reporter: f.opts.ResearchReporter}))
	mustProgress(t, activities.Register(graphexec.SyncBaseAction, SyncBaseActivity{binding}))
	request := GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now(), NextDueAt: f.opts.MCP.Now().Add(time.Minute)}
	step, err := ExecuteNodeAction(ctx, f.opts, request)
	mustProgress(t, err)
	if step.NextNode != "analyst" {
		t.Fatalf("initial conversation: %+v", step)
	}
	completeConversationAnalyst(t, f, instance, "done")
	_, err = ExecuteNodeAction(ctx, f.opts, request)
	mustProgress(t, err)
	base := g.advance(gittest.DefaultBranch, "before-approval.txt", "base before approval\n")
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	delivery, err := f.db.GetReportDelivery(ctx, wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: "/approve-bug analysis 1", At: request.At.Add(time.Minute)}
	_, err = AdmitGraphBugOwnerReply(ctx, f.db, reply)
	mustProgress(t, err)
	request.At = reply.At
	step, err = ExecuteNodeAction(ctx, f.opts, request)
	mustProgress(t, err)
	syncNode := step.NextNode
	node, ok := profile.Node(syncNode)
	if !ok || node.Action != graphexec.SyncBaseAction {
		t.Fatalf("approval bypassed sync: %+v", step)
	}
	var syncAttempts []int64
	var checkoutHeads []string
	fixer := &graphTestHandler{results: map[string][]ActivityResult{"fixer": {{Outcome: "succeeded"}, {Outcome: "succeeded"}}}, hook: func(ctx context.Context, _ Options, req ActivityRequest) {
		if req.Node != "fixer" {
			t.Fatalf("unexpected provider node: %s", req.Node)
		}
		attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
		mustProgress(t, err)
		var latest store.GraphNodeAttempt
		for _, attempt := range attempts {
			if attempt.Node == syncNode && attempt.Status == store.GraphSucceeded && attempt.ID > latest.ID {
				latest = attempt
			}
		}
		if latest.ID == 0 || (len(syncAttempts) > 0 && latest.ID <= syncAttempts[len(syncAttempts)-1]) {
			t.Fatalf("fixer reused sync evidence: %+v", latest)
		}
		outputs, err := f.db.ListGraphOutputs(ctx, latest.ID)
		mustProgress(t, err)
		var evidence syncBaseOutput
		for _, output := range outputs {
			var candidate syncBaseOutput
			if json.Unmarshal(output.Payload, &candidate) == nil && candidate.CheckoutVerified {
				evidence = candidate
			}
		}
		head, err := retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
		mustProgress(t, err)
		if !evidence.CheckoutVerified || evidence.Commit != head || latest.Outcome != "synced" {
			t.Fatalf("unverified handoff: attempt=%+v evidence=%+v head=%s", latest, evidence, head)
		}
		if ancestor := g.repo.Git("merge-base", base, head); ancestor != base {
			t.Fatalf("fixer checkout %s lacks fetched base %s", head, base)
		}
		syncAttempts = append(syncAttempts, latest.ID)
		checkoutHeads = append(checkoutHeads, head)
	}}
	mustProgress(t, activities.Register(ResearchAgentAction, graphBugRoleActivity{p: SimplifiedBugPolicy{Profile: profile, Fixer: fixer}}))
	finalizer := &graphTestHandler{results: map[string][]ActivityResult{"finalization": {{Outcome: "revalidate"}, {Outcome: "completed"}}}}
	mustProgress(t, activities.Register("lf_bug_finalize", finalizer))
	request.Activities = activities
	for attempt := 0; attempt < 2; attempt++ {
		step, err = ExecuteNodeAction(ctx, f.opts, request)
		mustProgress(t, err)
		if step.Node != syncNode || step.NextNode != "fixer" {
			t.Fatalf("sync step: %+v", step)
		}
		step, err = ExecuteNodeAction(ctx, f.opts, request)
		mustProgress(t, err)
		if step.NextNode != "finalization" {
			t.Fatalf("fixer step: %+v", step)
		}
		if attempt == 0 {
			base = g.advance(gittest.DefaultBranch, "before-revalidation.txt", "base before revalidation\n")
		}
		step, err = ExecuteNodeAction(ctx, f.opts, request)
		mustProgress(t, err)
		if attempt == 0 && step.NextNode != syncNode {
			t.Fatalf("revalidation bypassed sync: %+v", step)
		}
		if attempt == 1 && step.NextNode != "completed" {
			t.Fatalf("finalization: %+v", step)
		}
	}
	if len(fixer.calls) != 2 || len(syncAttempts) != 2 || checkoutHeads[0] == checkoutHeads[1] {
		t.Fatalf("calls=%d syncs=%v heads=%v", len(fixer.calls), syncAttempts, checkoutHeads)
	}
}
