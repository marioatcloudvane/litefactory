package daemon

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestLinearPlanShellDiscoveryAndManagedReview(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "fresh"
		if existing {
			name = "retained-discovery"
		}
		t.Run(name, func(t *testing.T) {
			f, remote, v, sub, q := linearObservationFixture(t)
			f.d.connections.SetRequestContext(f.d.bindQuotaRequest)
			f.d.quotas.tick(f.now)
			ctx := f.d.discoveryContext(t.Context())
			db := f.d.db
			handoffLinearObservation(t, f, v, sub)
			initial := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, initial))
			for _, c := range initial {
				if c.discoveryReservation != nil {
					c.discoveryReservation.finish()
				}
			}
			for _, c := range initial {
				for _, o := range c.observations {
					linearWorkflowCheck(t, db.ClearPendingTicketObservation(ctx, o.ConnectionVersionID, o.TicketID, o.Generation))
				}
			}

			parent, err := db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
			linearWorkflowCheck(t, err)
			parentSnapshot, err := db.GetExecutionSnapshot(ctx, parent.ID)
			linearWorkflowCheck(t, err)
			_, err = db.AdmitPlanWorkflowPath(ctx, parent.ID, "graph")
			linearWorkflowCheck(t, err)
			state := planbrief.PlanState{SharedRevision: "scope"}
			revision, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "item", Name: "A Linear story", Intent: "Keep its parent context"}})
			linearWorkflowCheck(t, err)
			_, err = state.PromoteStorySet(revision)
			linearWorkflowCheck(t, err)
			key := state.StorySet.Keys["item"]
			_, err = db.ReservePlanStory(ctx, parent.TaskRef, key)
			linearWorkflowCheck(t, err)
			_, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state)
			linearWorkflowCheck(t, err)
			clients, err := f.d.connections.Clients(ctx, v, parentSnapshot.ActorID)
			linearWorkflowCheck(t, err)
			bridge, err := turn.NewTicketWorkflow(ctx, db, clients.Linear, parentSnapshot.ActorID)
			linearWorkflowCheck(t, err)
			redactor := redact.New(redact.Options{Environ: []string{}})
			title, err := redactor.Apply(ctx, "A Linear story", 256)
			linearWorkflowCheck(t, err)
			body, err := redactor.Apply(ctx, "<!-- lf:plan-story parent="+strconv.Itoa(parent.IssueNumber)+" key="+key+" -->\nFirst draft in progress", 4096)
			linearWorkflowCheck(t, err)
			child, err := bridge.CreateIssueEffect(ctx, "plan-shell:"+parent.TaskRef+":"+key, q.forgeRepo, parent.IssueNumber, title, body)
			linearWorkflowCheck(t, err)
			ref := child.QualifiedSnapshot.Ref
			v, err = db.DisableConnection(ctx, v.RepoID, store.TicketConnection, v.Revision)
			linearWorkflowCheck(t, err)
			v.Enabled = true
			v, err = db.SetConnection(ctx, v, v.Revision)
			linearWorkflowCheck(t, err)
			_, err = db.InitTicketRecoveryPosition(ctx, v, f.now)
			linearWorkflowCheck(t, err)
			sub.ConnectionVersionID = v.ID
			sub.ID = "linear-reenabled"
			sub, err = db.PutTicketSubscription(ctx, sub, 0)
			linearWorkflowCheck(t, err)
			var saved store.Session
			var savedSnapshot store.ExecutionSnapshot
			var savedBinding store.TicketBinding
			if existing {
				// Reproduce the historical standalone admission before recognition existed.
				_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: q.repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "discovery"})
				linearWorkflowCheck(t, err)
				saved, err = AdmitProjectTicket(ctx, db, f.d.connections, q.repo, ref)
				linearWorkflowCheck(t, err)
				savedSnapshot, err = db.GetExecutionSnapshot(ctx, saved.ID)
				linearWorkflowCheck(t, err)
				savedBinding, err = db.GetTicketBindingByID(ctx, savedSnapshot.BindingID)
				linearWorkflowCheck(t, err)
			}
			observation := store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: ref.Identity.RemoteID, TicketKey: ref.Locator.Key, ObservedAt: f.now}
			// A failed remote verification must leave the observation owed.
			remote.mu.Lock()
			retainedChild := remote.issues[ref.Identity.RemoteID]
			delete(remote.issues, ref.Identity.RemoteID)
			remote.mu.Unlock()
			linearWorkflowCheck(t, db.HandoffTicketObservation(ctx, observation))
			failedSeen := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, failedSeen))
			if !strings.Contains(f.logs.String(), "child ticket observation remains pending") {
				t.Fatal("verification failure was not reached")
			}
			if len(failedSeen) != 0 {
				t.Fatalf("failed verification dispatched: %+v", failedSeen)
			}
			pending, err := db.ListPendingTicketObservations(ctx, v.ID)
			linearWorkflowCheck(t, err)
			if len(pending) != 1 || pending[0].TicketID != ref.Identity.RemoteID {
				t.Fatal("failed verification lost retry intent")
			}
			remote.mu.Lock()
			remote.issues[ref.Identity.RemoteID] = retainedChild
			remote.mu.Unlock()
			for _, mapped := range []bool{false, true} {
				if mapped {
					shells := turn.PlanStoryActivity{DB: db, Forge: bridge, Redactor: redactor, LockDir: t.TempDir()}
					linearWorkflowCheck(t, shells.ReconcileShells(ctx, turn.PlanStoryAuthority{Parent: parent.TaskRef, Repo: q.forgeRepo, ParentNumber: parent.IssueNumber, ParentID: int64(parent.IssueNumber)}))
					binding, e := db.GetPlanStoryDraft(ctx, parent.TaskRef, key)
					linearWorkflowCheck(t, e)
					if binding.IssueID != child.ID || binding.IssueNumber != child.Number || binding.Revision != turn.PlanIssueRevision(child.Title, child.Body) {
						t.Fatalf("recovered binding: %+v", binding)
					}
				}
				for _, webhook := range []bool{false, true} {
					if webhook {
						err = db.HandoffSubscriptionObservation(ctx, sub.ID, sub.Revision, observation)
					} else {
						err = db.HandoffTicketObservation(ctx, observation)
					}
					linearWorkflowCheck(t, err)
					seen := map[int64]*candidate{}
					linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
					if len(seen) != 1 || seen[parent.ID] == nil {
						t.Fatalf("mapped=%v webhook=%v did not wake only parent: %+v logs=%s", mapped, webhook, seen, f.logs.String())
					}
					for _, c := range seen {
						if c.discoveryReservation != nil {
							c.discoveryReservation.finish()
						}
					}
					if !existing {
						if _, err = db.GetSessionByTicket(ctx, ref.Identity); !errors.Is(err, store.ErrNotFound) {
							t.Fatalf("standalone admission: %v", err)
						}
					}
				}
				c, e := f.d.sight(ctx, q, child)
				linearWorkflowCheck(t, e)
				if c == nil || c.session.ID != parent.ID {
					t.Fatalf("final sight admitted child: %+v", c)
				}
			}
			managed, err := f.d.managedChildSession(ctx, q.repo, parent.TaskRef, child.Number, child.ID)
			linearWorkflowCheck(t, err)
			snap, err := db.GetExecutionSnapshot(ctx, managed.ID)
			linearWorkflowCheck(t, err)
			if existing {
				if managed.ID != saved.ID || snap != savedSnapshot {
					t.Fatalf("rewrote retained child: %+v", snap)
				}
				b, e := db.GetTicketBindingByID(ctx, snap.BindingID)
				linearWorkflowCheck(t, e)
				if b != savedBinding {
					t.Fatal("rewrote discovery binding")
				}
			} else if snap.TicketVersionID != parentSnapshot.TicketVersionID || snap.ActorID != parentSnapshot.ActorID || snap.CodeVersionID != parentSnapshot.CodeVersionID {
				t.Fatalf("fresh child did not inherit parent: %+v", snap)
			}
			again, err := f.d.managedChildSession(ctx, q.repo, parent.TaskRef, child.Number, child.ID)
			linearWorkflowCheck(t, err)
			if again.ID != managed.ID {
				t.Fatal("duplicate child")
			}
			childRef, err := naming.ParseRef(managed.TaskRef)
			linearWorkflowCheck(t, err)
			if _, due, err := f.d.planWorkFor(ctx, &task{repo: q.repo, session: managed, ref: childRef, issue: child}); err != nil || !due {
				t.Fatalf("story not runnable: %v %v", due, err)
			}
			// Exercise the stage result and real publication boundary on the child.
			draft := planbrief.PlanDraft{Key: key, Name: "A Linear story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "draft a Linear story", Benefit: "retain the Plan context"}, Problem: "Story discovery loses parent context", InScope: []string{"Managed Plan story"}, NotInScope: []string{"Standalone dispatch"}}
			activity := turn.PlanStoryActivity{DB: db, Forge: bridge, Bot: clients.Publisher.Identity, Redactor: redactor, LockDir: t.TempDir()}
			receipt, err := activity.Apply(ctx, turn.PlanStoryAuthority{Parent: parent.TaskRef, Repo: q.forgeRepo, ParentNumber: parent.IssueNumber, ParentID: int64(parent.IssueNumber), SelectedKey: key, Candidate: "candidate-1", Stage: "story", ContentScope: planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{"agent-setup"})}, graphexec.PlanStoryRequest{RequestID: "draft-1", Operation: "create", Stage: "story", Candidate: "candidate-1", SharedRevision: "scope", Draft: draft})
			linearWorkflowCheck(t, err)
			if receipt.Outcome != "applied" || receipt.IssueID != child.ID {
				t.Fatalf("stage failed to publish the child draft: %+v", receipt)
			}
			state, stateRevision, err := db.GetPlanWorkflowState(ctx, parent.TaskRef)
			linearWorkflowCheck(t, err)
			linearWorkflowCheck(t, state.OpenPlanWait(key, "review-1", "review", child.ID))
			_, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, stateRevision, state)
			linearWorkflowCheck(t, err)
			renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: f.d.log})
			linearWorkflowCheck(t, err)
			reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: f.d.log, BotUserID: parentSnapshot.ActorID})
			linearWorkflowCheck(t, err)
			_, err = reporter.ReportRun(ctx, turn.RunReport{Run: report.Run{Ref: childRef, Ordinal: 1, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Review the Linear story draft."}}, SessionID: managed.ID})
			linearWorkflowCheck(t, err)
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.creates != 1 {
				t.Fatalf("created %d children", remote.creates)
			}
			published := false
			for _, comment := range remote.comments {
				if strings.Contains(linearPlanCommentBody(t, comment), "Review the Linear story draft") {
					published = true
					if linearWorkflowObject(t, comment["issue"])["id"] != ref.Identity.RemoteID {
						t.Fatal("review published on parent")
					}
				}
			}
			if !published {
				t.Fatal("no child review")
			}
		})
	}
}

