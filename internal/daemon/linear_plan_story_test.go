package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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
					if mapped && existing {
						linearWorkflowCheck(t, db.ClearSessionNeedsPoll(ctx, parent.ID))
					}
					if webhook {
						err = db.HandoffSubscriptionObservation(ctx, sub.ID, sub.Revision, observation)
					} else {
						err = db.HandoffTicketObservation(ctx, observation)
					}
					linearWorkflowCheck(t, err)
					seen := map[int64]*candidate{}
					linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
					if mapped && existing {
						// A mapped story whose child has its own session answers
						// its own comments: the observation wakes the child, and
						// the parent only keeps the story owed for its sweep.
						if len(seen) != 1 || seen[saved.ID] == nil {
							t.Fatalf("webhook=%v did not wake only the child session: %+v logs=%s", webhook, seen, f.logs.String())
						}
						gotParent, e := db.GetSession(ctx, parent.ID)
						linearWorkflowCheck(t, e)
						gotChild, e := db.GetSession(ctx, saved.ID)
						linearWorkflowCheck(t, e)
						if gotParent.NeedsPoll || !gotChild.NeedsPoll {
							t.Fatalf("webhook=%v poll flags parent=%v child=%v", webhook, gotParent.NeedsPoll, gotChild.NeedsPoll)
						}
						owed, e := db.StoryDiscoveryOwed(ctx, parent.ID, child.Number)
						linearWorkflowCheck(t, e)
						if !owed {
							t.Fatalf("webhook=%v parent lost its lifecycle debt", webhook)
						}
						for _, o := range seen[saved.ID].observations {
							linearWorkflowCheck(t, db.ClearPendingTicketObservation(ctx, o.ConnectionVersionID, o.TicketID, o.Generation))
						}
						linearWorkflowCheck(t, db.ClearSessionNeedsPoll(ctx, saved.ID))
					} else if len(seen) != 1 || seen[parent.ID] == nil {
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
				if mapped && existing {
					// As for GitHub, a listed story with its own session is the
					// child's own candidate.
					if c == nil || c.session.ID != saved.ID {
						t.Fatalf("final sight did not take the child's own session: %+v", c)
					}
				} else if c == nil || c.session.ID != parent.ID {
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

// Retained parent source and approved child, with the real turn and reporter.
func linearAssemblyFixture(t *testing.T) (*linearOwnerWait, turn.Options, planbrief.PlanState) {
	t.Helper()
	w := newLinearOwnerWait(t)
	ctx, db := t.Context(), w.d.db
	snapshot, err := db.GetExecutionSnapshot(ctx, w.session.ID)
	linearWorkflowCheck(t, err)
	clients, err := w.d.connections.Clients(ctx, w.v, snapshot.ActorID)
	linearWorkflowCheck(t, err)
	bridge, err := turn.NewTicketWorkflow(ctx, db, clients.Linear, snapshot.ActorID)
	linearWorkflowCheck(t, err)
	redactor := redact.New(redact.Options{Environ: []string{}})
	renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: w.d.log})
	linearWorkflowCheck(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: w.d.log, BotUserID: snapshot.ActorID})
	linearWorkflowCheck(t, err)
	paths := config.NewPaths(t.TempDir())
	linearWorkflowCheck(t, paths.EnsureHome())
	linearWorkflowCheck(t, os.MkdirAll(paths.StoryProgressLocksDir(), 0700))
	binary, err := git.Resolve(ctx)
	linearWorkflowCheck(t, err)
	opts := turn.Options{DB: db, Git: binary, Paths: paths, InstanceID: "linear-assembly", Logger: w.d.log, ProjectApprovers: true, CloneGate: linearApprovalNoAgent{}, Runners: turn.RunnerSet{"claude": linearApprovalNoAgent{}}, ResearchReporter: reporter, SubIssueCreator: bridge, MCP: &turn.MCPRuntime{Paths: paths, Redactor: redactor, Now: time.Now}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		definition, err := teamsourcetest.CapturePlan(ctx, specs)
		if err != nil {
			return nil, err
		}
		configBytes := []byte("plan: plan/plan.dot\n")
		digest := sha256.Sum256(configBytes)
		definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "plan/plan.dot", GraphFile: "plan.dot", Team: definition.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
		return definition, nil
	}
	w.d.turn = turnExecutor{opts: opts}
	w.d.reporter = reporter
	_, err = db.AdmitPlanWorkflowPath(ctx, w.session.ID, "graph")
	linearWorkflowCheck(t, err)
	state := approvedPlanState(t, "go-implementation-engineer", true)
	_, err = db.ReservePlanStory(ctx, w.session.TaskRef, state.Stories[0].Key)
	linearWorkflowCheck(t, err)
	title, err := redactor.Apply(ctx, "Approved story", 256)
	linearWorkflowCheck(t, err)
	body, err := redactor.Apply(ctx, "Approved story content", 4096)
	linearWorkflowCheck(t, err)
	child, err := bridge.CreateIssueEffect(ctx, "assembly-child", w.q.forgeRepo, w.session.IssueNumber, title, body)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.BindPlanStoryShell(ctx, w.session.TaskRef, state.Stories[0].Key, child.ID, child.Number, turn.PlanIssueRevision(child.Title, child.Body)))
	linearWorkflowCheck(t, db.AdvanceStoryDiscovery(ctx, w.session.ID, child.Number, 1))
	_, err = db.SavePlanWorkflowState(ctx, w.session.TaskRef, 0, state)
	linearWorkflowCheck(t, err)
	// The parent set agent already finished. Assembly must reuse its source.
	segment, err := db.GetOrCreateSegment(ctx, w.session.ID, 1, w.session.TaskRef, store.AgentClaude)
	linearWorkflowCheck(t, err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: w.session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentClaude, TriggerCommentID: 101, StartedAt: w.now, LogPath: func(int) string { return "assembly-source.jsonl" }})
	linearWorkflowCheck(t, err)
	_, err = db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: w.now})
	linearWorkflowCheck(t, err)
	specs, err := turn.PlanSpecs()
	linearWorkflowCheck(t, err)
	capture, err := opts.WorkflowSource(ctx, w.q.repo, agent.WorkflowPlan, specs)
	linearWorkflowCheck(t, err)
	files, err := json.Marshal(capture.Files)
	linearWorkflowCheck(t, err)
	topology, err := graphexec.SemanticDigest(capture.Profile)
	linearWorkflowCheck(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: capture.Profile.Team, Mode: "plan", ProfileVersion: capture.Profile.ProfileVersion, Path: "plan.dot", Digest: capture.Profile.Digest, CreatedAt: w.now})
	linearWorkflowCheck(t, err)
	admission, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "plan", SessionID: w.session.ID, TriggerID: 101, DefinitionID: definition.ID, Start: "set_agent", ContentDigest: capture.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(capture.Origin)})
	linearWorkflowCheck(t, err)
	instance := admission.Instance
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, LaunchID: admission.LaunchID, Node: "set_agent", RunID: run.ID, StartedAt: w.now})
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "done", FinishedAt: w.now, InstanceStatus: store.GraphSucceeded}))
	return w, opts, state
}

