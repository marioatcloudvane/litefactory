package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
	"github.com/cloudvane/litefactory/internal/webhook"
)

// This exercises the shared admission, worker, reporting and consumption path:
// the HTTP delivery contains only a hint, and the missed instruction exists
// solely in the provider until a new daemon recovers it from persisted progress.
func TestTicketRecoveryAuthenticatedDeliveryThenRestart(t *testing.T) {
	w := newWebhookFixture(t)
	// Connection enrollment uses real persisted registration times; align the
	// controllable provider clock with them before adding any enrolled work.
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	version := configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "ticket-recovery-started", true)
	w.plantCursor()
	w.assignedIssue("Recover ticket updates", "Please investigate reliable ticket recovery.")
	ran := make(chan struct{}, 4)
	w.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		result, err := w.ranSuccessfully(ctx, req)
		ran <- struct{}{}
		return result, err
	})
	stop := w.run()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			if err := stop(); err != nil {
				t.Errorf("stop delivery daemon: %v", err)
			}
		}
	})
	body := []byte(fmt.Sprintf(`{"action":"opened","issue":{"number":%d,"url":%q},"repository":{"id":1,"full_name":%q}}`, testIssue,
		string(origin)+"/repos/"+w.repo.Slug()+"/issues/"+strconv.Itoa(testIssue), w.repo.Slug()))
	if got := postRecoveryDelivery(t, w, "recovery-invalid", "sha256="+strings.Repeat("0", 64), body); got != http.StatusUnauthorized {
		t.Fatalf("invalid signature: HTTP %d, want 401", got)
	}
	if got := postRecoveryDelivery(t, w, "recovery-ticket", webhook.Sign([]byte(webhookTestSecret), body), body); got != http.StatusAccepted {
		t.Fatalf("authenticated ticket delivery: HTTP %d, want 202", got)
	}
	select {
	case <-ran:
	case <-time.After(30 * time.Second):
		t.Fatalf("authenticated delivery did not reach processing\n%s", w.logs)
	}
	if err := w.daemon.Quiesce(t.Context()); err != nil {
		t.Fatalf("wait for delivery report confirmation: %v", err)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop daemon after delivery: %v", err)
	}
	stopped = true
	sessionID := w.session().ID
	saved, err := w.db.GetExecutionSnapshot(t.Context(), sessionID)
	if err != nil || saved.TicketVersionID != version.ID {
		t.Fatalf("delivery selected wrong source version: %+v, %v", saved, err)
	}
	assertRecoveryReports(t, w.fixture, 1)
	if got := w.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("delivery executions = %v, want only issue instruction", got)
	}

	// No webhook is delivered while the daemon is down. Its replacement has
	// none of the old hint or delivery-ID caches and uses the same durable DB.
	const missedReply int64 = 9001
	w.forge.addComment(testIssue, missedReply, owner(), "/spike investigate the missed update")
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "ticket-recovery-restarted", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{0, missedReply}) {
		t.Fatalf("recovered executions = %v, want issue then missed reply", got)
	}
	if got := w.session().ID; got != sessionID {
		t.Fatalf("restart replaced session %d with %d", sessionID, got)
	}
	if got := w.watermark(); got != missedReply {
		t.Fatalf("consumed reply = %d, want %d", got, missedReply)
	}
	assertRecoveryReports(t, w.fixture, 2)

	// Duplicate durable observations and another restart overlap the consumed reply.
	// They must leave both execution and confirmed publication counts intact.
	observation := store.PendingTicketObservation{
		RepoID: w.repo.ID, ConnectionVersionID: version.ID, Provider: ticket.GitHub,
		Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
		TicketID: strconv.Itoa(10000 + testIssue), TicketKey: strconv.Itoa(testIssue), ObservedAt: w.clock.peek(),
	}
	for range 2 {
		if err := w.db.HandoffTicketObservation(t.Context(), observation); err != nil {
			t.Fatalf("persist repeated observation: %v", err)
		}
	}
	w.settle()
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "ticket-recovery-repeated", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{0, missedReply}) {
		t.Fatalf("repeated observation executed instructions again: %v", got)
	}
	assertRecoveryReports(t, w.fixture, 2)
}

