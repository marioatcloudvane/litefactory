package owneraction

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type actionDomainFixture struct {
	facts            Facts
	stale            bool
	calls            int
	result           Result
	recoverResult    Result
	recovered        bool
	started, release chan struct{}
}

func (d *actionDomainFixture) Resolve(_ context.Context, _ Principal, in Invocation, _ time.Time) (Facts, error) {
	f := d.facts
	f.Invocation = in
	return f, nil
}
func (d *actionDomainFixture) Recheck(context.Context, Principal, Facts, time.Time) error {
	if d.stale {
		return errors.New("changed")
	}
	return nil
}
func (d *actionDomainFixture) Execute(ctx context.Context, _ Principal, _ Facts, _ string, _ time.Time) (Result, error) {
	d.calls++
	if d.started != nil {
		close(d.started)
		select {
		case <-d.release:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	return d.result, nil
}
func (d *actionDomainFixture) Recover(context.Context, Principal, Facts, string, time.Time) (Result, bool, error) {
	return d.recoverResult, d.recovered, nil
}

type actionDeliveryFixture struct {
	calls   int
	message string
	err     error
}

func (d *actionDeliveryFixture) Send(context.Context, Proposal) (string, error) {
	d.calls++
	return d.message, d.err
}

func actionServiceFixture(t *testing.T) (*Service, *actionDomainFixture, Principal, *time.Time) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: "github", Owner: "acme", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	domain := &actionDomainFixture{facts: Facts{RepositoryID: repo.ID, RepositoryRef: "github:acme/web", TargetKind: "task", TargetID: "github:acme/web#3", TargetRevision: "reviewed", Summary: "Fixed action", Payload: `{"target":"reviewed"}`}, result: Result{State: "succeeded", Summary: "Confirmed external completion"}}
	service := New(db, domain, func() time.Time { return at })
	service.SetDiscord("100", []string{"200"})
	p := Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "100", ChannelID: "200"}
	return service, domain, p, &at
}

func proposeDelivered(t *testing.T, s *Service, p Principal, inv Invocation, key string) Proposal {
	t.Helper()
	proposal, err := s.Propose(t.Context(), p, inv, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Available(t.Context(), proposal.ID, "confirmed-proposal-message"); err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestActionConfirmationTimeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name          string
		elapsed       time.Duration
		state, reason string
	}{{"too early", 5*time.Second - time.Nanosecond, "awaiting_confirmation", "too_early"}, {"minimum inclusive", 5 * time.Second, "admitted", "admitted"}, {"expiry exclusive", 15 * time.Minute, "expired", "expired_repropose"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p, at := actionServiceFixture(t)
			proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "key")
			*at = at.Add(tc.elapsed)
			out, err := s.Confirm(t.Context(), p, proposal.ID)
			if err != nil || out.State != tc.state || out.Reason != tc.reason {
				t.Fatalf("boundary: %+v %v", out, err)
			}
			if !out.ExpiresAt.Equal(proposal.ExpiresAt) {
				t.Fatal("confirmation extended expiry")
			}
		})
	}
}

func TestActionBindingNeedsExactOwnerEvidence(t *testing.T) {
	s, domain, p, at := actionServiceFixture(t)
	delivery := &actionDeliveryFixture{message: "approval-500"}
	s.SetDelivery(delivery)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "confirm"}, "binding")
	*at = at.Add(5 * time.Second)
	out, err := s.Confirm(t.Context(), p, proposal.ID)
	if err != nil || out.State != "awaiting_discord" {
		t.Fatalf("confirmation bypassed tap: %+v %v", out, err)
	}
	out, err = s.Step(t.Context(), proposal.ID)
	if err != nil || out.DeliveryState != "delivered" || domain.calls != 0 {
		t.Fatalf("delivery ran effect: %+v %v", out, err)
	}
	good := Reaction{ChannelID: "200", MessageID: "approval-500", UserID: "100", Emoji: "✅"}
	for _, mutate := range []func(*Reaction){func(r *Reaction) { r.UserID = "other" }, func(r *Reaction) { r.Bot = true }, func(r *Reaction) { r.EmojiID = "custom" }, func(r *Reaction) { r.Emoji = "☑" }} {
		bad := good
		mutate(&bad)
		if _, err = s.Reaction(t.Context(), bad); err == nil {
			t.Fatal("unauthorized reaction accepted")
		}
	}
	out, err = s.Reaction(t.Context(), good)
	if err != nil || out.State != "admitted" {
		t.Fatalf("owner evidence refused: %+v %v", out, err)
	}
	out, err = s.Step(t.Context(), proposal.ID)
	if err != nil || out.State != "succeeded" || domain.calls != 1 {
		t.Fatalf("effect: %+v %v calls=%d", out, err, domain.calls)
	}
	if _, err = s.Reaction(t.Context(), good); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Step(t.Context(), proposal.ID); err != nil || domain.calls != 1 {
		t.Fatalf("replayed effect: %v calls=%d", err, domain.calls)
	}
	inspected, err := s.Inspect(t.Context(), p, proposal.ID)
	if err != nil || len(inspected.Attempts) != 7 {
		t.Fatalf("durable decisions: %+v %v", inspected.Attempts, err)
	}
}

