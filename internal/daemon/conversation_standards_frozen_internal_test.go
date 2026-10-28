package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type frozenAuthorRunner struct{ calls int }

func (r *frozenAuthorRunner) Answer(context.Context, standards.AuthoringCall) ([]byte, error) {
	r.calls++
	return []byte(cleanDraft), nil
}

type frozenStandardsDomain struct{ adapter *ConversationStandards }

func frozenStandardsRequest(p owneraction.Principal, f owneraction.Facts, key string, at time.Time) messaging.StandardsRequest {
	kind := strings.TrimPrefix(f.Invocation.Command, "standards-")
	return messaging.StandardsRequest{Operation: kind + "." + f.Invocation.Arguments["action"], Arguments: f.Invocation.Arguments, OperationKey: key, At: at}
}
func (d frozenStandardsDomain) Resolve(ctx context.Context, p owneraction.Principal, i owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	return d.adapter.ResolveFrozen(ctx, p, frozenStandardsRequest(p, owneraction.Facts{Invocation: i}, "", at))
}
func (d frozenStandardsDomain) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	return d.adapter.RecheckFrozen(ctx, p, frozenStandardsRequest(p, f, "", at), f)
}
func (d frozenStandardsDomain) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	out, e := d.adapter.ExecuteFrozen(ctx, p, frozenStandardsRequest(p, f, key, at), f)
	state := "succeeded"
	if out.Interrupted {
		state = "uncertain"
	}
	if out.Status != "" {
		state = out.Status
	}
	return owneraction.Result{State: state, Summary: out.Text}, e
}
func (d frozenStandardsDomain) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	out, found, e := d.adapter.RecoverFrozen(ctx, p, frozenStandardsRequest(p, f, key, at), f)
	state := "succeeded"
	if out.Interrupted {
		state = "uncertain"
	}
	return owneraction.Result{State: state, Summary: out.Text}, found, e
}

type frozenApprovalDelivery struct{}

func (frozenApprovalDelivery) Send(context.Context, owneraction.Proposal) (string, error) {
	return "900", nil
}

func frozenStandardsFixture(t *testing.T) (*ConversationStandards, *owneraction.Service, owneraction.Principal, *time.Time, *frozenAuthorRunner) {
	t.Helper()
	db := authorDB(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 7, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ReconcileConnectorOwner(t.Context(), 7, at)
	if err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 7}
	runner := &frozenAuthorRunner{}
	a := &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: runner}, OwnerID: "17", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	service := owneraction.New(db, frozenStandardsDomain{a}, func() time.Time { return at })
	service.SetDiscord("17", []string{"3"})
	service.SetDelivery(frozenApprovalDelivery{})
	principal := owneraction.Principal{OwnerID: "7", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "17", ChannelID: "3"}
	return a, service, principal, &at, runner
}

