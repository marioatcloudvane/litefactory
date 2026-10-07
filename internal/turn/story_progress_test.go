package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type progressForge struct {
	publicationForge
	children                           map[int]github.Issue
	comments                           map[int][]github.Comment
	changes                            []string
	loseComment, loseClose, failReopen bool
}

func (f *progressForge) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	if n == testIssue {
		return github.Issue{ID: 1230, Number: n}, nil
	}
	return f.children[n], nil
}
func (f *progressForge) GetParentIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{ID: 1230, Number: testIssue}, nil
}
func (f *progressForge) ListCommentsAfter(_ context.Context, _ github.Repo, n int, _ int64) ([]github.Comment, error) {
	return f.comments[n], nil
}
func (f *progressForge) PostComment(_ context.Context, _ github.Repo, n int, b redact.Body) (github.Comment, error) {
	c := github.Comment{ID: int64(len(f.comments[n]) + 1), Body: b.String(), Author: github.User{ID: 9}}
	f.comments[n] = append(f.comments[n], c)
	if f.loseComment && strings.Contains(b.String(), "Closing this story now") {
		f.loseComment = false
		return github.Comment{}, errors.New("lost comment response")
	}
	return c, nil
}
func (f *progressForge) SetStoryState(_ context.Context, _ github.Repo, _ int, _ int64, n int, _ int64, closed bool) error {
	if !closed && f.failReopen {
		f.failReopen = false
		return errors.New("reopen failed")
	}
	i := f.children[n]
	i.State = "open"
	if closed {
		i.State = "closed"
		i.StateReason = "completed"
	}
	f.children[n] = i
	f.changes = append(f.changes, fmt.Sprintf("%d:%s", n, i.State))
	if closed && f.loseClose {
		f.loseClose = false
		return errors.New("lost close response")
	}
	return nil
}
func (*progressForge) CreateSubIssues(context.Context, github.Repo, int, []github.SubIssueSpec) ([]github.SubIssueResult, error) {
	return nil, nil
}
func setupProgressScope(t *testing.T, f *fixture) (*progressForge, *report.Reporter) {
	return setupVersionedProgressScope(t, f, store.WorkflowLegacy)
}

