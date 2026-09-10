package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func engineeringRun(t *testing.T) (*fixture, *progressForge, int64, string) {
	t.Helper()
	f := newFixture(t, nil)
	forge, _ := setupVersionedProgressScope(t, f, 0)
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if feature.WorkflowVersion != store.WorkflowSeparated {
		t.Fatal("new approval retained legacy version")
	}
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "session", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), &agent.ApprovedBundle{RunID: run.ID, Parent: f.session.TaskRef, Generation: 1}, worktree))
	return f, forge, run.ID, worktree
}

func engineeringEvent(t *testing.T, f *fixture, runID int64, root string) agent.StoryEvent {
	t.Helper()
	e := progressEvent(t, f, runID, root)
	e.Kind = "engineering_complete"
	e.Evidence.Acceptance = nil
	e.Evidence.Review = nil
	return e
}

func TestEngineeringCompletesWithoutAcceptanceAndReleasesDependencies(t *testing.T) {
	f, forge, runID, root := engineeringRun(t)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if !stories[0].EngineeringComplete || stories[0].State == "completed" || forge.children[200].State != "open" {
		t.Fatalf("engineering became acceptance: %+v", stories[0])
	}
	retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, "US-1")
	mustProgress(t, err)
	got, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	if got.Evidence.ImplementationDigest != e.Evidence.ImplementationDigest {
		t.Fatal("snapshot lost")
	}
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	in := harness.PromptInput{Mode: "implement", Ref: ref, RunOrdinal: 1}
	mustProgress(t, prepareApprovedContext(t.Context(), f.opts, &in, runID))
	if len(in.ApprovedStories.CompletedDependencies) != 1 || !in.ApprovedStories.Stories[0].EngineeringComplete {
		t.Fatal("engineering did not release dependency")
	}
	pending, err := f.db.PendingEngineering(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if fmt.Sprint(pending) != "[US-2]" {
		t.Fatalf("pending %v", pending)
	}
	for _, c := range forge.comments[200] {
		if strings.Contains(c.Body, "Closing this story") {
			t.Fatal("closure was proposed")
		}
	}
}

func TestEngineeringRejectsLegacyAcceptanceAndStaleSnapshots(t *testing.T) {
	for _, kind := range []string{"verified", "snapshot", "revision", "failed test", "generation", "run", "issue", "missing test outcomes", "out of sequence attempt"} {
		t.Run(kind, func(t *testing.T) {
			f, forge, runID, root := engineeringRun(t)
			e := engineeringEvent(t, f, runID, root)
			switch kind {
			case "verified":
				e = progressEvent(t, f, runID, root)
			case "snapshot":
				e.Evidence.Files[0].Content = []byte("wrong")
			case "revision":
				e.Revision = agent.StoryDigest([]byte("wrong"))
			case "failed test":
				e.Evidence.Tests[0].Passed = false
			case "generation":
				e.Generation++
			case "run":
				e.RunID++
			case "issue":
				e.IssueID++
			case "missing test outcomes":
				e.Evidence.Tests = nil
			case "out of sequence attempt":
				e.Attempt = 2
			}
			if kind == "run" {
				// Keep transport in the registered run directory so ingestion must
				// reject the payload identity, rather than simply not finding it.
				data, err := json.Marshal(e)
				mustProgress(t, err)
				dir := filepath.Join(root, agent.StoryEventsDirectory, fmt.Sprint(runID))
				mustProgress(t, os.MkdirAll(dir, 0o700))
				mustProgress(t, os.WriteFile(filepath.Join(dir, "000001.json"), data, 0o600))
			} else {
				writeProgressEvent(t, root, e)
			}
			if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
				t.Fatal("invalid engineering accepted")
			}
			pending, err := f.db.PendingEngineering(t.Context(), f.session.TaskRef, 1)
			mustProgress(t, err)
			if len(pending) != 2 || forge.children[200].State != "open" {
				t.Fatal("invalid event advanced engineering")
			}
		})
	}
}

func TestEngineeringRestartPreservesProgressWithoutClosure(t *testing.T) {
	f, forge, runID, root := engineeringRun(t)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	before := len(forge.comments[200])
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	restarted, err := NewStoryProgress(f.db, forge, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "story-progress-locks"))
	mustProgress(t, err)
	mustProgress(t, restarted.Poll(t.Context()))
	if len(forge.comments[200]) != before || forge.children[200].State != "open" {
		t.Fatal("restart duplicated or closed")
	}
}

