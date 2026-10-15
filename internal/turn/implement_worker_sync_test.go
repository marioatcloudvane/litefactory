package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// The real Implement driver reconstructs its runtime for every dispatch. This
// exercises both the first checkout and repairs that pause after sync and later
// recover their handoff from the captured graph and durable output.
func TestImplementWorkerSyncInitialAndRepairCheckoutHandoff(t *testing.T) {
	const storyBranch = "lf/story-001"
	const parentBranch = "lf/900-feature"
	var f *fixture
	var expectedBase string
	var launches []syncBaseOutput
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		instance, err := f.db.LatestWorkflowInstance(ctx, f.session.ID, "implement")
		mustProgress(t, err)
		attempt, err := f.db.LatestGraphNodeAttempt(ctx, instance.ID, "sync_before_worker")
		mustProgress(t, err)
		outputs, err := f.db.ListGraphOutputs(ctx, attempt.ID)
		mustProgress(t, err)
		var synced syncBaseOutput
		found := 0
		for _, output := range outputs {
			if output.Kind != store.GraphOutputJSON {
				continue
			}
			var candidate syncBaseOutput
			mustProgress(t, json.Unmarshal(output.Payload, &candidate))
			if candidate.Result == attempt.Outcome && candidate.CheckoutVerified {
				synced = candidate
				found++
			}
		}
		if found != 1 || len(launches) >= 3 {
			t.Fatalf("launch lacks unique verified sync handoff or exceeds worker budget: handoffs=%d launches=%d", found, len(launches))
		}
		wantOutcome := []string{"current", "synced", "current"}[len(launches)]
		if attempt.Outcome != wantOutcome || !synced.CheckoutVerified || synced.Working != storyBranch || synced.Base != parentBranch {
			t.Fatalf("launch %d handoff=%+v attempt=%+v", len(launches)+1, synced, attempt)
		}
		checkout, err := f.opts.Git.Open(ctx, req.WorktreePath)
		mustProgress(t, err)
		head, err := checkout.RevParse(ctx, "HEAD", git.RevParseOptions{})
		mustProgress(t, err)
		if head != synced.Commit || !remoteHolds(t, f, storyBranch, expectedBase) || f.repo.Git("merge-base", expectedBase, head) != expectedBase {
			t.Fatalf("launch %d checkout=%s synced=%s does not hold selected base %s", len(launches)+1, head, synced.Commit, expectedBase)
		}
		if got := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+storyBranch)); len(got) != 2 || got[0] != head {
			t.Fatalf("launch checkout is not the published gate result: %v head=%s", got, head)
		}
		if data, err := os.ReadFile(filepath.Join(req.WorktreePath, "dependency.txt")); err != nil || string(data) != "selected parent base\n" {
			t.Fatalf("launch did not receive the parent's dependency: %q %v", data, err)
		}
		launches = append(launches, synced)
		if len(launches) == 1 {
			// The next gate must fetch this newer base, rather than reuse this
			// worker's successful initial synchronization.
			expectedBase = advanceRemote(t, f, parentBranch, "repair-base.txt", "base advanced between permitted attempts\n")
		}
		result := implementIncompleteJSON
		if len(launches) == 3 {
			mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, "finished.txt"), []byte("done\n"), 0o600))
			result = implementCompletedJSON
		}
		submitLiveResult(t, req, result)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	var request Request
	f, _, request = assembledImplementFixture(t, runner)
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	ctx := t.Context()
	parent := f.otherSession(900)
	_, err := f.db.FreezeSessionSlug(ctx, parent.ID, "feature", parentBranch)
	mustProgress(t, err)
	f.repo.Git("push", "--quiet", "origin", "HEAD:refs/heads/"+parentBranch)
	expectedBase = advanceRemote(t, f, parentBranch, "dependency.txt", "selected parent base\n")
	if got := f.repo.Git("ls-remote", "origin", "refs/heads/"+storyBranch); got != "" {
		t.Fatalf("first invocation must initialize a missing branch: %q", got)
	}
	out, err := Execute(ctx, f.opts, request)
	if err != nil || len(launches) != 1 {
		t.Fatalf("first worker launch: calls=%d output=%+v error=%v", len(launches), out, err)
	}
	session, err := f.db.GetSession(ctx, f.session.ID)
	mustProgress(t, err)
	if session.Branch != storyBranch || session.FeatureSlug != "health-endpoint" {
		t.Fatalf("first branch did not use approved story and ledger identity: %+v", session)
	}
	// The repair gate executes before the driver pauses at the next agent.
	// Another Execute restores that graph and verifies its durable handoff.
	instance, err := f.db.LatestWorkflowInstance(ctx, f.session.ID, "implement")
	mustProgress(t, err)
	if instance.CurrentNode != "worker_agent" {
		t.Fatalf("repair did not pause after its gate: %+v", instance)
	}
	repair, err := f.db.LatestGraphNodeAttempt(ctx, instance.ID, "sync_before_worker")
	mustProgress(t, err)
	if repair.Outcome != "synced" {
		t.Fatalf("repair gate did not fetch changed parent before delayed dispatch: %+v", repair)
	}
	if ran := runOwedImplementStages(t, f, request); ran < 2 {
		t.Fatalf("repairs did not restore delayed handoffs: dispatches=%d", ran)
	}
	row := implementWorkerRow(t, f)
	if len(launches) != 3 || row.State != store.ImplementDone || row.Attempts != 2 {
		t.Fatalf("three permitted launches finish after two repairs: launches=%d row=%+v", len(launches), row)
	}
	if launches[0].Commit == launches[1].Commit || launches[1].Commit != launches[2].Commit {
		t.Fatalf("fresh base then current handoff: %+v", launches)
	}
	attempts, err := f.db.ListGraphNodeAttempts(ctx, instance.ID)
	mustProgress(t, err)
	count := 0
	for _, a := range attempts {
		if a.Node == "sync_before_worker" {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("each permitted entry traverses a fresh gate: sync dispatches=%d", count)
	}
}