func setupVersionedProgressScope(t *testing.T, f *fixture, version int) (*progressForge, *report.Reporter) {
	t.Helper()
	ctx := t.Context()
	mustProgress(t, f.db.PutManagedFeature(ctx, store.ManagedFeature{WorkflowVersion: version, ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	forge := &progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}
	var revisions []store.ApprovedStoryRevision
	for idx, key := range []string{"US-1", "US-2"} {
		n := 200 + idx
		body := "## Acceptance Criteria\n- AC-1: the scoped result passes\n"
		title := "Story " + key
		hash := planbrief.StoryHash(title, body)
		s := store.ManagedStory{ParentTask: f.session.TaskRef, Generation: 1, Key: key, IssueNumber: n, IssueID: int64(n * 10), BodyHash: hash, State: "planning"}
		mustProgress(t, f.db.PutManagedStory(ctx, s))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: s, ParentID: 1230, Title: title, Body: body, Hash: hash, FetchedAt: time.Now().UTC()})
		forge.children[n] = github.Issue{ID: s.IssueID, Number: n, Title: title, Body: body, State: "open"}
	}
	mustProgress(t, f.db.SetManagedFeatureReady(ctx, f.session.TaskRef, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(ctx, f.session.TaskRef, 1, revisions))
	for _, s := range revisions {
		mustProgress(t, f.db.DeliverStoryScopeNotice(ctx, s.ParentTask, 1, s.Key, "freeze"))
	}
	mustProgress(t, f.db.ActivateManagedScope(ctx, f.session.TaskRef, 1))
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	service, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "story-progress-locks"))
	mustProgress(t, err)
	f.opts.StoryProgress = service
	f.opts.SubIssueCreator = forge
	return forge, renderer
}
func mustProgress(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func progressEvent(t *testing.T, f *fixture, runID int64, worktree string) agent.StoryEvent {
	t.Helper()
	content := []byte("package sample\n")
	mustProgress(t, os.WriteFile(filepath.Join(worktree, "sample.go"), content, 0o600))
	files := []agent.StorySnapshotFile{{Path: "sample.go", Content: content, SHA256: agent.StoryDigest(content)}}
	digest := agent.SnapshotDigest(files)
	check := agent.StoryCheck{Name: "AC-1", Passed: true, Evidence: "scoped check passed", ImplementationDigest: digest}
	rs, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	return agent.StoryEvent{Version: 1, Parent: f.session.TaskRef, RunID: runID, Generation: 1, Story: "US-1", IssueID: 2000, Revision: rs[0].Hash, Attempt: 1, Sequence: 1, Kind: "verified", Evidence: &agent.StoryEvidence{ImplementationDigest: digest, Files: files, Acceptance: []agent.StoryCheck{check}, Review: []agent.StoryCheck{check}, Tests: []agent.StoryCheck{check}}}
}
func writeProgressEvent(t *testing.T, worktree string, e agent.StoryEvent) {
	t.Helper()
	data, err := json.Marshal(e)
	mustProgress(t, err)
	dir := filepath.Join(worktree, agent.StoryEventsDirectory, fmt.Sprint(e.RunID))
	mustProgress(t, os.MkdirAll(dir, 0o700))
	tmp := filepath.Join(dir, "pending")
	mustProgress(t, os.WriteFile(tmp, data, 0o600))
	mustProgress(t, os.Rename(tmp, filepath.Join(dir, fmt.Sprintf("%06d.json", e.Sequence))))
}
func progressOnlyRun(t *testing.T) (*fixture, *progressForge, *report.Reporter, int64, string) {
	t.Helper()
	f := newFixture(t, nil)
	forge, renderer := setupProgressScope(t, f)
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "session", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), &agent.ApprovedBundle{RunID: run.ID, Parent: f.session.TaskRef, Generation: 1}, worktree))
	return f, forge, renderer, run.ID, worktree
}
func TestStoryProgressClosesDuringHeldParentBeforeSave(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			ready := make(chan agent.Request, 1)
			release := make(chan struct{})
			siblingRelease := make(chan struct{})
			done := make(chan Result, 1)
			runErrors := make(chan error, 1)
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				ready <- req
				<-release
				<-siblingRelease
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			forge, _ := setupProgressScope(t, f)
			f.opts.Runners = RunnerSet{kind: runner}
			req := f.request()
			req.Mode = agent.ModeImplement
			req.Agent = kind
			req.Command = ParseCommand("/implement work")
			req.Trigger.Body = "/implement work"
			go func() { r, err := Execute(t.Context(), f.opts, req); runErrors <- err; done <- r }()
			var request agent.Request
			select {
			case request = <-ready:
			case err := <-runErrors:
				t.Fatalf("run failed before launch: %v", err)
			}
			runs, err := f.db.ListRuns(t.Context(), f.session.ID)
			mustProgress(t, err)
			e := progressEvent(t, f, runs[0].ID, request.WorktreePath)
			writeProgressEvent(t, request.WorktreePath, e)
			mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
			if forge.children[200].State != "closed" || forge.children[201].State != "open" {
				t.Fatal("scoped story not closed independently of held sibling")
			}
			stored, err := f.db.GetRun(t.Context(), e.RunID)
			mustProgress(t, err)
			if stored.Status != store.RunRunning || stored.CommitSHA != "" {
				t.Fatal("closure waited for parent completion/save")
			}
			// No parent/sibling can finish before this release; closure is already visible.
			close(release)
			close(siblingRelease)
			mustProgress(t, <-runErrors)
			result := <-done
			if !result.Succeeded() {
				t.Fatalf("parent failed: %+v", result)
			}
			for _, file := range result.Files() {
				if strings.Contains(file, agent.StoryEventsDirectory) {
					t.Fatal("transport leaked into saved files")
				}
			}
		})
	}
}
func TestStoryProgressResponseLossAndInvalidationSurviveNewService(t *testing.T) {
	f, forge, renderer, runID, worktree := progressOnlyRun(t)
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	forge.loseComment = true
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("lost response not surfaced")
	}
	if forge.children[200].State != "open" {
		t.Fatal("closed without confirmed completion comment")
	}
	restarted, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "story-progress-locks"))
	mustProgress(t, err)
	forge.loseClose = true
	if err := restarted.Poll(t.Context()); err == nil {
		t.Fatal("lost close not surfaced")
	}
	if len(forge.comments[200]) != 2 {
		t.Fatalf("duplicated completion comment: %d", len(forge.comments[200]))
	}
	// Invalidation supersedes the uncertain close before its local acknowledgement.
	e.Kind = "invalidated"
	e.Evidence = nil
	e.Reason = "later scoped regression"
	e.Sequence = 2
	writeProgressEvent(t, worktree, e)
	forge.failReopen = true
	if err := restarted.Poll(t.Context()); err == nil {
		t.Fatal("failed reopen not surfaced")
	}
	mustProgress(t, f.db.Close())
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, reopened.Close()) })
	f.db = reopened
	restarted, err = NewStoryProgress(reopened, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "story-progress-locks"))
	mustProgress(t, err)
	mustProgress(t, restarted.Poll(t.Context()))
	if forge.children[200].State != "open" {
		t.Fatal("restart did not reopen invalidated scope")
	}
	if strings.Join(forge.changes, ",") != "200:closed,200:open" {
		t.Fatalf("stale close replay: %v", forge.changes)
	}
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if stories[0].State != "blocked" {
		t.Fatal("invalidation did not revoke verification")
	}
}
func TestStoryProgressRejectsMutableEvidenceAndConflictingReplay(t *testing.T) {
	f, forge, _, runID, worktree := progressOnlyRun(t)
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	mustProgress(t, os.WriteFile(filepath.Join(worktree, "sample.go"), []byte("changed"), 0o600))
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("mutable evidence accepted")
	}
	if forge.children[200].State != "open" {
		t.Fatal("unstable story closed")
	}
	mustProgress(t, os.WriteFile(filepath.Join(worktree, "sample.go"), e.Evidence.Files[0].Content, 0o600))
	e.Sequence = 2
	writeProgressEvent(t, worktree, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	if len(forge.changes) != 1 {
		t.Fatal("identical replay repeated close")
	}
	e.Evidence.Tests[0].Evidence = "conflicting evidence"
	writeProgressEvent(t, worktree, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	if forge.children[200].State != "open" {
		t.Fatal("conflicting immutable replay retained closure")
	}
}

func TestStoryProgressPendingCloseBlocksReplanUntilDelivered(t *testing.T) {
	f, forge, _, runID, worktree := progressOnlyRun(t)
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	forge.loseClose = true
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("expected uncertain close")
	}
	_, err := f.db.FinishRun(t.Context(), runID, store.RunOutcome{Status: store.RunFailed, Error: "unrelated sibling failed"})
	mustProgress(t, err)
	mustProgress(t, f.db.EndStoryProgress(t.Context(), runID, "sibling unfinished"))
	if err := f.db.ReplanManagedFeature(t.Context(), f.session.TaskRef, 1); err == nil {
		t.Fatal("replan overtook undelivered verified close")
	}
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.db.ReplanManagedFeature(t.Context(), f.session.TaskRef, 1))
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 2)
	mustProgress(t, err)
	if stories[0].State != "completed" || stories[1].State != "planning" {
		t.Fatalf("unrelated failure lost valid completion: %+v", stories)
	}
}