func TestTicketRecoveryFirstSeenAfterOutageKeepsEnrolledReplies(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	version := configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "first-seen-enrolled", false)
	w.plantCursor()
	position, err := w.db.GetTicketRecoveryPosition(t.Context(), version.ID)
	if err != nil {
		t.Fatalf("read persisted enrollment: %v", err)
	}

	// Neither the ticket nor its replies is read while the daemon is down.
	// The older reply is historical discussion; the newer instruction belongs
	// to the enrolled interval despite sharing the delayed first sight.
	w.issue("Ticket first discovered after an outage")
	w.forge.addCommentAt(testIssue, 8, owner(), "/spike historical request", position.EnrollmentAt.Add(-time.Hour))
	w.clock.advance(2 * time.Hour)
	w.forge.addComment(testIssue, 9, owner(), "/spike eligible outage request")
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "first-seen-restarted", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{9}) {
		t.Fatalf("first-sight instructions = %v, want eligible reply 9 only\nlogs:\n%s", got, w.fixture.logs)
	}
	if got := w.watermark(); got != 9 {
		t.Fatalf("first-sight consumption = %d, want 9", got)
	}
	assertRecoveryReports(t, w.fixture, 1)
	retained, err := w.db.GetTicketRecoveryPosition(t.Context(), version.ID)
	if err != nil || !retained.EnrollmentAt.Equal(position.EnrollmentAt) || !retained.CompletedThrough.After(position.CompletedThrough) {
		t.Fatalf("outage recovery lost enrollment or did not complete coverage: %+v, %v", retained, err)
	}

	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "first-seen-repeated", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{9}) {
		t.Fatalf("repeated first-sight recovery executed %v, want [9]", got)
	}
	assertRecoveryReports(t, w.fixture, 1)
}

func TestTicketRecoveryReconciliationDoesNotRunPreEnrollmentBody(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	version := configureRecoverySource(t, w, origin)
	historical := version.CreatedAt.Add(-24 * time.Hour)
	w.forge.addIssue(fakeIssue{
		Number: testIssue, Title: "Historical assigned issue",
		Body: "Please investigate this historical request.",
		User: stranger(), Assignees: []fakeUser{owner()},
		CreatedAt: historical, UpdatedAt: historical,
	})
	w.daemon = newRecoveryDaemon(t, w, origin, "historical-body-reconciliation", false)
	w.settle()
	if got := w.turns.triggers(); len(got) != 0 {
		t.Fatalf("reconciliation executed a pre-enrollment body: %v", got)
	}
	position, err := w.db.GetTicketRecoveryPosition(t.Context(), version.ID)
	if err != nil || !position.EnrollmentAt.After(historical) || position.ReconcileCompletedAt.IsZero() {
		t.Fatalf("historical ticket was not reconciled under its enrollment boundary: %+v, %v", position, err)
	}
	if got := w.watermark(); got != 0 {
		t.Fatalf("historical ticket initialization = %d, want empty reply watermark", got)
	}
	assertRecoveryReports(t, w.fixture, 0)

	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "historical-body-restarted", false)
	w.settle()
	if got := w.turns.triggers(); len(got) != 0 {
		t.Fatalf("restart executed a historical body: %v", got)
	}
	assertRecoveryReports(t, w.fixture, 0)
}

