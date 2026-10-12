package messaging

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// typingSender records typing calls and deliveries in one ordered log.
type typingSender struct {
	mu     sync.Mutex
	events []string
}

func (p *typingSender) Typing(_ context.Context, channel string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "typing:"+channel)
}

func (p *typingSender) Deliver(_ context.Context, t Target, _ string) Delivery {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "deliver:"+t.Channel)
	return Delivery{Confirmed: true, PromptID: "900"}
}

func (p *typingSender) log() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func shortTyping(t *testing.T, d time.Duration) {
	t.Helper()
	previous := typingInterval
	typingInterval = d
	t.Cleanup(func() { typingInterval = previous })
}

// typingRun stages messages through intake and drains both workers.
func typingRun(t *testing.T, send *typingSender, interpreter Interpreter, messages []store.NaturalMessage) *store.DB {
	t.Helper()
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for i := range messages {
		messages[i].CreatedAt = at
	}
	s := New(db, "discord", "7", []string{"3"}, send, FactoryStatus{DB: db})
	s.EnableNatural(interpreter)
	if err := db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	intake := &Intake{DB: db, Reader: &spanReader{messages: messages}, Transport: "discord", Owner: "7", Channels: []string{"3"}, Protect: func(_ context.Context, text string) (string, error) { return text, nil }}
	if err := intake.Poll(ctx, at); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		natural, err := s.NaturalStep(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := s.Step(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		if !natural && !shared {
			return db
		}
	}
	t.Fatal("natural request did not settle")
	return nil
}

func TestTypingRefreshesUntilDeliveryAndStops(t *testing.T) {
	shortTyping(t, 5*time.Millisecond)
	send := &typingSender{}
	slow := funcInterpreter(func(context.Context, InterpretationRequest) ([]byte, error) {
		time.Sleep(60 * time.Millisecond)
		return []byte(`{"version":1,"kind":"clarify","question":"Which project?"}`), nil
	})
	typingRun(t, send, slow, []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "7", Text: "How is it going?"}})
	events := send.log()
	typing, delivered := 0, -1
	for i, e := range events {
		switch e {
		case "typing:3":
			if delivered >= 0 {
				t.Fatalf("typing after delivery: %v", events)
			}
			typing++
		case "deliver:3":
			delivered = i
		default:
			t.Fatalf("unexpected event %q", e)
		}
	}
	if delivered < 0 || typing < 2 {
		t.Fatalf("want at least 2 typing calls before delivery: %v", events)
	}
	time.Sleep(30 * time.Millisecond)
	if after := send.log(); len(after) != len(events) {
		t.Fatalf("typing continued after the workers returned: %v", after)
	}
}

func TestTypingSkipsIgnoredMessagesAndSlashCommands(t *testing.T) {
	shortTyping(t, 5*time.Millisecond)
	send := &typingSender{}
	interpreted := false
	interpreter := funcInterpreter(func(context.Context, InterpretationRequest) ([]byte, error) {
		interpreted = true
		return []byte(`{"version":1,"kind":"clarify","question":"Which project?"}`), nil
	})
	db := typingRun(t, send, interpreter, []store.NaturalMessage{{ChannelRef: "3", SourceID: "101", OwnerID: "8", Text: "Not the owner"}})
	if interpreted || len(send.log()) != 0 {
		t.Fatalf("ignored message showed typing or was interpreted: %v", send.log())
	}
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if _, err := db.AdmitChatReply(t.Context(), store.ChatReply{Transport: "discord", MessageID: "123456789", ChannelRef: "3", Command: "health", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	s := New(db, "discord", "7", []string{"3"}, send, FactoryStatus{DB: db})
	if _, err := s.Step(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if events := send.log(); len(events) != 1 || events[0] != "deliver:3" {
		t.Fatalf("slash command showed typing: %v", events)
	}
}
