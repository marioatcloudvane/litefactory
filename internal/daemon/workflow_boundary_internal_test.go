package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type boundaryAdmissionForge struct {
	Forge
	issue github.Issue
	err   error
	reads int
}

func (f *boundaryAdmissionForge) GetIssue(ctx context.Context, _ github.Repo, _ int) (github.Issue, error) {
	f.reads++
	if ctx.Err() != nil {
		return github.Issue{}, ctx.Err()
	}
	return f.issue, f.err
}

func TestWorkflowBoundaryUnreadableLabelsAndCancellation(t *testing.T) {
	for _, kind := range []string{"labels", "cancelled", "closed-store", "malformed-mode", "ambiguous-mode"} {
		t.Run(kind, func(t *testing.T) {
			db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(t.Context(), repo, 389)
			if err != nil {
				t.Fatal(err)
			}
			forge := &boundaryAdmissionForge{issue: github.Issue{Labels: []string{"bug"}}}
			ctx := t.Context()
			switch kind {
			case "labels":
				forge.err = errors.New("labels unavailable")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed-store":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			case "malformed-mode":
				forge.issue.Labels = append(forge.issue.Labels, "Mode: Bug")
			case "ambiguous-mode":
				forge.issue.Labels = append(forge.issue.Labels, "Mode: Plan", "Mode: Spike")
			}
			d := &Daemon{db: db, forge: forge}
			workflow, mode, err := d.admitWorkflow(ctx, &task{session: session, issue: github.Issue{Number: 389}}, turn.Trigger{CommentID: 501}, turn.Command{})
			if err == nil || workflow != "" || mode != "" {
				t.Fatalf("unsafe admission %q %q %v", workflow, mode, err)
			}
			if kind == "labels" && !strings.Contains(err.Error(), "labels unavailable") {
				t.Fatalf("cause lost: %v", err)
			}
			if kind != "closed-store" {
				if _, err := db.GetTaskWorkflowAdmission(t.Context(), session.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed observation retained route: %v", err)
				}
			}
		})
	}
}

func TestWorkflowBoundaryBugCannotBorrowPermissions(t *testing.T) {
	profiles := config.DefaultToolProfiles().Permissions
	profiles["bug"] = profiles["implement"]
	d := &Daemon{toolProfiles: profiles}
	for _, kind := range []agent.Kind{agent.Kind(config.KindClaude), agent.Kind(config.KindCodex)} {
		for _, mode := range []agent.Mode{"", agent.ModeResearch, agent.ModeImplement} {
			got, ok := d.resolveWorkflowTools(agent.WorkflowBug, mode, kind)
			if ok || got.Profile != "" || len(got.Tools) != 0 || len(got.AllowedTools) != 0 || got.Sandbox != "" || got.Attachments {
				t.Fatalf("Bug borrowed %s/%s permissions: %+v %v", mode, kind, got, ok)
			}
		}
		if _, ok := d.resolveWorkflowTools(agent.WorkflowResearch, agent.ModeResearch, kind); !ok {
			t.Fatalf("compatible research permissions refused for %s", kind)
		}
	}
}

func TestWorkflowBoundaryActiveGraphAndResumedBinding(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 389)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	definition, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Workflow: "bug", ProfileVersion: 1, Path: "core/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "analysis", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db} // No forge or classifier: retained admission must not use either.
	task := &task{session: session}
	if _, _, err := d.admitWorkflow(t.Context(), task, turn.Trigger{CommentID: 501}, turn.ParseCommand("/plan replace")); err == nil {
		t.Fatal("mode switch accepted")
	}
	for _, binding := range []store.ContinuationContext{{Workflow: "plan", Mode: "plan"}, {Workflow: "bug", Mode: "research"}, {Mode: "research"}} {
		task.continuation = &store.Continuation{Context: binding}
		workflow, mode, err := d.admitWorkflow(t.Context(), task, turn.Trigger{CommentID: 502}, turn.Command{})
		if err == nil || workflow != "" || mode != "" || !strings.Contains(err.Error(), "binding mismatch") {
			t.Fatalf("bad resumed binding accepted: %+v %q %q %v", binding, workflow, mode, err)
		}
	}
	retained, err := db.GetGraphInstance(t.Context(), graph.ID)
	if err != nil || retained.Status != store.GraphRunning || retained.CurrentNode != graph.CurrentNode || retained.DefinitionID != graph.DefinitionID {
		t.Fatalf("refusal changed active graph: %+v %v", retained, err)
	}
	got, err := db.GetTaskWorkflowAdmission(t.Context(), session.ID)
	if err != nil || got != admitted {
		t.Fatalf("refusal changed admission: %+v %v", got, err)
	}
}