func TestTicketRecoveryCredentialEditKeepsUnseenEnrolledReply(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	original := configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "credential-original-enrolled", false)
	w.plantCursor()
	before, err := w.db.GetTicketRecoveryPosition(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.issue("Unseen instruction survives a credential edit")
	// Connection timestamps retain milliseconds. Separate both real events
	// at that precision so a fast fixture cannot place them in the same tick.
	time.Sleep(2 * time.Millisecond)
	replyAt := time.Now().UTC().Truncate(time.Millisecond)
	w.forge.addCommentAt(testIssue, 13, owner(), "/spike recover the originally enrolled instruction", replyAt)
	if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{
		RepoID: w.repo.ID, ConnectionVersionID: original.ID, Provider: ticket.GitHub,
		Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
		TicketID: strconv.Itoa(10000 + testIssue), TicketKey: strconv.Itoa(testIssue), ObservedAt: replyAt,
	}); err != nil {
		t.Fatal(err)
	}
	w.spy.store("rotated-ticket-reader", testToken)
	revised := original
	revised.CredentialAccount = "rotated-ticket-reader"
	time.Sleep(2 * time.Millisecond)
	revised, err = w.db.SetConnection(t.Context(), revised, original.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !replyAt.After(before.EnrollmentAt) || !replyAt.Before(revised.CreatedAt) {
		t.Fatalf("reply %s must fall between original enrollment %s and credential edit %s", replyAt, before.EnrollmentAt, revised.CreatedAt)
	}
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "credential-edit-restarted", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{13}) {
		t.Fatalf("credential edit lost enrolled instruction: %v, want [13]", got)
	}
	snapshot, err := w.db.GetExecutionSnapshot(t.Context(), w.session().ID)
	if err != nil || snapshot.TicketVersionID != revised.ID {
		t.Fatalf("new admission did not use revised credentials: %+v, %v", snapshot, err)
	}
	position, err := w.db.GetTicketRecoveryPosition(t.Context(), revised.ID)
	if err != nil || !position.EnrollmentAt.Equal(before.EnrollmentAt) {
		t.Fatalf("credential edit changed source enrollment: %+v, %v", position, err)
	}
	retained, err := w.db.GetTicketRecoveryPosition(t.Context(), original.ID)
	if err != nil || retained != before {
		t.Fatalf("credential edit relabeled original recovery position: %+v, %v", retained, err)
	}
	assertRecoveryReports(t, w.fixture, 1)
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "credential-edit-repeated", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{13}) {
		t.Fatalf("credential edit recovery repeated instruction: %v", got)
	}
	assertRecoveryReports(t, w.fixture, 1)
}