func TestEngineeringHarvestRequiresEvidenceBeforeSaveAndSuggestion(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				if complete {
					runs, err := f.db.ListRuns(t.Context(), f.session.ID)
					mustProgress(t, err)
					e := engineeringEvent(t, f, runs[0].ID, req.WorktreePath)
					writeProgressEvent(t, req.WorktreePath, e)
					revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
					mustProgress(t, err)
					e.Sequence, e.Story, e.IssueID, e.Revision = 2, "US-2", 2010, revisions[1].Hash
					writeProgressEvent(t, req.WorktreePath, e)
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			forge, _ := setupVersionedProgressScope(t, f, 0)
			req := f.request()
			req.Mode = agent.ModeImplement
			req.Command = ParseCommand("/implement work")
			req.Trigger.Body = "/implement work"
			result := f.execute(req)
			if result.Succeeded() != complete {
				t.Fatalf("result succeeded=%v want %v: %+v", result.Succeeded(), complete, result)
			}
			feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			if feature.EngineeringSucceeded != complete || feature.State != "implementing" {
				t.Fatalf("engineering or acceptance state %+v", feature)
			}
			suggestion := result.Harness != nil && strings.Contains(result.Harness.ResultText, "Factory next action:")
			if suggestion != complete {
				t.Fatalf("suggestion=%v want %v", suggestion, complete)
			}
			if !complete && result.Run.CommitSHA != "" {
				t.Fatal("saved incomplete engineering")
			}
			if forge.children[200].State != "open" || forge.children[201].State != "open" {
				t.Fatal("engineering closed children")
			}
		})
	}
}

func TestEngineeringCannotInvalidateHistoricalAcceptance(t *testing.T) {
	f, forge, runID, root := engineeringRun(t)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	accepted := stories[0]
	prior := accepted.State
	accepted.State = "completed"
	mustProgress(t, f.db.CompareManagedStory(t.Context(), accepted, prior))
	child := forge.children[200]
	child.State = "closed"
	forge.children[200] = child
	e.Sequence = 2
	e.Kind = "invalidated"
	e.Reason = "later implementation changed"
	e.Evidence = nil
	writeProgressEvent(t, root, e)
	if err := f.opts.StoryProgress.Poll(t.Context()); err == nil {
		t.Fatal("historical invalidation accepted")
	}
	stories, err = f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if stories[0].State != "completed" || !stories[0].EngineeringComplete || forge.children[200].State != "closed" {
		t.Fatal("historical acceptance was changed")
	}
	// Conflicting transport is recorded, but cannot undo a historical decision.
	mustProgress(t, f.db.ConflictStoryProgress(t.Context(), runID, 1))
	stories, err = f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if stories[0].State != "completed" || forge.children[200].State != "closed" {
		t.Fatal("conflict reopened acceptance")
	}
}

type unavailableEngineeringCommentForge struct {
	*progressForge
	unavailable bool
}

func (f *unavailableEngineeringCommentForge) PostComment(ctx context.Context, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	if f.unavailable {
		return github.Comment{}, fmt.Errorf("status delivery unavailable")
	}
	return f.progressForge.PostComment(ctx, repo, number, body)
}

func TestEngineeringConflictingReplaySupersedesUndeliveredCompletion(t *testing.T) {
	f, forge, runID, root := engineeringRun(t)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	before := len(forge.comments[200])
	unavailable := &unavailableEngineeringCommentForge{progressForge: forge, unavailable: true}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	service, err := NewStoryProgress(f.db, unavailable, renderer, 9, filepath.Join(filepath.Dir(f.db.Path()), "story-progress-locks"))
	mustProgress(t, err)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	if err := service.Poll(t.Context()); err == nil {
		t.Fatal("expected failed status delivery")
	}
	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if !stories[0].EngineeringComplete {
		t.Fatal("engineering evidence was not retained before status delivery")
	}

	// A replaced immutable event must revoke the unsent claim before retrying it.
	e.Evidence.Tests[0].Evidence = "replaced immutable evidence"
	writeProgressEvent(t, root, e)
	unavailable.unavailable = false
	mustProgress(t, service.Poll(t.Context()))
	mustProgress(t, service.Poll(t.Context()))
	stories, err = f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if stories[0].EngineeringComplete || stories[0].State != "blocked" {
		t.Fatalf("conflicting evidence retained engineering progress: %+v", stories[0])
	}
	blocked := false
	for _, comment := range forge.comments[200][before:] {
		if strings.Contains(comment.Body, "Engineering and executed checks complete") {
			t.Fatal("recovery delivered superseded engineering completion")
		}
		blocked = blocked || strings.Contains(comment.Body, "unfinished or blocked")
	}
	if !blocked {
		t.Fatal("recovery omitted blocked diagnostic")
	}
	if len(forge.changes) != 0 || forge.children[200].State != "open" {
		t.Fatal("engineering conflict changed remote acceptance state")
	}
}
