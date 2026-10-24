package messaging_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type inboxWithheldSender struct{ texts []string }

func (s *inboxWithheldSender) Deliver(_ context.Context, _ messaging.Target, text string) messaging.Delivery {
	s.texts = append(s.texts, text)
	if len(s.texts) == 1 {
		return messaging.Delivery{RetryAfter: time.Second}
	}
	return messaging.Delivery{Confirmed: true}
}

func TestExpiredInboxDeliveryNoticeAllowsFreshBrowse(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := database(t)
	inboxJourneySeed(t, db, ticket.GitHub, at)
	provider := &inboxJourneyProvider{}
	sender := &inboxWithheldSender{}
	service := messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableOwnerInbox(&inboxJourneyTickets{})
	if err := service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: filepath.Join(t.TempDir(), "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	messages := &journeyReader{}
	intake := messaging.Intake{DB: db, Reader: messages, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
	admit := func(sequence int) {
		t.Helper()
		messages.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: fmt.Sprint(sequence), OwnerID: "7", Text: "Browse recorded owner waits", CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ConversationStep(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	admit(101)
	// The saved factual page expires before its first delivery attempt.
	at = at.Add(25 * time.Hour)
	if _, err := service.ConversationDeliveryStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 || strings.Contains(sender.texts[0], "decision-000") || !strings.Contains(strings.ToLower(sender.texts[0]), "refresh") {
		t.Fatalf("unsafe or missing withheld notice: %v", sender.texts)
	}
	at = at.Add(2 * time.Second)
	if _, err := service.ConversationDeliveryStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 2 || sender.texts[0] != sender.texts[1] {
		t.Fatal("notice retry changed bytes")
	}
	admit(102)
	if _, err := service.ConversationDeliveryStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) < 3 || !strings.Contains(sender.texts[2], "Recorded owner waits captured") {
		t.Fatalf("expired predecessor blocked fresh browse: %v", sender.texts)
	}
}

func TestInboxContinuationDeliversAvailableSourceAfterOtherSourceRevoked(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	db := database(t)
	inboxJourneySeed(t, db, ticket.GitHub, at)
	linear, _ := inboxJourneySeed(t, db, ticket.Linear, at)
	provider := &inboxJourneyProvider{}
	sender := &groundingSender{}
	service := messaging.New(db, "discord", "7", []string{"3"}, sender, messaging.FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableOwnerInbox(&inboxJourneyTickets{})
	if err := service.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: filepath.Join(t.TempDir(), "conversations"), Providers: map[string]messaging.ConversationProvider{"claude": provider}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	messages := &journeyReader{}
	intake := messaging.Intake{DB: db, Reader: messages, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: service.Protect}
	run := func(sequence int) {
		t.Helper()
		messages.messages = []store.NaturalMessage{{ChannelRef: "3", SourceID: fmt.Sprint(sequence), OwnerID: "7", Text: "Browse recorded owner waits", CreatedAt: at}}
		if err := intake.Poll(ctx, at); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ConversationStep(ctx, at); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ConversationDeliveryStep(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	run(101)
	if len(sender.texts) == 0 {
		t.Fatal("missing initial page")
	}
	reference := inboxJourneyReference(sender.texts[0], "Refresh")
	connection, err := db.CurrentConnection(ctx, linear.RepoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	revision := connection.Revision
	connection.Enabled = false
	if _, err = db.SetConnection(ctx, connection, revision); err != nil {
		t.Fatal(err)
	}
	provider.arguments = map[string]string{"operation": "return", "reference": reference}
	before := len(sender.texts)
	run(102)
	if len(sender.texts) <= before || !strings.Contains(sender.texts[before], "Recorded owner waits captured") || !strings.Contains(sender.texts[before], "github") {
		t.Fatalf("available-source continuation withheld after unrelated revocation: %v", sender.texts[before:])
	}
}