func TestStoryProgressUnreadInputPreventsPendingClose(t *testing.T) {
	f, forge, _, runID, worktree := progressOnlyRun(t)
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	forge.loseComment = true
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("expected pending completion")
	}
	e.Sequence, e.Kind, e.Evidence, e.Reason = 2, "invalidated", nil, "regression"
	writeProgressEvent(t, worktree, e)
	bad := filepath.Join(worktree, agent.StoryEventsDirectory, fmt.Sprint(runID), "unread.json")
	mustProgress(t, os.WriteFile(bad, []byte("broken"), 0o600))
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("expected structural failure")
	}
	if len(forge.changes) != 0 {
		t.Fatal("incomplete ingestion dispatched stale close")
	}
	mustProgress(t, os.Remove(bad))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	if len(forge.changes) != 0 || forge.children[200].State != "open" {
		t.Fatal("recovery closed invalidated story")
	}
}

type pausedProgressForge struct {
	*progressForge
	entered chan struct{}
	release chan struct{}
}

func (f *pausedProgressForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if f.entered != nil {
		entered := f.entered
		f.entered = nil
		close(entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return github.Issue{}, ctx.Err()
		}
	}
	return f.progressForge.GetIssue(ctx, repo, number)
}

func TestStoryProgressIndependentServicesSerializeIngestionAndDelivery(t *testing.T) {
	f, forge, renderer, runID, worktree := progressOnlyRun(t)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context())) // deliver admission first
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	entered, release := make(chan struct{}), make(chan struct{})
	paused := &pausedProgressForge{progressForge: forge, entered: entered, release: release}
	lockDir := f.paths.StoryProgressLocksDir()
	a, err := NewStoryProgress(f.db, paused, renderer, 9, lockDir)
	mustProgress(t, err)
	other, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, other.Close()) })
	b, err := NewStoryProgress(other, forge, renderer, 9, lockDir)
	mustProgress(t, err)
	done := make(chan error, 1)
	go func() { done <- a.Poll(t.Context()) }()
	<-entered
	e.Sequence, e.Kind, e.Evidence, e.Reason = 2, "invalidated", nil, "new regression"
	writeProgressEvent(t, worktree, e)
	if err := b.Poll(t.Context()); !errors.Is(err, store.ErrStoryProgressBusy) {
		t.Errorf("second service acquired busy parent: %v", err)
	}
	if _, err := other.GetStoryProgressEvent(t.Context(), runID, 2); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("invalidation ingested across active delivery: %v", err)
	}
	close(release)
	mustProgress(t, <-done)
	mustProgress(t, b.Poll(t.Context()))
	if got := strings.Join(forge.changes, ","); got != "200:closed,200:open" {
		t.Fatalf("wrong authoritative ordering: %s", got)
	}
	// Fresh ownership after both holders exited cannot replay the old close.
	mustProgress(t, a.Poll(t.Context()))
	if forge.children[200].State != "open" || len(forge.changes) != 2 {
		t.Fatal("stale close replayed")
	}
}

