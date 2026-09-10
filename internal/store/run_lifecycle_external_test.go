package store_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type lifecycleRunner struct{ calls int }

func (r *lifecycleRunner) Run(_ context.Context, req agent.Request) (*agent.RunResult, error) {
	r.calls++
	return &agent.RunResult{Outcome: agent.OutcomeSucceeded, HasResult: true, ResultText: "finished", ResumeToken: "retained-token", StartedAt: time.Now(), FinishedAt: time.Now()}, nil
}

type lifecycleGate struct{ mu sync.Mutex }

func (g *lifecycleGate) Acquire(ctx context.Context, _ string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	return g.mu.Unlock, nil
}

// Fault-control SQL stays inside store. The application uses its ordinary
// migrated DB, turn preparation and runner boundary; no application SQL is mocked.
func TestRunLifecycleStorageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
		launched      bool
	}{
		{"session-running", `CREATE TRIGGER lifecycle_fault BEFORE UPDATE OF state ON sessions WHEN NEW.state='running' BEGIN SELECT RAISE(ABORT,'injected session write failure'); END`, false},
		{"terminal-result", `CREATE TRIGGER lifecycle_fault BEFORE UPDATE OF status ON runs WHEN NEW.status='succeeded' BEGIN SELECT RAISE(ABORT,'injected terminal write failure'); END`, true},
		{"resume-token", `CREATE TRIGGER lifecycle_fault BEFORE UPDATE OF resume_token ON segments BEGIN SELECT RAISE(ABORT,'injected token write failure'); END`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := gittest.New(t)
			root := filepath.Dir(repo.Dir)
			home := filepath.Join(root, "state")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			paths := config.NewPaths(home)
			db, err := store.Open(t.Context(), paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			registered, err := db.UpsertRepo(t.Context(), store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: repo.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(t.Context(), registered, 123)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := git.Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			faults, err := sql.Open("sqlite", paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			faults.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := faults.Close(); err != nil {
					t.Error(err)
				}
			})
			if _, err := faults.ExecContext(t.Context(), tc.trigger); err != nil {
				t.Fatal(err)
			}
			runner := &lifecycleRunner{}
			opts := turn.Options{DB: db, Git: binary, Runners: turn.RunnerSet{agent.Kind(store.AgentClaude): runner}, Paths: paths, InstanceID: "owner", Author: git.Author{Name: "Test", Email: "test@example.invalid"}, CloneGate: &lifecycleGate{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			req := turn.Request{SessionID: session.ID, Agent: agent.Kind(store.AgentClaude), Mode: agent.ModeSpike, Issue: turn.Issue{Number: 123, Title: "Fault recovery"}, Trigger: turn.Trigger{CommentID: 1, Body: "/spike work"}, Command: turn.ParseCommand("/spike work")}
			result, err := turn.Execute(t.Context(), opts, req)
			if err == nil {
				t.Fatal("injected boundary did not fail")
			}
			runs, err := db.ListRuns(t.Context(), session.ID)
			if err != nil || len(runs) != 1 {
				t.Fatalf("allocation=%+v %v", runs, err)
			}
			if runs[0].Reportable() {
				t.Fatalf("failed persistence reportable=%+v", runs[0])
			}
			if tc.launched {
				if runner.calls != 1 || result.Repair == nil {
					t.Fatalf("stopped debt missing calls=%d result=%+v", runner.calls, result)
				}
				result, err = result.Repair(t.Context())
				if err == nil || result.Repair == nil || runner.calls != 1 {
					t.Fatal("failed repair lost evidence or relaunched")
				}
				locked, err := db.GetSession(t.Context(), session.ID)
				if err != nil || locked.LockOwner != "owner" {
					t.Fatalf("ownership=%+v %v", locked, err)
				}
			} else {
				current, err := db.GetSession(t.Context(), session.ID)
				if err != nil || current.State != session.State || runner.calls != 0 || runs[0].Status != store.RunPrelaunch {
					t.Fatalf("failed preparation changed lifecycle: %+v %+v %v", current, runs, err)
				}
			}
			wm, _, err := db.GetWatermark(t.Context(), session.ID)
			if err != nil || wm.LastCommentID != 0 {
				t.Fatalf("premature acknowledgment=%+v %v", wm, err)
			}
			if _, err := faults.ExecContext(t.Context(), `DROP TRIGGER lifecycle_fault`); err != nil {
				t.Fatal(err)
			}
			if tc.launched {
				result, err = result.Repair(t.Context())
			} else {
				result, err = turn.Execute(t.Context(), opts, req)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Repair != nil || !result.Run.Reportable() || result.Run.ID != runs[0].ID || result.Run.Ordinal != runs[0].Ordinal || runner.calls != 1 {
				t.Fatalf("repair=%+v calls=%d", result, runner.calls)
			}
			segment, err := db.GetSegment(t.Context(), result.Run.SegmentID)
			if err != nil || segment.ResumeToken != "retained-token" {
				t.Fatalf("recovered token=%+v %v", segment, err)
			}
			current, err := db.GetSession(t.Context(), session.ID)
			if err != nil || current.LockOwner != "" {
				t.Fatalf("released=%+v %v", current, err)
			}
		})
	}
}
