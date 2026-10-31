package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationActionReadsCarryTrustedSelectionAndPagination(t *testing.T) {
	s, actions, p, at, start, _ := sharedSelectionFixture(t)
	s.registerOwnerActionReads()
	for i := range 2 {
		if _, err := actions.Propose(t.Context(), p, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "id": fmt.Sprint("author-", i)}}, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
		*at = at.Add(time.Second)
	}
	run := start("show the next older action", *at)
	read, next, err := run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: map[string]string{"operation": "history", "limit": "1"}})
	if err != nil || next == nil || next.Arguments["cursor"] == "" {
		t.Fatalf("first page: %+v %+v %v", read, next, err)
	}
	var first owneraction.Page
	if err = json.Unmarshal([]byte(read.Result), &first); err != nil || len(first.Proposals) != 1 {
		t.Fatalf("first history: %+v %v", first, err)
	}
	refs := run.takeListed()
	if len(refs) != 1 || refs[0].Arguments["proposal_id"] != first.Proposals[0].ID {
		t.Fatalf("proposal reference missing: %+v", refs)
	}
	run.ordinal++
	more, _, err := run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: map[string]string{"operation": "more", "cursor": next.Arguments["cursor"], "limit": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	var second owneraction.Page
	if err = json.Unmarshal([]byte(more.Result), &second); err != nil || len(second.Proposals) != 1 || second.Proposals[0].ID == first.Proposals[0].ID {
		t.Fatalf("next history: %+v %v", second, err)
	}
	run.ordinal++
	inspected, ref, err := run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: map[string]string{"operation": "inspect", "proposal_id": refs[0].Arguments["proposal_id"]}})
	if err != nil || ref == nil {
		t.Fatalf("inspect: %+v %v", inspected, err)
	}
	var proposal owneraction.Proposal
	if err = json.Unmarshal([]byte(inspected.Result), &proposal); err != nil || proposal.ID != first.Proposals[0].ID || proposal.Facts.TargetRevision != "version 1" {
		t.Fatalf("inspection lost exact target: %+v %v", proposal, err)
	}
	if proposal.State != "awaiting_confirmation" {
		t.Fatal("read changed action state")
	}
}

func TestConversationActionReadsReauthorizeReferencesAndRefuseInvalidControls(t *testing.T) {
	s, actions, p, at, start, _ := sharedSelectionFixture(t)
	s.registerOwnerActionReads()
	proposal, err := actions.Propose(t.Context(), p, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "id": "author"}}, "key")
	if err != nil {
		t.Fatal(err)
	}
	run := start("inspect that author action", *at)
	for _, args := range []map[string]string{{"operation": "confirm"}, {"operation": "history", "limit": "51"}, {"operation": "inspect", "proposal_id": proposal.ID, "cursor": "replacement"}} {
		if _, _, err = run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: args}); err == nil {
			t.Fatalf("invalid read accepted: %+v", args)
		}
	}
	_, ref, err := run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: map[string]string{"operation": "inspect", "proposal_id": proposal.ID}})
	if err != nil || ref == nil {
		t.Fatal(err)
	}
	stored, err := s.db.ListConversationReferences(t.Context(), run.c.ID, *at, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("saved reference: %+v %v", stored, err)
	}
	current, err := run.ownerActionReferenceCurrent(t.Context(), stored[0], ref.Arguments)
	if err != nil || !current {
		t.Fatalf("current reference: %v %v", current, err)
	}
	s.ownerPrincipal = func(context.Context, string) (owneraction.Principal, error) {
		other := p
		other.OwnerID = "other"
		return other, nil
	}
	current, err = run.ownerActionReferenceCurrent(t.Context(), stored[0], ref.Arguments)
	if err != nil || current {
		t.Fatalf("revoked reference usable: %v %v", current, err)
	}
	if _, _, err = run.lookupOwnerActions(t.Context(), Invocation{Command: "actions", Arguments: map[string]string{"operation": "inspect", "proposal_id": proposal.ID}}); err == nil {
		t.Fatal("changed owner inspected proposal")
	}
}

func TestConversationCanCancelAuthorizedHistoryAcrossDestinations(t *testing.T) {
	_, actions, p, at, start, _ := sharedSelectionFixture(t)
	actions.SetDiscord(p.ActorID(), []string{"3", "4"})
	other := p
	other.ChannelID = "4"
	proposal, err := actions.Propose(t.Context(), other, owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "edit", "id": "other-destination-author"}}, "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	run := start("cancel the other author action", *at)
	if err = run.judgedShared(t.Context(), ConversationEnvelope{Proposal: proposal.ID}, true); err != nil {
		t.Fatal(err)
	}
	inspected, err := actions.Inspect(t.Context(), p, proposal.ID)
	if err != nil || inspected.State != "cancelled" {
		t.Fatalf("authorized history cancellation refused: %+v %v", inspected, err)
	}
	if _, err = run.s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cancelled history admitted effects: %v", err)
	}
}
