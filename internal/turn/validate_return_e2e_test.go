package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// AC-004 exercises the owner-facing graph and the normal no-agent coordinator
// path. Passing shared work goes to one worker; its original branch and the
// complete review remain bound to the return through completion.
func TestOwnerReturnPassingSharedWorkMainPath(t *testing.T) {
	f := newImplementBackFixture(t)
	f.stories = f.stories[:2]
	f.target.Stories = f.target.Stories[:2]
	f.reviewers.run = nil
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var approved []store.ApprovedStoryRevision
	for i, story := range f.stories {
		managed := store.ManagedStory{ParentTask: f.parent, Generation: 1, Key: story.Key, IssueNumber: f.target.Stories[i].Number, IssueID: f.target.Stories[i].ID, State: "planning"}
		mustProgress(t, f.db.PutManagedStory(t.Context(), managed))
		approved = append(approved, store.ApprovedStoryRevision{ManagedStory: managed, ParentID: 4970, Title: story.Title, Body: "The shared date formatter must remain consistent.", Hash: story.Revision, FetchedAt: f.clock})
	}
	mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.parent, 1, true))
	mustProgress(t, f.db.ApproveManagedStories(t.Context(), f.parent, 1, approved))
	for _, s := range f.stories {
		mustProgress(t, f.db.DeliverStoryScopeNotice(t.Context(), f.parent, 1, s.Key, "freeze"))
	}
	mustProgress(t, f.db.ActivateManagedScope(t.Context(), f.parent, 1))
	f.readyToPush()
	reviewed := f.state()
	const explanation = "Change the shared date format to ISO dates."
	nodes := f.say(f.command(ValidateReturnCommand, "--", explanation))
	if !strings.HasSuffix(joinNodes(nodes), "owner_resolve>owner_implement>reimplementing") {
		t.Fatalf("return path %s; notice %s", joinNodes(nodes), f.notice())
	}
	saved, err := f.db.GetValidateReturn(t.Context(), f.instance.ID)
	mustProgress(t, err)
	if saved.Commit != reviewed.Commit || saved.Explanation != explanation || saved.Selection != "" || saved.Applied {
		t.Fatalf("retained return: %+v", saved)
	}
	var reviewedContext reviewedReturnContext
	mustProgress(t, json.Unmarshal([]byte(saved.Context), &reviewedContext))
	if len(reviewedContext.State.Stories) != 2 || len(reviewedContext.State.Reviews) == 0 || reviewedContext.State.Commit != reviewed.Commit {
		t.Fatalf("full reviewed context not retained: %+v", reviewedContext)
	}
	parent, err := naming.ParseRef(f.parent)
	mustProgress(t, err)
	authority := ImplementAuthority{Parent: parent, Generation: 1, IntegrationBranch: f.target.Branch, BaseBranch: "main", Stories: []ImplementStory{{Key: "story-001", Name: "One", IssueNumber: 511, Branch: "lf/story-001"}, {Key: "story-002", Name: "Two", IssueNumber: 512, Branch: "lf/story-002"}}}
	b := ImplementBindings{DB: f.db, Bind: func(context.Context, Options, ActivityRequest) (ImplementAuthority, error) { return authority, nil }, Now: func() time.Time { return f.clock }, AdvanceReturn: func(_ context.Context, integration, branch, commit string) (bool, error) {
		if integration != f.target.Branch || commit != reviewed.Commit {
			t.Fatalf("return moved away from reviewed feature: %s %s", integration, commit)
		}
		f.advanced = append(f.advanced, branch+"@"+commit)
		return true, nil
	}}
	binary, err := git.Resolve(t.Context())
	mustProgress(t, err)
	clone, err := binary.Open(t.Context(), f.repo.Dir)
	mustProgress(t, err)
	for _, branch := range []string{f.target.Branch, "lf/story-001", "lf/story-002"} {
		f.repo.Git("push", "--quiet", "origin", reviewed.Commit+":refs/heads/"+branch)
	}
	b.Repo = GitImplementRepo{Clone: clone, Remote: "origin", ScratchRoot: t.TempDir(), Author: git.Author{Name: "Factory", Email: "factory@example.com"}}
	b.Forge = &implementForgeFake{}
	b.Redactor = redact.New(redact.Options{Environ: []string{}})
	b.LockDir = t.TempDir()
	req := ActivityRequest{Mode: agent.ModeImplement}
	ready, err := (ImplementReadyActivity{b}).Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if ready.Outcome != "started" || f.row("story-001").State != store.ImplementRunning || f.row("story-002").State != store.ImplementMerged {
		t.Fatalf("exactly one shared-change worker: %s %+v", ready.Outcome, f.rows())
	}
	selected := f.row("story-001")
	if selected.Branch != "lf/story-001" || !strings.Contains(selected.Guidance, explanation) || len(f.advanced) != 1 || f.schedules() != 1 {
		t.Fatalf("branch, explanation or once-only start: %+v %v %d", selected, f.advanced, f.schedules())
	}
	saved, err = f.db.GetValidateReturn(t.Context(), f.instance.ID)
	mustProgress(t, err)
	if !saved.Applied || saved.SelectionSource != "first-plan-story" || !strings.Contains(saved.Selection, "story-001") {
		t.Fatalf("fallback not retained: %+v", saved)
	}
	// A resumed ready step reuses the saved selection and never moves another
	// branch or dispatches a second shared-change worker.
	_, err = (ImplementReadyActivity{b}).Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if len(f.advanced) != 1 || f.row("story-002").State != store.ImplementMerged {
		t.Fatal("replay repeated or spread shared work")
	}

	// The worker stub receives the real factory handoff, makes the requested
	// shared-file change once on the selected branch, and reports done. Remote
	// model execution is deliberately replaced; the return, prompt, merge and
	// next assessment use production activities.
	session, err := f.db.GetSession(t.Context(), f.instance.SessionID)
	mustProgress(t, err)
	repository, err := f.db.GetRepo(t.Context(), session.RepoID)
	mustProgress(t, err)
	workerSession, err := f.db.GetOrCreateSession(t.Context(), repository, f.target.Stories[0].Number)
	mustProgress(t, err)
	mustProgress(t, f.db.SetImplementWorkerSession(t.Context(), f.parent, 1, "story-001", workerSession.ID, f.clock))
	workerInstance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: workerSession.ID, DefinitionID: f.instance.DefinitionID, CurrentNode: f.profile.Start, StartedAt: f.clock, NextDueAt: f.clock})
	mustProgress(t, err)
	prompt, err := implementWorkerHandoff(t.Context(), f.db, workerInstance.ID, 0, nil, ImplementWorkflowPolicy(), agentresult.Context{})
	mustProgress(t, err)
	for _, want := range []string{explanation, reviewed.Commit, "reviewed_return", "story-002", "A passed suite command covers it."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("worker handoff lacks %q: %s", want, prompt)
		}
	}
	worker, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: filepath.Join(t.TempDir(), "worker"), Branch: selected.Branch, BaseBranch: "refs/remotes/origin/" + selected.Branch})
	mustProgress(t, err)
	mustProgress(t, os.WriteFile(filepath.Join(worker.Worktree.Path(), "app/date.go"), []byte("package app\n\nfunc DateFormat() string { return \"2006-01-02\" }\n"), 0600))
	changed, err := worker.Worktree.Commit(t.Context(), gitCommitOptions())
	mustProgress(t, err)
	mustProgress(t, worker.Worktree.PushHeadTo(t.Context(), "origin", selected.Branch, reviewed.Commit))
	running := f.row("story-001")
	_, err = f.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: f.parent, Generation: 1, Key: running.Key, From: store.ImplementRunning, To: store.ImplementDone, ExpectedRevision: running.Revision, At: f.clock})
	mustProgress(t, err)
	merged, err := (ImplementMergeActivity{b}).Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if merged.Outcome != "merged" || f.row("story-002").State != store.ImplementMerged {
		t.Fatalf("merge result %s %+v", merged.Outcome, f.rows())
	}
	head := f.row("story-001").MergeCommit
	if head == reviewed.Commit || changed.Head == reviewed.Commit {
		t.Fatal("returned work did not change")
	}
	mustProgress(t, f.wt.ResetHard(t.Context(), head, flowSessionDir))
	content, err := os.ReadFile(filepath.Join(f.wt.Path(), "app/date.go"))
	mustProgress(t, err)
	if !strings.Contains(string(content), "2006-01-02") {
		t.Fatalf("requested shared change absent: %s", content)
	}
	// Model the existing automatic continuation by entering the next packaged
	// Validate graph directly at the merged feature head, without an owner reply.
	f.base = head
	previousReplies := f.nextReply
	f.instance, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: f.instance.DefinitionID, CurrentNode: f.profile.Start, StartedAt: f.clock, NextDueAt: f.clock})
	mustProgress(t, err)
	f.toOwner()
	next := f.state()
	if next.Commit != head || !next.Ready || f.nextReply != previousReplies {
		t.Fatalf("next Validate did not assess changed head without another owner command: %+v", next)
	}
}

