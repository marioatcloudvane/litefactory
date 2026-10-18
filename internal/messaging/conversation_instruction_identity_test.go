package messaging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationSavedInstructionIdentityBlocksFurtherInvocation(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"unused"}`}}
	run, _ := factoryUnitRun(t, provider)
	prompt, err := json.Marshal(factoryConversationPrompt{Instructions: "Different saved instructions", OwnerInput: run.t.Source})
	if err != nil {
		t.Fatal(err)
	}
	// A saved correction is a safe provider boundary, but its instruction identity
	// must agree with the binding before it can be used for another invocation.
	if err := run.s.db.SaveConversationExchange(t.Context(), run.t, store.ConversationExchange{TurnID: run.t.ID, Ordinal: 1, Kind: "correction", Request: string(prompt), CapturedAt: run.at, Complete: true}); err != nil {
		t.Fatal(err)
	}
	if err := run.s.runFactoryConversation(t.Context(), run.c, run.b, run.t, run.at); err == nil || !strings.Contains(err.Error(), "conflicts with retained instructions") {
		t.Fatalf("saved instruction mismatch accepted: %v", err)
	}
	if provider.calls != 0 {
		t.Fatal("conflicting saved prompt launched provider")
	}
	saved, err := run.s.db.ReadConversation(t.Context(), run.c.Transport, run.c.ChannelRef)
	if err != nil || !strings.Contains(saved.HealthReason, "model calls paused") {
		t.Fatalf("conflicting prompt not paused: %+v %v", saved, err)
	}
	turn, err := run.s.db.LatestConversationTurn(t.Context(), run.c.ID)
	if err != nil || turn.State != run.t.State {
		t.Fatalf("paused input was consumed: %+v %v", turn, err)
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil || len(exchanges) != 1 || exchanges[0].Request != string(prompt) {
		t.Fatal("authoritative saved prompt changed")
	}
}

func TestConversationSavedOutcomeSurvivesMissingInstructions(t *testing.T) {
	f := newInstructionFailureFixture(t, nil, true, "missing")
	if err := f.db.SetConversationHealth(t.Context(), f.conversation.ID, "", f.at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.AdmitConversationTurn(t.Context(), f.conversation.ID, "101", "saved prompt", f.at); err != nil {
		t.Fatal(err)
	}
	turn, err := f.db.ClaimConversationTurn(t.Context(), f.conversation.ID, "outcome-worker", f.at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.SaveConversationSession(t.Context(), turn, "saved-native-session"); err != nil {
		t.Fatal(err)
	}
	run := factoryConversationRun{s: f.service, c: f.conversation, t: turn, at: f.at}
	if err := run.narrate(t.Context(), "The action's saved result."); err != nil {
		t.Fatal(err)
	}
	reply, err := f.db.ClaimConversationReply(t.Context(), "discord", f.at)
	if err != nil || !strings.Contains(reply.ReplyText, "saved result") {
		t.Fatalf("saved outcome lost: %+v %v", reply, err)
	}
	if f.provider.calls != 0 {
		t.Fatal("missing instructions launched narration provider")
	}
}

func TestConversationRedactionCannotChangeCapturedInstructionIdentity(t *testing.T) {
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"unused"}`}}
	run, _ := factoryUnitRun(t, provider)
	capture, err := run.s.restoreConversationInstructions(t.Context(), run.c.ID, run.at)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(factoryConversationPrompt{Instructions: capture.Factory(), OwnerInput: "owner input"})
	if err != nil {
		t.Fatal(err)
	}
	run.s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "private owner conversation", "redacted conversation"), nil
	}
	if _, err := run.invoke(t.Context(), string(raw)); err == nil || !strings.Contains(err.Error(), "conflicts with retained instructions") {
		t.Fatalf("redaction changed captured instructions: %v", err)
	}
	if provider.calls != 0 {
		t.Fatal("changed instructions launched provider")
	}
	exchanges, err := run.s.db.ListConversationExchanges(t.Context(), run.t.ID)
	if err != nil || len(exchanges) != 0 {
		t.Fatal("changed instructions were journaled")
	}
}
