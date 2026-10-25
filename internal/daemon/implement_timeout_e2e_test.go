package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The sibling stays in its runner until the timed-out story's own issue notice
// and resumed runner have both been observed. Neither a parent approval nor a
// completed batch can make this regression pass.
func TestImplementTimeoutStoryReplyResumesWhileSiblingRuns(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	realRepo := gittest.New(t)
	realRepo.Git("clone", "--quiet", realRepo.Origin, f.repo.Path)
	f.repo.RemoteURL = realRepo.Origin
	var err error
	f.repo, err = f.db.UpsertRepo(ctx, f.repo)
	check(err)
	original := f.forge.server.Config.Handler
	f.forge.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/parent") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		original.ServeHTTP(w, r)
	})
	f.plantCursor()
	f.issueWithLabels("Parent", "Mode: Implement")
	f.settle()
	parent := f.session().TaskRef
	check(f.db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var revisions []store.ApprovedStoryRevision
	var seeds []store.ImplementStorySeed
	for i, key := range []string{"story-001", "story-002"} {
		story := store.ManagedStory{ParentTask: parent, Generation: 1, Key: key, IssueNumber: 201 + i, IssueID: int64(7001 + i), State: "planning"}
		f.forge.addIssue(fakeIssue{ID: story.IssueID, Number: story.IssueNumber, Title: key, User: stranger(), Labels: toFakeLabels("Mode: Implement")})
		check(f.db.PutManagedStory(ctx, story))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: story, ParentID: 1230, Title: key, Body: key, Hash: strings.Repeat("cd", 32), FetchedAt: f.clock.peek()})
		seeds = append(seeds, store.ImplementStorySeed{Key: key, Position: i, Branch: "lf/" + key})
	}
	check(f.db.SetManagedFeatureReady(ctx, parent, 1, true))
	check(f.db.ApproveManagedStories(ctx, parent, 1, revisions))
	for _, r := range revisions {
		check(f.db.DeliverStoryScopeNotice(ctx, parent, 1, r.Key, "freeze"))
	}
	check(f.db.ActivateManagedScope(ctx, parent, 1))
	check(f.db.EnsureImplementStories(ctx, parent, 1, seeds, f.clock.peek()))
	for _, seed := range seeds {
		row, e := f.db.GetImplementStory(ctx, parent, 1, seed.Key)
		check(e)
		_, e = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: f.clock.peek()})
		check(e)
	}
	// A short runtime default is a fixture input; the captured graph deliberately
	// has no node override, so expiry exercises the real deadline path.
	files := fstest.MapFS{}
	source := os.DirFS("../../teams/go-development/v1.0.2/implement")
	check(fs.WalkDir(source, ".", func(name string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		raw, e := fs.ReadFile(source, name)
		if e != nil {
			return e
		}
		files[name] = &fstest.MapFile{Data: raw, Mode: 0644}
		return nil
	}))
	binary, err := git.Resolve(ctx)
	check(err)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { check(authority.Close(context.WithoutCancel(ctx))) })
	release := make(chan struct{})
	defer func() {
		close(release)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		check(f.daemon.Quiesce(cleanup))
	}()
	siblingStarted := make(chan struct{}, 1)
	resumed := make(chan string, 1)
	settled := make(chan error, 4)
	var firstCalls atomic.Int32
	runner := retryPlanRunner(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Ref.Issue == 202 {
			siblingStarted <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return &agent.RunResult{Outcome: agent.OutcomeAbandoned, FinishedAt: time.Now()}, nil
		}
		if req.Ref.Issue != 201 {
			return nil, errors.New("unexpected coordinator agent")
		}
		if firstCalls.Add(1) == 1 {
			if e := os.WriteFile(filepath.Join(req.WorktreePath, "kept.txt"), []byte("partial work"), 0600); e != nil {
				return nil, e
			}
			<-ctx.Done()
			return &agent.RunResult{Outcome: agent.OutcomeTimedOut, FinishedAt: time.Now()}, ctx.Err()
		}
		data, e := os.ReadFile(filepath.Join(req.WorktreePath, "kept.txt"))
		if e != nil {
			return nil, e
		}
		resumed <- string(data)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &agent.RunResult{Outcome: agent.OutcomeAbandoned, FinishedAt: time.Now()}, nil
	})
	home, err := os.MkdirTemp("/tmp", "lf-timeout-")
	check(err)
	t.Cleanup(func() { check(os.RemoveAll(home)) })
	home, err = filepath.EvalSymlinks(home)
	check(err)
	paths := config.NewPaths(home)
	opts := turn.Options{WorkerTimeoutSeconds: 5, WorkerTimeoutSource: "configuration", DB: f.db, Git: binary, Paths: f.paths, InstanceID: "timeout-e2e", Logger: f.logger, CloneGate: make(lifecycleCloneGate, 1), Runners: turn.RunnerSet{"claude": runner}, Author: git.Author{Name: "Test", Email: "test@example.invalid"}, ResearchReporter: f.reporter, SubIssueCreator: f.client, MCP: &turn.MCPRuntime{Authority: authority, Paths: paths, Executable: "/factory/litefactory", Now: time.Now, Redactor: redact.New(redact.Options{}), Comments: f.client, BotID: botID}}
	opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "fixture-model"}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		definition, e := teamsource.CaptureWorkflow(ctx, files, "implement.dot", "go-development", agent.ModeImplement, specs)
		if e != nil {
			return nil, e
		}
		raw := []byte("implement: implement/implement.dot\n")
		digest := sha256.Sum256(raw)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "implement/implement.dot", GraphFile: "implement.dot", Team: definition.Profile.Team, TeamConfig: raw, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		local := opts
		session, e := f.db.GetSession(ctx, req.SessionID)
		if e != nil {
			return turn.Result{}, e
		}
		if session.IssueNumber == 202 || firstCalls.Load() > 0 {
			local.WorkerTimeoutSeconds = 120
		}
		result, e := turn.Execute(ctx, local, req)
		if result.Disposition == turn.DispositionPrepareFailed {
			e = fmt.Errorf("prepare failed: %s", result.Reason)
		}
		if session.IssueNumber == 201 {
			settled <- e
		}
		return result, e
	})
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(20 * time.Second):
			posts, _, _ := f.forge.snapshot()
			t.Fatalf("worker did not start posts=%v\n%s", posts, f.logs)
		}
	}
	f.tick()
	await(siblingStarted)
	select {
	case err = <-settled:
		check(err)
	case <-time.After(20 * time.Second):
		t.Fatalf("timeout did not settle\n%s", f.logs)
	}
	row, err := f.db.GetImplementStory(ctx, parent, 1, "story-001")
	check(err)
	if row.State != store.ImplementParked || row.TimeoutCommentID == "" {
		t.Fatalf("missing independent timeout notice: %+v\n%s", row, f.logs)
	}
	posts, _, _ := f.forge.snapshot()
	notices := 0
	for _, post := range posts {
		if strings.Contains(post, "Reply on this story issue") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("timeout notices=%d posts=%v", notices, posts)
	}
	// Owner reply goes to the stopped child's issue, through normal authenticated
	// comment admission and the scheduler. The sibling has not been released.
	noticeID, e := strconv.ParseInt(row.TimeoutCommentID, 10, 64)
	check(e)
	// This fake records POST bodies separately from its GET comment model.
	// Mirror the confirmed provider comment before reading its publication time.
	f.forge.addCommentAt(201, noticeID, bot(), "Story parked", time.Now().Add(-time.Second))
	// Its own confirmed notice is already read before the owner replies.
	_, err = f.reporter.Consume(ctx, row.WorkerSession, noticeID)
	check(err)
	f.forge.addCommentAt(201, 900001, owner(), "Continue the kept work.", time.Now().Add(time.Second))
	f.tick()
	f.tick()
	select {
	case data := <-resumed:
		if data != "partial work" {
			t.Fatalf("lost work: %q", data)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("reply did not resume before sibling finished\n%s", f.logs)
	}
	sibling, err := f.db.GetImplementStory(ctx, parent, 1, "story-002")
	check(err)
	if sibling.State != store.ImplementRunning {
		t.Fatalf("sibling already finished: %+v", sibling)
	}
	// Re-delivery cannot launch another resume while the resumed runner is live.
	f.tick()
	if firstCalls.Load() != 2 {
		t.Fatalf("reply launched %d worker calls", firstCalls.Load())
	}
}