func TestTicketRecoveryInterruptedBodyInitializationResumesOnce(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	source := configureRecoverySource(t, w, origin)
	w.assignedIssue("Interrupted first ticket instruction", "Please investigate recovery of an interrupted first instruction.")
	connections := projectconnection.NewFactory(projectconnection.Options{
		Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID,
	})
	ref := ticket.Ref{
		Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: strconv.Itoa(10000 + testIssue)},
		Locator:  ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: w.repo.Slug(), Key: strconv.Itoa(testIssue)},
	}
	if _, err := w.db.AddTicketBinding(t.Context(), store.TicketBinding{
		RepoID: w.repo.ID, ConnectionVersionID: source.ID, Ref: ref, Enabled: true, Provenance: "verified-discovery",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := daemon.AdmitProjectTicket(t.Context(), w.db, connections, w.repo, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.db.InitTicketRecoveryPosition(t.Context(), source, source.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := w.db.SetSessionNeedsPoll(t.Context(), session.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{
		RepoID: w.repo.ID, ConnectionVersionID: source.ID, Provider: ticket.GitHub,
		Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
		TicketID: ref.Identity.RemoteID, TicketKey: ref.Locator.Key, ObservedAt: w.clock.peek(),
	}); err != nil {
		t.Fatal(err)
	}
	// This is the exact durable state left when initialization committed its
	// reply watermark but the process stopped before allocating trigger 0.
	if _, created, err := w.db.BootstrapTicketWatermark(t.Context(), session.ID, 0); err != nil || !created {
		t.Fatalf("persist interrupted first sight: created=%t, %v", created, err)
	}
	if pending, err := w.db.TicketBodyPending(t.Context(), session.ID); err != nil || !pending {
		t.Fatalf("body initialization not retained: pending=%t, %v", pending, err)
	}
	assertRecoveryReports(t, w.fixture, 0)

	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "interrupted-body-restarted", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("interrupted body recovery = %v, want trigger 0 once", got)
	}
	if pending, err := w.db.TicketBodyPending(t.Context(), session.ID); err != nil || pending {
		t.Fatalf("confirmed body recovery remains pending=%t, %v", pending, err)
	}
	assertRecoveryReports(t, w.fixture, 1)

	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "interrupted-body-repeated", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{0}) {
		t.Fatalf("confirmed interrupted body executed again: %v", got)
	}
	assertRecoveryReports(t, w.fixture, 1)
}

func TestTicketRecoveryReplacementSourceNotStarvedByObsoletePending(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	original := configureRecoverySource(t, w, origin)
	w.daemon = newRecoveryDaemon(t, w, origin, "old-source-recovery", false)
	w.plantCursor()
	for index := range 100 {
		if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{
			RepoID: w.repo.ID, ConnectionVersionID: original.ID, Provider: ticket.GitHub,
			Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
			TicketID: fmt.Sprintf("old-unadmitted-%d", index), TicketKey: strconv.Itoa(index + 1), ObservedAt: w.clock.peek(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A revised connection must not be hidden behind the stale source's
	// unadmitted pending rows. Nothing permits those rows to become new work.
	replacement, err := w.db.SetConnection(t.Context(), original, original.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w.issue("Recovery after source replacement")
	w.forge.addComment(testIssue, 11, owner(), "/spike process the current source")
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "replacement-source-recovery", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{11}) {
		t.Fatalf("obsolete observations starved replacement source: executions %v, want [11]", got)
	}
	snapshot, err := w.db.GetExecutionSnapshot(t.Context(), w.session().ID)
	if err != nil || snapshot.TicketVersionID != replacement.ID {
		t.Fatalf("replacement work selected wrong source: %+v, %v", snapshot, err)
	}
	assertRecoveryReports(t, w.fixture, 1)
}

func TestTicketRecoveryPendingContainerCannotMoveAfterPinning(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	source := configureRecoverySource(t, w, origin)
	source.RemoteID = ""
	source, err := w.db.SetConnection(t.Context(), source, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w.daemon = newRecoveryDaemon(t, w, origin, "unresolved-container-enrolled", false)
	w.plantCursor()
	// Keep code delivery on an independent repository so its own identity
	// check cannot accidentally mask a missing ticket-container check.
	code, err := w.db.CurrentConnection(t.Context(), w.repo.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	code.Destination, code.RemoteID = testOwnerName+"/recovery-code", "2"
	if _, err := w.db.SetConnection(t.Context(), code, code.Revision); err != nil {
		t.Fatal(err)
	}
	w.issue("Transferred ticket must retain its pinned container")
	w.forge.addComment(testIssue, 12, owner(), "/spike do not redirect this instruction")
	if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{
		RepoID: w.repo.ID, ConnectionVersionID: source.ID, Provider: ticket.GitHub,
		Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
		TicketID: strconv.Itoa(10000 + testIssue), TicketKey: strconv.Itoa(testIssue), ObservedAt: w.clock.peek(),
	}); err != nil {
		t.Fatal(err)
	}
	// Model a provider-retained ticket ID at the same locator after that
	// repository name starts identifying a different immutable container.
	w.forge.mu.Lock()
	w.forge.state(testOwnerName, testRepoName).remoteID = 3
	w.forge.mu.Unlock()
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "container-changed-restarted", false)
	w.settle()
	if got := w.turns.triggers(); len(got) != 0 {
		t.Fatalf("pending observation redirected to another container: %v", got)
	}
	position, err := w.db.GetTicketRecoveryPosition(t.Context(), source.ID)
	if err != nil || position.ContainerID != "1" {
		t.Fatalf("changed container rewrote pinned source: %+v, %v", position, err)
	}
}

const recoverySelector = "recovery-subscription"

// The shared forge fixture predates qualified tickets. Supply the immutable
// IDs and authoritative API URLs that GitHub returns at the external boundary.
func recoveryProvider(t *testing.T, f *fixture) ticket.Origin {
	t.Helper()
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/"+ownerLogin {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(owner()); err != nil {
				t.Error(err)
			}
			return
		}
		response := httptest.NewRecorder()
		f.forge.server.Config.Handler.ServeHTTP(response, r)
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		var payload any
		if json.Unmarshal(response.Body.Bytes(), &payload) != nil {
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
			return
		}
		var qualify func(any)
		qualify = func(value any) {
			switch object := value.(type) {
			case []any:
				for _, item := range object {
					qualify(item)
				}
			case map[string]any:
				if number, ok := object["number"].(float64); ok {
					object["id"] = 10000 + number
					object["url"] = origin + "/repos/" + f.repo.Slug() + "/issues/" + strconv.Itoa(int(number))
				} else if id, ok := object["id"].(float64); ok && object["body"] != nil {
					object["url"] = origin + "/repos/" + f.repo.Slug() + "/issues/comments/" + strconv.Itoa(int(id))
					issueNumber := testIssue
					parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
					if len(parts) == 6 && parts[3] == "issues" && parts[5] == "comments" {
						if n, err := strconv.Atoi(parts[4]); err == nil {
							issueNumber = n
						}
					}
					object["issue_url"] = origin + "/repos/" + f.repo.Slug() + "/issues/" + strconv.Itoa(issueNumber)
				}
			}
		}
		qualify(payload)
		w.Header().Del("Content-Length")
		w.WriteHeader(response.Code)
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode controlled GitHub response: %v", err)
		}
	}))
	origin = server.URL
	t.Cleanup(server.Close)
	return ticket.Origin(origin)
}

