package turn

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestGranteeAuthorityAdmitsOnlyItsOwnAllowDecision(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "access.db"))
	mustProgress(t, err)
	defer func() { mustProgress(t, db.Close()) }()
	mustProgress(t, db.Migrate(t.Context()))
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/grantee-code", Forge: store.ForgeGitHub, Owner: "owner", Name: "code", RemoteURL: "git@github.com:owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/grantee-trees"})
	mustProgress(t, err)
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	mustProgress(t, err)
	other, err := db.GetOrCreateSession(t.Context(), repo, 8)
	mustProgress(t, err)

	origin := ticket.Origin("https://api.github.com")
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "77"}
	body := "please add a health endpoint"
	decide := func(source string, sessionID int64, decision string) store.AccessDecision {
		t.Helper()
		row, created, err := db.BeginAccessDecision(t.Context(), store.AccessDecision{RepoID: repo.ID, SessionID: sessionID,
			Source: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: source}, Actor: actor, Handle: "helper",
			TargetJSON: `{}`, RequestDigest: access.Digest(body)})
		if err != nil || !created {
			t.Fatalf("begin %s: %v %v", source, created, err)
		}
		if decision == "" {
			return row
		}
		row, err = db.FinishAccessDecision(t.Context(), row.ID, store.AccessDecisionOutcome{Decision: decision, Reason: "checked"})
		mustProgress(t, err)
		return row
	}
	request := func(row store.AccessDecision) Request {
		return Request{SessionID: session.ID, Issue: Issue{Number: 7, Body: "issue body"}, Trigger: Trigger{
			CommentID: 101, Body: body, Actor: actor, Source: row.Source, AuthorID: 77, Author: "helper",
			Grantee: &GranteeAuthority{Actor: actor, Handle: "helper", DecisionID: row.ID},
		}}
	}
	opts := Options{DB: db, ProjectApprovers: true}

	allowed := decide("101", session.ID, store.AccessAllow)
	mustProgress(t, verifyGranteeAuthority(t.Context(), opts, session, request(allowed)))

	refused := map[string]Request{}
	refused["deny"] = request(decide("102", session.ID, store.AccessDeny))
	refused["pending"] = request(decide("103", session.ID, ""))
	refused["wrong session"] = request(decide("104", other.ID, store.AccessAllow))
	wrongActor := request(allowed)
	wrongActor.Trigger.Actor.RemoteID = "78"
	wrongActor.Trigger.Grantee.Actor.RemoteID = "78"
	refused["wrong actor"] = wrongActor
	wrongGrantee := request(allowed)
	wrongGrantee.Trigger.Grantee.Actor.RemoteID = "78"
	refused["wrong grantee actor"] = wrongGrantee
	changed := request(allowed)
	changed.Trigger.Body = body + " and delete the repository"
	refused["changed body"] = changed
	wrongSource := request(allowed)
	wrongSource.Trigger.Source.RemoteID = "999"
	refused["wrong source"] = wrongSource
	missing := request(allowed)
	missing.Trigger.Grantee.DecisionID = allowed.ID + 1000
	refused["missing decision"] = missing
	none := request(allowed)
	none.Trigger.Grantee = nil
	refused["no grantee"] = none
	for name, req := range refused {
		if err := verifyGranteeAuthority(t.Context(), opts, session, req); !errors.Is(err, store.ErrApproverUnauthorized) {
			t.Errorf("%s: got %v, want ErrApproverUnauthorized", name, err)
		}
	}

	// An issue body is checked against the issue text the gate digested.
	issueRow, created, err := db.BeginAccessDecision(t.Context(), store.AccessDecision{RepoID: repo.ID, SessionID: session.ID,
		Source: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "issue-7"}, Actor: actor, Handle: "helper",
		TargetJSON: `{}`, RequestDigest: access.Digest("issue body")})
	if err != nil || !created {
		t.Fatalf("begin issue: %v %v", created, err)
	}
	issueRow, err = db.FinishAccessDecision(t.Context(), issueRow.ID, store.AccessDecisionOutcome{Decision: store.AccessAllow, Reason: "checked"})
	mustProgress(t, err)
	issueReq := request(issueRow)
	issueReq.Trigger.CommentID, issueReq.Trigger.Body = 0, ""
	mustProgress(t, verifyGranteeAuthority(t.Context(), opts, session, issueReq))
	issueReq.Issue.Body = "edited issue body"
	if err := verifyGranteeAuthority(t.Context(), opts, session, issueReq); !errors.Is(err, store.ErrApproverUnauthorized) {
		t.Errorf("edited issue body: got %v, want ErrApproverUnauthorized", err)
	}
}