func TestLinearUnprovenPlanMarkersRemainOrdinaryTickets(t *testing.T) {
	for _, kind := range []string{"root", "arbitrary-child", "copied-marker"} {
		t.Run(kind, func(t *testing.T) {
			f, remote, v, _, q := linearObservationFixture(t)
			ctx := t.Context()
			body := "Ordinary work"
			if kind == "copied-marker" {
				body = "<!-- lf:plan-story parent=parent-uuid story=story-001 -->\n/plan ordinary work"
			}
			remote.mu.Lock()
			item := remote.issue("unrelated", "LF-3", "Unrelated", body, "human-uuid")
			if kind != "root" {
				item["parent"] = remote.issues["parent-uuid"]
			}
			remote.issues["unrelated"] = item
			remote.mu.Unlock()
			clients, err := f.d.connections.Clients(ctx, v, 0)
			linearWorkflowCheck(t, err)
			snapshot, err := clients.Linear.Resolve(ctx, ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: "unrelated"})
			linearWorkflowCheck(t, err)
			slot, err := f.d.db.LocalTicketSlot(ctx, "issue", snapshot.Ref.Identity)
			linearWorkflowCheck(t, err)
			c, err := f.d.sight(ctx, q, github.Issue{ID: slot, Number: int(slot), State: github.StateOpen, Title: snapshot.Title, Body: snapshot.Body, QualifiedSnapshot: &snapshot})
			linearWorkflowCheck(t, err)
			if c == nil || c.session.IssueNumber != int(slot) {
				t.Fatalf("ordinary ticket suppressed: %+v", c)
			}
		})
	}
}