func TestActionCancelledOrStaleNeverExecutes(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale", true: "cancelled"}[cancel], func(t *testing.T) {
			s, d, p, at := actionServiceFixture(t)
			proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "key")
			*at = at.Add(5 * time.Second)
			var out Proposal
			var err error
			if cancel {
				out, err = s.Cancel(t.Context(), p, proposal.ID)
			} else {
				d.stale = true
				out, err = s.Confirm(t.Context(), p, proposal.ID)
			}
			if err != nil || out.State != map[bool]string{true: "cancelled", false: "invalidated"}[cancel] {
				t.Fatalf("pending terminal: %+v %v", out, err)
			}
			if _, err = s.Step(t.Context(), proposal.ID); err != nil || d.calls != 0 {
				t.Fatalf("terminal work executed: %v calls=%d", err, d.calls)
			}
		})
	}
}

func TestActionExecutionOwnsRequestCancellationAndConcurrentStep(t *testing.T) {
	s, d, p, at := actionServiceFixture(t)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "key")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	d.started = make(chan struct{})
	d.release = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var first Proposal
	var firstErr error
	// One goroutine is joined below and bounded by the test timeout.
	go func() { defer wg.Done(); first, firstErr = s.Step(ctx, proposal.ID) }()
	select {
	case <-d.started:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not start")
	}
	concurrent, err := s.Step(t.Context(), proposal.ID)
	if err != nil || concurrent.State != "executing" {
		t.Fatalf("concurrent reader relabeled active effect: %+v %v", concurrent, err)
	}
	close(d.release)
	wg.Wait()
	if firstErr != nil || first.State != "succeeded" || d.calls != 1 {
		t.Fatalf("request cancellation lost durable work: %+v %v", first, firstErr)
	}
}

func TestActionRestartRecoversUncertaintyWithoutReplay(t *testing.T) {
	s, d, p, at := actionServiceFixture(t)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "key")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.db.ClaimOwnerActionExecution(t.Context(), proposal.ID, *at); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	restarted := New(s.db, d, s.clock)
	restarted.SetDiscord("100", []string{"200"})
	out, err := restarted.Step(t.Context(), proposal.ID)
	if err != nil || out.State != "uncertain" || d.calls != 0 {
		t.Fatalf("restart replay: %+v %v", out, err)
	}
	*at = at.Add(100 * 24 * time.Hour)
	if err = s.db.PurgeOwnerActionHistory(t.Context(), *at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ReadOwnerAction(t.Context(), proposal.ID); err != nil {
		t.Fatalf("unresolved recovery purged: %v", err)
	}
	d.recovered = true
	d.recoverResult = Result{State: "succeeded", Summary: "Recovered confirmed receipt"}
	out, err = restarted.Step(t.Context(), proposal.ID)
	if err != nil || out.State != "succeeded" || d.calls != 0 {
		t.Fatalf("receipt recovery: %+v %v", out, err)
	}
}

