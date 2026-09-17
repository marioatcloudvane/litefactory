package turn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// multiParentForge is a minimal [StoryProgressForge] that, unlike
// story_progress_test.go's progressForge, does not hardcode a single parent
// issue: it resolves each child story's parent from an explicit map, so a
// single fixture can host two genuinely independent managed features (two
// different top-level issues) at once, the way real unrelated parents are.
type multiParentForge struct {
	parents     map[int]github.Issue // top-level issue number -> parent issue
	childParent map[int]int          // story issue number -> owning parent's issue number
	children    map[int]github.Issue // story issue number -> story issue
	comments    map[int][]github.Comment
}

func (f *multiParentForge) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	if issue, ok := f.parents[n]; ok {
		return issue, nil
	}
	return f.children[n], nil
}
func (f *multiParentForge) GetParentIssue(_ context.Context, _ github.Repo, child int) (github.Issue, error) {
	return f.parents[f.childParent[child]], nil
}
func (f *multiParentForge) ListCommentsAfter(_ context.Context, _ github.Repo, n int, _ int64) ([]github.Comment, error) {
	return f.comments[n], nil
}
func (f *multiParentForge) PostComment(_ context.Context, _ github.Repo, n int, b redact.Body) (github.Comment, error) {
	c := github.Comment{ID: int64(len(f.comments[n]) + 1), Body: b.String(), Author: github.User{ID: 9}}
	f.comments[n] = append(f.comments[n], c)
	return c, nil
}
func (f *multiParentForge) SetStoryState(_ context.Context, _ github.Repo, _ int, _ int64, n int, _ int64, closed bool) error {
	i := f.children[n]
	i.State = "open"
	if closed {
		i.State = "closed"
		i.StateReason = "completed"
	}
	f.children[n] = i
	return nil
}

// registerMinimalParent stands up one independent managed feature/parent —
// one approved story, activated scope, one registered Implement run — on
// forge, and returns the run and its worktree. suffix keeps two parents'
// keys, issue numbers and segment ordinals from colliding.
func registerMinimalParent(t *testing.T, f *fixture, forge *multiParentForge, session store.Session, topIssue, suffix int) (store.Run, string) {
	t.Helper()
	ctx := t.Context()
	parentID := int64(topIssue) * 10
	forge.parents[topIssue] = github.Issue{ID: parentID, Number: topIssue}
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning"}))
	key := fmt.Sprintf("US-%d", suffix)
	number := 900 + suffix
	issueID := int64(9000 + suffix)
	title, body := "Title "+key, "Body "+key
	hash := planbrief.StoryHash(title, body)
	story := store.ManagedStory{ParentTask: session.TaskRef, Generation: 1, Key: key, IssueNumber: number, IssueID: issueID, BodyHash: hash, State: "planning"}
	mustProgress(t, f.db.PutManagedStory(ctx, story))
	forge.childParent[number] = topIssue
	forge.children[number] = github.Issue{ID: issueID, Number: number, State: "open"}
	revision := store.ApprovedStoryRevision{ManagedStory: story, ParentID: parentID, Title: title, Body: body, Hash: hash, FetchedAt: time.Now().UTC()}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, session.TaskRef, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, session.TaskRef, 1, []store.ApprovedStoryRevision{revision}))
	mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, session.TaskRef, 1, story.Key, "freeze"))
	mustProgress(t, f.db.ActivateManagedScope(ctx, session.TaskRef, 1))
	seg, err := f.db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, fmt.Sprintf("finish-isolation-%d", suffix), store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: int64(suffix), LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(ctx, &agent.ApprovedBundle{RunID: run.ID, Parent: session.TaskRef, Generation: 1}, worktree))
	return run, worktree
}

// TestFinishIsolatesUnrelatedParentsBrokenTransport is issue #310's AC-003
// for the Finish call site specifically. Finish's own isolation boundary is
// the caller's registered *parent* (managed feature), not merely its run:
// [store.DB.ListStoryProgressRunsForParent] only ever reloads runs for one
// parent at a time. So this proves it with two genuinely distinct parents on
// the same database — parent A's story event transport is deliberately
// unreadable, parent B is healthy — and confirms Finish for parent B's own
// run neither fails nor touches parent A's registration, while the
// still-global daemon-wide Poll (AC-004) still discovers and fails on
// parent A's broken transport.
func TestFinishIsolatesUnrelatedParentsBrokenTransport(t *testing.T) {
	f := newFixture(t, nil)
	forge := &multiParentForge{parents: map[int]github.Issue{}, childParent: map[int]int{}, children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	progress, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "finish-isolation-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = progress

	runA, worktreeA := registerMinimalParent(t, f, forge, f.session, testIssue, 1)
	sessionB, err := f.db.GetOrCreateSession(t.Context(), f.storeRepo, 999)
	mustProgress(t, err)
	runB, _ := registerMinimalParent(t, f, forge, sessionB, 999, 2)

	// Deliver each parent's own freeze-scope admission notice up front, the
	// way a healthy daemon tick already would have by the time either run's
	// own story work began (mirrors the "deliver admission first" step other
	// story-progress tests use). Both runs are still "running", so this does
	// not retire either registration. This isolates what this test actually
	// exercises below: reconciliation of already-registered runs, not first
	// delivery.
	mustProgress(t, progress.Poll(t.Context()))

	// Break parent A's transport: a structurally invalid event file that
	// makes ingest fail loudly if this parent's registration is ever
	// reloaded and processed.
	dirA := filepath.Join(worktreeA, agent.StoryEventsDirectory, fmt.Sprint(runA.ID))
	mustProgress(t, os.MkdirAll(dirA, 0o700))
	mustProgress(t, os.WriteFile(filepath.Join(dirA, "unread.json"), []byte("not a story event"), 0o600))

	if err := f.opts.StoryProgress.Finish(t.Context(), runB.ID, "test reason"); err != nil {
		t.Fatalf("Finish for an unrelated healthy parent failed because of a different parent's broken transport: %v", err)
	}

	gotB, err := f.db.GetStoryProgressRun(t.Context(), runB.ID)
	mustProgress(t, err)
	if gotB.Active {
		t.Fatal("Finish did not retire the caller's own run registration")
	}
	gotA, err := f.db.GetStoryProgressRun(t.Context(), runA.ID)
	mustProgress(t, err)
	if !gotA.Active {
		t.Fatal("Finish touched an unrelated parent's registration")
	}

	// The daemon-wide sweep remains global (AC-004): it still discovers and
	// fails on parent A's broken transport.
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("the global daemon-wide Poll silently accepted a broken parent's story event transport")
	}
}