func TestLinearPlanAssemblyPublishesWithoutParentInput(t *testing.T) {
	w, _, state := linearAssemblyFixture(t)
	// The final approval is durable before the next daemon tick.
	for range 3 {
		w.heartbeat(t)
	}
	assertLinearAssembly(t, w, state, store.ScheduleSucceeded)
}

func assertLinearAssembly(t *testing.T, w *linearOwnerWait, state planbrief.PlanState, wantStatus store.ScheduleStatus) {
	t.Helper()
	ctx := t.Context()
	retained, _, err := w.d.db.GetPlanWorkflowState(ctx, w.session.TaskRef)
	linearWorkflowCheck(t, err)
	if retained.AssembledWait != state.PlanSetRevision() || retained.AssembledApproval != "" || retained.SavedRevision != "" {
		t.Fatalf("assembly not opened, or approved automatically: %+v\n%s", retained, w.f.logs.String())
	}
	before, err := json.Marshal(state.Stories)
	linearWorkflowCheck(t, err)
	after, err := json.Marshal(retained.Stories)
	linearWorkflowCheck(t, err)
	if string(before) != string(after) {
		t.Fatal("assembly changed stage approvals")
	}
	id := planWorkID("assemble", state.PlanSetRevision())
	status, found, err := w.d.db.ScheduleStatusBySource(ctx, w.session.ID, id)
	linearWorkflowCheck(t, err)
	if !found || status != wantStatus {
		t.Fatalf("assembly schedule: %s %v\n%s", status, found, w.f.logs.String())
	}
	delivery := linearAssemblyDelivery(t, w)
	if !delivery.Completed || delivery.CommentID == 0 || delivery.ConfirmedAt.IsZero() {
		t.Fatalf("unconfirmed assembly delivery: %+v", delivery)
	}
	display, err := w.d.db.ExplicitOwnerDecisionDisplay(ctx, w.session.TaskRef, "/approve-plan-set", state.PlanSetRevision())
	if err != nil {
		intent, _ := w.d.db.GetGraphComment(ctx, delivery.Key)
		t.Fatalf("approval display missing after delivery %s: %v candidate=%s", delivery.Key, err, intent.Candidate)
	}
	intent, err := w.d.db.GetGraphComment(ctx, delivery.Key)
	linearWorkflowCheck(t, err)
	var saved struct {
		Display store.OwnerDecisionDisplay `json:"decision_display"`
	}
	linearWorkflowCheck(t, json.Unmarshal(intent.Candidate, &saved))
	if store.FormatTime(display.CreatedAt) != store.FormatTime(saved.Display.CreatedAt) {
		t.Fatal("approval display timestamp changed")
	}
	if display.PublicationID != delivery.CommentID {
		t.Fatal("approval display not bound to confirmed review")
	}
	w.remote.mu.Lock()
	defer w.remote.mu.Unlock()
	count := 0
	for _, comment := range w.remote.comments {
		body := linearPlanCommentBody(t, comment)
		if !strings.Contains(body, "/approve-plan-set "+state.PlanSetRevision()) {
			continue
		}
		count++
		if linearWorkflowObject(t, comment["issue"])["id"] != "parent-uuid" {
			t.Fatal("review not on parent")
		}
		for _, fact := range []string{"story", "technical", "acceptance", "https://linear.app/"} {
			if !strings.Contains(body, fact) {
				t.Fatalf("review missing %s: %s", fact, body)
			}
		}
	}
	if count != 1 {
		t.Fatalf("got %d assembled reviews\n%s", count, w.f.logs.String())
	}
}

