package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type shellRecoveryTickets struct {
	PlanTickets
	correlatedTicketCreation
	parent, child  ticket.Snapshot
	creates, lists int
	readErr        error
	wrongParent    bool
	mutate         func(*ticket.Snapshot)
}

func (f *shellRecoveryTickets) Provider() ticket.Provider { return ticket.Linear }
func (f *shellRecoveryTickets) Origin() ticket.Origin     { return f.parent.Ref.Identity.Origin }
func (f *shellRecoveryTickets) Resolve(_ context.Context, l ticket.Locator) (ticket.Snapshot, error) {
	if l == f.parent.Ref.Locator {
		return f.parent, nil
	}
	if f.readErr != nil {
		return ticket.Snapshot{}, f.readErr
	}
	return f.child, nil
}
func (f *shellRecoveryTickets) Read(ctx context.Context, r ticket.Ref) (ticket.Snapshot, error) {
	return f.Resolve(ctx, r.Locator)
}
func (f *shellRecoveryTickets) Parent(context.Context, ticket.Ref) (ticket.Snapshot, error) {
	p := f.parent
	if f.wrongParent {
		p.Ref.Identity.RemoteID = "other-parent"
	}
	return p, nil
}
func (f *shellRecoveryTickets) Followups(context.Context, string) ([]ticket.Snapshot, error) {
	f.lists++
	return nil, nil
}
func (f *shellRecoveryTickets) CreateChildWithID(_ context.Context, container, id string, _ *ticket.Ref, title, body redact.Body) (ticket.Snapshot, error) {
	f.creates++
	f.child = f.parent
	f.child.Ref.Identity.RemoteID = id
	f.child.Ref.Locator.Key = id
	f.child.Ref.Locator.Container = container
	f.child.Title = title.String()
	f.child.Body = strings.TrimRight(body.String(), "\n")
	f.child.ProviderBody = linear.EncodeContent(f.child.Body)
	if f.mutate != nil {
		f.mutate(&f.child)
	}
	return f.child, nil
}

type shellRecoveryForge struct{ *TicketWorkflowBridge }

func (f shellRecoveryForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	return f.TicketPlanBridge.GetIssue(ctx, repo, number)
}

func TestLinearShellRetainedRecovery(t *testing.T) {
	for _, failure := range []string{"confirmed", "lost", "missing", "read-failed", "body", "title", "uuid", "publisher", "representation", "destination", "parent"} {
		t.Run(failure, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "shell.db")
			db, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(db.Migrate(ctx))
			bot := ticket.Identity{Provider: ticket.Linear, Origin: "https://api.linear.app", RemoteID: "publisher"}
			parent := ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear, Origin: bot.Origin, RemoteID: "parent"}, Locator: ticket.Locator{Provider: ticket.Linear, Origin: bot.Origin, Container: "team/project", Key: "parent"}}, Title: "Parent", State: "open", Representation: "linear-markdown/v1", Author: ticket.Actor{Identity: bot}}
			remote := &shellRecoveryTickets{parent: parent}
			bridge := &TicketWorkflowBridge{TicketPlanBridge: &TicketPlanBridge{DB: db, Tickets: remote, Bot: bot}}
			_, err = bridge.bind(ctx, parent)
			check(err)
			p, err := compatibilityIssue(ctx, db, parent)
			check(err)
			a := PlanStoryAuthority{Parent: "linear:team/project#" + strconv.Itoa(p.Number), Repo: github.Repo{Owner: "team", Name: "project"}, ParentNumber: p.Number, ParentID: p.ID}
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "linear", Owner: "team", Name: "project", RemoteURL: "https://example.test/team/project", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			check(err)
			_, err = db.GetOrCreateSession(ctx, repo, p.Number)
			check(err)
			state := planbrief.PlanState{SharedRevision: "scope"}
			entry := planbrief.PlanStorySetEntry{ID: "item", Name: "A story", Intent: "Keep parent context"}
			rev, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{entry})
			check(err)
			_, err = state.PromoteStorySet(rev)
			check(err)
			key := state.StorySet.Keys[entry.ID]
			draft, err := db.ReservePlanStory(ctx, a.Parent, key)
			check(err)
			_, err = db.SavePlanWorkflowState(ctx, a.Parent, 0, state)
			check(err)
			title, body := planStoryShell(a, p.Number, draft.Ordinal, key, entry, "Parent")
			effect := "plan-shell:" + a.Parent + ":" + key
			redactor := redact.New(redact.Options{Environ: []string{}})
			st, err := redactor.Apply(ctx, title, 256)
			check(err)
			sb, err := redactor.Apply(ctx, body, 4096)
			check(err)
			if failure != "confirmed" {
				remote.mutate = func(s *ticket.Snapshot) { s.Body += " changed" }
			}
			created, err := bridge.CreateIssueEffect(ctx, effect, a.Repo, p.Number, st, sb)
			if failure == "confirmed" {
				check(err)
			} else if err == nil {
				t.Fatal("changed creation accepted")
			}
			remote.child.Body = strings.TrimRight(body, "\n")
			prepared, err := db.GetTicketEffectPreparation(ctx, effect)
			check(err)
			if prepared.AuthoredContent != title+"\n"+body || (failure == "confirmed" && prepared.ObservedContent != title+"\n"+remote.child.ProviderBody) || (failure != "confirmed" && prepared.Observed) {
				t.Fatalf("lost frozen/raw bytes: %+v", prepared)
			}
			// Reopen the store with an unbound draft, as after a crash before shell binding.
			check(db.Close())
			db, err = store.Open(ctx, path)
			check(err)
			bridge.DB = db
			switch failure {
			case "missing":
				remote.readErr = &ticket.Failure{Kind: ticket.Refused, Operation: "resolve"}
			case "read-failed":
				remote.readErr = errors.New("read unavailable")
			case "body":
				remote.child.Body += " different meaning"
			case "title":
				remote.child.Title += " changed"
			case "uuid":
				remote.child.Ref.Identity.RemoteID = "other"
			case "publisher":
				remote.child.Author.Identity.RemoteID = "other"
			case "representation":
				remote.child.Representation = "other"
			case "destination":
				remote.child.Ref.Locator.Container = "other/project"
			case "parent":
				remote.wrongParent = true
			}
			activity := PlanStoryActivity{DB: db, Forge: shellRecoveryForge{bridge}, Redactor: redactor, LockDir: t.TempDir()}
			err = activity.ReconcileShells(ctx, a)
			if (err != nil) != (failure != "confirmed" && failure != "lost") {
				t.Fatalf("reconcile: %v", err)
			}
			draft, readErr := db.GetPlanStoryDraft(ctx, a.Parent, key)
			check(readErr)
			if failure == "confirmed" || failure == "lost" {
				if draft.IssueID == 0 || (failure == "confirmed" && draft.IssueID != created.ID) {
					t.Fatal("shell not bound")
				}
			} else if draft.IssueID != 0 {
				t.Fatal("mismatch bound")
			}
			retained, err := db.GetTicketEffectPreparation(ctx, effect)
			check(err)
			if retained.Observed != (failure == "confirmed" || failure == "lost") {
				t.Fatal("unexpected creation confirmation")
			}
			if remote.creates != 1 || remote.lists != 0 {
				t.Fatalf("creates=%d lists=%d", remote.creates, remote.lists)
			}
		})
	}
}
