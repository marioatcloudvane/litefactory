package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type resolutionResearchCounter struct {
	calls  int
	target string
}

func (p *resolutionResearchCounter) Current(_ context.Context, target string) (Question, error) {
	p.calls++
	p.target = target
	return Question{Text: "Which boundary?", Token: "123456789abc"}, nil
}
func (*resolutionResearchCounter) Answer(context.Context, string, string, string, time.Time) (Question, error) {
	return Question{}, errors.New("proposal resolution must not answer")
}

func TestOwnerActionResolutionChecksCoverageBeforeResearchRead(t *testing.T) {
	ctx := t.Context()
	db := naturalExecutionDB(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	var repos []store.Repo
	for _, name := range []string{"selected", "uncovered"} {
		repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: name, BaseBranch: "main"})
		if err != nil {
			t.Fatal(err)
		}
		session, e := db.GetOrCreateSession(ctx, repo, 42)
		if e != nil {
			t.Fatal(e)
		}
		if name == "uncovered" {
			if _, e = db.FreezeSessionSlug(ctx, session.ID, "private-payroll-change", "lf/private-payroll-change"); e != nil {
				t.Fatal(e)
			}
		}
		repos = append(repos, repo)
	}
	client := store.ConnectorClient{ID: "resolution-client", Name: "Owner client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	if err = db.RegisterConnectorClient(ctx, client, at); err != nil {
		t.Fatal(err)
	}
	browser := store.ConnectorBrowser{ID: "resolution-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: "https://factory.test/litefactory/mcp", Scope: "repository:actions", PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: at.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(ctx, browser, 42, at); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, 42, at); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: "resolution-grant", ClientID: client.ID, Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour), ActionCoverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repos[0].ID, RepositoryRef: "github:acme/selected"}}}}
	code := store.ConnectorCode{Digest: "resolution-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	if err = db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, 42, at, grant, code, true); err != nil {
		t.Fatal(err)
	}
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: client.ID, GrantID: grant.ID}
	reads := &resolutionResearchCounter{}
	backend := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	backend.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	backend.EnableResearch(nil, reads)
	actions := owneraction.New(db, OwnerActionDomain{Service: backend}, func() time.Time { return at })
	invocation := func(project string) owneraction.Invocation {
		return owneraction.Invocation{Command: "answer", Arguments: map[string]string{"project": project, "issue": "42", "revision": "123456789abc", "text": "Reviewed answer"}}
	}
	denied, err := actions.Propose(ctx, principal, invocation("acme/uncovered"), "denied")
	if err == nil || denied.ID != "" || reads.calls != 0 {
		t.Fatalf("uncovered repository reached external research read: proposal=%+v calls=%d err=%v", denied, reads.calls, err)
	}
	selected, err := actions.Propose(ctx, principal, invocation("acme/selected"), "selected")
	if err != nil || selected.Facts.RepositoryID != repos[0].ID || reads.calls != 1 || reads.target != "github:acme/selected#42" {
		t.Fatalf("selected repository failed resolution: proposal=%+v calls=%d target=%s err=%v", selected, reads.calls, reads.target, err)
	}
	inferred, err := actions.Propose(ctx, principal, invocation(""), "inferred")
	if err != nil || inferred.Facts.RepositoryID != repos[0].ID || reads.calls != 2 || reads.target != "github:acme/selected#42" {
		t.Fatalf("omitted project did not select only covered task: %+v calls=%d err=%v", inferred, reads.calls, err)
	}
	_, err = actions.Propose(ctx, principal, invocation("missing-project"), "missing")
	if err == nil || strings.Contains(err.Error(), "uncovered") || strings.Contains(err.Error(), "private payroll") {
		t.Fatalf("missing selection exposed private catalog: %v", err)
	}
	extra, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "allowed-extra", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOrCreateSession(ctx, extra, 42); err != nil {
		t.Fatal(err)
	}
	browser.ID = "ambiguous-browser"
	if err = db.CreateConnectorBrowser(ctx, browser, 42, at); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, 42, at); err != nil {
		t.Fatal(err)
	}
	grant.ID = "ambiguous-grant"
	grant.ActionCoverage = &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repos[0].ID, RepositoryRef: "github:acme/selected"}, {RepoID: extra.ID, RepositoryRef: "github:acme/allowed-extra"}}}
	code.GrantID = grant.ID
	code.Digest = "ambiguous-code"
	if err = db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, 42, at, grant, code, true); err != nil {
		t.Fatal(err)
	}
	wider := principal
	wider.GrantID = grant.ID
	_, err = actions.Propose(ctx, wider, invocation(""), "ambiguous")
	if err == nil || strings.Contains(err.Error(), "uncovered") || strings.Contains(err.Error(), "private payroll") || !strings.Contains(err.Error(), "selected") || !strings.Contains(err.Error(), "allowed-extra") {
		t.Fatalf("ambiguous selection did not retain authorized catalog only: %v", err)
	}
	if reads.calls != 2 {
		t.Fatalf("refused inference reached external research: %d", reads.calls)
	}
	legacy, err := backend.resolveNaturalProjectFocus(ctx, map[string]string{"issue": "42"}, "")
	if err != nil || !strings.Contains(legacy.Refusal, "uncovered") || !strings.Contains(legacy.Refusal, "private payroll") {
		t.Fatalf("legacy resolution without action context changed: %+v %v", legacy, err)
	}

}