func TestLinearPlanAssemblyEligibilityReadFailure(t *testing.T) {
	for _, kind := range []string{"scope", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			w := newLinearOwnerWait(t)
			ctx, db := t.Context(), w.d.db
			_, err := db.AdmitPlanWorkflowPath(ctx, w.session.ID, "graph")
			linearWorkflowCheck(t, err)
			_, err = db.ReservePlanStory(ctx, w.session.TaskRef, "story-a")
			linearWorkflowCheck(t, err)
			state := approvedPlanState(t, "go-implementation-engineer", true)
			_, err = db.SavePlanWorkflowState(ctx, w.session.TaskRef, 0, state)
			linearWorkflowCheck(t, err)
			original := w.d.turn
			failure := errors.New("scope lookup unavailable")
			w.d.turn = turnExecutor{opts: turn.Options{WorkflowSource: func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
				return nil, failure
			}}}
			if kind == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
				failure = context.Canceled
			}
			q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log}
			seen := map[int64]*candidate{}
			err = w.d.graphPlanParents(ctx, map[int64]*repoQueue{q.repo.ID: q}, seen)
			if !errors.Is(err, failure) || len(seen) != 0 {
				t.Fatalf("unknown eligibility admitted: %v %+v", err, seen)
			}
			w.d.turn = original
			linearWorkflowCheck(t, w.d.graphPlanParents(t.Context(), map[int64]*repoQueue{q.repo.ID: q}, seen))
			if len(seen) != 1 {
				t.Fatal("read failure lost recoverable assembly")
			}
			retained, _, err := db.GetPlanWorkflowState(t.Context(), w.session.TaskRef)
			linearWorkflowCheck(t, err)
			if retained.AssembledWait != "" || retained.AssembledApproval != "" {
				t.Fatal("read failure opened or approved assembly")
			}
		})
	}
}