func TestActionAvailabilityStartsAfterResolutionAndNeverResets(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	proposal, err := s.Propose(t.Context(), p, Invocation{Command: "diagnose"}, "key")
	if err != nil {
		t.Fatal(err)
	}
	*at = at.Add(10 * time.Second)
	if err = s.Available(t.Context(), proposal.ID, "first-message"); err != nil {
		t.Fatal(err)
	}
	if err = s.Available(t.Context(), proposal.ID, "replacement-message"); err != nil {
		t.Fatal(err)
	}
	out, err := s.Inspect(t.Context(), p, proposal.ID)
	if err != nil || !out.AvailableAt.Equal(*at) || !out.ConfirmNotBefore.Equal(at.Add(5*time.Second)) || out.ProposalMessage != "first-message" || !out.ExpiresAt.Equal(proposal.ExpiresAt) {
		t.Fatalf("availability rebound: %+v %v", out, err)
	}
}

func TestActionHistoryPaginationBindsOwnerFiltersAndEvidence(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	first := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "first")
	*at = at.Add(time.Second)
	second := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "second")
	if _, err := s.Cancel(t.Context(), p, second.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.History(t.Context(), p, "", 1)
	if err != nil || len(page.Proposals) != 1 || page.Proposals[0].ID != second.ID || len(page.Proposals[0].Attempts) != 1 || page.NextCursor == "" {
		t.Fatalf("history page: %+v %v", page, err)
	}
	next, err := s.History(t.Context(), p, page.NextCursor, 1)
	if err != nil || len(next.Proposals) != 1 || next.Proposals[0].ID != first.ID {
		t.Fatalf("stable next page: %+v %v", next, err)
	}
	if _, err = s.HistoryFiltered(t.Context(), p, Filters{State: "cancelled"}, page.NextCursor, 1); err == nil {
		t.Fatal("cursor adopted different filter")
	}
	other := p
	other.OwnerID = "other"
	if _, err = s.History(t.Context(), other, page.NextCursor, 1); err == nil {
		t.Fatal("cursor adopted different owner")
	}
}

func TestActionLostApprovalSendHasVisibleUncertaintyWithoutResend(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "confirm"}, "binding")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.db.ClaimOwnerActionDelivery(t.Context(), proposal.ID, *at); err != nil || !claimed {
		t.Fatalf("delivery intent: %v %v", claimed, err)
	}
	delivery := &actionDeliveryFixture{message: "would-be-duplicate"}
	s.SetDelivery(delivery)
	out, err := s.Step(t.Context(), proposal.ID)
	if err != nil || out.DeliveryState != "uncertain" || out.Reason != "discord_delivery_unconfirmed_no_execution" || delivery.calls != 0 {
		t.Fatalf("lost send blindly retried: %+v %v", out, err)
	}
}

func TestActionKnownRateLimitRetriesAreBoundedAndRespectTime(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	delivery := &actionDeliveryFixture{err: &DeliveryRetryError{RetryAfter: 10 * time.Second}}
	s.SetDelivery(delivery)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "confirm"}, "rate-limited")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		out, err := s.Step(t.Context(), proposal.ID)
		if err != nil || delivery.calls != attempt || out.DeliveryAttempts != attempt {
			t.Fatalf("delivery attempt %d: %+v %v calls=%d", attempt, out, err, delivery.calls)
		}
		if attempt == 3 {
			if out.DeliveryState != "failed" || out.State != "awaiting_discord" {
				t.Fatalf("retry limit bypassed: %+v", out)
			}
			break
		}
		if out.DeliveryState != "pending" || !out.DeliveryRetryAt.Equal(at.Add(10*time.Second)) {
			t.Fatalf("retry clock: %+v", out)
		}
		if _, err = s.Step(t.Context(), proposal.ID); err != nil || delivery.calls != attempt {
			t.Fatalf("retried before provider time: %v calls=%d", err, delivery.calls)
		}
		*at = at.Add(10 * time.Second)
	}
}