func configureRecoverySource(t *testing.T, w *webhookFixture, origin ticket.Origin) store.ConnectionVersion {
	t.Helper()
	w.spy.store("recovery-ticket-reader", testToken)
	w.spy.store(recoverySelector, webhookTestSecret)
	var source store.ConnectionVersion
	for _, role := range []store.ConnectionRole{store.CodeConnection, store.TicketConnection} {
		version, err := w.db.SetConnection(t.Context(), store.ConnectionVersion{
			RepoID: w.repo.ID, Role: role, Provider: ticket.GitHub, Origin: origin,
			Destination: w.repo.Slug(), RemoteID: "1", Enabled: true, Provenance: "explicit",
			CredentialSource: "keychain", CredentialService: "recovery-reader", CredentialAccount: "recovery-ticket-reader",
			ClonePath: w.repo.Path, RemoteURL: w.repo.RemoteURL, BaseBranch: w.repo.BaseBranch,
			FeaturePrefix: w.repo.FeaturePrefix, WorktreeRoot: w.repo.WorktreeRoot,
		}, 0)
		if err != nil {
			t.Fatalf("configure %s connection: %v", role, err)
		}
		if role == store.TicketConnection {
			source = version
		}
	}
	// This registered test project appoints its independently verified account.
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID})
	if _, err := (projectconnection.ApproverService{DB: w.db, Factory: factory}).Set(t.Context(), w.repo.ID, ownerLogin, source.Revision, 0); err != nil {
		t.Fatalf("verify recovery project approver: %v", err)
	}
	_, err := w.db.PutTicketSubscription(t.Context(), store.TicketSubscription{
		ID: recoverySelector, RepoID: w.repo.ID, ConnectionVersionID: source.ID,
		Provider: ticket.GitHub, Origin: origin, Container: w.repo.Slug(), ContainerID: "1",
		CallbackURL: "http://" + w.ln.Addr().String() + webhook.Path + "?subscription=" + recoverySelector,
		Events:      []string{"issues", "issue_comment", "sub_issues"}, DesiredEnabled: true,
		SecretService: daemon.WebhookKeychainService, SecretAccount: recoverySelector, SecretVersion: "1",
	}, 0)
	if err != nil {
		t.Fatalf("configure owned subscription: %v", err)
	}
	return source
}

func newRecoveryDaemon(t *testing.T, w *webhookFixture, origin ticket.Origin, instance string, listener bool) *daemon.Daemon {
	t.Helper()
	connections := projectconnection.NewFactory(projectconnection.Options{
		Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID,
	})
	identities := w.filter.Identities()
	identities.TicketOrigin = origin
	filter, err := turn.NewFilter(w.db, identities, w.logger)
	if err != nil {
		t.Fatal(err)
	}
	version, err := w.db.CurrentConnection(t.Context(), w.repo.ID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := connections.Clients(t.Context(), version, botID)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := turn.NewRoutedGitHubTicketWorkflow(w.db, clients.Tickets, botID, daemon.ProjectTicketSelector(w.db, connections))
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Logger: w.logger}), Limit: github.CommentSizeLimit, Logger: w.logger})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{
		DB: w.db, Forge: workflow, Renderer: renderer, Logger: w.logger,
		BotUserID: botID, StoryLockDir: w.paths.StoryProgressLocksDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	opts := daemon.Options{
		DefaultAgent: agent.Kind(store.AgentClaude), ToolProfiles: config.DefaultToolProfiles().Permissions,
		Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}},
		DB: w.db, Forge: clients.Code, Tickets: clients.Tickets, Filter: filter, Turn: w.turns, Reporter: reporter,
		Connections: connections, InstanceID: instance, Interval: testInterval,
		DiscoveryInterval: scheduleInterval, Ticks: w.ticks, Logger: w.logger,
		Now: w.clock.Now, ShutdownGrace: 500 * time.Millisecond,
	}
	if listener {
		opts.Webhook = &daemon.WebhookOptions{Listener: w.ln, Secrets: secrets.Resolver{Keychain: w.spy.lookup}, Now: w.clock.peek}
	}
	d, err := daemon.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func reopenRecoveryDatabase(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.db = db
	t.Cleanup(func() { _ = db.Close() })
}