func TestStoryFeatureCompletesOnlyAfterAggregateAndAllStories(t *testing.T) {
	f, _, _, runID, worktree := progressOnlyRun(t)
	e := progressEvent(t, f, runID, worktree)
	writeProgressEvent(t, worktree, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if feature.State != "implementing" {
		t.Fatal("completed feature before sibling")
	}
	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	e.Sequence, e.Story, e.IssueID, e.Revision = 2, "US-2", 2010, revisions[1].Hash
	writeProgressEvent(t, worktree, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.opts.StoryProgress.Finish(t.Context(), runID, ""))
	feature, err = f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if feature.State != "implementing" {
		t.Fatal("completed without aggregate success")
	}
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	input := harness.PromptInput{Mode: "implement", Ref: ref}
	if err := prepareApprovedContext(t.Context(), f.opts, &input, runID); err != nil {
		t.Fatalf("parent aggregate recovery was refused: %v", err)
	}
	if !input.ApprovedStories.ParentRecovery || input.ApprovedStories.EventDirectory != "" {
		t.Fatal("completed scope was not restricted to parent-only recovery")
	}
	_, err = f.db.FinishRun(t.Context(), runID, store.RunOutcome{Status: store.RunSucceeded})
	mustProgress(t, err)
	mustProgress(t, f.db.MarkStoryAggregateSucceeded(t.Context(), runID))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	feature, err = f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if feature.State != "completed" {
		t.Fatalf("aggregate gate did not complete feature: %s", feature.State)
	}
}