func linearPlanCommentBody(t *testing.T, comment map[string]any) string {
	t.Helper()
	body, ok := comment["body"].(string)
	if !ok {
		t.Fatal("provider returned a non-text comment")
	}
	return body
}

func TestLinearPlanChildConflictSkipsOnlyThatStory(t *testing.T) {
	f, remote, v, sub, q := linearObservationFixture(t)
	ctx := t.Context()
	db := f.d.db
	handoffLinearObservation(t, f, v, sub)
	seen := map[int64]*candidate{}
	linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
	parent, err := db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
	linearWorkflowCheck(t, err)
	snap, err := db.GetExecutionSnapshot(ctx, parent.ID)
	linearWorkflowCheck(t, err)
	_, err = db.AdmitPlanWorkflowPath(ctx, parent.ID, "graph")
	linearWorkflowCheck(t, err)
	state := planbrief.PlanState{SharedRevision: "scope"}
	revision, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "bad", Name: "Conflicting story", Intent: "Retain refusal"}, {ID: "good", Name: "Runnable story", Intent: "Keep working"}})
	linearWorkflowCheck(t, err)
	_, err = state.PromoteStorySet(revision)
	linearWorkflowCheck(t, err)
	_, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state)
	linearWorkflowCheck(t, err)
	code, err := db.CurrentConnection(ctx, q.repo.ID, store.CodeConnection)
	linearWorkflowCheck(t, err)
	var goodNumber int
	for _, id := range []string{"bad", "good"} {
		key := state.StorySet.Keys[id]
		_, err = db.ReservePlanStory(ctx, parent.TaskRef, key)
		linearWorkflowCheck(t, err)
		remote.mu.Lock()
		remote.issues[id] = remote.issue(id, "LF-"+id, id, "Draft", "publisher-uuid")
		remote.mu.Unlock()
		ref := ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: id}, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: id}}
		slot, e := db.LocalTicketSlot(ctx, "issue", ref.Identity)
		linearWorkflowCheck(t, e)
		linearWorkflowCheck(t, db.BindPlanStoryShell(ctx, parent.TaskRef, key, slot, int(slot), "shell"))
		if id == "good" {
			goodNumber = int(slot)
			continue
		}
		_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: q.repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "discovery"})
		linearWorkflowCheck(t, err)
		// A different retained publishing actor remains an ownership conflict.
		_, err = db.AdmitSessionWithPublishers(ctx, q.repo, ref, code.Revision, v.Revision, store.PublishingActor{ID: snap.ActorID + 1, Login: "other"}, store.PublishingActor{ID: snap.CodeActorID, Login: snap.CodeActorLogin}, "linear:team-uuid/project-uuid#"+strconv.FormatInt(slot, 10), 0, snap.CodeRemoteID, snap.TicketRemoteID)
		linearWorkflowCheck(t, err)
	}
	offered := map[int64]*candidate{}
	linearWorkflowCheck(t, f.d.graphPlanStories(ctx, map[int64]*repoQueue{q.repo.ID: q}, offered))
	if len(offered) != 1 {
		t.Fatalf("conflict stopped sibling: %+v", offered)
	}
	for _, c := range offered {
		if c.session.IssueNumber != goodNumber {
			t.Fatal("conflicting child offered")
		}
	}
	if !strings.Contains(f.logs.String(), "could not find the task of a story; skipping it this tick") {
		t.Fatal("missing per-story conflict diagnostic")
	}
}