func linearAssemblyDelivery(t *testing.T, w *linearOwnerWait) store.ReportDelivery {
	t.Helper()
	instance, err := w.d.db.LatestWorkflowInstance(t.Context(), w.session.ID, "plan")
	linearWorkflowCheck(t, err)
	comments, err := w.d.db.ListGraphComments(t.Context(), instance.ID, "", 100)
	linearWorkflowCheck(t, err)
	for _, comment := range comments {
		if comment.Kind == "final" {
			delivery, err := w.d.db.GetReportDelivery(t.Context(), comment.Key)
			if err != nil {
				t.Fatalf("delivery %s unavailable: %v", comment.Key, err)
			}
			return delivery
		}
	}
	t.Fatalf("no retained assembled publication\n%s", w.f.logs.String())
	return store.ReportDelivery{}
}

func TestLinearPlanAssemblyAdmissionDefers(t *testing.T) {
	for _, kind := range []string{"capacity", "session-lock"} {
		t.Run(kind, func(t *testing.T) {
			w, _, state := linearAssemblyFixture(t)
			ctx := t.Context()
			var release func()
			if kind == "capacity" {
				_, capacity := w.d.pool.capacity()
				if !w.d.pool.acquire(capacity) {
					t.Fatal("could not occupy pool")
				}
				release = func() { w.d.pool.release(capacity) }
			} else {
				ok, err := w.d.db.AcquireSessionLock(ctx, w.session.ID, "other-test-worker")
				linearWorkflowCheck(t, err)
				if !ok {
					t.Fatal("could not occupy session")
				}
				release = func() { linearWorkflowCheck(t, w.d.db.ReleaseSessionLock(ctx, w.session.ID, "other-test-worker")) }
			}
			w.heartbeat(t)
			retained, _, err := w.d.db.GetPlanWorkflowState(ctx, w.session.TaskRef)
			linearWorkflowCheck(t, err)
			if retained.AssembledWait != "" || retained.AssembledApproval != "" {
				t.Fatal("refused admission opened review")
			}
			release()
			for range 3 {
				w.heartbeat(t)
			}
			assertLinearAssembly(t, w, state, store.ScheduleSucceeded)
		})
	}
}

