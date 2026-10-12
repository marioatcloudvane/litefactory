package daemon_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestRecoveryApproverConfigurationFailureDefersOnlyItsProject(t *testing.T) {
	for _, configuration := range []string{"missing", "conflict"} {
		t.Run(configuration, func(t *testing.T) {
			w := newWebhookFixture(t)
			w.clock.mu.Lock()
			w.clock.now = time.Now().UTC().Add(time.Minute)
			w.clock.mu.Unlock()
			primaryOrigin := recoveryProvider(t, w.fixture)
			other := w.secondRepo()
			otherFixture := *w.fixture
			otherFixture.repo = other
			otherOrigin := recoveryProvider(t, &otherFixture)
			w.spy.store("recovery-ticket-reader", testToken)
			configure := func(repo store.Repo, origin ticket.Origin) store.ConnectionVersion {
				t.Helper()
				var source store.ConnectionVersion
				w.forge.mu.Lock()
				remoteID := strconv.FormatInt(w.forge.state(repo.Owner, repo.Name).remoteID, 10)
				w.forge.mu.Unlock()
				for _, role := range []store.ConnectionRole{store.CodeConnection, store.TicketConnection} {
					version, err := w.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: role, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: remoteID, Enabled: true, Provenance: "explicit", CredentialSource: "keychain", CredentialService: "recovery-reader", CredentialAccount: "recovery-ticket-reader", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
					if err != nil {
						t.Fatal(err)
					}
					if role == store.TicketConnection {
						source = version
					}
				}
				return source
			}
			primarySource := configure(w.repo, primaryOrigin)
			otherSource := configure(other, otherOrigin)
			factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID})
			approvers := projectconnection.ApproverService{DB: w.db, Factory: factory}
			if _, err := approvers.Set(t.Context(), other.ID, ownerLogin, otherSource.Revision, 0); err != nil {
				t.Fatal(err)
			}
			if configuration == "conflict" {
				if _, err := approvers.Set(t.Context(), w.repo.ID, ownerLogin, primarySource.Revision, 0); err != nil {
					t.Fatal(err)
				}
				if err := w.db.SetApproverInitialization(t.Context(), store.ApproverInitialization{RepoID: w.repo.ID, Status: "conflict", Reason: "retained historical approver identities conflict"}); err != nil {
					t.Fatal(err)
				}
			}
			w.daemon = newRecoveryDaemon(t, w, primaryOrigin, "approver-recovery-isolation-"+configuration, false)
			w.plantCursor()
			w.issue("Unconfigured project remains pending")
			const validNumber = testIssue
			w.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: validNumber, Title: "Independent configured project", Body: "/spike Continue authorized project work.", User: owner()})
			if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: w.repo.ID, ConnectionVersionID: primarySource.ID, Provider: ticket.GitHub, Origin: primaryOrigin, Container: w.repo.Slug(), ContainerID: primarySource.RemoteID, TicketID: strconv.Itoa(10000 + testIssue), TicketKey: strconv.Itoa(testIssue), ObservedAt: w.clock.peek()}); err != nil {
				t.Fatal(err)
			}
			if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: other.ID, ConnectionVersionID: otherSource.ID, Provider: ticket.GitHub, Origin: otherOrigin, Container: other.Slug(), ContainerID: otherSource.RemoteID, TicketID: strconv.Itoa(10000 + validNumber), TicketKey: strconv.Itoa(validNumber), ObservedAt: w.clock.peek()}); err != nil {
				t.Fatal(err)
			}
			ran := make(chan int64, 2)
			w.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
				session, err := w.db.GetSession(ctx, req.SessionID)
				if err != nil {
					return turn.Result{}, err
				}
				ran <- session.RepoID
				return w.ranSuccessfully(ctx, req)
			})
			result := w.settle()
			select {
			case projectID := <-ran:
				if projectID != other.ID {
					t.Fatalf("unconfigured project ran: project=%d", projectID)
				}
			default:
				sessions, _ := w.db.ListRepoSessions(t.Context(), other.ID)
				observations, _ := w.db.ListPendingTicketObservations(t.Context(), 0)
				t.Fatalf("failed approver configuration starved independent project; tick=%+v sessions=%+v pending=%+v\n%s", result, sessions, observations, w.fixture.logs)
			}
			if _, err := w.db.GetSessionByTaskRef(t.Context(), store.TaskRef(store.ForgeGitHub, w.repo.Owner, w.repo.Name, testIssue)); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("blocked project obtained admitted session: %v", err)
			}
			pending, err := w.db.ListPendingTicketObservations(t.Context(), primarySource.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, observation := range pending {
				if observation.TicketID == strconv.Itoa(10000+testIssue) {
					found = true
				}
			}
			if !found {
				t.Fatal("configuration refusal discarded unresolved ticket observation")
			}
			const replyID = 910
			w.clock.advance(2 * time.Hour)
			w.forge.addCommentTo(other.Owner, other.Name, validNumber, replyID, owner(), "/spike Continue with a fresh approver reply.")
			if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: other.ID, ConnectionVersionID: otherSource.ID, Provider: ticket.GitHub, Origin: otherOrigin, Container: other.Slug(), ContainerID: otherSource.RemoteID, TicketID: strconv.Itoa(10000 + validNumber), TicketKey: strconv.Itoa(validNumber), ObservedAt: w.clock.peek()}); err != nil {
				t.Fatal(err)
			}

			w.settle()
			select {
			case projectID := <-ran:
				if projectID != other.ID {
					t.Fatalf("reply ran for wrong project: %d", projectID)
				}
			default:
				t.Fatalf("configured project's fresh reply did not execute:\n%s", w.fixture.logs)
			}
			w.forge.mu.Lock()
			reacted := false
			for _, id := range w.forge.reactedTo {
				if id == replyID {
					reacted = true
				}
			}
			w.forge.mu.Unlock()
			if !reacted {
				t.Fatalf("fresh reply was not acknowledged through its saved ticket origin:\n%s", w.fixture.logs)
			}

		})
	}
}