func TestReviewedReturnSelectionRejectsUnclearSplitAndKeepsNamedFallback(t *testing.T) {
	raw, err := json.Marshal(reviewedReturnContext{State: ValidateState{Stories: []ValidateJoinedStory{{Story: "story-001"}, {Story: "story-002"}}}})
	mustProgress(t, err)
	r := store.ValidateReturn{Context: string(raw), Explanation: "Please change story-002."}
	cases := []struct {
		name   string
		advice []agentresult.ReturnAllocation
		source string
		keys   []string
	}{
		{"missing", nil, "named-story", []string{"story-002"}},
		{"unknown", []agentresult.ReturnAllocation{{Story: "unknown"}}, "named-story", []string{"story-002"}},
		{"shared formatter", []agentresult.ReturnAllocation{{Story: "story-001", Change: "change formatter"}, {Story: "story-002", Change: "fix formatter"}}, "named-story", []string{"story-002"}},
		{"same file", []agentresult.ReturnAllocation{{Story: "story-001", Change: "change app/date.go"}, {Story: "story-002", Change: "test app/date.go"}}, "named-story", []string{"story-002"}},
		{"separate files", []agentresult.ReturnAllocation{{Story: "story-001", Change: "change app/date.go"}, {Story: "story-002", Change: "change ui/style.css"}}, "agent", []string{"story-001", "story-002"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			picked, source, err := returnSelection(r, c.advice)
			mustProgress(t, err)
			var keys []string
			for _, s := range picked {
				keys = append(keys, s.Story)
			}
			if source != c.source || strings.Join(keys, ",") != strings.Join(c.keys, ",") {
				t.Fatalf("%s %+v", source, picked)
			}
		})
	}
}