func TestLinearPlanAssemblyLostPublicationResponse(t *testing.T) {
	w, opts, state := linearAssemblyFixture(t)
	ctx := t.Context()
	original := w.remote.server.Config.Handler
	w.remote.server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		if bytes.Contains(raw, []byte("commentCreate(")) && bytes.Contains(raw, []byte("/approve-plan-set")) {
			w.remote.mu.Lock()
			w.remote.loseReply = true
			w.remote.mu.Unlock()
		}
		original.ServeHTTP(writer, request)
	})
	for range 2 {
		// Also exercise the pre-existing publication path independently of discovery.
		linearWorkflowCheck(t, w.d.db.SetSessionNeedsPoll(ctx, w.session.ID))
		w.heartbeat(t)
	}
	delivery := linearAssemblyDelivery(t, w)
	if delivery.Completed || !delivery.PossiblySent || delivery.CommentID != 0 {
		t.Fatalf("lost response confirmed: %+v", delivery)
	}
	for range 2 {
		w.heartbeat(t)
	}
	q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log}
	seen := map[int64]*candidate{}
	linearWorkflowCheck(t, w.d.graphPlanParents(ctx, map[int64]*repoQueue{q.repo.ID: q}, seen))
	if len(seen) != 0 {
		t.Fatal("attempted assembly offered a fresh identity")
	}
	originalCandidate, err := w.d.db.GetGraphComment(ctx, delivery.Key)
	linearWorkflowCheck(t, err)
	opts.MCP.Now = func() time.Time { return w.now.Add(24 * time.Hour) }
	// Existing turn entry recovers the same retained publication before any work.
	ref, err := naming.ParseRef(w.session.TaskRef)
	linearWorkflowCheck(t, err)
	forge, ok := opts.SubIssueCreator.(turn.PlanStoryForge)
	if !ok {
		t.Fatal("fixture lacks Plan story forge")
	}
	issue, err := forge.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
	linearWorkflowCheck(t, err)
	out, err := turn.Execute(ctx, opts, turn.Request{SessionID: w.session.ID, Mode: agent.ModePlan, Agent: "claude", Issue: turn.Issue{Number: ref.Issue, Title: issue.Title, Body: issue.Body}, Trigger: turn.Trigger{CommentID: (store.Schedule{ID: 900}).TriggerID(), Body: turn.PlanWorkPayload}, Command: turn.ParseCommand(turn.PlanWorkPayload)})
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, out.Err)
	assertLinearAssembly(t, w, state, store.ScheduleFailed)
	replayedCandidate, err := w.d.db.GetGraphComment(ctx, delivery.Key)
	linearWorkflowCheck(t, err)
	if !bytes.Equal(originalCandidate.Candidate, replayedCandidate.Candidate) {
		t.Fatal("retry changed retained publication")
	}
}

func TestLinearPlanAssemblyRestartAndApprovalReplay(t *testing.T) {
	w, opts, state := linearAssemblyFixture(t)
	ctx := t.Context()
	previous := w.d
	var err error
	w.d, err = New(Options{ModeRole: testModeRole, DB: previous.db, Forge: w.f.client, Filter: previous.filter, Connections: previous.connections, Turn: turnExecutor{opts: opts}, Reporter: opts.ResearchReporter, InstanceID: "assembly-restart", Logger: previous.log, Now: func() time.Time { return w.now }, DiscoveryInterval: MaxDiscoveryInterval, MaxConcurrentRuns: 4, DefaultAgent: "claude", Agents: opts.Runners, Classifiers: map[agent.Kind]agent.Classifier{"claude": explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions})
	linearWorkflowCheck(t, err)
	w.d.schedule.ListingFinished(w.q.repo.ID, w.now)
	for range 2 {
		w.heartbeat(t)
	}
	linearWorkflowCheck(t, w.d.db.UpdatePlanWorkflowState(ctx, w.session.TaskRef, func(s *planbrief.PlanState) (bool, error) {
		return s.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: 12, OwnerID: 7, ActorID: 7, IssueID: 100, Key: "story-a", Stage: "acceptance", Candidate: "candidate", WaitID: "wait-acceptance", Body: "/approve-plan story-a acceptance candidate"})
	}))
	for range 2 {
		w.heartbeat(t)
	}
	assertLinearAssembly(t, w, state, store.ScheduleSucceeded)
}