func TestImplementWorkerDelayedDirtyHandoffCannotLaunch(t *testing.T) {
	calls := 0
	var retained string
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		retained = req.WorktreePath
		submitLiveResult(t, req, implementIncompleteJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledImplementFixture(t, runner)
	enableWorkerSyncFixture(f)
	seedImplementStory(t, f)
	_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, "lf/story-001")
	mustProgress(t, err)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if calls != 1 || retained == "" {
		t.Fatalf("initial launch: calls=%d retained=%q", calls, retained)
	}
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "implement")
	mustProgress(t, err)
	if instance.CurrentNode != "worker_agent" {
		t.Fatalf("no delayed handoff: %+v", instance)
	}
	const content = "retained work changed after sync\n"
	path := filepath.Join(retained, "keep.txt")
	mustProgress(t, os.WriteFile(path, []byte(content), 0o600))
	dispatch, ok, err := f.db.NextFreshGraphDispatch(t.Context(), f.session.ID)
	mustProgress(t, err)
	if !ok {
		t.Fatal("repair dispatch absent")
	}
	reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), dispatch.ID, dispatch.DueAt)
	mustProgress(t, err)
	if !ok {
		t.Fatal("repair dispatch could not be reserved")
	}
	req.Trigger.CommentID, req.Trigger.Body = reserved.TriggerID(), "Continue the workflow."
	_, err = Execute(t.Context(), f.opts, req)
	if err == nil || calls != 1 {
		t.Fatalf("unsafe delayed handoff launched: calls=%d error=%v", calls, err)
	}
	data, readErr := os.ReadFile(path)
	mustProgress(t, readErr)
	if string(data) != content {
		t.Fatalf("dirty retained work changed: %q", data)
	}
	if row := implementWorkerRow(t, f); row.Attempts != 1 {
		t.Fatalf("failed handoff spent worker repairs: %+v", row)
	}
}
