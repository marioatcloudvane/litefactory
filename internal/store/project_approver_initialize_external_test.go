package store_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// rewriteTransport sends every request for api.github.com to a test server.
type rewriteTransport struct{ target *url.URL }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.target.Scheme, r.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestInitializeApproversTreatsUnknownHistoricalAuthorAsOwner(t *testing.T) {
	for _, tc := range []struct {
		name         string
		otherAuthor  int64
		status       string
		reasonPrefix string
	}{
		{name: "unknown-author", status: "complete"},
		{name: "different-author", otherAuthor: 43, status: "conflict", reasonPrefix: "legacy_approval_actor_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					_, _ = w.Write([]byte(`{"id":7,"login":"publisher","type":"User"}`))
				case "/user/42":
					_, _ = w.Write([]byte(`{"id":42,"login":"owner","type":"User"}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			store.MigrateBefore(t, db, "project_approvers")
			if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 42, Source: store.SourceGH}); err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/legacy", Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", RemoteURL: "git@github.com:owner/repo.git"})
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(t.Context(), repo, 7)
			if err != nil {
				t.Fatal(err)
			}
			// An instruction comment without a recorded author id (NULL/0).
			if _, err = store.SeedHistoricalComment(t, db, store.Comment{SessionID: session.ID, ForgeCommentID: 123, AuthorHandle: "owner", Body: "instruction", CreatedAt: time.Now(), Classification: store.ClassificationInstruction}); err != nil {
				t.Fatal(err)
			}
			if tc.otherAuthor > 0 {
				if _, err = store.SeedHistoricalComment(t, db, store.Comment{SessionID: session.ID, ForgeCommentID: 124, AuthorHandle: "other", AuthorUserID: tc.otherAuthor, Body: "instruction", CreatedAt: time.Now(), Classification: store.ClassificationInstruction}); err != nil {
					t.Fatal(err)
				}
			}
			// A pending system report delivery without a run or comment trigger.
			if _, err = db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: "pending-bug-report", SessionID: session.ID, TriggerID: 0, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "bug", Purpose: "report", BotUserID: 7, Body: "original bytes", Marker: "retained"}); err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/repo", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "legacy"}, 0); err != nil {
				t.Fatal(err)
			}
			// An earlier build left the approver in conflict; the next start retries.
			if err = db.SetApproverInitialization(t.Context(), store.ApproverInitialization{RepoID: repo.ID, Status: "conflict", Reason: "legacy_approval_actor_conflict; reconcile historical owner identities"}); err != nil {
				t.Fatal(err)
			}
			factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, HTTPClient: &http.Client{Transport: rewriteTransport{target}}, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-test-token", true }}})
			if err = projectconnection.InitializeApprovers(t.Context(), db, factory); err != nil {
				t.Fatal(err)
			}
			state, err := db.GetApproverInitialization(t.Context(), repo.ID)
			if err != nil || state.Status != tc.status || (tc.reasonPrefix != "" && len(state.Reason) < len(tc.reasonPrefix)) || state.Reason[:len(tc.reasonPrefix)] != tc.reasonPrefix {
				t.Fatalf("approver initialization %+v %v", state, err)
			}
			approver, err := db.RawCurrentApprover(t.Context(), repo.ID)
			if tc.status == "complete" && (err != nil || approver.Actor.RemoteID != "42" || approver.Provenance != "legacy") {
				t.Fatalf("legacy owner not imported as approver: %+v %v", approver, err)
			}
			if tc.status != "complete" && err == nil {
				t.Fatalf("conflicting history admitted approver %+v", approver)
			}
			delivery, err := db.GetReportDelivery(t.Context(), "pending-bug-report")
			if err != nil || delivery.Body != "original bytes" || delivery.Completed {
				t.Fatalf("pending delivery changed: %+v %v", delivery, err)
			}
		})
	}
}
