package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func codeReviewSession(t *testing.T, db *DB) (Session, ConnectionVersion) {
	t.Helper()
	repo := testRepo(t, db, "acme", "tickets")
	codeInput := connectionFixture(repo, CodeConnection)
	codeInput.Destination = "acme/code"
	codeInput.RemoteID = "code-123"
	code, err := db.SetConnection(t.Context(), codeInput, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "7")
	_, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 42, "bot", "github:acme/tickets#7")
	if err != nil {
		t.Fatal(err)
	}
	s, err = db.FreezeSessionSlug(t.Context(), s.ID, "review", "lf/7-review")
	if err != nil {
		t.Fatal(err)
	}
	return s, code
}

func codeReviewConsent(t *testing.T, db *DB, id string, coverage *ConnectorCoverage) (ConnectorGrant, ConnectorExchange) {
	t.Helper()
	return grantCoverage(t, db, id, connectorTestScope+" "+connectorDiscussionScope+" "+connectorCodeReviewScope, coverage)
}

func TestConnectorCodeReviewDerivesTheTasksRetainedCodeAndFailsClosed(t *testing.T) {
	for _, scenario := range []string{"retained settings", "unknown task", "revoked", "expiry", "owner generation", "deleted snapshot", "changed task branch"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			connectorTestOwner(t, db, 42)
			connectorTestClient(t, db, "client")
			s, code := codeReviewSession(t, db)
			target, err := db.ResolveConnectorCodeTarget(t.Context(), s.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			if target.RepositoryRef != "github:acme/code" || target.TaskBranch != s.Branch || target.BaseBranch != "main" {
				t.Fatalf("target %+v", target)
			}
			// Coverage names the task's own repository; the code repository is derived.
			g, x := codeReviewConsent(t, db, "code", &ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: "github:acme/tickets"}}})
			now := connectorTestNow
			task := s.TaskRef
			switch scenario {
			case "retained settings":
				code.Destination = "new/destination"
				code.ClonePath = t.TempDir()
				if _, err = db.SetConnection(t.Context(), code, code.Revision); err != nil {
					t.Fatal(err)
				}
			case "unknown task":
				task = "github:acme/tickets#8"
			case "revoked":
				err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, now)
			case "expiry":
				now = now.Add(24 * time.Hour)
			case "owner generation":
				connectorTestOwner(t, db, 43)
			case "deleted snapshot":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM session_execution_snapshots WHERE session_id=?`, s.ID)
			case "changed task branch":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET branch='lf/wrong' WHERE id=?`, s.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, got, err := db.CheckConnectorCodeAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, task, now)
			switch scenario {
			case "retained settings":
				if err != nil || !reflect.DeepEqual(got, target) {
					t.Fatalf("retained target %+v %v", got, err)
				}
			case "unknown task":
				if ConnectorRefusalReason(err) != `task not found: "github:acme/tickets#8"` {
					t.Fatalf("unknown task: %v", err)
				}
			case "deleted snapshot":
				if !strings.HasPrefix(ConnectorRefusalReason(err), "no retained code for task") {
					t.Fatalf("missing code destination: %v", err)
				}
			case "changed task branch":
				// The code target is derived on each call, so it follows the task.
				if err != nil || got.TaskBranch != "lf/wrong" {
					t.Fatalf("derived target %+v %v", got, err)
				}
			default:
				if err == nil {
					t.Fatal("invalid code access allowed")
				}
			}
		})
	}
}

func TestConnectorCodeReviewRequiresBothScopesAndRetainedIdentity(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	s := testSession(t, db, 7)
	if _, err := db.ResolveConnectorCodeTarget(t.Context(), s.TaskRef); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("missing retained identity: %v", err)
	}
	_, x := grantCoverage(t, db, "discussion", connectorTestScope+" "+connectorDiscussionScope, &ConnectorCoverage{All: true})
	if _, _, err := db.CheckConnectorCodeAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, s.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("discussion granted code: %v", err)
	}
	b, g, c, _ := coverageConsentFixture(t, db, "code-only", connectorCodeReviewScope, &ConnectorCoverage{All: true})
	err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true)
	if !errors.Is(err, ErrConnectorScope) || ConnectorRefusalReason(err) != "task:code-review also needs task:discussion" {
		t.Fatalf("code without discussion: %v", err)
	}
}

