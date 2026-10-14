package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

type payloadSender struct {
	sender
	payloads []presentation.Payload
}

func (s *payloadSender) DeliverReply(_ context.Context, _ messaging.Target, p presentation.Payload) messaging.Delivery {
	s.payloads = append(s.payloads, p)
	return s.outcome
}

// TestNarratedReplyHasNoAttachmentAndDetailsReturnsIt checks that a narrated
// reply is delivered without raw details, which /details then returns.
func TestNarratedReplyHasNoAttachmentAndDetailsReturnsIt(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	send := &payloadSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}}
	s := messaging.New(db, "discord", "2", []string{"3", "4"}, send, messaging.FactoryStatus{DB: db})
	s.EnablePresentation(&replyModel{}, nil)
	found := false
	for _, def := range s.Declarations() {
		if def.Name == "details" {
			found = def.NaturalLanguage && def.Effect == messaging.ReadEffect && len(def.Arguments) == 1 && def.Arguments[0].Name == "answer" && !def.Arguments[0].Required
		}
	}
	if !found {
		t.Fatal("details not declared as an natural-language read with optional answer selector")
	}
	run := func(id, channel, command string) {
		t.Helper()
		if _, inserted, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: id, Channel: channel, Sender: "2", Command: command}, at); err != nil || !inserted {
			t.Fatalf("admit %s: %t %v", id, inserted, err)
		}
		if more, err := s.Step(t.Context(), at); err != nil || !more {
			t.Fatalf("step %s: %t %v", id, more, err)
		}
	}
	run("1", "3", "health")
	if len(send.payloads) != 0 || len(send.texts) != 1 || send.texts[0] != "Here is the result." {
		t.Fatalf("narrated reply carried raw details: %+v %q", send.payloads, send.texts)
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "1")
	if err != nil || row.State != "confirmed" || row.ReplyAttachment == "" {
		t.Fatalf("raw details not kept: %+v %v", row, err)
	}
	run("2", "3", "details")
	if len(send.payloads) != 1 || send.payloads[0].Content != "Raw details of your previous reply (legacy source text)." || send.payloads[0].Attachment == nil || send.payloads[0].Attachment.Name != presentation.RawFilename || send.payloads[0].Attachment.Content != row.ReplyAttachment {
		t.Fatalf("details: %+v", send.payloads)
	}
	run("3", "4", "details")
	if len(send.payloads) != 1 || send.texts[len(send.texts)-1] != "No earlier reply details are available here." {
		t.Fatalf("details refusal: %+v %q", send.payloads, send.texts)
	}
}