func TestActionReactionCannotAdmitAfterPairingChangeOrExpiry(t *testing.T) {
	for _, tc := range []string{"pairing", "expiry", "future intake"} {
		t.Run(tc, func(t *testing.T) {
			s, d, p, at := actionServiceFixture(t)
			s.SetDelivery(&actionDeliveryFixture{message: "approval"})
			proposal := proposeDelivered(t, s, p, Invocation{Command: "confirm"}, "binding")
			*at = at.Add(5 * time.Second)
			if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Step(t.Context(), proposal.ID); err != nil {
				t.Fatal(err)
			}
			r := Reaction{ChannelID: "200", MessageID: "approval", UserID: "100", Emoji: "✅", ReceivedAt: *at}
			switch tc {
			case "pairing":
				s.SetDiscord("replacement", []string{"200"})
			case "expiry":
				*at = proposal.ExpiresAt
				r.ReceivedAt = *at
			case "future intake":
				r.ReceivedAt = at.Add(time.Second)
			}
			out, err := s.Reaction(t.Context(), r)
			if tc == "expiry" {
				if err != nil || out.State != "expired" {
					t.Fatalf("expiry reaction: %+v %v", out, err)
				}
			} else if err == nil {
				t.Fatalf("unauthorized intake accepted: %+v", out)
			}
			if d.calls != 0 {
				t.Fatal("reaction executed effect")
			}
		})
	}
}

func actionMCPPrincipal(t *testing.T, s *Service, p Principal, id string, scope string) Principal {
	t.Helper()
	at := s.now()
	client := "client-" + id
	if err := s.db.RegisterConnectorClient(t.Context(), store.ConnectorClient{ID: client, Name: "Test client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}, at); err != nil {
		t.Fatal(err)
	}
	b := store.ConnectorBrowser{ID: "browser-" + id, ClientID: client, RedirectURI: "https://client.test/callback", State: "state", Resource: "https://factory.test/mcp", Scope: scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: p.OwnerGeneration, ExpiresAt: at.Add(10 * time.Minute)}
	if err := s.db.CreateConnectorBrowser(t.Context(), b, 42, at); err != nil {
		t.Fatal(err)
	}
	if err := s.db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, at); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: id, ClientID: client, Resource: b.Resource, Scope: scope, OwnerID: 42, OwnerGeneration: p.OwnerGeneration, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	if store.ConnectorScopeHas(scope, "repository:actions") {
		grant.ActionCoverage = &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: 1, RepositoryRef: "github:acme/web"}}}
	}
	code := store.ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: client, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	if err := s.db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, at, grant, code, true); err != nil {
		t.Fatal(err)
	}
	return Principal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: "mcp", ClientID: client, GrantID: id}
}

func TestActionMCPOriginAndCurrentHistoryAuthority(t *testing.T) {
	s, _, discord, at := actionServiceFixture(t)
	origin := actionMCPPrincipal(t, s, discord, "origin", "repository:actions")
	proposal, err := s.Propose(t.Context(), origin, Invocation{Command: "diagnose"}, "original")
	if err != nil {
		t.Fatal(err)
	}
	replacement := actionMCPPrincipal(t, s, discord, "replacement", "repository:actions")
	if _, err = s.Confirm(t.Context(), replacement, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("replacement grant adopted pending work: %v", err)
	}
	inspected, err := s.Inspect(t.Context(), replacement, proposal.ID)
	if err != nil || inspected.Principal.GrantID != origin.GrantID {
		t.Fatalf("current grant cannot inspect original history: %+v %v", inspected, err)
	}
	// Token refresh preserves this authenticated grant/client identity.
	refreshed := origin
	*at = at.Add(5 * time.Second)
	out, err := s.Confirm(t.Context(), refreshed, proposal.ID)
	if err != nil || out.State != "admitted" {
		t.Fatalf("same-grant refreshed identity refused: %+v %v", out, err)
	}
	if err = s.db.RevokeConnectorGrant(t.Context(), 42, origin.GrantID, *at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Propose(t.Context(), origin, Invocation{Command: "diagnose"}, "original"); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("revoked idempotent replay disclosed facts: %v", err)
	}
	if _, err = s.Inspect(t.Context(), replacement, proposal.ID); err != nil {
		t.Fatalf("replacement history wrongly relied on revoked origin: %v", err)
	}
}