func postRecoveryDelivery(t *testing.T, w *webhookFixture, delivery, signature string, body []byte) int {
	t.Helper()
	address := fmt.Sprintf("http://%s%s?subscription=%s", w.ln.Addr(), webhook.Path, recoverySelector)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", signature)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func assertRecoveryReports(t *testing.T, f *fixture, count int) {
	t.Helper()
	runs := f.runs()
	if len(runs) != count {
		t.Fatalf("durable runs = %d, want %d", len(runs), count)
	}
	for _, run := range runs {
		if run.Status != store.RunReported {
			t.Fatalf("run %d status = %s, want confirmed report", run.ID, run.Status)
		}
	}
	_, _, posts, _ := f.forge.counts()
	if posts != count {
		t.Fatalf("provider report publications = %d, want %d", posts, count)
	}
}

func TestTicketRecoveryRepositoryRefusalRetainsAndRetries(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	source := configureRecoverySource(t, w, origin)
	// The code endpoint fails independently of the readable ticket source.
	var refused atomic.Bool
	refused.Store(true)
	codeServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+w.repo.Slug() && refused.Load() {
			http.Error(rw, "repository inaccessible", http.StatusNotFound)
			return
		}
		w.forge.server.Config.Handler.ServeHTTP(rw, r)
	}))
	defer codeServer.Close()
	code, err := w.db.CurrentConnection(t.Context(), w.repo.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	code.Origin = ticket.Origin(codeServer.URL)
	if _, err := w.db.SetConnection(t.Context(), code, code.Revision); err != nil {
		t.Fatal(err)
	}

	other := w.secondRepo()
	otherFixture := *w.fixture
	otherFixture.repo = other
	otherOrigin := recoveryProvider(t, &otherFixture)
	var otherSource store.ConnectionVersion
	for _, role := range []store.ConnectionRole{store.CodeConnection, store.TicketConnection} {
		v := source
		v.ID, v.Revision, v.RepoID, v.Role = 0, 0, other.ID, role
		v.Origin, v.Destination, v.RemoteID = otherOrigin, other.Slug(), "2"
		v.ClonePath, v.RemoteURL, v.WorktreeRoot = other.Path, other.RemoteURL, other.WorktreeRoot
		v, err = w.db.SetConnection(t.Context(), v, 0)
		if err != nil {
			t.Fatal(err)
		}
		if role == store.TicketConnection {
			otherSource = v
		}
	}
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID})
	if _, err := (projectconnection.ApproverService{DB: w.db, Factory: factory}).Set(t.Context(), other.ID, ownerLogin, otherSource.Revision, 0); err != nil {
		t.Fatal(err)
	}
	w.daemon = newRecoveryDaemon(t, w, origin, "repository-refusal", false)
	w.plantCursor()
	w.issue("Repository access will recover")
	w.forge.addComment(testIssue, 901, owner(), "/spike Retry the retained instruction.")
	w.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: testIssue, Title: "Healthy project", User: owner()})
	w.forge.addCommentTo(other.Owner, other.Name, testIssue, 902, owner(), "/spike Process the healthy project.")
	for _, v := range []store.ConnectionVersion{source, otherSource} {
		if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{
			RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin,
			Container: v.Destination, ContainerID: v.RemoteID, TicketID: strconv.Itoa(10000 + testIssue),
			TicketKey: strconv.Itoa(testIssue), ObservedAt: w.clock.peek(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var completed time.Time
	for range 2 {
		result := w.settle()
		if !result.Cursor.LastTickAt.After(completed) {
			t.Fatalf("completed heartbeat did not advance: %+v", result)
		}
		completed = result.Cursor.LastTickAt
		w.clock.advance(time.Minute)
		if _, err := w.db.GetSessionByTicket(t.Context(), ticket.Identity{Provider: source.Provider, Origin: source.Origin, RemoteID: strconv.Itoa(10000 + testIssue)}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("failed admission created a session/watermark: %v", err)
		}
		pending, err := w.db.ListPendingTicketObservations(t.Context(), source.ID)
		if err != nil || len(pending) != 1 {
			t.Fatalf("lost retry: %+v %v", pending, err)
		}
	}
	if got := w.turns.triggers(); !slices.Equal(got, []int64{902}) {
		t.Fatalf("healthy comment did not run: %v\n%s", got, w.fixture.logs)
	}
	for _, want := range []string{"ticket observation cannot be admitted", fmt.Sprintf("project_id=%d", w.repo.ID), fmt.Sprintf("connection_version_id=%d", source.ID), string(source.Origin), strconv.Itoa(10000 + testIssue), "inspect repository", "404"} {
		if !strings.Contains(w.fixture.logs.String(), want) {
			t.Errorf("missing diagnostic %q: %s", want, w.fixture.logs)
		}
	}
	// No ticket timestamp change and no new comment or observation.
	refused.Store(false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{901, 902}) {
		t.Fatalf("retained instruction did not recover: %v\n%s", got, w.fixture.logs)
	}
	w.settle() // Ordinary follow-up drain consumes the now reported instruction.
	pending, err := w.db.ListPendingTicketObservations(t.Context(), source.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("successful drain retained observation: %+v %v", pending, err)
	}
}

func TestTicketRecoveryDoctorHoldSurvivesRestart(t *testing.T) {
	w := newWebhookFixture(t)
	w.clock.mu.Lock()
	w.clock.now = time.Now().UTC().Add(time.Minute)
	w.clock.mu.Unlock()
	origin := recoveryProvider(t, w.fixture)
	source := configureRecoverySource(t, w, origin)
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{Keychain: w.spy.lookup}, Logger: w.logger, ExpectedActorID: botID})
	w.issue("Held task")
	ref := ticket.Ref{Identity: ticket.Identity{Provider: source.Provider, Origin: origin, RemoteID: strconv.Itoa(10000 + testIssue)}, Locator: ticket.Locator{Provider: source.Provider, Origin: origin, Container: source.Destination, Key: strconv.Itoa(testIssue)}}
	if _, err := w.db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: w.repo.ID, ConnectionVersionID: source.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	held, err := daemon.AdmitProjectTicket(t.Context(), w.db, factory, w.repo, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.db.BootstrapTicketWatermark(t.Context(), held.ID, 0); err != nil {
		t.Fatal(err)
	}
	reserveRecoveryHold(t, w.db, held)
	w.forge.addComment(testIssue, 901, owner(), "/spike Recover this held instruction.")
	w.forge.addIssue(fakeIssue{Number: testIssue + 1, Title: "Healthy sibling", User: owner()})
	w.forge.addComment(testIssue+1, 902, owner(), "/spike Process the healthy sibling.")
	other := w.secondRepo()
	otherFixture := *w.fixture
	otherFixture.repo = other
	otherOrigin := recoveryProvider(t, &otherFixture)
	var otherSource store.ConnectionVersion
	for _, role := range []store.ConnectionRole{store.CodeConnection, store.TicketConnection} {
		v := source
		v.ID, v.Revision, v.RepoID, v.Role = 0, 0, other.ID, role
		v.Origin, v.Destination, v.RemoteID = otherOrigin, other.Slug(), "2"
		v.ClonePath, v.RemoteURL, v.WorktreeRoot = other.Path, other.RemoteURL, other.WorktreeRoot
		v, err = w.db.SetConnection(t.Context(), v, 0)
		if err != nil {
			t.Fatal(err)
		}
		if role == store.TicketConnection {
			otherSource = v
		}
	}
	if _, err := (projectconnection.ApproverService{DB: w.db, Factory: factory}).Set(t.Context(), other.ID, ownerLogin, otherSource.Revision, 0); err != nil {
		t.Fatal(err)
	}
	w.forge.addIssueIn(other.Owner, other.Name, fakeIssue{Number: testIssue, Title: "Healthy project", User: owner()})
	w.forge.addCommentTo(other.Owner, other.Name, testIssue, 903, owner(), "/spike Process the healthy project.")
	// Discovery is already beyond these updates. Only the durable observations
	// can recover the instructions, including after reopening the database.
	through := w.clock.peek().Add(time.Hour)
	for _, v := range []store.ConnectionVersion{source, otherSource} {
		p, err := w.db.InitTicketRecoveryPosition(t.Context(), v, v.CreatedAt)
		if err != nil {
			t.Fatal(err)
		}
		p, err = w.db.PinTicketRecoveryContainer(t.Context(), v.ID, p.Revision, v.RemoteID)
		if err != nil {
			t.Fatal(err)
		}
		p.CompletedThrough, p.ScanThrough, p.ReconcileCompletedAt = through, through, through
		p.TicketsComplete, p.RepliesComplete = true, true
		if _, err = w.db.CommitTicketRecoveryPage(t.Context(), p, p.Revision, nil); err != nil {
			t.Fatal(err)
		}
		numbers := []int{testIssue}
		if v.ID == source.ID {
			numbers = append(numbers, testIssue+1)
		}
		for _, n := range numbers {
			if err := w.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: strconv.Itoa(10000 + n), TicketKey: strconv.Itoa(n), ObservedAt: through}); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.clock.advance(time.Hour)
	w.daemon = newRecoveryDaemon(t, w, origin, "held-recovery", false)
	for range 2 {
		w.settle()
		pending, err := w.db.ListPendingTicketObservations(t.Context(), source.ID)
		var retained bool
		for _, p := range pending {
			if p.TicketID == ref.Identity.RemoteID {
				retained = p.Generation == 1
			}
		}
		if err != nil || !retained {
			t.Fatalf("held generation lost: %+v %v", pending, err)
		}
		watermark, exists, err := w.db.GetWatermark(t.Context(), held.ID)
		if err != nil || !exists || watermark.LastCommentID != 0 {
			t.Fatalf("held watermark advanced: %+v %v", watermark, err)
		}
	}
	if got := w.turns.triggers(); !slices.Equal(got, []int64{902, 903}) {
		t.Fatalf("unrelated work blocked: %v\n%s", got, w.fixture.logs)
	}
	reopenRecoveryDatabase(t, w.fixture)
	w.daemon = newRecoveryDaemon(t, w, origin, "held-restarted", false)
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{902, 903}) {
		t.Fatalf("held worker ran after restart: %v", got)
	}
	if err := w.db.StopDoctorRepair(t.Context(), held.TaskRef, "release fixture hold"); err != nil {
		t.Fatal(err)
	}
	w.settle()
	if got := w.turns.triggers(); !slices.Equal(got, []int64{901, 902, 903}) {
		t.Fatalf("release did not recover instruction: %v\n%s", got, w.fixture.logs)
	}
	w.settle()
	pending, err := w.db.ListPendingTicketObservations(t.Context(), source.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("successful drain did not clear: %+v %v", pending, err)
	}
}

