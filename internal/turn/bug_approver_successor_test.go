package turn

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestFinishedBugSessionAcceptsReplacementApproverWithoutRewritingHistory(t *testing.T) {
	f, forge, req := assembledResearchFixture(t, nil)
	req.Workflow = agent.WorkflowBug
	req.Mode = ""
	req.Trigger.Body = "Investigate the first bug"
	req.Command = ParseCommand(req.Trigger.Body)
	req.Trigger.AuthorID = 42
	origin := ticket.Origin("https://api.github.com")
	session, err := f.db.GetSession(t.Context(), req.SessionID)
	mustProgress(t, err)
	repo, err := f.db.GetRepo(t.Context(), session.RepoID)
	mustProgress(t, err)
	connection, err := f.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	mustProgress(t, err)
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}
	first, err := f.db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: repo.ID, Actor: actor, Handle: "original", VerifiedAt: time.Now(), Provenance: "explicit"}, connection.Revision, 0)
	mustProgress(t, err)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "issue-immutable-id"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.Slug(), Key: strconv.Itoa(session.IssueNumber)}}
	_, err = f.db.BindSessionTicket(t.Context(), session.ID, ref)
	mustProgress(t, err)
	attachTicketSnapshot(t, f.db, repo, session.ID, connection, ref)
	f.opts.ProjectApprovers = true
	f.opts, err = bindProjectApprover(t.Context(), f.opts, session.ID)
	mustProgress(t, err)
	req.Trigger.Actor = actor
	req.Trigger.Source = ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: strconv.FormatInt(req.Trigger.CommentID, 10)}
	req.Trigger.CreatedAt = first.ActivatedAt.Add(time.Second)
	originalClaim, err := claimTurnAuthority(t.Context(), f.opts, req)
	mustProgress(t, err)
	_, err = f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	runtime, err := NewGraphBugRuntime(t.Context(), f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) { return BugRolePermissions{}, nil }})
	mustProgress(t, err)
	original, err := runtime.graphAdmission(t.Context(), req)
	mustProgress(t, err)
	mustProgress(t, f.db.SetGraphInstanceStatus(t.Context(), original.Instance.ID, store.GraphSucceeded, time.Time{}, time.Now()))
	mustProgress(t, f.db.CompleteProjectAuthority(t.Context(), originalClaim.ID))
	finished, err := f.db.GetGraphInstance(t.Context(), original.Instance.ID)
	mustProgress(t, err)
	actor.RemoteID = "43"
	replacement, err := f.db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: repo.ID, Actor: actor, Handle: "replacement", VerifiedAt: time.Now(), Provenance: "explicit"}, connection.Revision, first.Revision)
	mustProgress(t, err)
	f.opts, err = bindProjectApprover(t.Context(), f.opts, session.ID)
	mustProgress(t, err)
	req.Trigger.CommentID++
	req.Trigger.AuthorID = 43
	req.Trigger.Actor = actor
	req.Trigger.Source.RemoteID = strconv.FormatInt(req.Trigger.CommentID, 10)
	req.Trigger.CreatedAt = replacement.ActivatedAt.Add(time.Second)
	req.Trigger.Body = "Investigate a fresh bug after replacement"
	req.Command = ParseCommand(req.Trigger.Body)
	newClaim, err := claimTurnAuthority(t.Context(), f.opts, req)
	mustProgress(t, err)
	defer func() {
		mustProgress(t, f.db.CompleteProjectAuthority(context.WithoutCancel(t.Context()), newClaim.ID))
	}()
	local := *runtime
	local.opts = f.opts
	local.cfg.OwnerID = 43
	successor, err := local.graphAdmission(t.Context(), req)
	mustProgress(t, err)
	if successor.Instance.ID == original.Instance.ID || successor.PredecessorID != original.Instance.ID || successor.Instance.Status != store.GraphRunning {
		t.Fatalf("fresh actor did not create successor: %+v", successor)
	}
	retained, err := f.db.GetGraphInstance(t.Context(), original.Instance.ID)
	mustProgress(t, err)
	if retained != finished {
		t.Fatalf("prior graph changed: before=%+v after=%+v", finished, retained)
	}
	priorAuthority, err := f.db.GetApproverAppointment(t.Context(), first.ID)
	mustProgress(t, err)
	if priorAuthority.Actor.RemoteID != "42" || priorAuthority.Revision != first.Revision {
		t.Fatalf("prior authority changed: %+v", priorAuthority)
	}
}