func frozenPropose(t *testing.T, service *owneraction.Service, p owneraction.Principal, at *time.Time, command string, args map[string]string, key string) owneraction.Proposal {
	t.Helper()
	proposal, err := service.Propose(t.Context(), p, owneraction.Invocation{Command: command, Arguments: args}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Available(t.Context(), proposal.ID, "800"); err != nil {
		t.Fatal(err)
	}
	*at = at.Add(5 * time.Second)
	proposal, err = service.Confirm(t.Context(), p, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Tier == owneraction.Binding {
		_, err = service.Step(t.Context(), proposal.ID)
		if err != nil {
			t.Fatal(err)
		}
		proposal, err = service.Reaction(t.Context(), owneraction.Reaction{ChannelID: "3", MessageID: "900", UserID: "17", Emoji: "✅"})
		if err != nil {
			t.Fatal(err)
		}
	}
	proposal, err = service.Step(t.Context(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestFrozenStandardsAuthorVersionAndBaseCannotAdvance(t *testing.T) {
	a, _, p, at, _ := frozenStandardsFixture(t)
	draft, err := a.Authoring.Start(t.Context(), standards.AuthoringStart{ID: "saved", Mode: "create", Intent: "Keep tests short"})
	if err != nil {
		t.Fatal(err)
	}
	r := messaging.StandardsRequest{Operation: "author.edit", Arguments: map[string]string{"id": draft.ID, "rule": "MUST keep tests bounded"}, At: *at}
	facts, err := a.ResolveFrozen(t.Context(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Invocation.Arguments["version"] != strconv.FormatInt(draft.Version, 10) || facts.Invocation.Arguments["base"] != "0" {
		t.Fatalf("unfixed draft: %+v", facts)
	}
	change := "Another owner edit"
	if _, err = a.Authoring.Edit(t.Context(), draft.ID, draft.Version, standards.AuthoringEdit{Description: &change}); err != nil {
		t.Fatal(err)
	}
	if err = a.RecheckFrozen(t.Context(), p, r, facts); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("newer draft adopted: %v", err)
	}
	if _, err = a.ResolveFrozen(t.Context(), p, messaging.StandardsRequest{Operation: "author.edit", Arguments: map[string]string{"id": draft.ID, "version": "1", "rule": "MUST use exact targets"}}); !errors.Is(err, store.ErrStandardsConflict) {
		t.Fatalf("explicit stale version accepted: %v", err)
	}
}

func TestFrozenStandardsPublicationRetainsExactCompositeReceipts(t *testing.T) {
	a, service, p, at, runner := frozenStandardsFixture(t)
	started := frozenPropose(t, service, p, at, "standards-author", map[string]string{"action": "start", "id": "saved", "intent": "Keep tests short"}, "start")
	if started.State != "succeeded" {
		t.Fatalf("start: %+v", started)
	}
	draft, err := a.Authoring.Show(t.Context(), "saved", 0)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || draft.Version != 2 || !draft.Ready() {
		t.Fatalf("derived draft missing: calls=%d %+v", runner.calls, draft)
	}
	published := frozenPropose(t, service, p, at, "standards-author", map[string]string{"action": "publish", "id": "saved"}, "publish")
	if published.State != "succeeded" {
		t.Fatalf("publish: %+v", published)
	}
	for _, suffix := range []string{"/complete", "/approve", "/publish"} {
		receipt, e := a.DB.GetStandardsConversationOperation(t.Context(), published.ExecutionID+suffix)
		if e != nil || receipt.Status != "completed" || !receipt.EffectCommitted {
			t.Fatalf("substep %s: %+v %v", suffix, receipt, e)
		}
	}
	book, err := a.Book.Current(t.Context())
	if err != nil || book.Publication == nil || book.Publication.Provenance != published.ExecutionID {
		t.Fatalf("publication provenance: %+v %v", book.Publication, err)
	}
	if _, err = service.Step(t.Context(), published.ID); err != nil {
		t.Fatal(err)
	}
	history, err := a.Book.History(t.Context())
	if err != nil || len(history) != 1 || runner.calls != 1 {
		t.Fatalf("replayed effect: revisions=%d calls=%d %v", len(history), runner.calls, err)
	}
}

func TestFrozenStandardsRecoveryDoesNotLaunchSavedUnstartedWork(t *testing.T) {
	a, _, p, at, runner := frozenStandardsFixture(t)
	request := messaging.StandardsRequest{Operation: "author.start", Arguments: map[string]string{"id": "unstarted", "intent": "Bound checks"}, OperationKey: "owner-action:saved", At: *at}
	facts, err := a.ResolveFrozen(t.Context(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	request = a.frozenRequest(p, request, facts)
	if _, _, done, err := a.admit(t.Context(), request); err != nil || done {
		t.Fatalf("saved request: %v %v", done, err)
	}
	result, found, err := a.RecoverFrozen(t.Context(), p, request, facts)
	if err != nil || !found || !result.Interrupted {
		t.Fatalf("recovery: %+v %v %v", result, found, err)
	}
	if runner.calls != 0 {
		t.Fatalf("recovery launched provider %d times", runner.calls)
	}
	if _, err := a.Authoring.Show(t.Context(), "unstarted", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recovery created a draft: %v", err)
	}
}