func reserveRecoveryHold(t *testing.T, db *store.DB, session store.Session) {
	t.Helper()
	ctx := t.Context()
	conversation, err := db.BeginDoctorAttempt(ctx, session.TaskRef, store.DoctorAttempt{ID: "recovery-hold", SourceID: "recovery-hold", OwnerText: "fixture", PromptVersion: "test", GuideVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(store.DoctorEvidence{TaskRef: session.TaskRef})
	if err := db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "recovery-hold", Provider: "claude", ConfiguredModel: "test", Evidence: string(evidence)}); err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, "recovery-hold")
	if err != nil {
		t.Fatal(err)
	}
	proposal := []byte(fmt.Sprintf(`[{"number":1,"target":"session:%d","action":{"kind":"poll_flag","before":"0","after":"1"}}]`, session.ID))
	snapshot := append(append([]byte(`{"proposal":`), proposal...), '}')
	if _, err := db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "recovery-hold-result", Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.AdoptDoctorResult(ctx, binding, "recovery-hold-result", proposal); err != nil {
		t.Fatal(err)
	}
	id, _, err := db.DoctorProposalReference(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PresentDoctorProposal(ctx, session.TaskRef, "recovery-hold-presentation", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "recovery-hold", SourceID: "recovery-hold-approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: "recovery-hold-presentation"}); err != nil {
		t.Fatal(err)
	}
}