func TestConnectorCodeReviewMappedParentBase(t *testing.T) {
	db := newTestDB(t)
	s, code := codeReviewSession(t, db)
	repo, err := db.GetRepo(t.Context(), s.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "8")
	ref.Locator.Key = "8"
	current, err := db.CurrentConnection(t.Context(), repo.ID, TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: current.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, current.Revision, 42, "bot", "github:acme/tickets#8")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.FreezeSessionSlug(t.Context(), parent.ID, "parent", "lf/8-parent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO implement_stories(parent,generation,story_key,position,branch,state,created_at,updated_at,state_at,worker_session) VALUES(?,1,'story-001',1,?,'running',?,?,?,?)`, parent.TaskRef, s.Branch, FormatTime(connectorTestNow), FormatTime(connectorTestNow), FormatTime(connectorTestNow), s.ID); err != nil {
		t.Fatal(err)
	}
	target, err := db.ResolveConnectorCodeTarget(t.Context(), s.TaskRef)
	if err != nil || target.BaseBranch != "lf/8-parent" {
		t.Fatalf("mapped parent %+v %v", target, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET branch=NULL WHERE id=?`, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ResolveConnectorCodeTarget(t.Context(), s.TaskRef); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("missing parent fell back: %v", err)
	}
}

func TestConnectorCodeReviewPlanMappingUsesRetainedTicketSource(t *testing.T) {
	for _, scenario := range []string{"foreign plan ignored", "missing parent identity", "exact worker foreign denied"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			oldChild, code := codeReviewSession(t, db)
			repo, err := db.GetRepo(t.Context(), oldChild.RepoID)
			if err != nil {
				t.Fatal(err)
			}
			tickets, err := db.CurrentConnection(t.Context(), repo.ID, TicketConnection)
			if err != nil {
				t.Fatal(err)
			}
			parentRef := resolvedSessionTicket(repo.Slug(), "8008")
			parentRef.Locator.Key = "8"
			_, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: parentRef, Enabled: true, Provenance: "explicit"})
			if err != nil {
				t.Fatal(err)
			}
			parent, err := db.AdmitSession(t.Context(), repo, parentRef, code.Revision, tickets.Revision, 42, "bot", "github:acme/tickets#8")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.FreezeSessionSlug(t.Context(), parent.ID, "old-parent", "lf/8-old-parent")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.AdmitPlanWorkflowPath(t.Context(), parent.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ReservePlanStory(t.Context(), parent.TaskRef, "story-001"); err != nil {
				t.Fatal(err)
			}
			if err = db.BindPlanStoryIssue(t.Context(), parent.TaskRef, "story-001", 7007, 7); err != nil {
				t.Fatal(err)
			}
			retained, err := db.ResolveConnectorCodeTarget(t.Context(), oldChild.TaskRef)
			if err != nil || retained.BaseBranch != "lf/8-old-parent" {
				t.Fatalf("original child mapping %+v %v", retained, err)
			}
			// The same code project now admits another #7 from a different ticket
			// destination. The older parent's #7 mapping cannot select its base.
			tickets.Destination = "acme/new-tickets"
			tickets, err = db.SetConnection(t.Context(), tickets, tickets.Revision)
			if err != nil {
				t.Fatal(err)
			}
			newRef := resolvedSessionTicket(tickets.Destination, "9007")
			_, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: newRef, Enabled: true, Provenance: "explicit"})
			if err != nil {
				t.Fatal(err)
			}
			newChild, err := db.AdmitSession(t.Context(), repo, newRef, code.Revision, tickets.Revision, 42, "bot", "github:acme/new-tickets#7")
			if err != nil {
				t.Fatal(err)
			}
			newChild, err = db.FreezeSessionSlug(t.Context(), newChild.ID, "new-child", "lf/7-new-child")
			if err != nil {
				t.Fatal(err)
			}
			if oldChild.IssueNumber != newChild.IssueNumber {
				t.Fatal("fixture must share display numbers")
			}
			switch scenario {
			case "missing parent identity":
				if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM session_ticket_identities WHERE session_id=?`, parent.ID); err != nil {
					t.Fatal(err)
				}
			case "exact worker foreign denied":
				if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO implement_stories(parent,generation,story_key,position,branch,state,created_at,updated_at,state_at,worker_session) VALUES(?,1,'story-001',1,?,'running',?,?,?,?)`, parent.TaskRef, newChild.Branch, FormatTime(connectorTestNow), FormatTime(connectorTestNow), FormatTime(connectorTestNow), newChild.ID); err != nil {
					t.Fatal(err)
				}
			}
			got, err := db.ResolveConnectorCodeTarget(t.Context(), newChild.TaskRef)
			if scenario == "foreign plan ignored" {
				if err != nil || got.BaseBranch != "main" || got.TaskBranch != "lf/7-new-child" {
					t.Fatalf("foreign parent selected %+v %v", got, err)
				}
			} else if !errors.Is(err, ErrConnectorInvalid) {
				t.Fatalf("unproven mapping fell back %+v %v", got, err)
			}
		})
	}
}