func TestLinearPlanAssemblyChangedBeforeExecution(t *testing.T) {
	w, opts, state := linearAssemblyFixture(t)
	ctx := t.Context()
	calls := 0
	w.d.turn = scheduledExecutor(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		calls++
		linearWorkflowCheck(t, w.d.db.UpdatePlanWorkflowState(ctx, w.session.TaskRef, func(s *planbrief.PlanState) (bool, error) {
			draft := s.Stories[0].Draft
			draft.Problem = "Changed before execution"
			return true, s.PutCandidate(draft, "story", "candidate-next")
		}))
		return turn.Execute(ctx, opts, req)
	})
	for range 2 {
		w.heartbeat(t)
	}
	if calls != 1 {
		t.Fatalf("assembly did not reach execution: %d\n%s", calls, w.f.logs.String())
	}
	retained, revision, err := w.d.db.GetPlanWorkflowState(ctx, w.session.TaskRef)
	linearWorkflowCheck(t, err)
	if retained.AssembledWait != "" || retained.AssembledApproval != "" {
		t.Fatal("stale set published or approved")
	}
	w.remote.mu.Lock()
	for _, comment := range w.remote.comments {
		if strings.Contains(linearPlanCommentBody(t, comment), "/approve-plan-set "+state.PlanSetRevision()) {
			t.Error("stale assembled review posted")
		}
	}
	w.remote.mu.Unlock()
	for _, stage := range []string{"story", "technical", "acceptance"} {
		linearWorkflowCheck(t, retained.OpenPlanWait("story-a", "new-"+stage, "review", 100))
		applied, e := retained.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: int64(100 + len(retained.ConsumedReplies)), OwnerID: 7, ActorID: 7, IssueID: 100, Key: "story-a", Stage: stage, Candidate: "candidate-next", WaitID: "new-" + stage, Body: "/approve-plan story-a " + stage + " candidate-next"})
		linearWorkflowCheck(t, e)
		if !applied {
			t.Fatal("new revision approval not applied")
		}
	}
	_, err = w.d.db.SavePlanWorkflowState(ctx, w.session.TaskRef, revision, retained)
	linearWorkflowCheck(t, err)
	w.d.turn = turnExecutor{opts: opts}
	for range 3 {
		w.heartbeat(t)
	}
	assertLinearAssembly(t, w, retained, store.ScheduleSucceeded)
}

func TestLinearPlanAssemblyPartialDiscovery(t *testing.T) {
	w := newLinearOwnerWait(t)
	ctx, db := t.Context(), w.d.db
	second, err := db.GetOrCreateSession(ctx, w.q.repo, w.session.IssueNumber+1)
	linearWorkflowCheck(t, err)
	state := approvedPlanState(t, "go-implementation-engineer", true)
	for _, session := range []store.Session{w.session, second} {
		_, err = db.AdmitPlanWorkflowPath(ctx, session.ID, "graph")
		linearWorkflowCheck(t, err)
		_, err = db.ReservePlanStory(ctx, session.TaskRef, "story-a")
		linearWorkflowCheck(t, err)
		_, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state)
		linearWorkflowCheck(t, err)
	}
	failure := errors.New("second parent scope unavailable")
	calls := 0
	w.d.turn = turnExecutor{opts: turn.Options{WorkflowSource: func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		calls++
		if calls == 2 {
			return nil, failure
		}
		return teamsourcetest.CapturePlan(ctx, specs)
	}}}
	q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log}
	seen := map[int64]*candidate{}
	err = w.d.graphPlanParents(ctx, map[int64]*repoQueue{q.repo.ID: q}, seen)
	if !errors.Is(err, failure) || len(seen) != 1 || len(q.candidates) != 1 {
		t.Fatalf("partial discovery: %v candidates=%d", err, len(q.candidates))
	}
	for range 2 {
		linearWorkflowCheck(t, w.d.graphPlanParents(ctx, map[int64]*repoQueue{q.repo.ID: q}, seen))
	}
	if len(seen) != 2 || len(q.candidates) != 2 {
		t.Fatal("retry lost or duplicated parent")
	}
	for _, session := range []store.Session{w.session, second} {
		retained, _, e := db.GetPlanWorkflowState(ctx, session.TaskRef)
		linearWorkflowCheck(t, e)
		if retained.AssembledWait != "" || retained.AssembledApproval != "" {
			t.Fatal("discovery falsely completed assembly")
		}
		_, exists, e := db.ScheduleStatusBySource(ctx, session.ID, planWorkID("assemble", state.PlanSetRevision()))
		linearWorkflowCheck(t, e)
		if exists {
			t.Fatal("discovery allocated a schedule before worker admission")
		}
	}
}
