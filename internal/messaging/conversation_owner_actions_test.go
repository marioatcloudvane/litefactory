package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

type selectionDomain struct{}

func (selectionDomain) Resolve(_ context.Context, _ owneraction.Principal, in owneraction.Invocation, _ time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{Invocation: in, TargetKind: "author", TargetID: in.Arguments["id"], TargetRevision: "version 1", Summary: "Edit reviewed author", Estimate: "unknown", Payload: `{"exact":true}`}, nil
}
func (selectionDomain) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return nil
}
func (selectionDomain) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	return owneraction.Result{State: "succeeded"}, nil
}
func (selectionDomain) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, bool, error) {
	return owneraction.Result{}, false, nil
}

func sharedSelectionFixture(t *testing.T) (*Service, *owneraction.Service, owneraction.Principal, *time.Time, func(string, time.Time) *factoryConversationRun, *standardsDeliverySender) {
	t.Helper()
	s, _, start, sender := plainFixture(t)
	if _, err := s.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := s.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "7", ChannelID: "3"}
	actions := owneraction.New(s.db, selectionDomain{}, func() time.Time { return at })
	actions.SetDiscord("7", []string{"3"})
	s.EnableOwnerActions(actions, func(context.Context, string) (owneraction.Principal, error) { return principal, nil })
	return s, actions, principal, &at, start, sender
}

func TestSharedConversationBindsDeliveryBeforePlainYes(t *testing.T) {
	s, actions, principal, at, start, sender := sharedSelectionFixture(t)
	run := start("edit that author", *at)
	if err := run.propose(t.Context(), Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "id": "author-a"}}, ConversationEnvelope{}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := actions.History(t.Context(), principal, "", 10)
	if err != nil || len(page.Proposals) != 1 {
		t.Fatalf("proposal: %+v %v", page, err)
	}
	proposal := page.Proposals[0]
	if !proposal.AvailableAt.IsZero() {
		t.Fatal("undelivered proposal available")
	}
	deliverAll(t, s, sender, "500", *at)
	saved, err := actions.Inspect(t.Context(), principal, proposal.ID)
	if err != nil || saved.ProposalMessage != "500" || saved.ConfirmNotBefore != at.Add(5*time.Second) {
		t.Fatalf("wrong delivery binding: %+v %v", saved, err)
	}
	*at = at.Add(4 * time.Second)
	run = start("yes", *at)
	run.t.ReplyTo = "500"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("early confirm: %v %v", handled, err)
	}
	saved, err = actions.Inspect(t.Context(), principal, proposal.ID)
	if err != nil || saved.State != "awaiting_confirmation" || saved.Reason != "too_early" {
		t.Fatalf("early admitted: %+v %v", saved, err)
	}
	deliverAll(t, s, sender, "501", *at)
	*at = at.Add(time.Second)
	run = start("yes", *at)
	run.t.ReplyTo = "499"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("wrong reply: %v %v", handled, err)
	}
	saved, err = actions.Inspect(t.Context(), principal, proposal.ID)
	if err != nil || saved.State != "awaiting_confirmation" {
		t.Fatal("wrong reply admitted")
	}
	deliverAll(t, s, sender, "502", *at)
	*at = at.Add(time.Second)
	run = start("yes", *at)
	run.t.ReplyTo = "500"
	if handled, err := run.confirm(t.Context()); !handled || err != nil {
		t.Fatalf("separate yes: %v %v", handled, err)
	}
	saved, err = actions.Inspect(t.Context(), principal, proposal.ID)
	if err != nil || saved.State != "admitted" {
		t.Fatalf("not admitted: %+v %v", saved, err)
	}
}

func TestSharedConversationAmbiguityNeverSelectsAProposal(t *testing.T) {
	_, actions, principal, at, start, _ := sharedSelectionFixture(t)
	for i := range 2 {
		p, err := actions.Propose(t.Context(), principal, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "id": fmt.Sprint(i)}}, fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
		if err = actions.Available(t.Context(), p.ID, fmt.Sprint(500+i)); err != nil {
			t.Fatal(err)
		}
	}
	*at = at.Add(6 * time.Second)
	run := start("yes", *at)
	if p, err := run.sharedSelection(t.Context(), true); err == nil || p != nil {
		t.Fatalf("ambiguous selection: %+v %v", p, err)
	}
	page, err := actions.History(t.Context(), principal, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range page.Proposals {
		if p.State != "awaiting_confirmation" {
			t.Fatal("ambiguous yes changed state")
		}
	}
	raw, _ := json.Marshal(page)
	if !strings.Contains(string(raw), "proposal_id") {
		t.Fatal("history omitted client references")
	}
}