func TestActionRevokedMCPGrantCannotBeRevivedByDiscordTap(t *testing.T) {
	s, d, discord, at := actionServiceFixture(t)
	origin := actionMCPPrincipal(t, s, discord, "origin", "repository:actions")
	s.SetDelivery(&actionDeliveryFixture{message: "approval"})
	proposal, err := s.Propose(t.Context(), origin, Invocation{Command: "confirm"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	*at = at.Add(5 * time.Second)
	if _, err = s.Confirm(t.Context(), origin, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.db.RevokeConnectorGrant(t.Context(), 42, origin.GrantID, *at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reaction(t.Context(), Reaction{ChannelID: "200", MessageID: "approval", UserID: "100", Emoji: "✅", ReceivedAt: *at}); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("tap revived revoked grant: %v", err)
	}
	if _, err = s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked reaction admitted effect: %v", err)
	}
	if d.calls != 0 {
		t.Fatal("revoked effect ran")
	}
}

func TestActionHistoryCursorCannotMoveToReplacementGrant(t *testing.T) {
	s, _, discord, _ := actionServiceFixture(t)
	origin := actionMCPPrincipal(t, s, discord, "origin", "repository:actions")
	replacement := actionMCPPrincipal(t, s, discord, "replacement", "repository:actions")
	if _, err := s.Propose(t.Context(), origin, Invocation{Command: "diagnose"}, "first"); err != nil {
		t.Fatal(err)
	}
	page, err := s.History(t.Context(), origin, "", 1)
	if err != nil || page.NextCursor == "" {
		t.Fatalf("first cursor: %+v %v", page, err)
	}
	if _, err = s.History(t.Context(), replacement, page.NextCursor, 1); err == nil {
		t.Fatal("history cursor adopted replacement grant")
	}
}

func TestActionDigestRejectsChangedProtectedInput(t *testing.T) {
	s, _, p, _ := actionServiceFixture(t)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "key")
	record, err := s.db.ReadOwnerAction(t.Context(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload = `{"target":"different"}`
	if _, err = s.present(record); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("changed protected payload accepted: %v", err)
	}
	record, err = s.db.ReadOwnerAction(t.Context(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Facts = `{"invocation":{"command":"diagnose"},"target_revision":"different"}`
	if _, err = s.present(record); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("changed target facts accepted: %v", err)
	}
}

func TestActionDelayedReactionCannotBackdateAdmission(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	s.SetDelivery(&actionDeliveryFixture{message: "approval"})
	proposal := proposeDelivered(t, s, p, Invocation{Command: "confirm"}, "binding")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(t.Context(), proposal.ID); err != nil {
		t.Fatal(err)
	}
	received := *at
	*at = proposal.ExpiresAt
	out, err := s.Reaction(t.Context(), Reaction{ChannelID: "200", MessageID: "approval", UserID: "100", Emoji: "✅", ReceivedAt: received})
	if err != nil || out.State != "expired" {
		t.Fatalf("old receipt backdated admission: %+v %v", out, err)
	}
}

func TestActionMinimumDelayRetainsSubmillisecondPrecision(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	*at = at.Add(123456789 * time.Nanosecond)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "precise")
	*at = at.Add(5*time.Second - time.Nanosecond)
	out, err := s.Confirm(t.Context(), p, proposal.ID)
	if err != nil || out.Reason != "too_early" {
		t.Fatalf("fractional delay truncated: %+v %v", out, err)
	}
	*at = at.Add(time.Nanosecond)
	out, err = s.Confirm(t.Context(), p, proposal.ID)
	if err != nil || out.State != "admitted" {
		t.Fatalf("exact minimum refused: %+v %v", out, err)
	}
}

func TestActionDaemonShutdownRecordsUncertainty(t *testing.T) {
	s, d, p, at := actionServiceFixture(t)
	proposal := proposeDelivered(t, s, p, Invocation{Command: "diagnose"}, "shutdown")
	*at = at.Add(5 * time.Second)
	if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.SetLifetime(lifetime)
	d.started = make(chan struct{})
	d.release = make(chan struct{})
	done := make(chan error, 1)
	// The domain exits on the owned lifetime and the result is joined below.
	go func() {
		out, err := s.Step(t.Context(), proposal.ID)
		if err == nil && out.State != "uncertain" {
			err = errors.New("shutdown did not preserve uncertainty")
		}
		done <- err
	}()
	select {
	case <-d.started:
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon execution ignored shutdown")
	}
	if d.calls != 1 {
		t.Fatal("shutdown replayed effect")
	}
}
